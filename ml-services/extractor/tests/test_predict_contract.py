"""Контрактные тесты ``POST /predict`` по README.md (раздел «Стык с радаром»).

Позитивный вход — пример из контракта без правок, кроме ``model_version``: версия обязана
указывать на наш артефакт, поэтому пакет с чужой версией отклоняется, и это тоже проверяется.
"""

from __future__ import annotations

import json
from copy import deepcopy

import pytest

from extractor.interfaces import ModelUnavailable, StageScorer
from extractor.pipeline import Components, PredictService
from extractor.schemas import (
    FEATURE_SCHEMA,
    MAX_REQUEST_BYTES,
    PredictLabel,
    PredictRequest,
    PredictResponse,
    is_russian,
)

CONTRACT_LABELS = {"weak_signal", "mature", "noise", "failed"}


def post(client, payload: dict):
    return client.post("/predict", json=payload)


def check_contract_response(payload: dict, request: dict) -> PredictResponse:
    response = PredictResponse.model_validate(payload)
    assert response.feature_schema == FEATURE_SCHEMA

    wanted = [candidate["id"] for candidate in request["candidates"]]
    got = [prediction.id for prediction in response.predictions]
    assert sorted(got) == sorted(wanted), "one answer per input id, no gaps, no new ids"
    assert len(got) == len(set(got)), "no repeated ids"

    for prediction in response.predictions:
        assert prediction.label.value in CONTRACT_LABELS
        assert 0.0 <= prediction.value <= 1.0
        assert 0.0 <= prediction.threshold <= 1.0
        assert prediction.model
        assert prediction.version == request["model_version"]
        assert prediction.feature_schema == FEATURE_SCHEMA
        assert prediction.basis and is_russian(prediction.basis)
        assert prediction.features, "explanations are mandatory"
        for feature in prediction.features:
            assert feature.name and is_russian(feature.name)
            assert isinstance(feature.value, str) and feature.value.strip()
            assert isinstance(feature.contribution, float)
    return response


def test_handoff_example_is_answered_per_contract(
    client, handoff_request, service_version
):
    request = {**handoff_request, "model_version": service_version}
    response = post(client, request)
    assert response.status_code == 200
    body = check_contract_response(response.json(), request)
    prediction = body.predictions[0]
    # В примере источники-заглушки, стадия не подтверждена: «недостаточно данных»
    # уходит как noise ровно с этим basis.
    assert prediction.label is PredictLabel.NOISE
    assert "нет подтверждённой стадии" in prediction.basis


def test_demo_batch_covers_weak_signal_and_mature(client, demo_predict_request):
    demo_predict_request = deepcopy(demo_predict_request)
    demo_predict_request["candidates"][0]["surface_forms"] = ["batch fermentation"]
    demo_predict_request["candidates"][1]["surface_forms"] = [
        "холодная листовая штамповка"
    ]
    response = post(client, demo_predict_request)
    assert response.status_code == 200
    body = check_contract_response(response.json(), demo_predict_request)
    labels = {prediction.id: prediction.label for prediction in body.predictions}
    # Стадия 2 (биореактор 10 л) — слабый сигнал, стадия 5 («нашли широкое применение») —
    # зрелая; обе совпадают с ручной разметкой стадий.
    assert labels["cand-batch-fermentation"] is PredictLabel.WEAK_SIGNAL
    assert labels["cand-cold-stamping"] is PredictLabel.MATURE


def test_foreign_model_version_is_refused(client, handoff_request):
    """В примере контракта weak-signal-2026-09-17.1, а не наша версия."""
    response = post(client, handoff_request)
    assert response.status_code == 409
    body = response.json()
    assert body["error"] == "version_mismatch"
    assert is_russian(body["message"])
    assert "predictions" not in body


def test_wrong_feature_schema_is_refused(client, handoff_request, service_version):
    request = {
        **handoff_request,
        "model_version": service_version,
        "feature_schema": "weak-signal-features-v2",
    }
    response = post(client, request)
    assert response.status_code == 400
    assert response.json()["error"] == "invalid_feature_schema"


def test_duplicate_candidate_id_is_refused(client, handoff_request, service_version):
    candidate = handoff_request["candidates"][0]
    request = {
        **handoff_request,
        "model_version": service_version,
        "candidates": [candidate, dict(candidate)],
    }
    response = post(client, request)
    assert response.status_code == 400
    assert response.json()["error"] == "duplicate_id"


def test_empty_batch_is_refused(client, handoff_request, service_version):
    request = {**handoff_request, "model_version": service_version, "candidates": []}
    response = post(client, request)
    assert response.status_code == 400
    assert response.json()["error"] == "invalid_request"


def test_missing_features_is_a_contract_error_not_a_stacktrace(
    client, handoff_request, service_version
):
    candidate = {
        k: v for k, v in handoff_request["candidates"][0].items() if k != "features"
    }
    request = {
        **handoff_request,
        "model_version": service_version,
        "candidates": [candidate],
    }
    response = post(client, request)
    assert response.status_code == 422
    body = response.json()
    assert body["error"] == "invalid_request"
    assert is_russian(body["message"])
    assert body["detail"], "the caller is told which field is wrong"


def test_unknown_feature_name_is_rejected(client, handoff_request, service_version):
    candidate = json.loads(json.dumps(handoff_request["candidates"][0]))
    candidate["features"]["invented_feature"] = 1
    request = {
        **handoff_request,
        "model_version": service_version,
        "candidates": [candidate],
    }
    response = post(client, request)
    assert response.status_code == 422
    assert response.json()["error"] == "invalid_request"


def test_null_measurement_is_not_replaced_by_zero(handoff_request, service_version):
    model = PredictRequest.model_validate(
        {**handoff_request, "model_version": service_version}
    )
    features = model.candidates[0].features
    assert features.share_growth_pp is None
    assert features.top_group_share == 0.33
    trend = features.to_trend_features()
    assert trend.growth is None
    assert trend.documents == 4


def test_body_over_four_mebibytes_is_rejected_before_parsing(client):
    oversized = (
        b'{"feature_schema":"x","candidates":[],"pad":"'
        + b"a" * MAX_REQUEST_BYTES
        + b'"}'
    )
    response = client.post(
        "/predict", content=oversized, headers={"content-type": "application/json"}
    )
    assert response.status_code == 413
    assert response.json()["error"] == "payload_too_large"


class BrokenStageScorer(StageScorer):
    version = "broken@0"

    def score(self, sentences):
        raise ModelUnavailable("веса стадии не загрузились")


class SlowStageScorer(StageScorer):
    version = "slow@0"

    def score(self, sentences):
        import time

        time.sleep(0.5)
        return []


def test_model_failure_yields_failed_for_every_candidate(demo_predict_request):
    from fastapi.testclient import TestClient

    from extractor.api import create_app

    service = PredictService(Components(stage_scorer=BrokenStageScorer()))
    with TestClient(create_app(predict_service=service)) as client:
        response = client.post("/predict", json=demo_predict_request)
    assert response.status_code == 200
    body = check_contract_response(response.json(), demo_predict_request)
    assert {p.label for p in body.predictions} == {PredictLabel.FAILED}
    for prediction in body.predictions:
        assert prediction.value == 0.0
        assert "не подставляется" in prediction.basis


def test_timeout_yields_failed_not_a_partial_batch(demo_predict_request):
    from fastapi.testclient import TestClient

    from extractor.api import create_app

    service = PredictService(Components(stage_scorer=SlowStageScorer()))
    app = create_app(predict_service=service, predict_timeout=0.01)
    with TestClient(app) as client:
        response = client.post("/predict", json=demo_predict_request)
    assert response.status_code == 200
    body = check_contract_response(response.json(), demo_predict_request)
    assert {p.label for p in body.predictions} == {PredictLabel.FAILED}
    assert all("бюджет" in p.basis for p in body.predictions)


def test_predict_has_its_own_slot_and_extract_has_a_deadline(demo_predict_request):
    import threading
    import time

    from fastapi.testclient import TestClient

    from extractor.api import create_app
    from extractor.outputs import ExtractPipeline
    from extractor.schemas import ErrorResponse

    release = threading.Event()

    class BlockingPipeline(ExtractPipeline):
        def run(self, request):
            release.wait(10)
            return super().run(request)

    components = Components()
    app = create_app(
        extract_pipeline=BlockingPipeline(components),
        predict_service=PredictService(components),
        extract_timeout=0.2,
    )
    body = {"documents": [{"doc_id": "d", "source": "t", "title": "Фазовый детектор", "abstract": "Прототип."}]}
    with TestClient(app) as client:
        try:
            started = time.perf_counter()
            response = client.post("/extract", json=body)
            assert response.status_code == 504, response.text
            error = ErrorResponse.model_validate(response.json())
            assert error.error == "timeout" and is_russian(error.message)
            # Брошенное извлечение ещё держит свой слот: /predict не должен его ждать.
            predicted = client.post("/predict", json=demo_predict_request)
            assert predicted.status_code == 200
            labels = {p.label for p in check_contract_response(predicted.json(), demo_predict_request).predictions}
            assert PredictLabel.FAILED not in labels
            # Следующее извлечение встаёт за ним в очередь и тоже получает ошибку контракта.
            assert client.post("/extract", json=body).json()["error"] == "timeout"
            assert time.perf_counter() - started < 5
        finally:
            release.set()


@pytest.mark.parametrize("endpoint", ["/health", "/metrics"])
def test_probe_endpoints(client, endpoint):
    response = client.get(endpoint)
    assert response.status_code == 200


def test_go_extract_fixture_is_accepted_by_the_service_and_response_schema(monkeypatch):
    from pathlib import Path

    from fastapi.testclient import TestClient

    from extractor.api import create_app
    from extractor.outputs import ExtractPipeline, PredictService
    from extractor.registry import ModelRegistry, default_registry
    from extractor.schemas import ExtractResponse

    fixtures = Path(__file__).resolve().parents[3] / "backend/main-service/internal/application/testdata/ml_candidate_source"
    # Шлюз извлекает кандидатов, а стадию оценивает своей встроенной моделью.
    request = json.loads((fixtures / "extract_request.json").read_text())
    expected = json.loads((fixtures / "extract_response.json").read_text())
    ExtractResponse.model_validate(expected)
    config = default_registry().config.model_copy(deep=True)
    config.service_version = request["model_version"]
    registry = ModelRegistry(config)
    monkeypatch.delenv("RADAR_CLASSIFIER_VERSION", raising=False)
    with TestClient(create_app(
        registry=registry,
        extract_pipeline=ExtractPipeline(Components(), registry=registry),
        predict_service=PredictService(Components(), registry=registry),
    )) as client:
        response = client.post("/extract", json=request)
        assert response.status_code == 200, response.text
        result = ExtractResponse.model_validate(response.json())
        assert result.coverage.documents_processed == len(request["documents"])
        assert result.model_versions["service"] == request["model_version"]


def test_deployment_version_only_checks_registry(monkeypatch):
    from extractor.api import create_app, deployment_registry
    from extractor.registry import default_registry

    registry = default_registry()
    monkeypatch.delenv("RADAR_CLASSIFIER_VERSION", raising=False)
    assert deployment_registry(registry) is registry
    # Compose передаёт переменную, даже когда в infra/.env она пустая.
    monkeypatch.setenv("RADAR_CLASSIFIER_VERSION", "")
    assert deployment_registry(registry) is registry
    monkeypatch.setenv("RADAR_CLASSIFIER_VERSION", registry.service_version)
    assert deployment_registry(registry) is registry
    other_version = "weak-signal-2026-09-21.3"
    assert other_version != registry.service_version
    monkeypatch.setenv("RADAR_CLASSIFIER_VERSION", other_version)
    for start in (deployment_registry, lambda r: create_app(registry=r)):
        with pytest.raises(ValueError, match="RADAR_CLASSIFIER_VERSION=.*does not match the registry"):
            start(registry)
    assert default_registry().service_version == registry.service_version
