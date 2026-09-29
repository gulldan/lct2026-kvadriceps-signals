"""HTTP-слой: ``/predict``, ``/extract``, ``/health``, ``/metrics``.

Контракт ML_CLASSIFIER_HANDOFF.md: тело запроса до 4 МиБ режется до разбора, ответ до 1 МиБ,
без редиректов. У /predict свой слот вывода и срок 30 с, чтобы пакет оценки не ждал долгое
извлечение; срок /extract короче таймаута вызывающей стороны. Пакет либо проверен целиком,
либо отклонён: частичный ответ смешал бы на панели оценки модели с оценками правил. При отказе
модели каждый кандидат получает ``label: failed`` с причиной по-русски, а не число правил.
"""

from __future__ import annotations

import asyncio
import copy
import importlib
import json
import logging
import math
import os
import re
import time
from collections.abc import Awaitable, Callable
from contextlib import AsyncExitStack, asynccontextmanager
from contextvars import ContextVar
from dataclasses import replace
from typing import Any
from uuid import uuid4

from fastapi import FastAPI, Request
from fastapi.exceptions import RequestValidationError
from fastapi.responses import JSONResponse, PlainTextResponse, Response
from prometheus_client import (
    CollectorRegistry,
    Counter,
    Gauge,
    Histogram,
    generate_latest,
)
from prometheus_client.exposition import CONTENT_TYPE_LATEST
from pydantic import TypeAdapter, ValidationError
from asyncpg import InterfaceError, PostgresError

from extractor.runtime.cache import CacheConflict, CacheCorruptionError
from starlette.concurrency import run_in_threadpool
from starlette.exceptions import HTTPException

from extractor.interfaces import ModelUnavailable, SpanTagger, StageScorer
from extractor.outputs import ExtractPipeline, PredictService
from extractor.pipeline import Components
from extractor.registry import ModelNotPinned, ModelRegistry
from extractor.rules import MethodologyRuler
from extractor.schemas import (
    FEATURE_SCHEMA,
    MAX_REQUEST_BYTES,
    MAX_RESPONSE_BYTES,
    PREDICT_TIMEOUT_SECONDS,
    SERVICE_VERSION_RE,
    Document,
    ErrorCode,
    ErrorResponse,
    ExtractRequest,
    ExtractResponse,
    HealthResponse,
    PredictLabel,
    PredictRequest,
    PredictResponse,
)
from extractor.store import InMemorySpanCache
from extractor.store.runtime import open_storage

log = logging.getLogger("extractor.api")
JOB_ID = ContextVar("extractor_job_id", default="-")
_EXTRACT_INPUT = TypeAdapter(ExtractRequest | list[Document])


def log_event(event: str, **fields) -> None:
    log.info(
        json.dumps(
            {"event": event, "job_id": JOB_ID.get(), **fields}, ensure_ascii=False
        )
    )


#: Свой реестр: иначе второе приложение в тестах падает на «duplicate metric»
#: в глобальном реестре по умолчанию.
METRICS = CollectorRegistry()

REQUESTS = Counter(
    "extractor_requests_total",
    "HTTP requests by endpoint and outcome.",
    ("endpoint", "outcome"),
    registry=METRICS,
)
LATENCY = Histogram(
    "extractor_request_seconds",
    "Wall-clock seconds per request.",
    ("endpoint",),
    buckets=(0.01, 0.05, 0.1, 0.5, 1.0, 5.0, 10.0, 30.0, 60.0),
    registry=METRICS,
)
PREDICTIONS = Counter(
    "extractor_predictions_total",
    "Predictions emitted by label.",
    ("label",),
    registry=METRICS,
)
CANDIDATES = Counter(
    "extractor_candidates_total",
    "Candidates produced by /extract.",
    registry=METRICS,
)
CACHE = Counter(
    "extractor_span_cache_total",
    "Span cache lookups by outcome.",
    ("outcome",),
    registry=METRICS,
)
DEGRADED = Gauge(
    "extractor_degraded",
    "1 when the last run had to degrade (model or cache failure).",
    registry=METRICS,
)
STEPS = Histogram(
    "extractor_step_seconds",
    "Time spent in pipeline component calls.",
    ("endpoint", "step"),
    registry=METRICS,
)
BATCH = Histogram(
    "extractor_batch_size",
    "Input items per pipeline invocation.",
    ("endpoint",),
    buckets=(1, 2, 8, 16, 32, 45, 64, 128, 512),
    registry=METRICS,
)
DOCUMENTS = Counter(
    "extractor_documents_total",
    "Documents or candidate sources received.",
    ("endpoint",),
    registry=METRICS,
)


class TimedComponent:
    """Обёртка на экземпляр: общие объекты моделей не меняются."""

    def __init__(self, inner, endpoint, methods):
        self.inner, self.endpoint, self.methods = inner, endpoint, methods

    def with_documents(self, *args, **kwargs):
        bind = getattr(self.inner, "with_documents", None)
        bound = bind(*args, **kwargs) if callable(bind) else self.inner
        return TimedComponent(bound, self.endpoint, self.methods)

    def with_observation_date(self, *args, **kwargs):
        return TimedComponent(self.inner.with_observation_date(*args, **kwargs), self.endpoint, self.methods)

    @property
    def config(self):
        return getattr(self.inner, "config", None)

    @property
    def threshold(self):
        return self.inner.threshold

    def __getattr__(self, name):
        target = getattr(self.inner, name)
        if name not in self.methods:
            return target

        def timed(*args, **kwargs):
            started = time.perf_counter()
            try:
                return target(*args, **kwargs)
            finally:
                elapsed = time.perf_counter() - started
                step = self.methods[name]
                STEPS.labels(self.endpoint, step).observe(elapsed)
                log_event(
                    "pipeline_step", endpoint=self.endpoint, step=step, seconds=elapsed
                )

        return timed


def instrument(service, endpoint):
    service = copy.copy(service)
    fields = {
        "tagger": {"tag": "tagger"},
        "canonicalizer": {"group": "canonicalizer"},
        "featurizer": {"compute": "featurizer"},
        "stage_scorer": {"score": "stage"},
        "ruler": {"score": "ruler"},
        "cache": {"get": "cache_get", "put": "cache_put"},
        "evidence_finder": {"find": "evidence"},
    }
    service.components = replace(
        service.components,
        **{
            name: TimedComponent(getattr(service.components, name), endpoint, methods)
            for name, methods in fields.items()
        },
    )
    return service


class UnavailableTagger(SpanTagger):
    version = "unavailable"

    def tag(self, docs):
        raise ModelUnavailable("рантайм теггера не подключён")


class UnavailableStage(StageScorer):
    version = "unavailable"

    def score(self, sentences):
        raise ModelUnavailable("рантайм классификатора стадии не подключён")


def runtime_components(registry):
    """Заглушки только по явному флагу: прод не должен молча взять тестовые модели."""
    if os.getenv("EXTRACTOR_USE_STUBS") == "1":
        return Components(
            ruler=MethodologyRuler(threshold=registry.thresholds.predict),
            cache=InMemorySpanCache(),
        )
    factory = os.getenv(
        "EXTRACTOR_COMPONENTS_FACTORY", "extractor.assembly:build_components"
    )
    if factory:
        module, name = factory.split(":", 1)
        return getattr(importlib.import_module(module), name)(
            registry=registry, device=os.getenv("EXTRACTOR_DEVICE", "cpu")
        )
    return Components(
        tagger=UnavailableTagger(),
        stage_scorer=UnavailableStage(),
        ruler=MethodologyRuler(threshold=registry.thresholds.predict),
        cache=InMemorySpanCache(),
    )


def deployment_registry(registry):
    version = os.getenv("RADAR_CLASSIFIER_VERSION", "").strip() or registry.service_version
    if not SERVICE_VERSION_RE.fullmatch(version):
        raise ValueError("invalid RADAR_CLASSIFIER_VERSION format")
    if version != registry.service_version:
        raise ValueError(
            f"RADAR_CLASSIFIER_VERSION={version!r} does not match the registry "
            f"service_version={registry.service_version!r} (release.json of the bundle, "
            "or models.yaml in local mode); change the registry, not the environment"
        )
    return registry


def validate_components(components, registry):
    if components.ruler.threshold != registry.thresholds.predict:
        raise ValueError("ruler.threshold must equal registry.thresholds.predict")


def validate_service(service, registry):
    if service.config.service_version != registry.service_version:
        raise ValueError(
            "injected pipeline version must equal registry service_version"
        )
    validate_components(service.components, registry)


async def warm_up_models(components) -> None:
    """Веса грузятся сразу после старта, а не на первом запросе.

    Иначе /health остаётся degraded до первого трафика, и строгий HEALTHCHECK образа
    (status == ok) после start period помечает контейнер нездоровым. Ошибка только
    логируется: /health покажет незагруженный рантайм, следующий запрос повторит загрузку.
    """
    for name in ("tagger", "stage_scorer"):
        runtime = getattr(getattr(components, name, None), "runtime", None)
        if runtime is None or not callable(getattr(runtime, "load", None)):
            continue
        try:
            await run_in_threadpool(runtime.load)
        except Exception as exc:  # noqa: BLE001 - reported by /health, retried on demand
            log_event("runtime_warmup_failed", component=name, error_type=type(exc).__name__)
            return
    log_event("runtime_warmup_done")


def _byte_limit(value, variable, default, label):
    raw = os.getenv(variable, str(default)) if value is None else value
    if raw is True or raw is False:
        raise ValueError(f"{label} must be a positive integer")
    try:
        limit = int(raw)
    except (ValueError, TypeError, OverflowError) as exc:
        raise ValueError(f"{label} must be a positive integer") from exc
    if limit <= 0 or (isinstance(raw, float) and raw != limit):
        raise ValueError(f"{label} must be a positive integer")
    return limit


def extract_request_limit(value=None):
    """Предел только для /extract; у /predict он фиксирован контрактом."""
    return _byte_limit(value, "EXTRACTOR_MAX_EXTRACT_REQUEST_BYTES", MAX_REQUEST_BYTES, "extract request limit")


def extract_response_limit(value=None):
    return _byte_limit(value, "EXTRACTOR_MAX_EXTRACT_RESPONSE_BYTES", MAX_RESPONSE_BYTES, "extract response limit")


#: Чуть меньше таймаута вызывающей стороны (RADAR_ML_EXTRACT_TIMEOUT = 300 с),
#: чтобы отказ пришёл по контракту, а не обрывом соединения.
DEFAULT_EXTRACT_TIMEOUT_SECONDS = 290.0


def extract_budget(value=None):
    raw = os.getenv("EXTRACTOR_EXTRACT_TIMEOUT", str(DEFAULT_EXTRACT_TIMEOUT_SECONDS)) if value is None else value
    try:
        budget = float(raw)
    except (TypeError, ValueError) as exc:
        raise ValueError("extract timeout must be a positive number of seconds") from exc
    if not math.isfinite(budget) or budget <= 0:
        raise ValueError("extract timeout must be a positive number of seconds")
    return budget


def predict_budget(value=None):
    value = (
        float(os.getenv("EXTRACTOR_PREDICT_TIMEOUT", "27")) if value is None else value
    )
    if not 0 < value < PREDICT_TIMEOUT_SECONDS:
        raise ValueError("predict timeout must be in (0, 30)")
    return value


class InferenceDeadline(TimeoutError):
    """Срок запроса истёк; вызов модели может ещё держать слот."""


class InferenceRunner:
    """Синхронный вызов после таймаута держит слот, пока действительно не завершится."""

    def __init__(self, slots=1):
        if slots < 1:
            raise ValueError("inference slots must be positive")
        self.semaphore = asyncio.Semaphore(slots)
        self.pending = set()

    async def run(self, function, argument, *, timeout=None):
        async def start():
            await self.semaphore.acquire()
            task = asyncio.create_task(run_in_threadpool(function, argument))
            self.pending.add(task)

            def finished(done):
                self.pending.discard(done)
                self.semaphore.release()
                if not done.cancelled():
                    done.exception()  # Забираем позднюю ошибку модели, пришедшую после HTTP-таймаута.

            task.add_done_callback(finished)
            return await asyncio.shield(task)

        if timeout is None:
            return await start()
        scope = asyncio.timeout(timeout)
        try:
            async with scope:
                return await start()
        except TimeoutError:
            # TimeoutError самого вызова (хранилища) — не истечение срока.
            if scope.expired():
                raise InferenceDeadline(f"превышен срок обработки {timeout:g} с") from None
            raise

    async def drain(self):
        if self.pending:
            await asyncio.gather(*self.pending, return_exceptions=True)


class RequestContext:
    """Самый внешний пользовательский middleware: заголовки версии и JSON-логи даже при ошибках."""

    def __init__(self, app, version):
        self.app, self.version = app, version

    async def __call__(self, scope, receive, send):
        if scope["type"] != "http":
            return await self.app(scope, receive, send)
        headers = dict(scope.get("headers", []))
        supplied = headers.get(b"x-job-id", b"").decode("latin-1")
        job_id = (
            supplied
            if re.fullmatch(r"[A-Za-z0-9_.:-]{1,128}", supplied)
            else uuid4().hex
        )
        token = JOB_ID.set(job_id)
        started, sent, status = time.perf_counter(), False, 500

        async def wrapped_send(message):
            nonlocal sent, status
            if message["type"] == "http.response.start":
                sent, status = True, message["status"]
                message["headers"] = [
                    *message.get("headers", []),
                    (b"x-radar-classifier-version", self.version.encode()),
                    (b"x-job-id", job_id.encode()),
                ]
            await send(message)

        try:
            await self.app(scope, receive, wrapped_send)
        except Exception as exc:
            log_event("http_error", error_type=type(exc).__name__)
            if sent:
                raise
            await _error(ErrorCode.INTERNAL_ERROR, "внутренняя ошибка сервиса", 500)(
                scope, receive, wrapped_send
            )
        finally:
            log_event(
                "http_request",
                method=scope["method"],
                path=scope["path"],
                status=status,
                version=self.version,
                seconds=time.perf_counter() - started,
            )
            JOB_ID.reset(token)


class BodyLimit:
    """Отказ по размеру тела до любого разбора.

    Тело буферизуется один раз и проигрывается заново, поэтому предел действует
    и на chunked-загрузку без ``Content-Length``.
    """

    def __init__(
        self, app: Any, max_bytes: int = MAX_REQUEST_BYTES, *, extract_max_bytes: int | None = None
    ) -> None:
        self.app = app
        self.max_bytes = max_bytes
        self.extract_max_bytes = extract_max_bytes or max_bytes

    async def __call__(self, scope: dict, receive: Callable, send: Callable) -> None:
        if scope.get("type") != "http":
            await self.app(scope, receive, send)
            return
        limit = self.extract_max_bytes if scope.get("path") == "/extract" else self.max_bytes
        headers = {
            key.decode("latin-1").lower(): value
            for key, value in scope.get("headers", [])
        }
        declared = headers.get("content-length")
        if (
            declared is not None
            and declared.isdigit()
            and int(declared) > limit
        ):
            await self._too_large(scope, receive, send, int(declared), limit)
            return
        chunks = []
        size = 0
        while True:
            message = await receive()
            if message["type"] == "http.disconnect":
                return
            chunk = message.get("body", b"")
            size += len(chunk)
            if size > limit:
                await self._too_large(scope, receive, send, size, limit)
                return
            if chunk:
                chunks.append(chunk)
            if not message.get("more_body", False):
                break
        body = chunks[0] if len(chunks) == 1 else b"".join(chunks)
        chunks.clear()
        replayed = [{"type": "http.request", "body": body, "more_body": False}]

        async def replay() -> dict:
            if replayed:
                return replayed.pop(0)
            return await receive()

        await self.app(scope, replay, send)

    async def _too_large(
        self, scope: dict, receive: Callable, send: Callable, size: int, limit: int
    ) -> None:
        REQUESTS.labels(
            endpoint=scope.get("path", "?"), outcome="payload_too_large"
        ).inc()
        payload = ErrorResponse(
            error=ErrorCode.PAYLOAD_TOO_LARGE,
            message=(
                f"тело запроса {size} байт превышает предел "
                f"{limit} байт"
            ),
        )
        response = JSONResponse(payload.model_dump(mode="json"), status_code=413)
        await response(scope, receive, send)


def _error(
    code: ErrorCode, message: str, status: int, detail: list[str] | None = None
) -> Response:
    payload = ErrorResponse(error=code, message=message, detail=detail or [])
    return _json_response(payload, status=status)


def _json_response(
    payload: PredictResponse | ExtractResponse | ErrorResponse | HealthResponse,
    status: int = 200,
    *,
    max_bytes: int = MAX_RESPONSE_BYTES,
) -> Response:
    body = payload.model_dump_json().encode("utf-8")
    if len(body) > max_bytes:
        return _error(
            ErrorCode.RESPONSE_TOO_LARGE,
            f"ответ {len(body)} байт превышает предел {max_bytes} байт",
            status=413,
        )
    return Response(content=body, media_type="application/json", status_code=status)


async def _timed(endpoint: str, work: Awaitable[Any]) -> Any:
    started = time.perf_counter()
    try:
        return await work
    finally:
        LATENCY.labels(endpoint=endpoint).observe(time.perf_counter() - started)


def create_app(
    *,
    registry: ModelRegistry | None = None,
    extract_pipeline: ExtractPipeline | None = None,
    predict_service: PredictService | None = None,
    predict_timeout: float | None = None,
    extract_timeout: float | None = None,
    span_cache=None,
    inference_slots: int | None = None,
    max_extract_request_bytes: int | None = None,
    max_extract_response_bytes: int | None = None,
) -> FastAPI:
    registry = registry or ModelRegistry.load(
        os.getenv("EXTRACTOR_MODELS_YAML") or None
    )
    registry = deployment_registry(registry)
    needs_extract, needs_predict = extract_pipeline is None, predict_service is None
    # При импорте — только лёгкие адаптеры-отказы; веса грузятся один раз в lifespan.
    unavailable = Components(
        tagger=UnavailableTagger(),
        stage_scorer=UnavailableStage(),
        ruler=MethodologyRuler(threshold=registry.thresholds.predict),
        cache=InMemorySpanCache(),
    )
    extract_pipeline = extract_pipeline or ExtractPipeline(
        unavailable, registry=registry
    )
    predict_service = predict_service or PredictService(unavailable, registry=registry)
    service_version = registry.service_version
    extract_pipeline, predict_service = (
        copy.copy(extract_pipeline),
        copy.copy(predict_service),
    )
    for service in (extract_pipeline, predict_service):
        validate_service(service, registry)
    predict_timeout = predict_budget(predict_timeout)
    extract_timeout = extract_budget(extract_timeout)
    max_extract_response_bytes = extract_response_limit(max_extract_response_bytes)
    slots = (
        inference_slots
        if inference_slots is not None
        else int(os.getenv("EXTRACTOR_SLOTS", "1"))
    )
    # У /predict свои слоты: пакет оценки не должен ждать извлечение (возможно, брошенное
    # вызывающей стороной), которое ещё держит слот.
    runner, predict_runner = InferenceRunner(slots), InferenceRunner(slots)

    @asynccontextmanager
    async def lifespan(app):
        logging.basicConfig(level=os.getenv("LOG_LEVEL", "INFO"), format="%(message)s")
        log.setLevel(os.getenv("LOG_LEVEL", "INFO"))
        async with AsyncExitStack() as stack:
            if span_cache is None and os.getenv("EXTRACTOR_POSTGRES_DSN"):
                try:
                    cache = await stack.enter_async_context(
                        open_storage(slots=slots)
                    )
                    app.state.extract_pipeline.components.cache = TimedComponent(
                        cache, "/extract", {"get": "cache_get", "put": "cache_put"}
                    )
                    app.state.span_cache = cache
                except Exception as exc:  # noqa: BLE001 - keep health reachable on storage failure
                    app.state.storage_error = type(exc).__name__
                    log_event("storage_start_failed", error_type=type(exc).__name__)
            if needs_extract or needs_predict:
                try:
                    components = await run_in_threadpool(runtime_components, registry)
                except Exception as exc:  # noqa: BLE001 - expose model failure, never use test rules
                    components = unavailable
                    app.state.runtime_error = type(exc).__name__
                    log_event("runtime_start_failed", error_type=type(exc).__name__)
                validate_components(components, registry)
                for service_type in (ExtractPipeline, PredictService):
                    validate_service(
                        service_type(components, registry=registry), registry
                    )
                if needs_extract:
                    if span_cache is not None:
                        components = replace(components, cache=span_cache)
                    elif os.getenv("EXTRACTOR_POSTGRES_DSN"):
                        components = replace(components, cache=app.state.span_cache)
                    app.state.extract_pipeline = instrument(
                        ExtractPipeline(components, registry=registry), "/extract"
                    )
                    app.state.span_cache = components.cache
                if needs_predict:
                    app.state.predict_service = instrument(
                        PredictService(components, registry=registry), "/predict"
                    )
                if app.state.runtime_error is None:
                    app.state.model_warmup = asyncio.create_task(
                        warm_up_models(components)
                    )
            try:
                yield
            finally:
                await runner.drain()
                await predict_runner.drain()

    app = FastAPI(
        title="weak-signal extractor",
        version=service_version,
        # Редиректы запрещены контрактом.
        redirect_slashes=False,
        lifespan=lifespan,
    )
    app.add_middleware(
        BodyLimit, max_bytes=MAX_REQUEST_BYTES,
        extract_max_bytes=extract_request_limit(max_extract_request_bytes),
    )
    app.add_middleware(RequestContext, version=service_version)
    app.state.registry = registry
    if span_cache is not None:
        extract_pipeline = copy.copy(extract_pipeline)
        extract_pipeline.components = replace(
            extract_pipeline.components, cache=span_cache
        )
    app.state.extract_pipeline = instrument(extract_pipeline, "/extract")
    app.state.predict_service = instrument(predict_service, "/predict")
    app.state.span_cache = extract_pipeline.components.cache
    app.state.storage_error = None
    app.state.runtime_error = None
    app.state.runner = runner
    app.state.predict_runner = predict_runner
    app.state.predict_timeout = predict_timeout
    app.state.extract_timeout = extract_timeout

    @app.exception_handler(HTTPException)
    async def _http_error(request, exc):
        return _error(
            ErrorCode.INVALID_REQUEST,
            "неизвестный маршрут или недопустимый метод",
            exc.status_code,
        )

    @app.exception_handler(RequestValidationError)
    async def _validation_error(
        request: Request, exc: RequestValidationError
    ) -> Response:
        REQUESTS.labels(endpoint=request.url.path, outcome="invalid_request").inc()
        detail = [
            f"{'.'.join(str(part) for part in err.get('loc', ()))}: {err.get('msg', '')}"
            for err in exc.errors()
        ][:20]
        return _error(
            ErrorCode.INVALID_REQUEST,
            "тело запроса не соответствует схеме, пакет отклонён целиком",
            status=422,
            detail=detail,
        )

    @app.post("/predict")
    async def predict(request: PredictRequest) -> Response:
        problem = request.check_contract(service_version)
        if problem is not None:
            code, message = problem
            REQUESTS.labels(endpoint="/predict", outcome=code.value).inc()
            status = 409 if code is ErrorCode.VERSION_MISMATCH else 400
            return _error(code, message, status=status)

        service: PredictService = app.state.predict_service
        BATCH.labels("/predict").observe(len(request.candidates))
        DOCUMENTS.labels("/predict").inc(
            sum(len(c.sources) for c in request.candidates)
        )
        try:
            if service.components.stage_scorer.version == "unavailable":
                raise ModelUnavailable("рантайм классификатора стадии не подключён")
            response: PredictResponse = await _timed(
                "/predict",
                predict_runner.run(service.predict, request, timeout=predict_timeout),
            )
            response.check_against(request)
            failed_count = sum(
                p.label is PredictLabel.FAILED for p in response.predictions
            )
            if 0 < failed_count < len(response.predictions):
                response = service.failed(request, "модель не оценила весь пакет")
        except TimeoutError:
            DEGRADED.set(1)
            REQUESTS.labels(endpoint="/predict", outcome="timeout").inc()
            response = service.failed(
                request, f"превышен бюджет обработки {predict_timeout:g} с"
            )
        except ModelUnavailable as exc:
            DEGRADED.set(1)
            REQUESTS.labels(endpoint="/predict", outcome="classifier_unavailable").inc()
            response = service.failed(request, str(exc))
        except Exception as exc:  # noqa: BLE001 - HTTP boundary, uniform error body
            log_event("predict_error", error_type=type(exc).__name__)
            REQUESTS.labels(endpoint="/predict", outcome="internal_error").inc()
            return _error(
                ErrorCode.INTERNAL_ERROR,
                "внутренняя ошибка сервиса, пакет не оценён",
                status=500,
                detail=[type(exc).__name__],
            )
        else:
            failed = any(p.label is PredictLabel.FAILED for p in response.predictions)
            REQUESTS.labels(
                endpoint="/predict",
                outcome="classifier_unavailable" if failed else "ok",
            ).inc()
            DEGRADED.set(int(failed))

        response.check_against(request)
        for prediction in response.predictions:
            PREDICTIONS.labels(label=prediction.label.value).inc()
        return _json_response(response)

    @app.post("/extract", openapi_extra={
        "requestBody": {"required": True, "content": {"application/json": {
            "schema": _EXTRACT_INPUT.json_schema(
                ref_template="#/paths/~1extract/post/requestBody/content/application~1json/schema/$defs/{model}"
            ),
        }}},
    })
    async def extract(http_request: Request) -> Response:
        # Разбор байтов сразу в модели исключает промежуточный словарь всего корпуса.
        try:
            # Завершающий пустой блок ASGI не должен вызывать копирование всего тела.
            chunks = [chunk async for chunk in http_request.stream() if chunk]
            raw = chunks[0] if len(chunks) == 1 else b"".join(chunks)
            request = _EXTRACT_INPUT.validate_json(raw)
            del raw, chunks
        except ValidationError as exc:
            raise RequestValidationError(exc.errors(include_input=False)) from exc
        if isinstance(request, list):
            try:
                request = ExtractRequest(documents=request)
            except ValidationError as exc:
                raise RequestValidationError(exc.errors()) from exc
        if request.model_version and request.model_version != service_version:
            REQUESTS.labels(endpoint="/extract", outcome="version_mismatch").inc()
            return _error(
                ErrorCode.VERSION_MISMATCH,
                f"версия модели {request.model_version!r} не совпадает с версией "
                f"сервиса {service_version!r}",
                status=409,
            )
        pipeline: ExtractPipeline = app.state.extract_pipeline
        if app.state.storage_error:
            return _error(ErrorCode.STORAGE_UNAVAILABLE, "хранилище недоступно", 503)
        BATCH.labels("/extract").observe(len(request.documents))
        DOCUMENTS.labels("/extract").inc(len(request.documents))
        try:
            response: ExtractResponse = await _timed(
                "/extract", runner.run(pipeline.run, request, timeout=extract_timeout)
            )
        except InferenceDeadline as exc:
            log_event("extract_timeout", seconds=extract_timeout)
            REQUESTS.labels(endpoint="/extract", outcome="timeout").inc()
            DEGRADED.set(1)
            return _error(ErrorCode.TIMEOUT, str(exc), status=504)
        except ModelUnavailable as exc:
            REQUESTS.labels(endpoint="/extract", outcome="classifier_unavailable").inc()
            DEGRADED.set(1)
            return _error(
                ErrorCode.CLASSIFIER_UNAVAILABLE,
                f"модель недоступна: {exc}",
                status=503,
            )
        except (CacheCorruptionError, CacheConflict, ValidationError, json.JSONDecodeError) as exc:
            log_event("cache_invalid", error_type=type(exc).__name__)
            REQUESTS.labels(endpoint="/extract", outcome="cache_corrupted").inc()
            DEGRADED.set(1)
            return _error(ErrorCode.CACHE_CORRUPTED, "повреждены сохранённые данные кэша", 422)
        except (OSError, TimeoutError, InterfaceError, PostgresError) as exc:
            log_event("storage_unavailable", error_type=type(exc).__name__)
            REQUESTS.labels(endpoint="/extract", outcome="storage_unavailable").inc()
            DEGRADED.set(1)
            return _error(ErrorCode.STORAGE_UNAVAILABLE, "хранилище недоступно", 503)
        except Exception as exc:  # noqa: BLE001 - HTTP boundary, uniform error body
            log_event("extract_error", error_type=type(exc).__name__)
            REQUESTS.labels(endpoint="/extract", outcome="internal_error").inc()
            return _error(
                ErrorCode.INTERNAL_ERROR,
                "внутренняя ошибка сервиса, документы не обработаны",
                status=500,
                detail=[type(exc).__name__],
            )
        REQUESTS.labels(endpoint="/extract", outcome="ok").inc()
        CANDIDATES.inc(len(response.candidates))
        CACHE.labels(outcome="hit").inc(response.coverage.cache_hits)
        CACHE.labels(outcome="miss").inc(response.coverage.cache_misses)
        DEGRADED.set(1 if response.coverage.degraded else 0)
        return _json_response(response, max_bytes=max_extract_response_bytes)

    @app.get("/health")
    async def health() -> Response:
        detail: list[str] = []
        services = (
            ("extract", app.state.extract_pipeline),
            ("predict", app.state.predict_service),
        )
        required_models = {
            model_name
            for _, service in services
            for name in ("tagger", "stage_scorer")
            if (runtime := getattr(getattr(service.components, name), "runtime", None)) is not None
            for model_name in getattr(runtime, "required_models", (runtime.model_name,))
        }
        # Адаптеры-отказы и заглушки не должны скрывать объявленные для прода закрепления моделей.
        if not required_models:
            required_models = {"tagger-v1", "stage-v1", "stage-encoder-e5"} & registry.config.models.keys()
        for name in sorted(required_models):
            try:
                await run_in_threadpool(registry.verify, name)
            except ModelNotPinned:
                detail.append(f"{name}: артефакт не закреплён (sha256 не заполнен)")
            except Exception as exc:  # noqa: BLE001 - reported, not raised
                detail.append(f"{name}: {exc}")
        degraded = bool(detail)
        embedding_probes = {}
        for endpoint, service in services:
            for name in (
                "tagger",
                "stage_scorer",
                "canonicalizer",
                "featurizer",
                "ruler",
                "evidence_finder",
            ):
                component = getattr(service.components, name, None)
                try:
                    version = getattr(component, "version", None) or getattr(
                        component, "model_name", None
                    )
                except Exception as exc:  # noqa: BLE001 - keep readiness available for bad model config
                    degraded = True
                    version = "недоступна"
                    detail.append(f"{endpoint}.{name}: ошибка версии {type(exc).__name__}")
                embedder = getattr(component, "embedding_client", None)
                if embedder is not None:
                    if id(embedder) not in embedding_probes:
                        try:
                            async with asyncio.timeout(2.5):
                                embedding_probes[id(embedder)] = await run_in_threadpool(embedder.health)
                        except Exception:  # noqa: BLE001 - dependency failure is degraded readiness
                            embedding_probes[id(embedder)] = False
                    ready = embedding_probes[id(embedder)]
                    degraded |= not ready
                    detail.append(
                        f"сервис {endpoint}.embeddings: {'ok' if ready else 'недоступно'}"
                    )
                    detail.append(
                        f"загруженный компонент {endpoint}.embeddings: "
                        f"{getattr(embedder, 'version', 'не заявлена')}"
                    )
                runtime = getattr(component, "runtime", None)
                if runtime is not None:
                    loaded = runtime.backend is not None
                    degraded |= not loaded
                    state = "loaded" if loaded else "not loaded"
                    detail.append(f"runtime {endpoint}.{name}: {state}")
                detail.append(
                    f"загруженный компонент {endpoint}.{name}: {version or 'не заявлена'}"
                )
        store = app.state.span_cache
        backend = "PostgresSpanCache" if app.state.storage_error else type(store).__name__
        try:
            async with asyncio.timeout(2):
                ok = not app.state.storage_error and (
                    await store.health() if hasattr(store, "health") else True
                )
        except Exception:  # noqa: BLE001 - a failed probe is degraded health
            ok = False
        degraded |= not ok
        detail.append(
            f"хранилище span_cache ({backend}): {'ok' if ok else 'недоступно'}"
        )
        if isinstance(app.state.predict_service.components.stage_scorer.inner, UnavailableStage):
            degraded = True
            detail.append("рантайм классификатора стадии не подключён")
        if isinstance(app.state.extract_pipeline.components.tagger.inner, UnavailableTagger):
            degraded = True
            detail.append("рантайм теггера не подключён")
        if app.state.runtime_error:
            degraded = True
            detail.append(f"ошибка загрузки моделей: {app.state.runtime_error}")
        REQUESTS.labels(endpoint="/health", outcome="ok").inc()
        return _json_response(
            HealthResponse(
                status="degraded" if degraded else "ok",
                service="extractor",
                version=service_version,
                models=registry.versions(),
                feature_schema=FEATURE_SCHEMA,
                detail=detail,
            )
        )

    @app.get("/metrics")
    async def metrics() -> Response:
        return PlainTextResponse(
            generate_latest(METRICS), media_type=CONTENT_TYPE_LATEST
        )

    return app


app = create_app()


__all__ = ["METRICS", "BodyLimit", "InferenceDeadline", "app", "create_app"]
