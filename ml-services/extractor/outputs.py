"""Публичные сервисы и сборщики ответов HTTP API; их экземпляры передаются в api.create_app.

Внутренняя оркестрация — в pipeline.py, правила — только в rules.py.
"""

from __future__ import annotations

import math
from collections.abc import Mapping, Sequence

from extractor.interfaces import Ruler
from extractor.pipeline import ExtractPipeline as BaseExtractPipeline
from extractor.pipeline import PredictService as BasePredictService
from extractor.schemas import (
    FEATURE_SCHEMA,
    INSUFFICIENT_BASIS_RU,
    Candidate,
    CandidateClass,
    CandidateStage,
    CandidateTrend,
    TrendFeatures,
    ExtractCoverage,
    ExtractRequest,
    ExtractResponse,
    FeatureContribution,
    Prediction,
    PredictRequest,
    PredictResponse,
    is_russian,
    round6,
)


def _rounded(value):
    if isinstance(value, float):
        if not math.isfinite(value):
            raise ValueError("числа в ответе должны быть конечными")
        return round6(value)
    if isinstance(value, dict):
        return {key: _rounded(item) for key, item in value.items()}
    if isinstance(value, list):
        return [_rounded(item) for item in value]
    return value


def _explanation(items: Sequence[FeatureContribution]) -> list[FeatureContribution]:
    if any(not is_russian(item.name) for item in items):
        raise ValueError("названия признаков должны быть по-русски")
    checked = [FeatureContribution.model_validate(item) for item in items]
    return sorted(
        checked,
        key=lambda item: (-abs(item.contribution), item.name, item.model_dump_json()),
    )[:5]


def build_extract_response(
    candidates: Sequence[Candidate],
    coverage: ExtractCoverage,
    model_versions: Mapping[str, str],
) -> ExtractResponse:
    ordered = []
    for candidate in candidates:
        # Повторно проверяем скаляры, не сериализуя тысячи уже проверенных цитат.
        data = dict(candidate.__dict__)
        data["value"] = _rounded(candidate.value)
        data["stage"] = CandidateStage.model_validate(_rounded(dict(candidate.stage.__dict__)))
        data["trend"] = CandidateTrend(
            code=candidate.trend.code,
            features=TrendFeatures.model_validate(_rounded(dict(candidate.trend.features.__dict__))),
        )
        data["doc_ids"] = sorted(set(candidate.doc_ids))
        data["record_ids"] = sorted(set(candidate.record_ids))
        data["flags"] = sorted(set(candidate.flags))
        evidence = {}
        for item in candidate.evidence:
            previous = evidence.get(item.sort_key)
            if previous is None or item.model_dump_json() < previous.model_dump_json():
                evidence[item.sort_key] = item
        # Ограничиваем только представление после расчёта всех признаков.
        # Цитата итоговой стадии идёт первой, чтобы её источник оставался проверяемым.
        items = [evidence[key] for key in sorted(evidence)]
        stage = data["stage"]
        supporting = [item for item in items if item.doc_id == stage.doc_id and stage.quote in item.sentence]
        selected = []
        selected_keys = set()
        budget = 24000
        for item in [*supporting[:1], *items]:
            if item.sort_key in selected_keys:
                continue
            key = item.sort_key
            if len(item.sentence) > 2000:
                offset = item.sentence.find(stage.quote) if item.doc_id == stage.doc_id else 0
                offset = max(0, offset)
                excerpt = item.sentence[offset:offset + 2000]
                item = item.model_copy(update={"start": item.start + offset,
                                               "end": item.start + offset + len(excerpt), "sentence": excerpt})
            if len(selected) >= 24 or len(item.sentence) > budget:
                continue
            selected.append(item)
            selected_keys.add(key)
            budget -= len(item.sentence)
        data["evidence"] = sorted(selected, key=lambda item: item.sort_key)
        if len(stage.quote) > 2000:
            data["stage"] = stage.model_copy(update={"quote": stage.quote[:2000]})
        data["explanation"] = _explanation(candidate.explanation)
        ordered.append(Candidate.model_validate(data))
    ordered.sort(key=lambda candidate: candidate.sort_key)
    if len({candidate.id for candidate in ordered}) != len(ordered):
        raise ValueError("повтор идентификатора кандидата")
    coverage = coverage.model_copy(
        update={
            "candidates_total": len(ordered),
            "stage_confirmed": sum(candidate.stage.confirmed for candidate in ordered),
            "notes": sorted(set(coverage.notes)),
        }
    )
    return ExtractResponse(
        candidates=ordered,
        coverage=coverage,
        model_versions=dict(sorted(model_versions.items())),
    )


def build_predict_response(
    request: PredictRequest,
    predictions: Sequence[Prediction],
    *,
    version: str,
) -> PredictResponse:
    if problem := request.check_contract(version):
        raise ValueError(problem[1])
    items = []
    for prediction in predictions:
        if prediction.version != version or prediction.feature_schema != FEATURE_SCHEMA:
            raise ValueError("версия или схема ответа не соответствует запросу")
        data = _rounded(prediction.model_dump(mode="json"))
        # Обязательные числовые поля не входят в лимит объясняющих факторов.
        data["features"] = (
            list(prediction.features)
            if prediction.ml is not None
            else _explanation(prediction.features)
        )
        items.append(Prediction.model_validate(data))
    response = PredictResponse(predictions=sorted(items, key=lambda item: item.id))
    response.check_against(request)
    return response


class ExtractPipeline(BaseExtractPipeline):
    def model_versions(self) -> dict[str, str]:
        versions = super().model_versions()
        for name in ("canonicalizer", "featurizer"):
            component = getattr(self.components, name)
            version = getattr(component, "version", None)
            if version:
                versions[name] = version
        return versions

    def run(self, request: ExtractRequest) -> ExtractResponse:
        result = super().run(request)
        return build_extract_response(
            result.candidates, result.coverage, result.model_versions
        )


class PredictService(BasePredictService):
    def predict(self, request: PredictRequest) -> PredictResponse:
        if problem := request.check_contract(self.config.service_version):
            raise ValueError(problem[1])
        response = super().predict(request)
        return build_predict_response(
            request,
            response.predictions,
            version=self.config.service_version,
        )

    def failed(self, request: PredictRequest, reason: str) -> PredictResponse:
        response = super().failed(request, reason)
        return build_predict_response(
            request, response.predictions, version=self.config.service_version
        )


def prediction_from_candidate(
    candidate: Candidate, ruler: Ruler, version: str
) -> Prediction:
    """Ответ /predict из уже оценённого кандидата, без повторного вывода."""
    from extractor.schemas import CLASS_TO_LABEL

    basis = (
        ruler.basis_for(candidate.label) if hasattr(ruler, "basis_for") else ruler.basis
    )
    if (
        candidate.label is CandidateClass.INSUFFICIENT_DATA
        and INSUFFICIENT_BASIS_RU not in basis
    ):
        basis = f"{basis}; {INSUFFICIENT_BASIS_RU}"
    return Prediction(
        id=candidate.id,
        label=CLASS_TO_LABEL[candidate.label],
        value=candidate.value,
        threshold=ruler.threshold,
        model=ruler.model_name,
        version=version,
        feature_schema=FEATURE_SCHEMA,
        basis=basis,
        features=_explanation(candidate.explanation),
    )
