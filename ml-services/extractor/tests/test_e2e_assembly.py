import sys
import time
from types import SimpleNamespace

import pytest
from fastapi.testclient import TestClient

from extractor.api import create_app
from extractor.interfaces import ModelUnavailable
from extractor.pipeline import Components
from extractor.registry import default_registry
from extractor.rules import MethodologyRuler
from extractor.runtime.cache import InMemorySpanCache
from extractor.schemas import (
    FEATURE_SCHEMA,
    Document,
    ExtractResponse,
    PredictCandidate,
    PredictLabel,
    PredictRequest,
    PredictResponse,
    PredictSource,
    is_russian,
)
from extractor.stubs import DictionarySpanTagger
from extractor.tagger_hf import TaggedSpans


@pytest.fixture
def five_documents():
    dates = ["2025-01-15", "2025-10-01", "2026-01-01", "2026-04-01", "2026-09-21"]
    return [
        Document(
            doc_id=f"d{i}",
            source="e2e",
            source_type="journal_article",
            trust_level="low" if i == 0 else "high",
            date=day,
            authors=[f"Группа {i}"],
            title=f"Отчёт {i}: цифровой двойник и машинное обучение",
            abstract=(
                "Разработан прототип: цифровой двойник прошёл демонстрацию. "
                "Машинное обучение: предложен метод для лабораторного исследования."
            ),
        )
        for i, day in enumerate(dates)
    ]


@pytest.fixture
def assembled(monkeypatch):
    # Проверяется настоящий путь запуска module:function, а не внедрённые сервисы.
    for name in (
        "EXTRACTOR_USE_STUBS",
        "RADAR_CLASSIFIER_VERSION",
        "EXTRACTOR_POSTGRES_DSN",
    ):
        monkeypatch.delenv(name, raising=False)
    monkeypatch.setenv("EXTRACTOR_DEVICE", "cpu")
    monkeypatch.setenv("EXTRACTOR_COMPONENTS_FACTORY", "e2e_factory:build")
    registry = default_registry()

    class Tagger(DictionarySpanTagger):
        calls = 0

        def tag(self, docs):
            self.calls += 1
            return TaggedSpans(
                super().tag(docs),
                windows=[3 if doc.doc_id in {"d0", "d1"} else 1 for doc in docs],
            )

    components = Components(
        tagger=Tagger(),
        ruler=MethodologyRuler(threshold=registry.thresholds.predict),
        cache=InMemorySpanCache(),
    )
    calls = []

    def build(*, registry, device):
        calls.append((registry.service_version, device))
        return components

    monkeypatch.setitem(sys.modules, "e2e_factory", SimpleNamespace(build=build))
    return SimpleNamespace(registry=registry, components=components, calls=calls)


def extract_body(documents, registry):
    return {
        "model_version": registry.service_version,
        "documents": [doc.model_dump(mode="json") for doc in documents],
    }


def predict_body(extracted, documents, registry):
    by_id = {doc.doc_id: doc for doc in documents}
    return PredictRequest(
        feature_schema=FEATURE_SCHEMA,
        model_version=registry.service_version,
        candidates=[
            PredictCandidate(
                id=c.id,
                name=c.canonical_ru,
                surface_forms=c.surface_forms,
                features=c.trend.features.to_predict_features(),
                sources=[
                    PredictSource(
                        id=key, title=by_id[key].title, abstract=by_id[key].abstract
                    )
                    for key in c.doc_ids
                ],
            )
            for c in extracted.candidates
        ],
    ).model_dump(mode="json")


def unavailable(*args, **kwargs):
    raise ModelUnavailable("модель временно недоступна")


def test_http_extract_then_predict_and_repeats(assembled, five_documents):
    registry, components = assembled.registry, assembled.components
    with TestClient(create_app(registry=registry)) as client:
        assert assembled.calls == [(registry.service_version, "cpu")]
        body = extract_body(five_documents, registry)
        cold = client.post("/extract", json=body)
        assert cold.status_code == 200, cold.text
        extracted = ExtractResponse.model_validate(cold.json())
        assert len(extracted.candidates) == 2
        assert extracted.coverage.documents_processed == 5
        assert extracted.coverage.documents_multi_window == 2
        assert not extracted.coverage.degraded
        for candidate in extracted.candidates:
            assert candidate.trend.features.growth == 4.0
            assert candidate.trend.code == 3
            assert candidate.score == candidate.stage.code + 3
            assert candidate.value == round(candidate.stage.prob * 0.8, 6)
            assert all(
                isinstance(f.value, str) and f.value for f in candidate.explanation
            )
            for evidence in candidate.evidence:
                doc = next(d for d in five_documents if d.doc_id == evidence.doc_id)
                assert doc.text[evidence.start : evidence.end] == evidence.sentence
        warm = client.post("/extract", json=body)
        assert warm.content == client.post("/extract", json=body).content
        assert warm.json()["candidates"] == cold.json()["candidates"]
        assert warm.json()["coverage"]["cache_hits"] == 5
        assert warm.json()["coverage"]["documents_multi_window"] is None
        assert components.tagger.calls == 1
        components.cache.clear()
        assert cold.content == client.post("/extract", json=body).content

        payload = predict_body(extracted, five_documents, registry)
        response = client.post("/predict", json=payload)
        assert response.status_code == 200, response.text
        predicted = PredictResponse.model_validate(response.json())
        predicted.check_against(PredictRequest.model_validate(payload))
        assert any(p.value > 0 for p in predicted.predictions)
        for p in predicted.predictions:
            assert p.threshold == registry.thresholds.predict
            assert p.version == registry.service_version
            assert all(isinstance(f.value, str) and f.value for f in p.features)
        assert response.content == client.post("/predict", json=payload).content
        assert (
            client.get("/health")
            .json()["detail"]
            .count("хранилище span_cache (InMemorySpanCache): ok")
            == 1
        )


def test_startup_loads_model_runtimes_without_traffic(assembled):
    class Runtime:
        model_name, backend, loads = "tagger-v1", None, 0

        def load(self):
            self.loads += 1
            self.backend = object()
            return self

    runtime = assembled.components.tagger.runtime = Runtime()
    with TestClient(create_app(registry=assembled.registry)) as client:
        for _ in range(100):
            if runtime.backend is not None:
                break
            time.sleep(0.01)
        detail = client.get("/health").json()["detail"]
    assert runtime.loads == 1
    assert "runtime extract.tagger: loaded" in detail


@pytest.mark.parametrize(
    "failed_component,status", [("stage_scorer", 200), ("tagger", 503)]
)
def test_http_extract_failures(
    assembled, five_documents, monkeypatch, failed_component, status
):
    component = getattr(assembled.components, failed_component)
    monkeypatch.setattr(
        component, "tag" if failed_component == "tagger" else "score", unavailable
    )
    with TestClient(create_app(registry=assembled.registry)) as client:
        response = client.post(
            "/extract", json=extract_body(five_documents, assembled.registry)
        )
    assert response.status_code == status, response.text
    if status == 200:
        result = ExtractResponse.model_validate(response.json())
        assert result.coverage.degraded and result.coverage.documents_failed == 5
        assert all(c.stage.code == 0 and c.value == 0 for c in result.candidates)
    else:
        assert response.json()["error"] == "classifier_unavailable"
        assert is_russian(response.json()["message"])
    for doc in five_documents:
        assert (
            assembled.components.cache.get(
                doc.text_sha256, assembled.components.tagger.version
            )
            is None
        )


@pytest.mark.parametrize("failed_component", ["stage_scorer", "ruler"])
def test_http_predict_model_failure_rejects_the_entire_batch(
    assembled, five_documents, monkeypatch, failed_component
):
    with TestClient(create_app(registry=assembled.registry)) as client:
        extracted = ExtractResponse.model_validate(
            client.post(
                "/extract", json=extract_body(five_documents, assembled.registry)
            ).json()
        )
        payload = predict_body(extracted, five_documents, assembled.registry)
        # Правила привязывает конструктор конвейера: ломаем именно активный экземпляр.
        component = getattr(
            client.app.state.predict_service.components, failed_component
        ).inner
        original, calls = component.score, []

        def fail_after_first_candidate(candidate):
            calls.append(candidate.id)
            if len(calls) == 2:
                unavailable()
            return original(candidate)

        monkeypatch.setattr(
            component,
            "score",
            fail_after_first_candidate if failed_component == "ruler" else unavailable,
        )
        response = client.post("/predict", json=payload)
        assert response.status_code == 200, response.text
        result = PredictResponse.model_validate(response.json())
        result.check_against(PredictRequest.model_validate(payload))
        assert len(result.predictions) == 2
        assert all(
            p.label is PredictLabel.FAILED and p.value == 0 for p in result.predictions
        )
        assert all(
            p.threshold == assembled.registry.thresholds.predict
            for p in result.predictions
        )
        if failed_component == "ruler":
            assert len(calls) == 2  # первое, уже посчитанное предсказание отбрасывается


@pytest.mark.parametrize("list_input", [False, True])
def test_http_conflicting_doc_id_is_russian_422(assembled, five_documents, list_input):
    doc = five_documents[0].model_dump(mode="json")
    conflict = {**doc, "authors": ["Другая группа"]}
    body = [doc, conflict] if list_input else {"documents": [doc, conflict]}
    with TestClient(create_app(registry=assembled.registry)) as client:
        response = client.post("/extract", json=body)
    assert response.status_code == 422, response.text
    assert response.json()["error"] == "invalid_request"
    assert is_russian(response.json()["message"])
    assert "конфликт текста или метаданных doc_id" in response.text
    assert assembled.components.tagger.calls == 0
