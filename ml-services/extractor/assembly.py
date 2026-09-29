"""Сборка прод-компонентов; импорт не грузит веса и не открывает соединений."""

import json
import math
import os
import time
import urllib.error
import urllib.request

from extractor.canon import Canonicalizer
from extractor.evidence import EvidenceFinder
from extractor.interfaces import (
    EmbeddingClient,
    EmbeddingClientConfig,
    EmbeddingUnavailable,
)
from extractor.pipeline import Components
from extractor.registry import Backend
from extractor.rules import CorpusFeaturizer, MethodologyRuler
from extractor.runtime import RuntimeConfig
from extractor.stage_e5_lr import E5LRStageScorer
from extractor.stage_hf import HFStageScorer
from extractor.store.runtime import configured_span_cache
from extractor.tagger_hf import HFSpanTagger


class RegistryEmbeddingClient(EmbeddingClient):
    def __init__(self, registry):
        spec = registry.spec("f2llm-embeddings")
        if not spec.url or not spec.dim:
            raise ValueError("f2llm-embeddings requires url and dim")
        self.version = spec.ref
        self.expected_revision = spec.expected_revision
        if self.expected_revision is not None:
            self.version = f"{spec.ref}|{self.expected_revision}"
        self.config = EmbeddingClientConfig(
            base_url=spec.url, dim=spec.dim,
            max_batch=_positive_setting("EXTRACTOR_EMBEDDING_BATCH_SIZE", 64, os.environ),
        )

    def _check_identity(self, response):
        if not isinstance(response, dict):
            raise EmbeddingUnavailable("неверный ответ сервиса эмбеддингов")
        if self.expected_revision is not None and (
            response.get("revision") != self.expected_revision
            or response.get("model") != self.config.model
        ):
            raise EmbeddingUnavailable("модель или revision эмбеддингов не совпадает с реестром")

    def _request(self, url, body=None, *, timeout=None, retries=None):
        request = urllib.request.Request(
            url, data=body, headers={"Content-Type": "application/json"}
        )
        retries = self.config.retries if retries is None else retries
        timeout = self.config.timeout_seconds if timeout is None else timeout
        for attempt in range(retries + 1):
            try:
                with urllib.request.urlopen(
                    request, timeout=timeout
                ) as response:
                    return json.load(response)
            except (urllib.error.URLError, TimeoutError, OSError) as exc:
                retryable = (
                    not isinstance(exc, urllib.error.HTTPError) or exc.code >= 500
                )
                if not retryable or attempt == retries:
                    raise EmbeddingUnavailable("сервис эмбеддингов недоступен") from exc
                time.sleep(self.config.backoff_seconds * (attempt + 1))
        raise AssertionError("unreachable")

    def embed(self, texts):
        # Ограничены и число текстов, и байты UTF-8 вместе с накладными расходами JSON.
        batches, batch = [], []
        overhead = len(self._body([]))
        size = overhead
        for text in texts:
            if len(text.encode("utf-8")) > self.config.max_text_bytes:
                raise EmbeddingUnavailable("текст превышает лимит эмбеддингов")
            encoded_size = len(json.dumps(text, ensure_ascii=False).encode("utf-8"))
            added = encoded_size + (2 if batch else 0)
            if batch and (
                len(batch) >= self.config.max_batch
                or size + added > self.config.max_request_bytes
            ):
                batches.append(batch)
                batch, size, added = [], overhead, encoded_size
            if size + added > self.config.max_request_bytes:
                raise EmbeddingUnavailable("пакет превышает лимит эмбеддингов")
            batch.append(text)
            size += added
        if batch:
            batches.append(batch)
        vectors = []
        try:
            for batch in batches:
                response = self._request(self.config.embeddings_url, self._body(batch))
                self._check_identity(response)
                rows = sorted(response["data"], key=lambda item: item["index"])
                if [row["index"] for row in rows] != list(range(len(batch))):
                    raise ValueError("embedding indices do not match input")
                for row in rows:
                    vector = [float(value) for value in row["embedding"]]
                    if len(vector) != self.config.dim or not all(
                        map(math.isfinite, vector)
                    ):
                        raise ValueError("invalid embedding dimensions or values")
                    norm = math.sqrt(sum(value * value for value in vector))
                    if not math.isclose(norm, 1.0, abs_tol=0.01):
                        raise ValueError("embedding is not L2-normalized")
                    vectors.append(vector)
        except (KeyError, TypeError, ValueError) as exc:
            raise EmbeddingUnavailable("неверный ответ сервиса эмбеддингов") from exc
        return vectors

    def _body(self, texts):
        return json.dumps(
            {"input": texts, "model": self.config.model}, ensure_ascii=False
        ).encode("utf-8")

    def health(self):
        try:
            response = self._request(self.config.health_url, timeout=2.0, retries=0)
            self._check_identity(response)
            return response.get("status") == "ok"
        except (EmbeddingUnavailable, ValueError, TypeError, AttributeError):
            return False


def _cuda_setting(component, name, default, environ):
    # Частная настройка компонента важнее общей EXTRACTOR_CUDA_*; без частной общая
    # задаёт точность обеих моделей.
    return (
        environ.get(f"EXTRACTOR_{component}_CUDA_{name}")
        or environ.get(f"EXTRACTOR_CUDA_{name}")
        or default
    )


def _positive_setting(name, default, environ):
    value = environ.get(name) or ""
    if not value:
        return default
    if not value.isdigit() or int(value) < 1:
        raise ValueError(f"{name} must be a positive integer, got {value!r}")
    return int(value)


def runtime_configs(device, *, e5_stage=True, environ=None):
    """(tagger, stage) RuntimeConfig из окружения; на CPU ``cuda_*`` игнорируются.

    На CUDA по умолчанию SDPA и fp32 — точность проверенного сквозного прогона (torch 2.8.0+cu128);
    bf16 включается отдельно для каждой модели, пока паритет не измерен на целевом GPU. LR поверх E5
    обучена на bf16-признаках, поэтому модель стадии — первый кандидат на
    ``EXTRACTOR_STAGE_CUDA_DTYPE=bf16``. Размеры пакетов задаются на развёртывание: корзины длин
    фиксируют дополнение, так что размер пакета меняет скорость и память, но не метки.
    """
    environ = os.environ if environ is None else environ
    tagger = RuntimeConfig(
        device=device,
        cuda_dtype=_cuda_setting("TAGGER", "DTYPE", "fp32", environ),
        cuda_attention=_cuda_setting("TAGGER", "ATTENTION", "sdpa", environ),
        batch_size=_positive_setting("EXTRACTOR_TAGGER_BATCH_SIZE", RuntimeConfig.batch_size, environ),
    )
    stage = RuntimeConfig(
        device=device,
        cuda_dtype=_cuda_setting("STAGE", "DTYPE", "fp32", environ),
        cuda_attention=_cuda_setting("STAGE", "ATTENTION", "sdpa", environ),
        batch_size=_positive_setting(
            "EXTRACTOR_STAGE_BATCH_SIZE", 8 if e5_stage else RuntimeConfig.batch_size, environ
        ),
    )
    return tagger, stage


def build_components(*, registry, device) -> Components:
    stage_name = os.getenv("EXTRACTOR_STAGE_MODEL", "stage-v1")
    stage_class = (
        E5LRStageScorer
        if registry.spec(stage_name).backend is Backend.E5_LR
        else HFStageScorer
    )
    config, stage_config = runtime_configs(device, e5_stage=stage_class is E5LRStageScorer)
    return Components(
        tagger=HFSpanTagger(
            registry, os.getenv("EXTRACTOR_TAGGER_MODEL", "tagger-v1"), config=config
        ),
        stage_scorer=stage_class(registry, stage_name, config=stage_config),
        canonicalizer=Canonicalizer(RegistryEmbeddingClient(registry)),
        featurizer=CorpusFeaturizer(),
        ruler=MethodologyRuler(threshold=registry.thresholds.predict),
        cache=configured_span_cache(),
        evidence_finder=EvidenceFinder(max_sentences=2**31 - 1),
    )
