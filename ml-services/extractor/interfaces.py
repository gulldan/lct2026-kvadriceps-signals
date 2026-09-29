"""Границы моделей, канонизации, правил и кэша."""

from __future__ import annotations

import abc
from collections.abc import Mapping, Sequence
from dataclasses import dataclass
from typing import NamedTuple

from extractor.schemas import (
    Candidate,
    CandidateClass,
    Document,
    FeatureContribution,
    SentenceInput,
    SentenceStage,
    Span,
    TrendFeatures,
)


class ModelUnavailable(RuntimeError):
    """Модель не загрузилась или не дала пригодного ответа.

    Пакет тогда получает ``label: failed``: оценка правил вместо оценки модели не подставляется.
    """


class EmbeddingUnavailable(ModelUnavailable):
    """Сервис эмбеддингов не ответил в пределах бюджета."""


class DocumentInputError(ModelUnavailable):
    """Документ нельзя спроецировать точно; остальные документы обрабатываются дальше."""


class SpanTagger(abc.ABC):
    """Находит упоминания TECH. Реализация обязана соблюдать:

    * по внутреннему списку на документ, в порядке ``docs``;
    * ``text[start:end] == span.text`` по ``Document.text``; спаны документа отсортированы
      по ``(start, end, canonical_ru)``;
    * ``canonical_ru`` одинаков для всех упоминаний одной технологии внутри документа;
    * пакетирование, усечение и токенизация — внутри теггера, вызывающий передаёт целые документы.
    """

    #: ``name@version`` артефакта; входит в ключ кэша спанов.
    version: str = "unset"

    def input_identity(self, doc: Document) -> str:
        """Ключ кэша; реализации, зависящие от метаданных, обязаны его переопределить."""
        return doc.text_sha256

    def input_notes(self, doc: Document) -> list[str]:
        """Пометки аудита запроса; отдаются и при попадании в кэш."""
        return []

    @abc.abstractmethod
    def tag(self, docs: Sequence[Document]) -> list[list[Span]]:
        """При отказе модели бросает :class:`ModelUnavailable`."""


class StageScorer(abc.ABC):
    """Код стадии 0–5 для предложений-доказательств. Реализация обязана соблюдать:

    * по одному :class:`SentenceStage` на предложение, в том же порядке;
    * каждая цитата — дословная подстрока своего предложения («стадия без цитаты
      не показывается»); ``code > 0`` требует хотя бы одной цитаты;
    * ``prob`` — калиброванная вероятность выбранного кода; ``probs`` — распределение по кодам
      0..5 или ``None``, ``probs[code] == prob``, по нему правила считают вероятность диапазона класса;
    * необязательный ``p14_platt`` калибрует сумму вероятностей кодов 1–4, без него правила
      берут сумму как есть.
    """

    version: str = "unset"

    @abc.abstractmethod
    def score(self, sentences: Sequence[SentenceInput]) -> list[SentenceStage]:
        """При отказе модели бросает :class:`ModelUnavailable`."""


class Canonicalizer(abc.ABC):
    """Группирует спаны многих документов в кандидатов. Реализация обязана соблюдать:

    * детерминированный порядок кандидатов по каноническому ключу;
    * ``doc_ids``, ``record_ids`` и ``evidence`` (по ``(doc_id, start, end)``) отсортированы
      и без повторов;
    * оценок ещё нет: ``stage``, ``trend``, ``score``, ``label``, ``value`` и ``explanation``
      заполняются позже.
    """

    @abc.abstractmethod
    def group(self, spans_by_doc: Mapping[str, Sequence[Span]]) -> list[Candidate]:
        """Группирует ``doc_id -> спаны`` в кандидатов."""


class Featurizer(abc.ABC):
    """Счётчики тренда методологии для одного кандидата.

    ``None`` — «не измерено»: контракт запрещает заменять отсутствующее измерение нулём.
    """

    @abc.abstractmethod
    def compute(self, candidate: Candidate, docs: Sequence[Document]) -> TrendFeatures:
        """Счётчики ``candidate`` по ``docs``."""


class RuleOutcome(NamedTuple):
    score: int
    label: CandidateClass
    value: float
    explanation: list[FeatureContribution]


class Ruler(abc.ABC):
    """Методология оценки кандидата. Реализация обязана буквально соблюдать:

    * score = код стадии + код тренда;
    * класс: ``not_a_technology``/``only_marketing`` — шум; ``insufficient_evidence`` или
      стадия 0 — недостаточно данных; стадия 5 — зрелая; иначе слабый сигнал;
    * ``value`` в [0, 1] с вкладами именно того метода, что назван в ``basis``: выдуманное
      разложение нарушает контракт;
    * ``explanation`` — пять крупнейших по модулю вкладов с русскими именами.
    """

    #: Порог включения в ТОП-15: подобран на валидации и входит в версию.
    threshold: float = 0.5
    #: Уходит в ``Prediction.model``.
    model_name: str = "unset"
    #: Метод объяснения и единицы по-русски; уходит в ``Prediction.basis``.
    basis: str = "не указан"

    @abc.abstractmethod
    def score(self, candidate: Candidate) -> RuleOutcome:
        """Без модели уверенности бросает :class:`ModelUnavailable`, и вызывающий отвечает ``failed``."""


@dataclass(frozen=True, slots=True)
class EmbeddingClientConfig:
    """Настройки клиента f2llm-service (``ml-services/f2llm-service/f2llm_server.py``).

    * ``codefuse-ai/F2LLM-v2-0.6B``, пулинг last-token + L2: векторы уже единичные, ``dim`` = 1024;
      смена эмбеддера обесценивает все сохранённые векторы;
    * сервер принимает до 128 текстов, 64 КиБ на текст и 512 КиБ всего при одном слоте вывода:
      шлём не больше 64 текстов, чтобы слот делили два клиента;
    * повтор только при ошибке соединения, таймауте и 5xx с детерминированной паузой;
      4xx — наша ошибка, она пробрасывается.
    """

    base_url: str = "http://f2llm-service:8085"
    model: str = "codefuse-ai/F2LLM-v2-0.6B"
    dim: int = 1024
    max_batch: int = 64
    max_text_bytes: int = 64 * 1024
    max_request_bytes: int = 512 * 1024
    timeout_seconds: float = 30.0
    retries: int = 2
    backoff_seconds: float = 0.5

    @property
    def embeddings_url(self) -> str:
        return f"{self.base_url.rstrip('/')}/v1/embeddings"

    @property
    def health_url(self) -> str:
        return f"{self.base_url.rstrip('/')}/health"


class EmbeddingClient(abc.ABC):
    """Клиент сервиса эмбеддингов. Реализация обязана соблюдать:

    * по одному L2-нормированному вектору длины ``config.dim`` на текст, в порядке входа;
    * пакеты не больше ``config.max_batch``;
    * после исчерпания повторов — :class:`EmbeddingUnavailable`, никогда не нули и не короткий список.
    """

    config: EmbeddingClientConfig = EmbeddingClientConfig()

    @abc.abstractmethod
    def embed(self, texts: Sequence[str]) -> list[list[float]]:
        """При отказе бросает :class:`EmbeddingUnavailable`."""

    @abc.abstractmethod
    def health(self) -> bool:
        """True, если ``/health`` отвечает ``status: ok``."""

    def batches(self, texts: Sequence[str]) -> list[list[str]]:
        size = max(1, self.config.max_batch)
        return [list(texts[i : i + size]) for i in range(0, len(texts), size)]


class SpanCache(abc.ABC):
    """Кэш вывода теггера по ``(input_identity, model_version)``.

    Старое имя ``text_sha256`` хранит этот непрозрачный SHA-256: у HF-входов он связывает
    ещё заголовок, аннотацию и политику проекции и потому намеренно отличается от
    ``Document.text_sha256``. ``doc_id`` в ключ не входит: один текст из двух источников
    размечается один раз. Промах неотличим от холодного старта, попадание возвращает ровно
    то, что вернул теггер.
    """

    @abc.abstractmethod
    def get(self, text_sha256: str, model_version: str) -> list[Span] | None:
        """``None`` при промахе."""

    @abc.abstractmethod
    def put(self, text_sha256: str, model_version: str, spans: Sequence[Span]) -> None:
        """Перезапись того же ключа другими спанами — ошибка."""

    def get_many(
        self, keys: Sequence[tuple[str, str]]
    ) -> dict[tuple[str, str], list[Span]]:
        found: dict[tuple[str, str], list[Span]] = {}
        for text_sha256, model_version in keys:
            spans = self.get(text_sha256, model_version)
            if spans is not None:
                found[(text_sha256, model_version)] = spans
        return found


__all__ = [
    "Canonicalizer",
    "EmbeddingClient",
    "EmbeddingClientConfig",
    "EmbeddingUnavailable",
    "Featurizer",
    "ModelUnavailable",
    "RuleOutcome",
    "Ruler",
    "SpanCache",
    "SpanTagger",
    "StageScorer",
]
