"""Счётчики корпуса и явные правила методологии, без обученного суррогата.

Уверенность — явное произведение пяти множителей, объяснение — точное телескопическое
разложение в единицах вероятности, а не SHAP.
"""

from __future__ import annotations

import calendar
import math
import re
from collections import Counter
from collections.abc import Iterable, Mapping, Sequence
from copy import copy
from dataclasses import dataclass, replace
from datetime import date

from extractor.interfaces import Featurizer, RuleOutcome, Ruler
from extractor.schemas import (
    INSUFFICIENT_BASIS_RU,
    MARKETING_SOURCE_TYPES,
    WEAK_SIGNAL_STAGE_CODES,
    Candidate,
    CandidateClass,
    CandidateStage,
    Document,
    FeatureContribution,
    NoiseFlag,
    P14Platt,
    TrendFeatures,
    TrustLevel,
    display_feature_value,
    normalize_key,
    round6,
)

# ТУ только перед номером: указательное «ту» — не технические условия.
STANDARD_RE = re.compile(
    r"\b(?:ГОСТ(?:\s+Р)?|ISO|IEC|RFC|IEEE|ANSI|ETSI|3GPP|ОСТ|СТО|ASTM|DIN)\b"
    r"|\bТУ(?=\s*[-№]?\s*\d)",
    re.IGNORECASE,
)

# Только точные имена: «биология» — область, а «метод анализа в биологии» из-за этого слова
# не отбрасывается. Расширяется через RulesConfig.
NOT_TECHNOLOGIES = frozenset(
    {
        "биология",
        "физика",
        "химия",
        "медицина",
        "математика",
        "биотехнологии",
        "машиностроение",
        "кибербезопасность",
        "искусственный интеллект",
        "обнаружение мошенничества",
        "телефонное мошенничество",
        "защита информации",
        "повышение эффективности",
        "снижение затрат",
        "доля рынка",
        "объем рынка",
        "рыночная капитализация",
        "выручка",
        "обучение персонала",
        "проектное обучение",
        "проблемное обучение",
        "перевернутый класс",
        "biology",
        "physics",
        "chemistry",
        "medicine",
        "mathematics",
        "biotechnology",
        "cybersecurity",
        "artificial intelligence",
        "fraud detection",
        "market share",
        "market size",
        "revenue",
        "staff training",
        "project based learning",
        "problem based learning",
        "flipped classroom",
    }
)


#: Одна статья — уже наблюдение, а не догадка, поэтому множитель не ниже 0,75;
#: единица остаётся за независимым повторением в трёх документах.
DOCUMENT_CONFIRMATION: tuple[float, ...] = (0.0, 0.75, 0.9, 1.0)

#: Отношение по единицам документов — шум: 2 документа против 0 дают рост 2,
#: и без этого предела тренд стал бы 2. Действует для окон 12/12 и 6/6 месяцев.
MIN_TREND_RECENT_DOCUMENTS = 3

GROWTH_NAMES = {
    "legacy": "Рост за 12 месяцев",
    "documents_12m": "Рост за 12 месяцев",
    "documents_6m": "Рост за 6 месяцев к предыдущим 6",
}

UNMEASURED_NEUTRAL = "не измерено; нейтральный множитель 1"


def document_confirmation(count: int) -> float:
    return DOCUMENT_CONFIRMATION[min(max(count, 0), 3)]


def stage_range_probability(
    stage: CandidateStage, platt: P14Platt | None = None
) -> tuple[float, bool]:
    """Вероятность, что стадия лежит в диапазоне класса, и точна ли эта оценка.

    Для слабого сигнала диапазон — коды 1–4: колебание модели между соседними кодами внутри него
    сигнал не опровергает, поэтому их вероятности складываются. Для кода 5 берётся ``prob``
    стадии, где уже учтён множитель 0,6 одиночного фонового источника. Без распределения ``prob``
    кода 1–4 оценивает сумму снизу, и второй элемент — ``False``. ``platt`` монотонен, поэтому
    нижняя граница остаётся нижней; коды 0 и 5 он не меняет.
    """
    if not stage.confirmed:
        return 0.0, True
    if stage.code == 5:
        return stage.prob, True
    exact = stage.probs is not None
    total = min(1.0, sum(stage.probs[code] for code in WEAK_SIGNAL_STAGE_CODES)) if exact else stage.prob
    return (platt(total) if platt is not None else total), exact


def _name_key(text: str) -> str:
    return " ".join(re.findall(r"[^\W_]+", normalize_key(text).replace("ё", "е")))


@dataclass(frozen=True, slots=True)
class RulesConfig:
    trend_t1: float | None = None
    trend_t2: float | None = None
    threshold: float = 0.5
    only_marketing_penalty: float = 0.5
    not_a_technology_penalty: float = 1.0
    intent_penalty: float = 0.25
    not_technologies: frozenset[str] = NOT_TECHNOLOGIES

    def __post_init__(self) -> None:
        for value in (
            self.threshold,
            self.only_marketing_penalty,
            self.not_a_technology_penalty,
            self.intent_penalty,
        ):
            if not math.isfinite(value) or not 0 <= value <= 1:
                raise ValueError("threshold and penalties must be finite and in [0, 1]")
        for value in (self.trend_t1, self.trend_t2):
            if value is not None and (not math.isfinite(value) or value <= 0):
                raise ValueError("trend thresholds must be positive and finite")
        if (
            self.trend_t1 is not None
            and self.trend_t2 is not None
            and self.trend_t2 < self.trend_t1
        ):
            raise ValueError("trend_t2 must be >= trend_t1")


def detect_has_standard(texts: Iterable[str]) -> bool:
    return any(STANDARD_RE.search(text) is not None for text in texts)


def trend_code(features: TrendFeatures, t1: float | None, t2: float | None) -> int:
    """Без калиброванного порога и нескольких документов тренд не повышается.

    Рост — отношение числа документов в соседних окнах (12/12 или, внутри окна поиска, 6/6 месяцев);
    процентные пункты его не заменяют. Рыночное ускорение тоже должно быть измерено.
    """
    RulesConfig(trend_t1=t1, trend_t2=t2)
    growth = features.growth
    recent = features.docs_recent
    # С окнами тренд задаётся только отношением документов, с одними T1/T2 для обоих окон;
    # входы без окна сохраняют ограничения по группам и рыночным наблюдениям.
    if features.growth_basis in ("documents_12m", "documents_6m"):
        if growth is None or not math.isfinite(growth):
            return 1
        if recent is None or recent < MIN_TREND_RECENT_DOCUMENTS:
            return 1
        if t2 is not None and growth >= t2:
            return 3
        return 2 if t1 is not None and growth >= t1 else 1
    if growth is None or not math.isfinite(growth) or features.documents < 2:
        return 1
    if recent is not None and recent < MIN_TREND_RECENT_DOCUMENTS:
        return 1
    if t2 is not None and (
        (growth >= t2 and features.author_groups >= 3)
        or (features.market_records >= 2 and growth >= 2.0)
    ):
        return 3
    if (
        t1 is not None
        and growth >= t1
        and features.author_groups >= 2
        and features.active_periods >= 2
    ):
        return 2
    return 1


def compute_flags(
    candidate: Candidate,
    docs: Mapping[str, Document],
    config: RulesConfig | None = None,
) -> list[NoiseFlag]:
    config = config or RulesConfig()
    # Независимые флаги стадии сохраняются, выводимые здесь — пересчитываются.
    flags = set(candidate.flags) & {NoiseFlag.INTENT, NoiseFlag.NOT_A_TECHNOLOGY}
    if not candidate.stage.confirmed:
        flags.add(NoiseFlag.INSUFFICIENT_EVIDENCE)
    if candidate.stage.code == 5:
        flags.add(NoiseFlag.MASS_ADOPTION_MARKER)
    if candidate.has_standard:
        flags.add(NoiseFlag.HAS_STANDARD)
    if candidate.stage.single_source or candidate.trend.features.documents <= 1:
        flags.add(NoiseFlag.SINGLE_SOURCE)
    names = {_name_key(candidate.name), _name_key(candidate.canonical_ru)}
    if names & {_name_key(name) for name in config.not_technologies}:
        flags.add(NoiseFlag.NOT_A_TECHNOLOGY)
    ids = set(candidate.doc_ids)
    # Неизвестный жанр — не свидетельство того, что все источники рекламные.
    if (
        ids
        and ids <= docs.keys()
        and all(docs[doc_id].source_type in MARKETING_SOURCE_TYPES for doc_id in ids)
    ):
        flags.add(NoiseFlag.ONLY_MARKETING)
    return sorted(flags, key=lambda flag: flag.value)


def _months_before(day: date, months: int) -> date:
    year, month0 = divmod(day.year * 12 + day.month - 1 - months, 12)
    month = month0 + 1
    return date(year, month, min(day.day, calendar.monthrange(year, month)[1]))


class CorpusFeaturizer(Featurizer):
    """Уникальные документы, наблюдаемые группы авторов и календарные годы.

    Опорная дата — явная дата наблюдения или самая новая дата кандидата; документы без даты
    и из будущего в окна не входят. Без наблюдений в предыдущем окне growth=None, в том числе
    на границе 29 февраля.
    """

    version = "corpus-features@1"

    def __init__(self, *, today: date | None = None) -> None:
        self.today = today
        self._documents = None

    def with_observation_date(self, observation_date: date) -> CorpusFeaturizer:
        bound = copy(self)
        bound.today = observation_date
        return bound

    def with_documents(self, documents: Mapping[str, Document]) -> CorpusFeaturizer:
        bound = copy(self)
        bound._documents = documents
        return bound

    def compute(self, candidate: Candidate, docs: Sequence[Document]) -> TrendFeatures:
        # Конвейер уже проверил уникальность документов; индекс общий для кандидатов.
        by_id = self._documents
        if by_id is None:
            by_id = {}
            for doc in docs:
                if doc.doc_id in by_id and by_id[doc.doc_id] != doc:
                    raise ValueError(f"conflicting document metadata for {doc.doc_id}")
                by_id[doc.doc_id] = doc
        mine = [by_id[key] for key in sorted(set(candidate.doc_ids)) if key in by_id]
        candidate_dates = [doc.date for doc in mine if doc.date is not None]
        anchor = self.today or max(candidate_dates, default=None)
        dates = sorted(
            doc.date
            for doc in mine
            if doc.date is not None and (anchor is None or doc.date <= anchor)
        )
        growth = recent = None
        if anchor is not None:
            recent_start, previous_start = (
                _months_before(anchor, 12),
                _months_before(anchor, 24),
            )
            recent = sum(recent_start < day <= anchor for day in dates)
            previous = sum(previous_start < day <= recent_start for day in dates)
            if previous:
                growth = round6(recent / previous)
        groups = Counter(doc.group_key for doc in mine)
        return TrendFeatures(
            documents=len(mine),
            scientific_works=sum(doc.is_scientific for doc in mine),
            market_records=sum(doc.is_market_record for doc in mine),
            author_groups=len(groups),
            active_periods=len({day.year for day in dates}),
            growth=growth,
            docs_recent=recent,
            observation_date=anchor,
            top_group_share=round6(max(groups.values()) / len(mine)) if mine else None,
            share_growth_pp=None,
            first_observed=dates[0].strftime("%Y-%m") if dates else None,
        )


class MethodologyRuler(Ruler):
    """Явная формула уверенности, а не обученный классификатор слабых сигналов.

    Неизмеренное доверие остаётся None, его множитель нейтрален (1): /predict не получает доверие
    из счётчика документов или URL. Вероятность головы кандидата без теггера с головой
    (заглушки, клиент /predict без этого поля) тоже не измерена и даёт множитель 1.
    """

    model_name = "weak-signal-rules-v2"
    basis = (
        "Правила методологии v2: вероятность стадии в диапазоне класса × "
        "вероятность головы кандидата × подтверждение документами "
        "(0 / 0,75 / 0,9 / 1 для 0 / 1 / 2 / 3 и более документов) × "
        "доля доверенных источников × (1 − штраф шума). "
        "Диапазон слабого сигнала — коды 1–4: складываются вероятности этих кодов "
        "модели стадии для предложения-доказательства; для зрелой технологии "
        "берётся вероятность кода 5. Без распределения стадии используется "
        "вероятность выбранного кода — нижняя граница суммы. Для массовости "
        "из одного фонового источника вероятность кода 5 умножается на 0,6. "
        "Вероятность головы кандидата — максимум по упоминаниям средней "
        "по токенам вероятности класса candidate теггера. Неизмеренные голова "
        "кандидата и доля доверенных источников имеют нейтральный множитель 1 "
        "и нулевой вклад; это не подтверждение. Неизмеренный рост "
        "не влияет на оценку. Вклады — последовательные изменения "
        "в долях единицы от нулевого уровня: стадия, голова, документы, доверие, "
        "шум; это формула, не калиброванная вероятность и не TreeSHAP"
    )

    def __init__(
        self,
        threshold: float | None = None,
        *,
        config: RulesConfig | None = None,
        documents: Mapping[str, Document] | None = None,
    ) -> None:
        self.config = config or RulesConfig()
        if threshold is not None:
            self.config = replace(self.config, threshold=threshold)
        self.threshold = self.config.threshold
        if not math.isfinite(self.threshold) or not 0 <= self.threshold <= 1:
            raise ValueError("threshold must be in [0, 1]")
        self.documents = dict(documents or {})
        #: Калибровка суммы p(1..4) активной модели стадии; ``None`` — тождественно.
        self.p14_platt: P14Platt | None = None

    def with_p14_platt(self, platt: P14Platt | None) -> MethodologyRuler:
        bound = copy(self)
        bound.p14_platt = platt
        return bound

    def with_threshold(self, threshold: float) -> MethodologyRuler:
        bound = copy(self)
        bound.config = replace(self.config, threshold=threshold)
        bound.threshold = bound.config.threshold
        return bound

    def with_documents(self, docs: Mapping[str, Document]) -> MethodologyRuler:
        """Контекст вызова привязывается к копии: правила общие для HTTP-запросов."""
        bound = copy(self)
        bound.documents = dict(docs)
        return bound

    @staticmethod
    def classify(candidate: Candidate) -> CandidateClass:
        flags = set(candidate.flags)
        if flags & {NoiseFlag.NOT_A_TECHNOLOGY, NoiseFlag.ONLY_MARKETING}:
            return CandidateClass.NOISE
        if NoiseFlag.INSUFFICIENT_EVIDENCE in flags or not candidate.stage.confirmed:
            return CandidateClass.INSUFFICIENT_DATA
        if candidate.stage.code == 5:
            return CandidateClass.MATURE
        return CandidateClass.WEAK_SIGNAL

    def _trust_share(self, candidate: Candidate) -> float | None:
        if candidate.source_trust is not None:
            return candidate.source_trust.share
        ids = set(candidate.doc_ids)
        known = [self.documents[key] for key in sorted(ids) if key in self.documents]
        if not known or all(doc.trust_level is TrustLevel.UNKNOWN for doc in known):
            return None
        trusted = sum(
            doc.trust_level in (TrustLevel.HIGH, TrustLevel.MEDIUM)
            and doc.source_type not in MARKETING_SOURCE_TYPES
            for doc in known
        )
        return trusted / len(ids)

    def _contributions(self, candidate: Candidate) -> list[FeatureContribution]:
        probability, exact = stage_range_probability(candidate.stage, self.p14_platt)
        head = candidate.head_prob
        count = candidate.trend.features.documents
        confirmation = document_confirmation(count)
        share = self._trust_share(candidate)
        trust = 1.0 if share is None else share
        flags = set(candidate.flags)
        penalty = min(
            1.0,
            sum(
                value
                for flag, value in (
                    (NoiseFlag.ONLY_MARKETING, self.config.only_marketing_penalty),
                    (NoiseFlag.NOT_A_TECHNOLOGY, self.config.not_a_technology_penalty),
                    (NoiseFlag.INTENT, self.config.intent_penalty),
                    (NoiseFlag.INSUFFICIENT_EVIDENCE, 1.0),
                )
                if flag in flags
            ),
        )
        after_head = probability * (1.0 if head is None else head)
        after_docs = after_head * confirmation
        after_trust = after_docs * trust
        after_noise = after_trust * (1 - penalty)
        # Разности округлённых накопленных значений телескопируются точно на точности ответа.
        values = [
            round6(value)
            for value in (probability, after_head, after_docs, after_trust, after_noise)
        ]
        stage_value = round6(probability)
        return [
            FeatureContribution(
                name="Вероятность стадии в диапазоне класса",
                value=stage_value
                if exact
                else (
                    f"{display_feature_value(stage_value)}; распределение стадии "
                    "неизвестно, нижняя граница"
                ),
                raw_value=stage_value,
                contribution=values[0],
            ),
            FeatureContribution(
                name="Вероятность головы кандидата",
                value=UNMEASURED_NEUTRAL if head is None else round6(head),
                raw_value=None if head is None else round6(head),
                contribution=values[1] - values[0],
            ),
            FeatureContribution(
                name="Подтверждающие документы",
                value=count,
                contribution=values[2] - values[1],
            ),
            FeatureContribution(
                name="Доля доверенных источников",
                value=UNMEASURED_NEUTRAL if share is None else round6(share),
                raw_value=None if share is None else round6(share),
                contribution=values[3] - values[2],
            ),
            FeatureContribution(
                name="Штраф шума",
                value=round6(penalty),
                contribution=values[4] - values[3],
            ),
            FeatureContribution(
                name=GROWTH_NAMES.get(candidate.trend.features.growth_basis, GROWTH_NAMES["legacy"]),
                value=candidate.trend.features.growth,
                contribution=0.0,
            ),
        ]

    def score(self, candidate: Candidate) -> RuleOutcome:
        # Флаги вызывающего сохраняются; словарь добавляется, даже если правила подключены
        # прямо в Components конвейера.
        flags = set(candidate.flags) | set(
            compute_flags(candidate, self.documents, self.config)
        )
        candidate = candidate.model_copy(update={"flags": sorted(flags, key=str)})
        items = self._contributions(candidate)
        value = round6(sum(item.contribution for item in items))
        # Пять множителей и нулевой маркер роста: при равных вкладах отбор идёт
        # в порядке формулы, чтобы выпадал маркер роста, а не множитель формулы.
        kept = sorted(range(len(items)), key=lambda i: (-abs(items[i].contribution), i))[:5]
        return RuleOutcome(
            score=candidate.stage.code + candidate.trend.code,
            label=self.classify(candidate),
            value=min(1.0, max(0.0, value)),
            explanation=sorted(
                (items[i] for i in kept),
                key=lambda item: (-abs(item.contribution), item.name),
            ),
        )

    def basis_for(self, label: CandidateClass) -> str:
        basis = self.basis
        if self.p14_platt is not None:
            a, b = (f"{value:g}".replace(".", ",").replace("-", "−") for value in (self.p14_platt.a, self.p14_platt.b))
            basis += (
                f"; сумма вероятностей кодов 1–4 откалибрована по Платту модели стадии: "
                f"sigmoid(a·logit(p) + b), a = {a}, b = {b}"
            )
        if label is CandidateClass.INSUFFICIENT_DATA:
            return f"{basis}; {INSUFFICIENT_BASIS_RU}"
        if label is CandidateClass.MATURE:
            return f"{basis}; исключение: зрелая технология, стадия 5"
        if label is CandidateClass.NOISE:
            return f"{basis}; исключение по флагам шума"
        return basis
