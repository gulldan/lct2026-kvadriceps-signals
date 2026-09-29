"""Минимальные детерминированные компоненты для тестов и явного режима заглушек."""

from __future__ import annotations

import re
from collections.abc import Iterable, Mapping, Sequence
from copy import copy

from extractor.evidence import evidence_from_span, sentence_bounds
from extractor.interfaces import (
    Canonicalizer,
    SpanTagger,
    StageScorer,
)
from extractor.schemas import (
    CURRENT_SUBTYPES,
    Candidate,
    Document,
    NoiseFlag,
    SentenceInput,
    SentenceStage,
    Span,
    SpanRole,
    SpanSubtype,
    SpanType,
    normalize_key,
)


BACKGROUND_MARKERS: tuple[str, ...] = (
    "in our previous study",
    "in our previouse study",
    "previously we",
    "ранее нами",
    "в предыдущей работе",
    "в нашей предыдущей",
    "широко применяется",
    "нашли широкое применение",
    "нашло широкое применение",
    "известны",
)

CONTRIBUTION_MARKERS: tuple[str, ...] = (
    "we propose",
    "we present",
    "we produced",
    "we developed",
    "we introduce",
    "in the present study",
    "in this study",
    "this paper proposes",
    "предложен",
    "предлагается",
    "разработан",
    "в данной работе",
    "в настоящей работе",
)

APPLICATION_MARKERS: tuple[str, ...] = (
    "using",
    "was carried out",
    "were carried out",
    "was performed",
    "we used",
    "применял",
    "применяется для",
    "с помощью",
    "использов",
)

STAGE_KEYWORDS: dict[int, tuple[str, ...]] = {
    5: (
        "широко применяется",
        "нашли широкое применение",
        "нашло широкое применение",
        "общепринят",
        "массово применяется",
        "доля рынка",
        "многие поставщики",
        "widely used",
        "widely adopted",
        "industry standard",
        "de facto standard",
        "market share",
    ),
    4: (
        "продаётся",
        "поставляется заказчикам",
        "платящие клиенты",
        "первые контракты",
        "мелкосерийн",
        "доступно заказчикам",
        "серийное производство",
        "commercially available",
        "paying customers",
        "first contracts",
        "now shipping",
        "on sale",
    ),
    3: (
        "пилотный проект",
        "пилотное внедрение",
        "полевые испытания",
        "лётные испытания",
        "тестовые клиенты",
        "опытная эксплуатация",
        "регуляторная песочница",
        "pilot project",
        "pilot deployment",
        "field trial",
        "flight test",
        "trial customers",
        "regulatory sandbox",
        "first customer traffic",
        "real traffic",
    ),
    2: (
        "прототип",
        "опытный образец",
        "демонстрац",
        "испытательный стенд",
        "биореактор",
        "укрупнённый",
        "bioreactor",
        "prototype",
        "proof of concept",
        "proof-of-concept",
        "demonstrated",
        "demonstration",
        "benchmark",
        "pilot-scale",
        "bench-scale",
        "testbed",
        "semi industrial",
        "semi-industrial",
    ),
    1: (
        "показана возможность",
        "предложен метод",
        "лаборатор",
        "моделирование",
        "расчёт",
        "исследован",
        "we propose",
        "we produced",
        "laboratory scale",
        "in the present study",
        "in this study",
        "was studied",
        "results show",
        "simulation",
    ),
}

INTENT_MARKERS: tuple[str, ...] = (
    "планирует",
    "намерен",
    "будет запущен",
    "will launch",
    "plans to",
    "is expected to",
    "intends to",
)

STAGE_PROB_BY_CODE: dict[int, float] = {
    0: 0.4,
    1: 0.55,
    2: 0.6,
    3: 0.6,
    4: 0.6,
    5: 0.65,
}


DEFAULT_VOCABULARY: dict[str, dict[str, str]] = {
    "machine learning": {"canonical_ru": "машинное обучение", "subtype": "method"},
    "машинное обучение": {"canonical_ru": "машинное обучение", "subtype": "method"},
    "federated learning": {
        "canonical_ru": "федеративное обучение",
        "subtype": "method",
    },
    "digital twin": {"canonical_ru": "цифровой двойник", "subtype": "method"},
    "цифровой двойник": {"canonical_ru": "цифровой двойник", "subtype": "method"},
}


class DictionarySpanTagger(SpanTagger):
    version = "stub-dictionary@1"

    def __init__(
        self, vocabulary: Mapping[str, Mapping[str, str]] | None = None
    ) -> None:
        source = dict(DEFAULT_VOCABULARY if vocabulary is None else vocabulary)
        # Сначала самая длинная поверхность, затем по алфавиту: конфликты разрешаются детерминированно.
        self._surfaces: list[tuple[str, dict[str, str]]] = sorted(
            (
                (surface, dict(entry))
                for surface, entry in source.items()
                if surface.strip()
            ),
            key=lambda item: (-len(item[0]), item[0]),
        )
        self._patterns = [
            (re.compile(re.escape(surface), re.IGNORECASE), entry)
            for surface, entry in self._surfaces
        ]

    @classmethod
    def from_annotations(cls, annotations: Iterable) -> DictionarySpanTagger:
        vocabulary: dict[str, dict[str, str]] = {}
        for annotation in annotations:
            for span in annotation.spans:
                vocabulary.setdefault(
                    span.text,
                    {"canonical_ru": span.canonical_ru, "subtype": span.subtype.value},
                )
        return cls(vocabulary)

    @staticmethod
    def _role(sentence: str, in_title: bool) -> SpanRole:
        lowered = sentence.casefold()
        if any(marker in lowered for marker in BACKGROUND_MARKERS):
            return SpanRole.BACKGROUND
        if in_title:
            return SpanRole.CONTRIBUTION
        if any(marker in lowered for marker in CONTRIBUTION_MARKERS):
            return SpanRole.CONTRIBUTION
        if any(marker in lowered for marker in APPLICATION_MARKERS):
            return SpanRole.APPLICATION
        return SpanRole.BACKGROUND

    def tag(self, docs: Sequence[Document]) -> list[list[Span]]:
        return [self._tag_one(doc) for doc in docs]

    def _tag_one(self, doc: Document) -> list[Span]:
        text = doc.text
        taken: list[tuple[int, int]] = []
        spans: list[Span] = []
        for pattern, entry in self._patterns:
            for match in pattern.finditer(text):
                start, end = match.span()
                if any(start < t_end and t_start < end for t_start, t_end in taken):
                    continue
                taken.append((start, end))
                sent_start, sent_end = sentence_bounds(text, start, end)
                in_title = start < doc.title_end
                role = self._role(text[sent_start:sent_end], in_title)
                subtype = entry.get("subtype", SpanSubtype.METHOD.value)
                spans.append(
                    Span(
                        start=start,
                        end=end,
                        text=text[start:end],
                        type=SpanType.TECH,
                        subtype=subtype,
                        role=role,
                        is_candidate=role is SpanRole.CONTRIBUTION,
                        in_title=in_title,
                        canonical_ru=entry["canonical_ru"],
                        confidence=0.5,
                        doc_id=doc.doc_id,
                        sentence=text[sent_start:sent_end],
                    )
                )
        spans.sort(key=lambda span: (span.start, span.end, span.canonical_ru))
        return spans


class KeywordStageScorer(StageScorer):
    """Распределения по кодам у таблицы ключевых слов нет, поэтому ``probs`` — ``None``:
    правила берут вероятность выбранного кода как нижнюю границу диапазона.
    """

    version = "stub-keywords@1"

    def score(self, sentences: Sequence[SentenceInput]) -> list[SentenceStage]:
        return [self._score_one(item) for item in sentences]

    @staticmethod
    def _matches(sentence: str, markers: Sequence[str]) -> list[str]:
        lowered = sentence.casefold()
        hits: list[tuple[int, str]] = []
        for marker in markers:
            index = lowered.find(marker.casefold())
            if index >= 0:
                hits.append((index, sentence[index : index + len(marker)]))
        return [quote for _, quote in sorted(hits)]

    def _score_one(self, item: SentenceInput) -> SentenceStage:
        code = 0
        quotes: list[str] = []
        for candidate_code in (5, 4, 3, 2, 1):
            found = self._matches(item.sentence, STAGE_KEYWORDS[candidate_code])
            if found:
                code, quotes = candidate_code, found
                break
        flags: list[NoiseFlag] = []
        intent = self._matches(item.sentence, INTENT_MARKERS)
        if intent:
            # Намерение ничего не подтверждает.
            flags.append(NoiseFlag.INTENT)
            code, quotes = 0, []
        return SentenceStage(
            sent_start=item.sent_start,
            sent_end=item.sent_end,
            sentence=item.sentence,
            code=code,
            quotes=quotes,
            prob=STAGE_PROB_BY_CODE[code],
            doc_id=item.doc_id,
            single_source=False,
            noise_flags=flags,
            confidence=0.5,
        )


class DefaultCanonicalizer(Canonicalizer):
    """Группировка по ``canonical_ru``; в проде — ``canon.Canonicalizer``.

    Спаны на карантине и подтипы вне допустимых (материалы, стандарты, штаммы) отбрасываются:
    это не технологии.
    """

    def __init__(
        self,
        *,
        skip_quarantined: bool = True,
        allowed_subtypes: frozenset[SpanSubtype] = CURRENT_SUBTYPES,
        documents: Mapping[str, Document] | None = None,
    ) -> None:
        self.skip_quarantined = skip_quarantined
        self.allowed_subtypes = allowed_subtypes
        self.documents = dict(documents or {})

    def with_documents(self, documents: Mapping[str, Document]) -> DefaultCanonicalizer:
        bound = copy(self)
        bound.documents = dict(documents)
        return bound

    def keep(self, span: Span) -> bool:
        if self.skip_quarantined and span.quarantined:
            return False
        return (
            span.subtype in self.allowed_subtypes
            and bool(re.search(r"[^\W_]", span.canonical_ru))
            and bool(re.search(r"[^\W_]", span.text))
        )

    def group(self, spans_by_doc: Mapping[str, Sequence[Span]]) -> list[Candidate]:
        groups: dict[str, dict] = {}
        for doc_id in sorted(spans_by_doc):
            for span in sorted(spans_by_doc[doc_id], key=lambda s: (s.start, s.end)):
                if not self.keep(span):
                    continue
                key = normalize_key(span.canonical_ru)
                bucket = groups.setdefault(
                    key,
                    {"names": {}, "doc_ids": set(), "evidence": {}, "surfaces": set(),
                     "heads": []},
                )
                if span.head_prob is not None:
                    bucket["heads"].append(span.head_prob)
                bucket["names"][span.canonical_ru] = (
                    bucket["names"].get(span.canonical_ru, 0) + 1
                )
                bucket["doc_ids"].add(doc_id)
                bucket["surfaces"].add(span.text)
                evidence_key = (doc_id, span.start, span.end)
                bucket["evidence"][evidence_key] = evidence_from_span(
                    span, doc_id, self.documents.get(doc_id)
                )
        candidates: list[Candidate] = []
        for key in sorted(groups):
            bucket = groups[key]
            name = min(bucket["names"].items(), key=lambda item: (-item[1], item[0]))[0]
            evidence = [bucket["evidence"][k] for k in sorted(bucket["evidence"])]
            candidates.append(
                Candidate(
                    name=name,
                    canonical_ru=name,
                    surface_forms=sorted(bucket["surfaces"]),
                    doc_ids=sorted(bucket["doc_ids"]),
                    record_ids=[],
                    evidence=evidence,
                    head_prob=max(bucket["heads"], default=None),
                )
            )
        return candidates
