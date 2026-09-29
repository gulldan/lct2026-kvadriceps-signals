from datetime import date

import pytest

from extractor.rules import (
    CorpusFeaturizer,
    MethodologyRuler,
    compute_flags,
    detect_has_standard,
    stage_range_probability,
    trend_code,
)
from extractor.schemas import (
    Candidate,
    CandidateClass,
    CandidateStage,
    CandidateTrend,
    Document,
    NoiseFlag,
    P14Platt,
    TrendFeatures,
    is_russian,
)


def candidate(stage=3, count=3, **kwargs):
    return Candidate(
        name="орбитальный компьютер",
        canonical_ru="орбитальный компьютер",
        doc_ids=[str(index) for index in range(count)],
        stage=CandidateStage(code=stage, quote="пилот" if stage else "", prob=0.9),
        trend=CandidateTrend(code=3, features=TrendFeatures(documents=count)),
        **kwargs,
    )


def documents_for(item, trusts=None, source_type="journal_article"):
    trusts = trusts or ["high"] * len(item.doc_ids)
    return {
        key: Document(
            doc_id=key, source="test", trust_level=trust, source_type=source_type
        )
        for key, trust in zip(item.doc_ids, trusts, strict=True)
    }


@pytest.mark.parametrize(
    "standard",
    [
        "ГОСТ 6533-78",
        "ГОСТ Р 123",
        "ISO/IEC 27001",
        "IEC 61508",
        "RFC 8446",
        "IEEE 802.11",
        "ТУ 123-456",
        "ANSI X3",
        "ETSI EN 300",
        "3GPP TS 38.211",
    ],
)
def test_standard_families(standard):
    assert detect_has_standard([f"Требования: {standard}."])


@pytest.mark.parametrize(
    "text", ["ту технологию", "ГОСТиница", "isomorphic", "IECxyz", "3GPPish"]
)
def test_standard_boundaries(text):
    assert not detect_has_standard([text])


@pytest.mark.parametrize(
    "stage,flags,label",
    [
        (0, [], CandidateClass.INSUFFICIENT_DATA),
        (0, [NoiseFlag.ONLY_MARKETING], CandidateClass.NOISE),
        (5, [NoiseFlag.NOT_A_TECHNOLOGY], CandidateClass.NOISE),
        (5, [NoiseFlag.INSUFFICIENT_EVIDENCE], CandidateClass.INSUFFICIENT_DATA),
        (5, [], CandidateClass.MATURE),
        (1, [], CandidateClass.WEAK_SIGNAL),
        (2, [], CandidateClass.WEAK_SIGNAL),
        (3, [], CandidateClass.WEAK_SIGNAL),
        (4, [], CandidateClass.WEAK_SIGNAL),
    ],
)
def test_class_precedence_and_score(stage, flags, label):
    item = candidate(stage=stage, flags=flags)
    outcome = MethodologyRuler(documents=documents_for(item)).score(item)
    assert outcome.label is label
    assert outcome.score == stage + 3


@pytest.mark.parametrize("count,factor", [(0, 0), (1, 0.75), (2, 0.9), (3, 1), (8, 1)])
def test_confirmation_factors_and_exact_explanation(count, factor):
    item = candidate(count=count)
    ruler = MethodologyRuler(documents=documents_for(item))
    outcome = ruler.score(item)
    assert outcome.value == pytest.approx(0.9 * factor)
    assert sum(entry.contribution for entry in outcome.explanation) == pytest.approx(
        outcome.value
    )
    assert len(outcome.explanation) == 5
    assert all(is_russian(entry.name) for entry in outcome.explanation)
    assert [abs(entry.contribution) for entry in outcome.explanation] == sorted(
        [abs(entry.contribution) for entry in outcome.explanation],
        reverse=True,
    )
    assert "вероятность стадии в диапазоне класса ×" in ruler.basis


def test_trust_and_noise_multiply_without_duplicate_penalties():
    item = candidate(count=2, flags=[NoiseFlag.INTENT, NoiseFlag.INTENT])
    ruler = MethodologyRuler(documents=documents_for(item, ["high", "low"]))
    result = ruler.score(item)
    assert result.value == 0.30375  # .9 * .9 * .5 * (1-.25)
    named = {entry.name: entry for entry in result.explanation}
    assert named["Доля доверенных источников"].raw_value == 0.5
    assert named["Штраф шума"].raw_value == 0.25
    assert sum(entry.contribution for entry in result.explanation) == pytest.approx(
        0.30375
    )


# Tequila из прогона E2E: распределение и голова пересчитаны офлайн на CPU
# по тому же бандлу моделей.
TEQUILA_PROBS = [0.114196, 0.150512, 0.536427, 0.028384, 0.100343, 0.070138]


def test_weak_signal_range_sums_codes_one_to_four_and_head_multiplies():
    stage = CandidateStage(code=2, quote="пилот", prob=0.536427, probs=TEQUILA_PROBS)
    item = candidate(count=1).model_copy(update={"stage": stage, "head_prob": 0.999919})
    result = MethodologyRuler(documents=documents_for(item)).score(item)
    assert result.value == 0.6117  # 0,815666 × 0,999919 × 0,75
    named = {entry.name: entry for entry in result.explanation}
    assert named["Вероятность стадии в диапазоне класса"].raw_value == 0.815666
    assert named["Вероятность головы кандидата"].raw_value == 0.999919
    assert sum(e.contribution for e in result.explanation) == pytest.approx(result.value)


def test_p14_platt_calibrates_only_the_weak_signal_range():
    # Параметры stage-v3 из отчёта MODELS_V3_20260923: sigmoid(0,8435·logit(p) − 1,1996).
    platt = P14Platt.parse({"a": 0.8435, "b": -1.1996})
    stage = CandidateStage(code=2, quote="пилот", prob=0.536427, probs=TEQUILA_PROBS)
    item = candidate(count=1).model_copy(update={"stage": stage, "head_prob": 0.999919})
    ruler = MethodologyRuler(documents=documents_for(item))
    assert ruler.score(item).value == 0.6117  # без параметров — тождество (бандл v1.1)
    calibrated = ruler.with_p14_platt(platt)
    result = calibrated.score(item)
    named = {entry.name: entry for entry in result.explanation}
    assert named["Вероятность стадии в диапазоне класса"].raw_value == 0.513722
    assert result.value == 0.38526  # 0,513722 × 0,999919 × 0,75
    assert ruler.p14_platt is None and "Платту" in calibrated.basis_for(result.label)
    assert "Платту" not in ruler.basis_for(result.label)
    # Нижняя граница без распределения калибруется тем же монотонным правилом, код 5 — нет.
    assert stage_range_probability(candidate(count=1).stage, platt) == (pytest.approx(platt(0.9)), False)
    mature = CandidateStage(code=5, quote="widely used", prob=0.48)
    assert stage_range_probability(mature, platt) == (0.48, True)
    assert P14Platt.parse(None) is None
    for bad in ({"a": 0, "b": 1}, {"a": 1}, {"a": "1", "b": 0}, {"a": float("nan"), "b": 0}, [1, 2]):
        with pytest.raises(ValueError, match="p14_platt"):
            P14Platt.parse(bad)


def test_unknown_distribution_and_head_are_explicit_and_mature_uses_code_five():
    # Без распределения — нижняя граница, без головы — нейтральный множитель.
    item = candidate(count=3)
    result = MethodologyRuler().score(item)
    named = {entry.name: entry for entry in result.explanation}
    assert result.value == 0.9
    assert "нижняя граница" in named["Вероятность стадии в диапазоне класса"].value
    assert named["Вероятность головы кандидата"].raw_value is None
    assert "не измерено" in named["Вероятность головы кандидата"].value
    # Для кода 5 берётся prob стадии: в нём уже учтён множитель 0,6 фоновой массовости.
    probs = [0.0, 0.1, 0.1, 0.0, 0.0, 0.8]
    mature = candidate(stage=5, count=3).model_copy(update={"stage": CandidateStage(
        code=5, quote="widely used", prob=0.48, probs=probs, single_source=True)})
    outcome = MethodologyRuler().score(mature)
    assert outcome.label is CandidateClass.MATURE and outcome.value == 0.48
    with pytest.raises(ValueError, match="шесть"):
        CandidateStage(code=2, quote="пилот", prob=0.5, probs=[0.5, 0.5])
    with pytest.raises(ValueError, match="сумма"):
        CandidateStage(code=2, quote="пилот", prob=0.5, probs=[0.1] * 6)


def test_missing_trust_stays_none_and_has_explicit_neutral_factor():
    result = MethodologyRuler().score(candidate())
    trust = next(
        item for item in result.explanation if item.name == "Доля доверенных источников"
    )
    assert trust.raw_value is None
    assert "не измерено" in trust.value
    assert trust.contribution == 0
    assert result.value == 0.9


def test_only_marketing_uses_genre_not_low_trust_alone():
    item = candidate(count=2)
    releases = documents_for(item, source_type="press_release")
    assert NoiseFlag.ONLY_MARKETING in compute_flags(item, releases)
    assert (
        MethodologyRuler(documents=releases).score(item).label is CandidateClass.NOISE
    )
    low_science = documents_for(item, ["low", "low"])
    assert NoiseFlag.ONLY_MARKETING not in compute_flags(item, low_science)
    mixed = {**releases, "0": low_science["0"]}
    assert NoiseFlag.ONLY_MARKETING not in compute_flags(item, mixed)
    assert NoiseFlag.ONLY_MARKETING not in compute_flags(item, {"0": releases["0"]})
    assert NoiseFlag.ONLY_MARKETING not in compute_flags(item, {})


@pytest.mark.parametrize(
    "name",
    ["Биология", "обнаружение мошенничества", "доля рынка", "проектное обучение"],
)
def test_non_technology_dictionary(name):
    item = candidate().model_copy(update={"name": name, "canonical_ru": name})
    assert NoiseFlag.NOT_A_TECHNOLOGY in compute_flags(item, {})
    assert MethodologyRuler().score(item).label is CandidateClass.NOISE


def test_standard_is_maturity_feature_not_a_stage():
    item = candidate(stage=0, has_standard=True)
    flags = compute_flags(item, {})
    assert NoiseFlag.HAS_STANDARD in flags
    assert NoiseFlag.INSUFFICIENT_EVIDENCE in flags
    assert MethodologyRuler().score(item).label is CandidateClass.INSUFFICIENT_DATA


@pytest.mark.parametrize(
    "growth,groups,periods,market,expected",
    [
        (None, 5, 3, 3, 1),
        (1.1, 4, 2, 0, 1),
        (1.2, 1, 2, 0, 1),
        (1.2, 2, 2, 0, 2),
        (2.0, 3, 2, 0, 3),
        (3.0, 2, 2, 0, 2),
        (3.0, 1, 2, 2, 3),
        (3.0, 3, 1, 0, 3),
    ],
)
def test_trend_rules(growth, groups, periods, market, expected):
    features = TrendFeatures(
        documents=4,
        growth=growth,
        author_groups=groups,
        active_periods=periods,
        market_records=market,
    )
    assert trend_code(features, 1.2, 2.0) == expected
    assert trend_code(features, None, None) == 1


def test_one_document_never_measures_a_trend():
    features = TrendFeatures(
        documents=1, growth=10, author_groups=3, active_periods=3, market_records=3
    )
    assert trend_code(features, 1.2, 2.0) == 1


def test_market_doubling_is_the_separate_fast_growth_branch():
    features = TrendFeatures(documents=3, growth=2, author_groups=1, market_records=2)
    assert trend_code(features, 1.2, 4.0) == 3
    assert trend_code(features, None, None) == 1


def test_trend_promotion_needs_three_recent_documents():
    for basis in ("documents_12m", "documents_6m"):
        few = TrendFeatures(documents=2, growth=2.0, growth_basis=basis, docs_recent=2)
        assert trend_code(few, 1.2, 3.0) == 1
        enough = few.model_copy(update={"documents": 3, "growth": 3.0, "docs_recent": 3})
        assert trend_code(enough, 1.2, 3.0) == 3
        unmeasured = enough.model_copy(update={"docs_recent": None})
        assert trend_code(unmeasured, 1.2, 3.0) == 1
    docs = [
        Document(doc_id=str(i), source="test", date=day, authors=[f"группа {i}"])
        for i, day in enumerate(["2022-06-01", "2024-01-10", "2024-02-01"])
    ]
    features = CorpusFeaturizer(today=date(2024, 3, 1)).compute(candidate(count=3), docs)
    assert (features.growth, features.docs_recent) == (2.0, 2)
    assert trend_code(features, 1.2, 3.0) == 1
    assert "docs_recent" not in features.model_dump()


def test_featurizer_deduplicates_ids_and_preserves_missing_measurements(documents):
    item = candidate().model_copy(
        update={"doc_ids": [doc.doc_id for doc in documents] * 2}
    )
    features = CorpusFeaturizer().compute(item, documents * 2)
    assert features.documents == 2
    assert features.scientific_works == 2
    assert features.author_groups == 2
    assert features.active_periods == 2
    assert features.top_group_share == 0.5
    assert features.growth == 1.0
    assert features.share_growth_pp is None
    single = item.model_copy(update={"doc_ids": [documents[0].doc_id]})
    assert CorpusFeaturizer().compute(single, documents).growth is None
    empty = CorpusFeaturizer().compute(item, [])
    assert empty.growth is empty.top_group_share is empty.share_growth_pp is None


def test_growth_calendar_boundary_leap_day_and_future_dates():
    dates = ["2022-02-28", "2023-02-28", "2023-03-01", "2024-02-29", "2024-03-01"]
    docs = [
        Document(doc_id=str(i), source="test", date=day) for i, day in enumerate(dates)
    ]
    features = CorpusFeaturizer(today=date(2024, 2, 29)).compute(
        candidate(count=5), docs
    )
    assert features.growth == 2.0  # два в последнем окне, один в предыдущем, будущее исключено
