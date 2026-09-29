"""Регрессии ревью на реальных обработчиках, без готовых ответов модели."""

import pytest
from fastapi.testclient import TestClient

from extractor.schemas import FEATURE_SCHEMA
from extractor.tests.review_http_bridge import inject_storage_fault, review_app


def predict_from_extract(candidate, documents, versions):
    stage = candidate["stage"]
    features = {
        "documents": len(documents), "scientific_works": len(documents),
        "market_records": 0, "active_periods": 1, "author_groups": 1,
        "observation_date": "2026-09-22",
        **{flag: flag in candidate["flags"] for flag in (
            "only_marketing", "not_a_technology", "insufficient_evidence", "has_standard", "single_source"
        )},
        "stage_code": stage["code"], "stage_quote": stage["quote"],
        "stage_prob": stage["prob"], "stage_doc_id": stage["doc_id"],
        "stage_model_version": versions["stage"],
        "stage_single_source": stage["single_source"],
    }
    if stage.get("status") == "failed":
        for key in list(features):
            if key.startswith("stage_") or key in ("insufficient_evidence", "single_source"):
                del features[key]
    return {
        "feature_schema": FEATURE_SCHEMA, "model_version": versions["service"],
        "candidates": [{
            "id": candidate["id"], "name": candidate["canonical_ru"], "features": features,
            "sources": [{"id": doc["doc_id"], **{k: doc[k] for k in (
                "title", "abstract", "source_type", "trust_level"
            )}} for doc in documents],
        }],
    }


def review_documents(source_type="press_release", trust_level="low"):
    return [{
        "doc_id": "review", "source": "review", "title": "Фазовый детектор",
        "abstract": "Разработан Фазовый детектор: прототип прошёл испытания.",
        "source_type": source_type, "trust_level": trust_level,
        "date": "2026-09-01", "lang": "ru",
    }]


@pytest.mark.parametrize("genre,trust,label", [
    ("press_release", "low", "noise"), ("journal_article", "low", "weak_signal"),
    ("journal_article", "high", "weak_signal"),
])
def test_review_preserves_flags_and_trust(genre, trust, label):
    app, stage = review_app()
    documents = review_documents(genre, trust)
    with TestClient(app) as client:
        extracted = client.post("/extract", json={"documents": documents})
        assert extracted.status_code == 200, extracted.text
        body = extracted.json()
        candidate = body["candidates"][0]
        assert candidate["stage"]["code"] == 2
        request = predict_from_extract(candidate, documents, body["model_versions"])
        predicted = client.post("/predict", json=request)
    assert predicted.status_code == 200, predicted.text
    prediction = predicted.json()["predictions"][0]
    assert prediction["label"] == candidate["label"] == label
    assert prediction["value"] == candidate["value"]
    # Один документ: 0,99 × 0,75; голова и распределение стадии не измерены.
    assert prediction["value"] == (0 if trust == "low" else 0.7425)
    assert candidate["head_prob"] is None and candidate["stage"]["probs"] is None
    assert stage.calls == 1


@pytest.mark.parametrize("recover", [True, False])
def test_review_failed_stage_is_retried(recover):
    app, stage = review_app()
    stage.fail = True
    documents = review_documents("journal_article", "high")
    with TestClient(app) as client:
        extracted = client.post("/extract", json={"documents": documents})
        assert extracted.status_code == 200, extracted.text
        body = extracted.json()
        assert body["coverage"]["degraded"]
        candidate = body["candidates"][0]
        assert candidate["stage"]["status"] == "failed"
        request = predict_from_extract(candidate, documents, body["model_versions"])
        stage.fail = not recover
        response = client.post("/predict", json=request)
    assert response.status_code == 200, response.text
    prediction = response.json()["predictions"][0]
    assert prediction["label"] == ("weak_signal" if recover else "failed")
    assert stage.calls == 2


def test_review_computed_zero_is_reused():
    app, stage = review_app()
    documents = review_documents("journal_article", "high")
    documents[0]["abstract"] = "Фазовый детектор описан без подтверждения стадии."
    with TestClient(app) as client:
        body = client.post("/extract", json={"documents": documents}).json()
        candidate = body["candidates"][0]
        assert candidate["stage"]["status"] == "computed" and candidate["stage"]["code"] == 0
        response = client.post("/predict", json=predict_from_extract(candidate, documents, body["model_versions"]))
    assert response.status_code == 200, response.text
    assert response.json()["predictions"][0]["label"] == "noise"
    assert stage.calls == 1


@pytest.mark.parametrize("fault,status,code", [
    ("span_storage", 503, "storage_unavailable"), ("cache", 422, "cache_corrupted"),
    ("response", 413, "response_too_large"),
])
def test_review_infrastructure_errors_are_contractual(fault, status, code, tmp_path):
    from extractor.schemas import ErrorResponse

    app, _ = review_app(max_response=256 if fault == "response" else 16 << 20)
    documents = review_documents()
    with TestClient(app) as client:
        if fault != "response":
            assert client.post("/extract", json={"documents": documents}).status_code == 200
        inject_storage_fault(app, fault, documents, tmp_path)
        response = client.post("/extract", json={"documents": documents})
    assert response.status_code == status, response.text
    error = ErrorResponse.model_validate(response.json())
    assert error.error == code


def test_review_source_sample_preserves_full_corpus_trust():
    app, _ = review_app()
    documents = [
        {**review_documents("journal_article", "high" if i < 9 else "low")[0], "doc_id": f"review:{i:02d}"}
        for i in range(40)
    ]
    with TestClient(app) as client:
        extracted = client.post("/extract", json={"documents": documents}).json()
        candidate = extracted["candidates"][0]
        request = predict_from_extract(candidate, documents, extracted["model_versions"])
        item = request["candidates"][0]
        item["features"]["source_trust"] = {"trusted": 9, "total": 40}
        item["sources"] = item["sources"][:24]
        predicted = client.post("/predict", json=request)
    assert predicted.status_code == 200, predicted.text
    assert predicted.json()["predictions"][0]["value"] == candidate["value"] == 0.22275
