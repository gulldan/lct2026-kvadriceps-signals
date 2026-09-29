"""Сквозной контракт чисел и переиспользования стадии без загрузки весов."""

from copy import deepcopy

import pytest
from fastapi.testclient import TestClient

from extractor.api import create_app
from extractor.outputs import ExtractPipeline, PredictService
from extractor.pipeline import Components
from extractor.registry import default_registry
from extractor.schemas import FEATURE_SCHEMA, P14Platt, PredictFeatures
from extractor.stubs import KeywordStageScorer


class CountingStage(KeywordStageScorer):
    def __init__(self):
        self.version = default_registry().spec("stage-v1").ref
        self.calls = 0
        self.documents = []

    def score(self, sentences):
        self.calls += 1
        self.documents.extend(sentence.doc_id for sentence in sentences)
        return super().score(sentences)


def payload(ratio=3):
    return {
        "feature_schema": FEATURE_SCHEMA,
        "model_version": default_registry().service_version,
        "candidates": [
            {
                "id": "p0",
                "name": "Фазовый детектор",
                "features": {
                    "documents": 16,
                    "scientific_works": 16,
                    "market_records": 0,
                    "author_groups": 1,
                    "active_periods": 1,
                    "docs_recent_12m": 12,
                    "docs_previous_12m": 4,
                    "growth_ratio": ratio,
                    "observation_date": "2026-09-22",
                    "share_growth_pp": -20,
                    "stage_code": 3,
                    "stage_quote": "Фазовый детектор прошёл пилот.",
                    "stage_model_version": default_registry().spec("stage-v1").ref,
                    "stage_prob": 0.91,
                    "stage_doc_id": "full-document",
                },
                "sources": [
                    {
                        "id": "full-document",
                        "title": "Фазовый детектор",
                        "abstract": "Короткая аннотация без стадии.",
                    }
                ],
            }
        ],
    }


@pytest.mark.parametrize(
    "ratio,trend", [(None, 1), (0, 1), (1.19, 1), (1.2, 2), (2.99, 2), (3, 3)]
)
def test_predict_p0_reuses_stage_and_registry_ratio_thresholds(ratio, trend):
    stage = CountingStage()
    components = Components(stage_scorer=stage)
    app = create_app(
        extract_pipeline=ExtractPipeline(components),
        predict_service=PredictService(components),
    )
    request = payload(ratio)
    features = PredictFeatures.model_validate(
        request["candidates"][0]["features"]
    ).to_trend_features()
    assert features.growth == ratio and features.share_growth_pp == -20
    assert features.observation_date.isoformat() == "2026-09-22"
    with TestClient(app) as client:
        response = client.post("/predict", json=request)
    assert response.status_code == 200, response.text
    prediction = response.json()["predictions"][0]
    ml = prediction["ml"]
    assert (ml["stage_code"], ml["trend_code"], ml["score"]) == (3, trend, 3 + trend)
    assert ml["stage_label"] == "пилот"
    assert ml["class"] == prediction["label"] == "weak_signal"
    assert ml["confidence"] == prediction["value"] == 0.91
    assert "стадия сохранена" in ml["basis"] == prediction["basis"]
    # Клиент без stage_probs: диапазон оценён снизу вероятностью кода.
    assert "оценена снизу" in prediction["basis"]
    assert stage.calls == 0
    for key, value in ml.items():
        item = next(
            item for item in prediction["features"] if item["name"].endswith(f"({key})")
        )
        assert isinstance(item["value"], str) and item["value"] == str(value)
    assert sum(
        item["contribution"] for item in prediction["features"]
    ) == pytest.approx(ml["confidence"])


def test_predict_p0_uses_stage_distribution_and_head_probability():
    request = payload()
    features = request["candidates"][0]["features"]
    features.update(stage_probs=[0.02, 0.03, 0.04, 0.91, 0.0, 0.0], head_prob=0.5)
    app = create_app(
        extract_pipeline=ExtractPipeline(Components()),
        predict_service=PredictService(Components(stage_scorer=CountingStage())),
    )
    with TestClient(app) as client:
        response = client.post("/predict", json=request)
        assert response.status_code == 200, response.text
        prediction = response.json()["predictions"][0]
        # P(коды 1–4) = 0,98; × голова 0,5; 16 документов и доверие не измерено.
        assert prediction["value"] == prediction["ml"]["confidence"] == 0.49
        assert "оценена снизу" not in prediction["basis"] and "Платту" not in prediction["basis"]
    # Калибровка модели стадии применяется и к стадии, переиспользованной из /extract.
    calibrated_stage = CountingStage()
    calibrated_stage.p14_platt = P14Platt(0.8435, -1.1996)
    app = create_app(
        extract_pipeline=ExtractPipeline(Components()),
        predict_service=PredictService(Components(stage_scorer=calibrated_stage)),
    )
    with TestClient(app) as client:
        prediction = client.post("/predict", json=request).json()["predictions"][0]
        assert prediction["value"] == round(calibrated_stage.p14_platt(0.98) * 0.5, 6)
        assert "Платту" in prediction["basis"] and calibrated_stage.calls == 0
    with TestClient(create_app(
        extract_pipeline=ExtractPipeline(Components()),
        predict_service=PredictService(Components(stage_scorer=CountingStage())),
    )) as client:
        for key, value in (
            ("stage_probs", [0.5, 0.5]),
            ("stage_probs", [0.2] * 6),
            ("stage_probs", [1.5, -0.5, 0, 0, 0, 0]),
            ("head_prob", 1.2),
        ):
            broken = deepcopy(request)
            broken["candidates"][0]["features"][key] = value
            rejected = client.post("/predict", json=broken)
            assert rejected.status_code == 422, (key, value)
            assert rejected.json()["error"] == "invalid_request"


def test_predict_p0_mixed_versions_only_recompute_stale_stage():
    stage = CountingStage()
    service = PredictService(Components(stage_scorer=stage))
    app = create_app(
        extract_pipeline=ExtractPipeline(Components()), predict_service=service
    )
    request = payload()
    stale = deepcopy(request["candidates"][0])
    stale["id"] = "stale"
    stale["sources"][0]["id"] = "short-document"
    stale["features"]["stage_model_version"] = "old-stage"
    request["candidates"].append(stale)
    with TestClient(app) as client:
        response = client.post("/predict", json=request)
    assert response.status_code == 200, response.text
    predictions = {item["id"]: item for item in response.json()["predictions"]}
    assert predictions["p0"]["ml"]["stage_code"] == 3
    assert predictions["stale"]["ml"]["stage_code"] == 0
    assert "версия модели стадии не совпадает" in predictions["stale"]["basis"]
    assert stage.calls == 1 and set(stage.documents) == {"short-document"}


def test_predict_p0_rejects_unquoted_stage_and_preserves_zero():
    from extractor.schemas import PredictRequest

    request = payload()
    request["candidates"][0]["features"]["stage_quote"] = ""
    with pytest.raises(ValueError, match="цитату"):
        PredictRequest.model_validate(request)
    request["candidates"][0]["features"]["stage_code"] = 0
    stage = CountingStage()
    prediction = (
        PredictService(Components(stage_scorer=stage))
        .predict(PredictRequest.model_validate(request))
        .predictions[0]
    )
    assert prediction.ml.stage_code == 0 and prediction.ml.score == 3
    assert prediction.ml.confidence == 0 and stage.calls == 0


def test_predict_p0_half_year_growth_within_search_window():
    from extractor.schemas import PredictRequest

    # Окно коллеги 14 месяцев: 3 документа за последние 6 полных месяцев и 1 за
    # предыдущие 6 дают отношение 3, при T2 = 3 реестра — тренд 3.
    request = payload(ratio=None)
    features = request["candidates"][0]["features"]
    for key in ("docs_recent_12m", "docs_previous_12m", "growth_ratio"):
        features.pop(key)
    features.update(growth_window="6m/6m", docs_recent=3, docs_previous=1, growth_ratio=3)
    assert default_registry().thresholds.trend_t2 == 3
    prediction = (
        PredictService(Components(stage_scorer=CountingStage()))
        .predict(PredictRequest.model_validate(request))
        .predictions[0]
    )
    assert (prediction.ml.trend_code, prediction.ml.score) == (3, 6)
    assert "6 полных месяцев" in prediction.basis
    for update in (
        {"growth_ratio": 2},  # отношение не совпадает со счётчиками
        {"docs_previous": None},  # окно без счётчика
        {"growth_window": None},  # счётчики без имени окна
        {"docs_recent": 0, "docs_previous": 0},  # нулевые окна не измеряют рост
    ):
        broken = deepcopy(request)
        broken["candidates"][0]["features"].update(update)
        with pytest.raises(ValueError):
            PredictRequest.model_validate(broken)
    features.update(docs_recent=0, docs_previous=0, growth_ratio=None)
    prediction = (
        PredictService(Components(stage_scorer=CountingStage()))
        .predict(PredictRequest.model_validate(request))
        .predictions[0]
    )
    assert prediction.ml.trend_code == 1


def test_predict_p0_trend_needs_three_recent_documents():
    from extractor.schemas import PredictRequest

    def trend(**window):
        request = payload(ratio=None)
        features = request["candidates"][0]["features"]
        for key in ("docs_recent_12m", "docs_previous_12m", "growth_ratio"):
            features.pop(key)
        features.update(window)
        return (
            PredictService(Components(stage_scorer=CountingStage()))
            .predict(PredictRequest.model_validate(request))
            .predictions[0]
            .ml.trend_code
        )

    # Два документа против нуля — отношение 2, но это единицы документов.
    assert trend(growth_window="6m/6m", docs_recent=2, docs_previous=0, growth_ratio=2) == 1
    assert trend(growth_window="6m/6m", docs_recent=3, docs_previous=0, growth_ratio=3) == 3
    assert trend(docs_recent_12m=2, docs_previous_12m=1, growth_ratio=2) == 1
    assert trend(docs_recent_12m=3, docs_previous_12m=2, growth_ratio=1.5) == 2
