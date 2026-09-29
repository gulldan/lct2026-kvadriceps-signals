"""Pydantic-модели всего, что пересекает границы модулей.

Запрос и ответ /predict повторяют ``ml-services/extractor/ML_CLASSIFIER_HANDOFF.md`` поле в поле:
помеченное «контракт» нельзя переименовывать, менять по смыслу или давать ему умолчание,
которого нет в контракте. Смещения считаются по ``Document.text`` (``title + "\\n\\n" + abstract``),
``end`` не включается; исходящие float округляются :func:`round6`, чтобы два прогона одной
версии давали побайтно одинаковый JSON.
"""

from __future__ import annotations

import hashlib
import math
import re
import unicodedata
from datetime import date as DateType
from enum import StrEnum
from typing import Any, Literal, Self

from pydantic import (
    BaseModel,
    ConfigDict,
    Field,
    NonNegativeInt,
    StrictBool,
    StrictFloat,
    StrictInt,
    StrictStr,
    field_serializer,
    field_validator,
    model_validator,
)

#: Контракт: сверяется побайтно.
FEATURE_SCHEMA = "weak-signal-features-v1"

#: Пределы тела запроса и ответа, срок /predict и длина аннотации заданы контрактом.
MAX_REQUEST_BYTES = 4 * 1024 * 1024
MAX_RESPONSE_BYTES = 1 * 1024 * 1024
PREDICT_TIMEOUT_SECONDS = 30.0
MAX_SOURCE_ABSTRACT_CHARS = 2000

#: Формат RADAR_CLASSIFIER_VERSION.
SERVICE_VERSION_RE = re.compile(r"^weak-signal-\d{4}-\d{2}-\d{2}\.\d+$")

FLOAT_DECIMALS = 6

_CYRILLIC_RE = re.compile(r"[Ѐ-ӿ]")


def round6(value: float) -> float:
    """Округление ради детерминизма ответа, а не красоты."""
    return round(float(value), FLOAT_DECIMALS)


def sha256_text(text: str) -> str:
    return hashlib.sha256(text.encode("utf-8")).hexdigest()


def normalize_key(value: str) -> str:
    folded = unicodedata.normalize("NFKC", value).casefold()
    return " ".join(folded.split())


def is_russian(text: str) -> bool:
    """Контракт требует ``basis`` на русском."""
    return bool(_CYRILLIC_RE.search(text))


#: Коды стадии слабого сигнала; зрелость (код 5) и отсутствие стадии (код 0) вне его.
WEAK_SIGNAL_STAGE_CODES: tuple[int, ...] = (1, 2, 3, 4)
#: Шесть значений округлены до шести знаков, и клиент может сериализовать их заново:
#: точное равенство единице отклоняло бы честные ответы модели.
STAGE_PROBS_SUM_TOLERANCE = 1e-3


def check_stage_probs(value: list[float] | None) -> list[float] | None:
    if value is None:
        return None
    if len(value) != 6:
        raise ValueError(
            "распределение стадии должно содержать шесть вероятностей (коды 0..5)"
        )
    probs = [float(item) for item in value]
    if not all(math.isfinite(item) and 0.0 <= item <= 1.0 for item in probs):
        raise ValueError("вероятности стадии должны быть конечными и лежать в [0, 1]")
    if abs(sum(probs) - 1.0) > STAGE_PROBS_SUM_TOLERANCE:
        raise ValueError("сумма вероятностей стадии должна быть равна 1")
    # Округление делает ответ и производную уверенность побайтно повторяемыми.
    return [round6(item) for item in probs]


#: Предел logit при калибровке: сумма 0 или 1 после округления не даёт бесконечности.
_P14_EPSILON = 1e-6


class P14Platt:
    """Platt-калибровка суммы p(1..4): ``sigmoid(a·logit(p) + b)``.

    LR стадии обучен с балансировкой классов и завышает сумму p(1..4), температура этого
    не исправляет. Без параметров в описании модели стадии преобразование тождественное.
    """

    __slots__ = ("a", "b")

    def __init__(self, a: float, b: float) -> None:
        if not (math.isfinite(a) and math.isfinite(b)) or a <= 0:
            raise ValueError("p14_platt: a и b должны быть конечными, a > 0")
        self.a, self.b = float(a), float(b)

    @classmethod
    def parse(cls, value: Any) -> P14Platt | None:
        if value is None:
            return None
        if not isinstance(value, dict) or set(value) != {"a", "b"} or any(
            isinstance(value[key], bool) or not isinstance(value[key], int | float) for key in ("a", "b")
        ):
            raise ValueError("p14_platt должен быть объектом {a, b} с числами")
        return cls(value["a"], value["b"])

    def __call__(self, probability: float) -> float:
        p = min(1.0 - _P14_EPSILON, max(_P14_EPSILON, probability))
        z = self.a * math.log(p / (1.0 - p)) + self.b
        return 1.0 / (1.0 + math.exp(-z))

    def __eq__(self, other: object) -> bool:
        return isinstance(other, P14Platt) and (self.a, self.b) == (other.a, other.b)

    def __hash__(self) -> int:
        return hash((self.a, self.b))

    def __repr__(self) -> str:
        return f"P14Platt(a={self.a}, b={self.b})"


class Lang(StrEnum):
    RU = "ru"
    EN = "en"


class SourceType(StrEnum):
    JOURNAL_ARTICLE = "journal_article"
    PREPRINT = "preprint"
    CONFERENCE = "conference"
    DISSERTATION = "dissertation"
    PATENT = "patent"
    NEWS = "news"
    PRESS_RELEASE = "press_release"
    BLOG = "blog"
    REGISTRY = "registry"
    REPOSITORY = "repository"
    STANDARD = "standard"
    OTHER = "other"


SCIENTIFIC_SOURCE_TYPES: frozenset[SourceType] = frozenset(
    {
        SourceType.JOURNAL_ARTICLE,
        SourceType.PREPRINT,
        SourceType.CONFERENCE,
        SourceType.DISSERTATION,
    }
)

MARKET_SOURCE_TYPES: frozenset[SourceType] = frozenset(
    {
        SourceType.PATENT,
        SourceType.NEWS,
        SourceType.PRESS_RELEASE,
        SourceType.REGISTRY,
        SourceType.REPOSITORY,
    }
)

#: Такие источники сами по себе кандидата не подтверждают (флаг ``only_marketing``).
MARKETING_SOURCE_TYPES: frozenset[SourceType] = frozenset(
    {SourceType.PRESS_RELEASE, SourceType.BLOG}
)


class PublisherType(StrEnum):
    PUBLISHER = "publisher"
    PREPRINT_SERVER = "preprint_server"
    VENDOR = "vendor"
    MEDIA = "media"
    GOVERNMENT = "government"
    COMMUNITY = "community"
    UNKNOWN = "unknown"


class TrustLevel(StrEnum):
    HIGH = "high"
    MEDIUM = "medium"
    LOW = "low"
    UNKNOWN = "unknown"


class SpanType(StrEnum):
    """Размечаются только технологии; материалы, стандарты и штаммы — нет."""

    TECH = "TECH"


class SpanSubtype(StrEnum):
    METHOD = "method"
    DEVICE = "device"
    PROCESS = "process"
    SOFTWARE = "software"
    PROTOCOL = "protocol"
    PRODUCT = "product"
    # Только старые записи на карантине: оставлены, чтобы такие файлы загружались без правки.
    MATERIAL = "material"
    ORGANISM_TOOL = "organism_tool"
    STANDARD = "standard"
    DATASET = "dataset"
    OTHER = "other"


CURRENT_SUBTYPES: frozenset[SpanSubtype] = frozenset(
    {
        SpanSubtype.METHOD,
        SpanSubtype.DEVICE,
        SpanSubtype.PROCESS,
        SpanSubtype.SOFTWARE,
        SpanSubtype.PROTOCOL,
        SpanSubtype.PRODUCT,
    }
)


class SpanRole(StrEnum):
    CONTRIBUTION = "contribution"
    APPLICATION = "application"
    BACKGROUND = "background"


class Coverage(StrEnum):
    FULL = "full"
    CONTRIBUTION_ONLY = "contribution_only"


#: Индекс равен коду стадии.
STAGE_LABELS_RU: tuple[str, ...] = (
    "не подтверждена",
    "концепция / исследование",
    "прототип / PoC",
    "пилот",
    "раннее внедрение",
    "массовое внедрение",
)

#: Индекс равен коду тренда минус один.
TREND_LABELS_RU: tuple[str, ...] = ("стабильный", "растёт", "растёт быстро")


class NoiseFlag(StrEnum):
    """``has_standard`` и ``mass_adoption_marker`` — признаки зрелости, а не шума."""

    ONLY_MARKETING = "only_marketing"
    NOT_A_TECHNOLOGY = "not_a_technology"
    INSUFFICIENT_EVIDENCE = "insufficient_evidence"
    HAS_STANDARD = "has_standard"
    MASS_ADOPTION_MARKER = "mass_adoption_marker"
    INTENT = "intent"
    SINGLE_SOURCE = "single_source"


class CandidateClass(StrEnum):
    """``INSUFFICIENT_DATA`` нет среди меток контракта: наружу он уходит как ``noise``
    с basis «нет подтверждённой стадии».
    """

    WEAK_SIGNAL = "weak_signal"
    MATURE = "mature"
    NOISE = "noise"
    INSUFFICIENT_DATA = "insufficient_data"


CANDIDATE_CLASS_RU: dict[CandidateClass, str] = {
    CandidateClass.WEAK_SIGNAL: "слабый сигнал",
    CandidateClass.MATURE: "зрелая",
    CandidateClass.NOISE: "шум",
    CandidateClass.INSUFFICIENT_DATA: "недостаточно данных",
}


class PredictLabel(StrEnum):
    WEAK_SIGNAL = "weak_signal"
    MATURE = "mature"
    NOISE = "noise"
    FAILED = "failed"


CLASS_TO_LABEL: dict[CandidateClass, PredictLabel] = {
    CandidateClass.WEAK_SIGNAL: PredictLabel.WEAK_SIGNAL,
    CandidateClass.MATURE: PredictLabel.MATURE,
    CandidateClass.NOISE: PredictLabel.NOISE,
    CandidateClass.INSUFFICIENT_DATA: PredictLabel.NOISE,
}

INSUFFICIENT_BASIS_RU = "нет подтверждённой стадии"


class ErrorCode(StrEnum):
    INVALID_FEATURE_SCHEMA = "invalid_feature_schema"
    VERSION_MISMATCH = "version_mismatch"
    INVALID_REQUEST = "invalid_request"
    DUPLICATE_ID = "duplicate_id"
    PAYLOAD_TOO_LARGE = "payload_too_large"
    RESPONSE_TOO_LARGE = "response_too_large"
    TIMEOUT = "timeout"
    CLASSIFIER_UNAVAILABLE = "classifier_unavailable"
    STORAGE_UNAVAILABLE = "storage_unavailable"
    CACHE_CORRUPTED = "cache_corrupted"
    INTERNAL_ERROR = "internal_error"


class Model(BaseModel):
    model_config = ConfigDict(
        extra="ignore",
        use_enum_values=False,
        validate_assignment=False,
        allow_inf_nan=False,
    )


class StrictModel(Model):
    """Для исходящих объектов лишнее поле — ошибка, а не совместимость вперёд."""

    model_config = ConfigDict(extra="forbid")


class Document(Model):
    doc_id: str = Field(min_length=1)
    source: str = Field(
        min_length=1, description="Collector name: arxiv, openalex, cyberleninka, ..."
    )
    url: str | None = None
    title: str = ""
    abstract: str = ""
    text: str = ""
    text_sha256: str = Field(default="", pattern=r"^([0-9a-f]{64})?$")
    lang: Lang = Lang.RU
    source_type: SourceType = SourceType.OTHER
    publisher_type: PublisherType = PublisherType.UNKNOWN
    trust_level: TrustLevel = TrustLevel.UNKNOWN
    date: DateType | None = None
    authors: list[str] = Field(default_factory=list)
    orgs: list[str] = Field(default_factory=list)
    areas: list[str] = Field(default_factory=list)
    etalon_blacklist: bool = False

    @model_validator(mode="after")
    def _compose_text_and_hash(self) -> Self:
        if not self.text:
            object.__setattr__(self, "text", f"{self.title}\n\n{self.abstract}")
        digest = sha256_text(self.text)
        if not self.text_sha256:
            object.__setattr__(self, "text_sha256", digest)
        elif self.text_sha256 != digest:
            raise ValueError(
                f"text_sha256 does not match text for {self.doc_id}: {self.text_sha256} != {digest}"
            )
        return self

    @property
    def title_end(self) -> int:
        cut = self.text.find("\n\n")
        return len(self.text) if cut < 0 else cut

    @property
    def is_scientific(self) -> bool:
        return self.source_type in SCIENTIFIC_SOURCE_TYPES

    @property
    def is_market_record(self) -> bool:
        return self.source_type in MARKET_SOURCE_TYPES

    @property
    def group_key(self) -> str:
        if self.orgs:
            return normalize_key(self.orgs[0])
        if self.authors:
            return normalize_key(self.authors[0])
        return normalize_key(self.source)


class Span(Model):
    start: NonNegativeInt
    end: NonNegativeInt
    text: str = Field(min_length=1)
    type: SpanType = SpanType.TECH
    subtype: SpanSubtype
    role: SpanRole
    is_candidate: bool = False
    in_title: bool = False
    canonical_ru: str = Field(min_length=1)
    confidence: float = Field(ge=0.0, le=1.0)
    #: Средняя по токенам вероятность отдельной головы кандидата теггера;
    #: ``None`` — головы нет (заглушка, чекпойнт без головы).
    head_prob: float | None = Field(default=None, ge=0.0, le=1.0)
    doc_id: str | None = None
    sentence: str | None = None
    note: str | None = None
    quarantined: bool = False

    @model_validator(mode="after")
    def _check_offsets(self) -> Self:
        if self.end <= self.start:
            raise ValueError(
                f"span end must be greater than start: {self.start}..{self.end}"
            )
        if self.end - self.start != len(self.text):
            raise ValueError(
                f"span length {self.end - self.start} does not match text {len(self.text)!r}"
            )
        return self

    def check_against(self, document: Document) -> None:
        actual = document.text[self.start : self.end]
        if actual != self.text:
            raise ValueError(
                f"{document.doc_id}: text[{self.start}:{self.end}] is {actual!r}, span says {self.text!r}"
            )

    @property
    def sort_key(self) -> tuple[str, int, int, str]:
        return (self.doc_id or "", self.start, self.end, self.canonical_ru)


class QuarantineItem(Model):
    text: str
    start: int | None = None
    end: int | None = None
    question: str


class NotTaggedItem(Model):
    text: str
    reason: str


class Provenance(Model):
    instruction_version: str = ""
    annotator_model: str = ""
    arbiter_model: str = ""
    date: DateType | None = None
    offset_base: Literal["text", "window"] = "text"


class SpanAnnotation(Model):
    doc_id: str
    text_sha256: str = Field(pattern=r"^([0-9a-f]{64})?$", default="")
    lang: Lang = Lang.RU
    coverage: Coverage = Coverage.FULL
    spans: list[Span] = Field(default_factory=list)
    contribution_spans: list[int] = Field(default_factory=list)
    quarantine: list[QuarantineItem] = Field(default_factory=list)
    not_tagged: list[NotTaggedItem] = Field(default_factory=list)
    provenance: Provenance = Field(default_factory=Provenance)

    @model_validator(mode="after")
    def _check_indices(self) -> Self:
        for idx in self.contribution_spans:
            if not 0 <= idx < len(self.spans):
                raise ValueError(f"contribution_spans index {idx} out of range")
        return self


class SentenceInput(Model):
    doc_id: str
    sent_start: NonNegativeInt
    sent_end: NonNegativeInt
    sentence: str = Field(min_length=1)
    context_before: str = ""
    context_after: str = ""
    candidate_text: str = ""
    role: SpanRole = SpanRole.BACKGROUND
    source_type: SourceType = SourceType.OTHER
    date: DateType | None = None

    @property
    def sort_key(self) -> tuple[str, int, int]:
        return (self.doc_id, self.sent_start, self.sent_end)


class SentenceStage(Model):
    sent_start: NonNegativeInt
    sent_end: NonNegativeInt
    sentence: str = Field(min_length=1)
    code: int = Field(ge=0, le=5)
    quotes: list[str] = Field(default_factory=list)
    prob: float = Field(ge=0.0, le=1.0)
    #: Распределение модели по кодам 0..5; ``None`` у заглушек без модели.
    probs: list[float] | None = None
    doc_id: str | None = None
    spread: str | None = None
    single_source: bool = False
    noise_flags: list[NoiseFlag] = Field(default_factory=list)
    confidence: float | None = Field(default=None, ge=0.0, le=1.0)

    @field_validator("probs")
    @classmethod
    def _probs(cls, value: list[float] | None) -> list[float] | None:
        return check_stage_probs(value)

    @model_validator(mode="after")
    def _check(self) -> Self:
        if self.sent_end <= self.sent_start:
            raise ValueError("sent_end must be greater than sent_start")
        for quote in self.quotes:
            if quote not in self.sentence:
                raise ValueError(f"quote is not a verbatim substring: {quote!r}")
        if self.code > 0 and not self.quotes:
            raise ValueError("a stage above 0 must be backed by at least one quote")
        # prob по контракту StageScorer — вероятность выбранного кода; расхождение
        # означает смешение вердикта и распределения из разных прогонов.
        if self.probs is not None and abs(self.probs[self.code] - self.prob) > 1e-6:
            raise ValueError("распределение стадии не согласовано с вероятностью кода")
        return self

    @property
    def label_ru(self) -> str:
        return STAGE_LABELS_RU[self.code]


class Evidence(Model):
    doc_id: str
    start: NonNegativeInt
    end: NonNegativeInt
    sentence: str
    role: SpanRole
    in_title: bool = False

    def check_against(self, document: Document) -> None:
        if (
            document.doc_id != self.doc_id
            or document.text[self.start : self.end] != self.sentence
        ):
            raise ValueError("координаты доказательства не воспроизводят предложение")

    @property
    def sort_key(self) -> tuple[str, int, int]:
        return (self.doc_id, self.start, self.end)


class CandidateStage(Model):
    status: Literal["computed", "failed"] = "computed"
    code: int = Field(default=0, ge=0, le=5)
    quote: str = ""
    doc_id: str | None = None
    prob: float = Field(default=0.0, ge=0.0, le=1.0)
    single_source: bool = False
    #: Распределение по кодам 0..5 для предложения, давшего стадию; ``None`` — не вычислено
    #: или неизвестно. ``prob`` может отличаться от ``probs[code]`` множителем 0,6 одиночной
    #: фоновой массовости.
    probs: list[float] | None = None

    @field_validator("probs")
    @classmethod
    def _probs(cls, value: list[float] | None) -> list[float] | None:
        return check_stage_probs(value)

    @model_validator(mode="after")
    def _quote_required(self) -> Self:
        if self.code > 0 and not self.quote:
            raise ValueError(
                "стадия без цитаты не показывается: code > 0 requires a quote"
            )
        return self

    @property
    def label_ru(self) -> str:
        return STAGE_LABELS_RU[self.code]

    @property
    def confirmed(self) -> bool:
        return self.code > 0 and bool(self.quote)


class TrendFeatures(Model):
    documents: NonNegativeInt = 0
    scientific_works: NonNegativeInt = 0
    market_records: NonNegativeInt = 0
    author_groups: NonNegativeInt = 0
    active_periods: NonNegativeInt = 0
    #: Отношение числа документов в соседних окнах 12/12 месяцев (для ``documents_6m`` — 6/6).
    #: ``None`` — не измерено: контракт запрещает подставлять 0.
    growth: float | None = None
    growth_basis: Literal["legacy", "documents_12m", "documents_6m"] = Field(
        default="legacy", exclude=True
    )
    #: Число документов в последнем окне ``growth``. Внутреннее поле: тренд не повышается
    #: по единицам документов.
    docs_recent: NonNegativeInt | None = Field(default=None, exclude=True)
    top_group_share: float | None = Field(default=None, ge=0.0, le=1.0)
    share_growth_pp: float | None = None
    first_observed: str | None = Field(default=None, pattern=r"^\d{4}(-\d{2})?$")

    observation_date: DateType | None = None

    def to_predict_features(self) -> PredictFeatures:
        return PredictFeatures(
            documents=self.documents,
            scientific_works=self.scientific_works,
            market_records=self.market_records,
            active_periods=self.active_periods,
            author_groups=self.author_groups,
            top_group_share=self.top_group_share,
            share_growth_pp=self.share_growth_pp,
            first_observed=self.first_observed,
        )


class CandidateTrend(Model):
    code: int = Field(ge=1, le=3)
    features: TrendFeatures = Field(default_factory=TrendFeatures)

    @property
    def label_ru(self) -> str:
        return TREND_LABELS_RU[self.code - 1]


FeatureValue = StrictBool | StrictInt | StrictFloat | StrictStr | None


def display_feature_value(value: FeatureValue) -> str:
    """Go-потребитель требует непустую строку, в том числе для отсутствующих значений."""
    if value is None or (isinstance(value, str) and not value.strip()):
        return "не измерено"
    if isinstance(value, bool):
        return "да" if value else "нет"
    if isinstance(value, float):
        if not math.isfinite(value):
            raise ValueError("числа в ответе должны быть конечными")
        return f"{round6(value):.6f}".rstrip("0").rstrip(".") or "0"
    return str(value)


class FeatureContribution(Model):
    """Наружу значение уходит текстом; ``raw_value`` хранит исходное измерение."""

    model_config = ConfigDict(revalidate_instances="always", validate_assignment=True)

    name: str = Field(min_length=1)
    value: str = Field(default="не измерено", min_length=1)
    raw_value: FeatureValue = Field(default=None, exclude=True)
    contribution: float

    @model_validator(mode="before")
    @classmethod
    def _display_value(cls, data):
        if isinstance(data, dict):
            data = dict(data)
            raw = data.get("raw_value", data.get("value"))
            data["raw_value"] = raw
            data["value"] = display_feature_value(data.get("value", raw))
        return data

    @field_validator("name")
    @classmethod
    def _russian_name(cls, value: str) -> str:
        if not is_russian(value):
            raise ValueError("названия признаков должны быть по-русски")
        return value

    @field_validator("contribution")
    @classmethod
    def _round(cls, value: float) -> float:
        if not math.isfinite(value):
            raise ValueError("числа в ответе должны быть конечными")
        return round6(value)

    @field_serializer("value")
    def _wire_value(self, value: str) -> str:
        return display_feature_value(value)

    @field_serializer("contribution")
    def _wire_contribution(self, value: float) -> float:
        if not math.isfinite(value):
            raise ValueError("числа в ответе должны быть конечными")
        return round6(value)


class SourceTrust(Model):
    """Доверие по всем документам; отсутствие оценки отличается от нулевой доли."""

    total: NonNegativeInt
    trusted: NonNegativeInt | None = None

    @model_validator(mode="after")
    def _counts(self) -> Self:
        if self.trusted is not None and self.trusted > self.total:
            raise ValueError("число доверенных документов превышает общее число")
        return self

    @property
    def share(self) -> float | None:
        return self.trusted / self.total if self.total and self.trusted is not None else None


class Candidate(Model):
    name: str = Field(min_length=1)
    canonical_ru: str = Field(min_length=1)
    doc_ids: list[str] = Field(default_factory=list)
    record_ids: list[str] = Field(default_factory=list)
    surface_forms: list[str] = Field(default_factory=list, exclude=True)
    evidence: list[Evidence] = Field(default_factory=list)
    stage: CandidateStage = Field(default_factory=CandidateStage)
    trend: CandidateTrend = Field(default_factory=lambda: CandidateTrend(code=1))
    flags: list[NoiseFlag] = Field(default_factory=list)
    has_standard: bool = False
    source_trust: SourceTrust | None = Field(default=None, exclude=True)
    #: Вероятность головы кандидата: максимум ``Span.head_prob`` по упоминаниям,
    #: из которых собран кандидат. ``None`` — не измерено (нейтральный множитель).
    head_prob: float | None = Field(default=None, ge=0.0, le=1.0)
    #: Код стадии плюс код тренда, на практике 2–8; кандидат со стадией 0 даёт 1
    #: и исключается как «недостаточно данных».
    score: int = Field(default=1, ge=0, le=8)
    label: CandidateClass = CandidateClass.INSUFFICIENT_DATA
    value: float = Field(default=0.0, ge=0.0, le=1.0)
    explanation: list[FeatureContribution] = Field(default_factory=list)
    #: Стабильный идентификатор для /predict и интерфейса; по умолчанию — канонический ключ.
    id: str = ""

    @model_validator(mode="after")
    def _defaults(self) -> Self:
        if not self.id:
            object.__setattr__(self, "id", candidate_id(self.canonical_ru))
        object.__setattr__(self, "value", round6(self.value))
        return self

    @property
    def label_ru(self) -> str:
        return CANDIDATE_CLASS_RU[self.label]

    @property
    def sort_key(self) -> tuple[float, int, str, str]:
        return (-self.value, -self.score, normalize_key(self.canonical_ru), self.id)


def candidate_id(canonical_ru: str) -> str:
    return (
        "cand-"
        + hashlib.sha256(normalize_key(canonical_ru).encode("utf-8")).hexdigest()[:16]
    )


def half_year_growth(recent: int, previous: int) -> float | None:
    """Два нулевых окна рост не измеряют."""
    return round6(recent / max(1, previous)) if recent or previous else None


class PredictFeatures(Model):
    """``null`` — «не измерено» и остаётся ``null``: контракт запрещает подставлять ноль."""

    model_config = ConfigDict(extra="forbid")

    documents: NonNegativeInt
    scientific_works: NonNegativeInt
    market_records: NonNegativeInt
    active_periods: NonNegativeInt
    author_groups: NonNegativeInt
    top_group_share: float | None = Field(default=None, ge=0.0, le=1.0)
    share_growth_pp: float | None = None
    first_observed: str | None = Field(default=None, pattern=r"^\d{4}(-\d{2})?$")

    docs_recent_12m: NonNegativeInt | None = None
    docs_previous_12m: NonNegativeInt | None = None
    growth_ratio: float | None = Field(default=None, ge=0.0)
    #: Без поля — окна 12/12 месяцев. ``6m/6m`` — два последних полных полугодия окна поиска;
    #: отношение тогда считается по ``docs_recent`` и ``docs_previous``.
    growth_window: Literal["12m/12m", "6m/6m"] | None = None
    docs_recent: NonNegativeInt | None = None
    docs_previous: NonNegativeInt | None = None
    observation_date: DateType | None = None
    stage_code: int | None = Field(default=None, ge=0, le=5)
    stage_quote: str | None = None
    stage_model_version: str | None = None
    stage_prob: float | None = Field(default=None, ge=0.0, le=1.0)
    stage_doc_id: str | None = None
    stage_single_source: bool = False
    #: ``/extract candidates[].stage.probs``: распределение по кодам 0..5.
    #: Используется только вместе с переиспользуемой стадией и ``stage_prob``.
    stage_probs: list[float] | None = None
    #: ``/extract candidates[].head_prob``; отсутствие — нейтральный множитель 1.
    head_prob: float | None = Field(default=None, ge=0.0, le=1.0)
    only_marketing: bool | None = None
    not_a_technology: bool | None = None
    insufficient_evidence: bool | None = None
    has_standard: bool | None = None
    single_source: bool | None = None
    source_trust: SourceTrust | None = None

    @field_validator("first_observed", mode="before")
    @classmethod
    def _empty_first_observed(cls, value):
        return None if isinstance(value, str) and not value.strip() else value

    @field_validator("stage_probs")
    @classmethod
    def _stage_probs(cls, value: list[float] | None) -> list[float] | None:
        return check_stage_probs(value)

    @model_validator(mode="after")
    def _stage_evidence(self) -> Self:
        if (
            self.stage_code is not None
            and self.stage_code > 0
            and not (self.stage_quote or "").strip()
        ):
            raise ValueError("подтверждённая стадия требует цитату")
        return self

    @model_validator(mode="after")
    def _half_year_growth(self) -> Self:
        counters = (self.docs_recent, self.docs_previous)
        if self.growth_window != "6m/6m":
            if counters != (None, None):
                raise ValueError("docs_recent и docs_previous передаются только с growth_window=6m/6m")
            return self
        if None in counters:
            raise ValueError("growth_window=6m/6m требует docs_recent и docs_previous")
        expected = half_year_growth(*counters)
        if (expected is None) != (self.growth_ratio is None) or (
            expected is not None and not math.isclose(expected, self.growth_ratio, abs_tol=1e-6)
        ):
            raise ValueError("growth_ratio не совпадает с docs_recent / max(1, docs_previous)")
        return self

    def to_trend_features(self) -> TrendFeatures:
        return TrendFeatures(
            documents=self.documents,
            scientific_works=self.scientific_works,
            market_records=self.market_records,
            active_periods=self.active_periods,
            author_groups=self.author_groups,
            growth=self.growth_ratio,
            growth_basis="documents_6m" if self.growth_window == "6m/6m" else "documents_12m",
            docs_recent=self.docs_recent if self.growth_window == "6m/6m" else self.docs_recent_12m,
            observation_date=self.observation_date,
            top_group_share=self.top_group_share,
            share_growth_pp=self.share_growth_pp,
            first_observed=self.first_observed,
        )


class PredictSource(Model):
    id: str = Field(min_length=1)
    source_type: SourceType = SourceType.OTHER
    trust_level: TrustLevel = TrustLevel.UNKNOWN
    title: str = ""
    abstract: str = Field(default="", max_length=MAX_SOURCE_ABSTRACT_CHARS)
    date: str | None = None
    url: str | None = None


class PredictCandidate(Model):
    id: str = Field(min_length=1)
    name: str = Field(min_length=1)
    features: PredictFeatures
    surface_forms: list[str] = Field(default_factory=list)
    sources: list[PredictSource] = Field(default_factory=list)


class PredictRequest(Model):
    """``feature_schema`` — строка намеренно: неверная схема или версия должна вернуться
    телом ошибки контракта, а не 422 FastAPI; обработчик вызывает :meth:`check_contract`.
    """

    feature_schema: str
    model_version: str
    query: str = Field(
        default="",
        description="Лексический приоритет доказательств перед бюджетом; не проверка смысловой релевантности",
    )
    candidates: list[PredictCandidate] = Field(default_factory=list)

    @property
    def ids(self) -> list[str]:
        return [candidate.id for candidate in self.candidates]

    def check_contract(self, service_version: str) -> tuple[ErrorCode, str] | None:
        if self.feature_schema != FEATURE_SCHEMA:
            return (
                ErrorCode.INVALID_FEATURE_SCHEMA,
                (
                    f"неизвестная схема признаков {self.feature_schema!r}, сервис принимает только {FEATURE_SCHEMA!r}"
                ),
            )
        if self.model_version != service_version:
            return (
                ErrorCode.VERSION_MISMATCH,
                (
                    f"версия модели {self.model_version!r} не совпадает с версией "
                    f"сервиса {service_version!r}; смешивать версии нельзя"
                ),
            )
        if not self.candidates:
            return (ErrorCode.INVALID_REQUEST, "в пакете нет кандидатов")
        seen: set[str] = set()
        for cid in self.ids:
            if cid in seen:
                return (
                    ErrorCode.DUPLICATE_ID,
                    f"идентификатор кандидата повторяется: {cid!r}",
                )
            seen.add(cid)
        return None


class MLAssessment(StrictModel):
    stage_code: int = Field(ge=0, le=5)
    stage_label: str
    trend_code: int = Field(ge=1, le=3)
    score: int = Field(ge=1, le=8)
    class_: CandidateClass = Field(alias="class")
    confidence: float = Field(ge=0.0, le=1.0)
    basis: str

    model_config = ConfigDict(serialize_by_alias=True)

    def feature_values(self) -> list[FeatureContribution]:
        names = {
            "stage_code": "Код стадии",
            "stage_label": "Стадия",
            "trend_code": "Код тренда",
            "score": "Балл",
            "class": "Класс",
            "confidence": "Уверенность",
            "basis": "Основание",
        }
        return [
            FeatureContribution(
                name=f"{names[key]} ({key})", value=value, contribution=0.0
            )
            for key, value in self.model_dump(mode="json").items()
        ]


class Prediction(StrictModel):
    """Все поля обязательны по контракту."""

    id: str = Field(min_length=1)
    label: PredictLabel
    value: float = Field(ge=0.0, le=1.0)
    threshold: float = Field(ge=0.0, le=1.0)
    model: str = Field(min_length=1)
    version: str = Field(min_length=1)
    feature_schema: str = Field(min_length=1)
    basis: str = Field(min_length=1)
    features: list[FeatureContribution] = Field(min_length=1)
    ml: MLAssessment | None = Field(
        default=None, exclude_if=lambda value: value is None
    )

    @field_validator("basis")
    @classmethod
    def _basis_is_russian(cls, value: str) -> str:
        if not is_russian(value):
            raise ValueError("basis must be written in Russian")
        return value

    @field_validator("value", "threshold")
    @classmethod
    def _round(cls, value: float) -> float:
        return round6(value)


class PredictResponse(StrictModel):
    feature_schema: str = FEATURE_SCHEMA
    predictions: list[Prediction] = Field(default_factory=list)

    @model_validator(mode="after")
    def _no_duplicates(self) -> Self:
        seen: set[str] = set()
        for prediction in self.predictions:
            if prediction.id in seen:
                raise ValueError(f"duplicate prediction id: {prediction.id}")
            seen.add(prediction.id)
        return self

    def check_against(self, request: PredictRequest) -> None:
        wanted = set(request.ids)
        got = [prediction.id for prediction in self.predictions]
        if len(got) != len(set(got)):
            raise ValueError("predictions contain duplicate ids")
        missing = sorted(wanted - set(got))
        extra = sorted(set(got) - wanted)
        if missing or extra:
            raise ValueError(f"id mismatch: missing={missing} extra={extra}")


class ErrorResponse(StrictModel):
    """Отказ всего пакета: частичных оценок контракт не допускает."""

    feature_schema: str = FEATURE_SCHEMA
    error: ErrorCode
    message: str = Field(min_length=1)
    detail: list[str] = Field(default_factory=list)

    @field_validator("message")
    @classmethod
    def _message_is_russian(cls, value: str) -> str:
        if not is_russian(value):
            raise ValueError("error message must be written in Russian")
        return value


class ExtractCoverage(StrictModel):
    documents_in: NonNegativeInt = 0
    documents_processed: NonNegativeInt = 0
    documents_failed: NonNegativeInt = 0
    spans_total: NonNegativeInt = 0
    documents_multi_window: NonNegativeInt | None = None
    candidates_total: NonNegativeInt = 0
    sentences_scored: NonNegativeInt = 0
    stage_confirmed: NonNegativeInt = 0
    cache_hits: NonNegativeInt = 0
    cache_misses: NonNegativeInt = 0
    degraded: bool = False
    notes: list[str] = Field(default_factory=list)


class ExtractRequest(Model):
    documents: list[Document] = Field(default_factory=list)
    query: str = Field(
        default="",
        description="Лексический приоритет доказательств перед бюджетом; не проверка смысловой релевантности",
    )
    coverage: Coverage = Coverage.FULL
    #: Если задано и не совпадает с версией сервиса, запрос отклоняется.
    model_version: str | None = None

    observation_date: DateType | None = None

    @field_validator("coverage")
    @classmethod
    def _supported_coverage(cls, value: Coverage) -> Coverage:
        if value is not Coverage.FULL:
            raise ValueError("режим coverage=contribution_only пока не реализован")
        return value

    @field_validator("documents")
    @classmethod
    def _no_conflicting_documents(cls, documents: list[Document]) -> list[Document]:
        seen: dict[str, Document] = {}
        for document in documents:
            if document.doc_id in seen and seen[document.doc_id] != document:
                raise ValueError(
                    f"конфликт текста или метаданных doc_id={document.doc_id!r}"
                )
            seen[document.doc_id] = document
        return documents


class ExtractResponse(StrictModel):
    candidates: list[Candidate] = Field(default_factory=list)
    coverage: ExtractCoverage = Field(default_factory=ExtractCoverage)
    model_versions: dict[str, str] = Field(default_factory=dict)


class EmbeddingRequest(Model):
    """Тело ``POST /v1/embeddings`` в точности как у ``f2llm_server.py``."""

    input: str | list[str]
    model: str | None = Field(default=None, max_length=256)


class EmbeddingItem(Model):
    object: Literal["embedding"] = "embedding"
    index: NonNegativeInt
    embedding: list[float]


class EmbeddingResponse(Model):
    object: Literal["list"] = "list"
    data: list[EmbeddingItem] = Field(default_factory=list)
    model: str = ""
    #: Формат: ``<weights>/last-token-l2-v1/max<N>/<dtype>/torch<v>/transformers<v>``.
    revision: str = ""

    def vectors(self) -> list[list[float]]:
        """Сервер может вернуть элементы не по порядку."""
        return [item.embedding for item in sorted(self.data, key=lambda i: i.index)]


class HealthResponse(StrictModel):
    """Форма как у остальных ML-сервисов плюс поля версий."""

    status: Literal["ok", "degraded"] = "ok"
    service: str = "extractor"
    version: str = ""
    models: dict[str, str] = Field(default_factory=dict)
    feature_schema: str = FEATURE_SCHEMA
    detail: list[str] = Field(default_factory=list)


def dumps_canonical(payload: Any) -> str:
    import json

    def plain(value: Any) -> Any:
        if isinstance(value, BaseModel):
            return value.model_dump(mode="json")
        raise TypeError(f"not JSON serialisable: {type(value).__name__}")

    return json.dumps(
        payload,
        ensure_ascii=False,
        sort_keys=True,
        separators=(",", ":"),
        default=plain,
        allow_nan=False,
    )
