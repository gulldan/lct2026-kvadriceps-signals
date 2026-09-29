"""Сборка конвейера: порядок, пакетная обработка и кэширование компонентов."""

from __future__ import annotations

import re
from collections import defaultdict
from bisect import bisect_left, bisect_right
from collections.abc import Iterable, Mapping, Sequence
from copy import copy
from dataclasses import dataclass, field, replace
from datetime import date
from functools import lru_cache

from extractor.evidence import EvidenceFinder
from extractor.interfaces import (
    Canonicalizer,
    DocumentInputError,
    Featurizer,
    ModelUnavailable,
    Ruler,
    SpanCache,
    SpanTagger,
    StageScorer,
)
from extractor.registry import ModelRegistry, Thresholds, default_registry
from extractor.rules import (
    CorpusFeaturizer,
    MethodologyRuler,
    compute_flags,
    detect_has_standard,
    trend_code,
)
from extractor.runtime.cache import CacheCorruptionError, InMemorySpanCache
from extractor.runtime.text import TextNormalizer, marker_role, sentence_spans
from extractor.schemas import (
    CLASS_TO_LABEL,
    FEATURE_SCHEMA,
    WEAK_SIGNAL_STAGE_CODES,
    Candidate,
    CandidateStage,
    CandidateTrend,
    Document,
    Evidence,
    ExtractCoverage,
    ExtractRequest,
    ExtractResponse,
    FeatureContribution,
    MLAssessment,
    NoiseFlag,
    Prediction,
    PredictLabel,
    PredictRequest,
    PredictResponse,
    SentenceInput,
    SentenceStage,
    Span,
    SpanRole,
    normalize_key,
    round6,
)
from extractor.stage_cache import STAGE_CACHE_VERSION, EvidenceStageCache
from extractor.stubs import (
    DefaultCanonicalizer,
    DictionarySpanTagger,
    KeywordStageScorer,
)

_MIN_TOKEN = 4
_TOKEN_RE = re.compile(r"\w+", re.UNICODE)


def name_tokens(name: str) -> list[str]:
    """Грубые основы слов имени для предварительного отбора предложений.

    Отрезание до трёх последних символов позволяет по «штамповка» в имени найти «штамповки».
    Это приём полноты для бюджетного фильтра, а не морфология: ложное совпадение лишь
    отправит классификатору стадии ещё одно предложение.
    """
    stems = set()
    for token in _TOKEN_RE.findall(normalize_key(name)):
        if len(token) >= _MIN_TOKEN:
            stems.add(token[: max(_MIN_TOKEN, len(token) - 3)])
    return sorted(stems)


_MASS_ADOPTION = re.compile(
    r"широк\w* примен|нашл[ио] широкое применение|общепринят|массов\w* "
    r"(?:примен|внедр)|много\w* поставщик|дол[яию] рынка|зрел\w* инфраструктур|"
    r"стандарт|widely (?:used|adopted)|mass adoption|industry standard|"
    r"de facto standard|market share|many suppliers",
    re.IGNORECASE,
)


def pick_stage(
    verdicts: Sequence[SentenceStage],
    min_prob: float = 0.0,
    *,
    evidence: Sequence[Evidence] = (),
    documents: Mapping[str, Document] | None = None,
) -> CandidateStage:
    """Максимальная стадия с цитатой; фоновая зрелость требует прямой формулировки массовости.

    Копии одного текста или URL и одна группа авторов независимым подтверждением не считаются.
    Одиночная фоновая стадия 5 остаётся пятой с видимым флагом single_source и множителем 0,6.
    Распределение выбранного предложения переносится без множителя: он уже учтён в ``prob`` кода 5.
    """
    roles = {(e.doc_id, e.start, e.end): e.role for e in evidence}
    documents = documents or {}

    def role(verdict):
        return roles.get(
            (verdict.doc_id, verdict.sent_start, verdict.sent_end), SpanRole.BACKGROUND
        )

    usable = [
        v
        for v in verdicts
        if v.code > 0
        and v.quotes
        and v.prob >= min_prob
        and (
            v.code != 5
            or role(v) is not SpanRole.BACKGROUND
            or _MASS_ADOPTION.search(" ".join(v.quotes))
        )
    ]
    if not usable:
        return CandidateStage()
    best = max(usable, key=lambda v: (v.code, v.prob, v.doc_id or "", -v.sent_start))
    supporting = [v for v in usable if v.code == best.code]
    # Нужны разные тексты, URL и наблюдаемые группы авторов; неизвестная группа независимости
    # не доказывает, поэтому запасной критерий — разные документы.
    hashes, urls, groups, independent = set(), set(), set(), set()
    for verdict in sorted(supporting, key=lambda v: (v.doc_id or "", v.sent_start)):
        doc = documents.get(verdict.doc_id)
        key = verdict.doc_id or "unknown"
        if doc:
            group = doc.group_key if doc.orgs or doc.authors else None
            if (
                doc.text_sha256 in hashes
                or (doc.url and doc.url in urls)
                or (group and group in groups)
            ):
                continue
            hashes.add(doc.text_sha256)
            if doc.url:
                urls.add(doc.url)
            if group:
                groups.add(group)
        independent.add(key)
    single = best.single_source or len(independent) < 2
    background_single = best.code == 5 and role(best) is SpanRole.BACKGROUND and single
    return CandidateStage(
        code=best.code,
        quote=best.quotes[0],
        doc_id=best.doc_id,
        prob=round6(best.prob * (0.6 if background_single else 1.0)),
        single_source=single,
        probs=best.probs,
    )


def _check_verdicts(
    sentences: Sequence[SentenceInput], verdicts: Sequence[SentenceStage]
) -> list[SentenceStage]:
    """Проверка до публикации доказательств и записи в кэш."""
    if len(verdicts) != len(sentences):
        raise ModelUnavailable(
            f"stage scorer returned {len(verdicts)} verdicts for {len(sentences)} sentences"
        )
    checked = []
    for sentence, verdict in zip(sentences, verdicts, strict=True):
        try:
            verdict = SentenceStage.model_validate(verdict.__dict__)
        except ValueError as exc:
            raise ModelUnavailable("неверная цитата или значение стадии") from exc
        if (verdict.sent_start, verdict.sent_end, verdict.sentence) != (
            sentence.sent_start,
            sentence.sent_end,
            sentence.sentence,
        ) or verdict.doc_id not in (None, sentence.doc_id):
            raise ModelUnavailable(
                "стадия не совпадает с исходным предложением документа"
            )
        checked.append(verdict if verdict.doc_id == sentence.doc_id else verdict.model_copy(update={"doc_id": sentence.doc_id}))
    return checked


def _evidence(
    sentences: Sequence[SentenceInput], docs: Mapping[str, Document], *,
    literal_title: bool = False,
) -> list[Evidence]:
    def in_title(sentence):
        doc = docs[sentence.doc_id]
        if not literal_title:
            return sentence.sent_end <= doc.title_end
        start = doc.text.find(doc.title) if doc.title.strip() else -1
        return start >= 0 and start <= sentence.sent_start < sentence.sent_end <= start + len(doc.title)

    return [
        Evidence(
            doc_id=s.doc_id,
            start=s.sent_start,
            end=s.sent_end,
            sentence=s.sentence,
            role=s.role,
            in_title=in_title(s),
        )
        for s in sentences
    ]


@lru_cache(maxsize=1)
def _text_normalizer() -> TextNormalizer:
    return TextNormalizer()


def _query_order(sentences: Sequence[SentenceInput], query: str) -> list[SentenceInput]:
    """Запрос лишь поднимает доказательства со своими словами до бюджета; вход модели не меняется."""
    tokens = set(name_tokens(query))
    if not tokens:
        return sorted(sentences, key=lambda item: item.sort_key)
    return sorted(
        sentences,
        key=lambda item: (
            -sum(token in normalize_key(item.sentence) for token in tokens),
            item.sort_key,
        ),
    )


def _bind_stage_calibration(ruler, stage_scorer):
    """Правила калибруют p(1..4) той моделью стадии, что дала стадию.

    Сюда доходит только подтверждённая стадия: её модель загружена, и сломанная калибровка
    уже уронила стадию, а не деградировала молча.
    """
    bind = getattr(ruler, "with_p14_platt", None)
    return bind(getattr(stage_scorer, "p14_platt", None)) if callable(bind) else ruler


def _configure_components(components: Components, config: PipelineConfig) -> Components:
    """Порог реестра привязывается без изменения компонентов, общих для запросов."""
    ruler = components.ruler
    bind = getattr(ruler, "with_threshold", None)
    if callable(bind):
        ruler = bind(config.thresholds.predict)
    elif ruler.threshold != config.thresholds.predict:
        ruler = copy(ruler)
        ruler.threshold = config.thresholds.predict
    return replace(components, ruler=ruler)


@dataclass(frozen=True, slots=True)
class PipelineConfig:
    """Всё настраиваемое входит в версию сервиса."""

    service_version: str
    thresholds: Thresholds
    max_sentences_per_doc: int = 8
    #: Бюджет контракта: 45 кандидатов × 24 источника × 2000 символов.
    max_sources_per_candidate: int = 24
    max_sentences_per_source: int = 16
    #: Сид для компонентов со случайностью; сейчас таких нет.
    seed: int = 20260921
    observation_date: date | None = field(default=None, kw_only=True)

    @classmethod
    def from_registry(cls, registry: ModelRegistry, **overrides) -> PipelineConfig:
        return cls(
            service_version=registry.service_version,
            thresholds=registry.thresholds,
            **overrides,
        )


@dataclass(slots=True)
class Components:
    tagger: SpanTagger = field(default_factory=DictionarySpanTagger)
    canonicalizer: Canonicalizer = field(default_factory=DefaultCanonicalizer)
    featurizer: Featurizer = field(default_factory=CorpusFeaturizer)
    stage_scorer: StageScorer = field(default_factory=KeywordStageScorer)
    ruler: Ruler = field(default_factory=MethodologyRuler)
    cache: SpanCache = field(default_factory=InMemorySpanCache)
    stage_cache: EvidenceStageCache = field(default_factory=EvidenceStageCache)
    evidence_finder: EvidenceFinder = field(
        default_factory=lambda: EvidenceFinder(max_sentences=2**31 - 1), kw_only=True
    )


class ExtractPipeline:
    """Внутренняя база; наружу публикуется ``outputs.ExtractPipeline``."""

    def __init__(
        self,
        components: Components | None = None,
        config: PipelineConfig | None = None,
        registry: ModelRegistry | None = None,
    ) -> None:
        self.registry = registry or default_registry()
        self.config = config or PipelineConfig.from_registry(self.registry)
        self.components = _configure_components(components or Components(), self.config)

    def model_versions(self) -> dict[str, str]:
        return {
            "service": self.config.service_version,
            "feature_schema": FEATURE_SCHEMA,
            "tagger": self.components.tagger.version,
            "stage": self.components.stage_scorer.version,
            "ruler": self.components.ruler.model_name,
        }

    def _spans(
        self,
        docs: Sequence[Document],
        coverage: ExtractCoverage,
        *,
        pending: dict[tuple[str, str], list[Span]] | None = None,
        failed_documents: set[str] | None = None,
    ) -> dict[str, list[Span]]:
        docs = _unique_documents(docs)
        version = self.components.tagger.version
        result: dict[str, list[Span]] = {}
        missing: dict[tuple[str, str], list[Document]] = {}
        looked_up: dict[tuple[str, str], list[Span] | None] = {}
        identity_for = getattr(self.components.tagger, "input_identity", lambda doc: doc.text_sha256)
        notes_for = getattr(self.components.tagger, "input_notes", lambda doc: [])
        for doc in docs:
            try:
                identity = identity_for(doc)
                notes = notes_for(doc)
            except DocumentInputError as exc:
                result[doc.doc_id] = []
                coverage.degraded = True
                coverage.notes.append(f"документ {doc.doc_id}: {exc}")
                if failed_documents is not None:
                    failed_documents.add(doc.doc_id)
                continue
            coverage.notes.extend(f"документ {doc.doc_id}: {note}" for note in notes)
            key = (identity, version)
            if key not in looked_up:
                looked_up[key] = self.components.cache.get(*key)
            cached = looked_up[key]
            if cached is None:
                missing.setdefault(key, []).append(doc)
                coverage.cache_misses += 1
            else:
                checked = self._validate(doc, cached, coverage)
                if len(checked) != len(cached):
                    raise CacheCorruptionError("кэш содержит спаны с неверными координатами")
                result[doc.doc_id] = checked
                coverage.cache_hits += 1
        if missing:
            representatives = [group[0] for group in missing.values()]
            tagged = self.components.tagger.tag(representatives)
            if len(tagged) != len(representatives):
                raise ModelUnavailable(
                    f"tagger returned {len(tagged)} span lists for {len(representatives)} documents"
                )
            windows = getattr(tagged, "windows", None)
            if windows is None:
                windows = getattr(self.components.tagger, "window_counts", None)
            if windows is not None:
                if isinstance(windows, Mapping):
                    counts = [windows.get(doc.doc_id) for doc in representatives]
                else:
                    counts = list(windows)
                if len(counts) == len(representatives) and all(
                    isinstance(n, int) and n > 0 for n in counts
                ):
                    coverage.documents_multi_window = sum(
                        len(group)
                        for group, n in zip(missing.values(), counts, strict=True)
                        if n > 1
                    )
            for (key, group), spans in zip(missing.items(), tagged, strict=True):
                doc = group[0]
                checked = self._validate(doc, spans, coverage)
                if len(checked) != len(spans):
                    if failed_documents is not None:
                        failed_documents.update(item.doc_id for item in group)
                elif pending is not None:
                    pending[key] = checked
                for item in group:
                    result[item.doc_id] = [
                        (span if span.doc_id == item.doc_id else span.model_copy(update={"doc_id": item.doc_id}))
                        for span in checked
                    ]
        return result

    @staticmethod
    def _validate(
        doc: Document, spans: Sequence[Span], coverage: ExtractCoverage
    ) -> list[Span]:
        """Смещения, не воспроизводящие текст спана, отбрасываются: теггеру не доверяем."""
        kept: list[Span] = []
        for span in spans:
            try:
                span.check_against(doc)
            except ValueError as exc:
                coverage.degraded = True
                coverage.notes.append(f"спан отброшен: {exc}")
                continue
            kept.append(span if span.doc_id == doc.doc_id else span.model_copy(update={"doc_id": doc.doc_id}))
        kept.sort(key=lambda span: (span.start, span.end, span.canonical_ru))
        return kept

    def _sentences(
        self, candidate: Candidate, docs: Mapping[str, Document], *, query: str = ""
    ) -> list[SentenceInput]:
        found = []
        for doc_id in sorted(set(candidate.doc_ids)):
            if doc_id in docs:
                found.extend(
                    self.components.evidence_finder.find(
                        candidate,
                        docs[doc_id],
                        surface_forms=candidate.surface_forms,
                    )
                )
        evidence_by_doc = defaultdict(list)
        for evidence in candidate.evidence:
            evidence_by_doc[evidence.doc_id].append(evidence)
        starts = {}
        for doc_id, evidence in evidence_by_doc.items():
            evidence.sort(key=lambda item: item.start)
            starts[doc_id] = [item.start for item in evidence]
        for item in found:
            evidence = evidence_by_doc[item.doc_id]
            offsets = starts.get(item.doc_id, [])
            roles = [
                e.role for e in evidence[
                    bisect_left(offsets, item.sent_start):bisect_right(offsets, item.sent_end)
                ] if e.end <= item.sent_end
            ]
            if roles:
                item.role = min(
                    roles,
                    key=lambda role: {
                        SpanRole.CONTRIBUTION: 0,
                        SpanRole.APPLICATION: 1,
                        SpanRole.BACKGROUND: 2,
                    }[role],
                )
        # Обязательный фильтр по имени и поверхностям уже сделал поиск; бюджет — второй шаг.
        per_doc: dict[str, int] = {}
        out = []
        for item in _query_order(found, query):
            if per_doc.get(item.doc_id, 0) < self.config.max_sentences_per_doc:
                out.append(item)
                per_doc[item.doc_id] = per_doc.get(item.doc_id, 0) + 1
        return sorted(out, key=lambda item: item.sort_key)

    def run(self, request: ExtractRequest) -> ExtractResponse:
        coverage = ExtractCoverage(documents_in=len(request.documents))
        docs = _unique_documents(request.documents)
        coverage.documents_processed = len(docs)
        by_id = {doc.doc_id: doc for doc in docs}

        pending: dict[tuple[str, str], list[Span]] = {}
        failed_documents: set[str] = set()
        spans_by_doc = self._spans(
            docs, coverage, pending=pending, failed_documents=failed_documents
        )
        coverage.spans_total = sum(len(spans) for spans in spans_by_doc.values())

        canonicalizer = self.components.canonicalizer
        bind = getattr(canonicalizer, "with_documents", None)
        if callable(bind):
            canonicalizer = bind(by_id)
        bind_notes = getattr(canonicalizer, "with_notes", None)
        if callable(bind_notes):
            canonicalizer = bind_notes(coverage.notes)
        candidates = canonicalizer.group(spans_by_doc)
        coverage.candidates_total = len(candidates)

        # Один вызов stage_scorer на весь прогон: пакетирование — забота рантайма.
        evidence_pipeline = self
        bind = getattr(self.components.evidence_finder, "with_documents", None)
        if callable(bind):
            evidence_pipeline = copy(self)
            evidence_pipeline.components = replace(
                self.components, evidence_finder=bind(by_id)
            )
        sentences: list[SentenceInput] = []
        slices: list[tuple[int, int]] = []
        cached_verdicts = []
        missing_indices = []
        pending_stages = []
        versions = tuple(sorted(self.model_versions().items()))
        literal_title = getattr(self.components.tagger, "input_policy", None) == "title_abstract"
        for candidate in candidates:
            lo = len(sentences)
            source_evidence = defaultdict(list)
            for item in candidate.evidence:
                source_evidence[item.doc_id].append(item)
            evidence = []
            for doc_id in sorted(set(candidate.doc_ids)):
                if doc_id not in by_id:
                    continue
                doc = by_id[doc_id]
                # Метаданные и выборка предложений влияют на доказательства даже
                # при том же тексте; чужой запрос не должен менять попадание.
                candidate_key = (
                    candidate.id, candidate.canonical_ru, candidate.name,
                    tuple(sorted(candidate.surface_forms)), request.query,
                    self.config.max_sentences_per_doc, literal_title,
                    doc.title, doc.source_type, doc.date,
                    tuple(sorted((e.start, e.end, e.role, e.in_title)
                                 for e in source_evidence[doc_id])),
                    getattr(self.components.evidence_finder, "version", "default"),
                    getattr(self.components.evidence_finder, "max_sentences", None),
                )
                key = (STAGE_CACHE_VERSION, doc.text_sha256, versions, candidate_key)
                cached = self.components.stage_cache.get(key, doc_id)
                if cached is None:
                    local_candidate = candidate.model_copy(update={
                        "doc_ids": [doc_id], "evidence": source_evidence[doc_id],
                    })
                    block = evidence_pipeline._sentences(local_candidate, by_id, query=request.query)
                    proofs = _evidence(block, by_id, literal_title=literal_title)
                    begin = len(sentences)
                    pending_stages.append((key, block, proofs, begin, begin + len(block)))
                    missing_indices.extend(range(begin, begin + len(block)))
                    checked = [None] * len(block)
                else:
                    block, proofs, checked = cached
                sentences.extend(block)
                evidence.extend(proofs)
                cached_verdicts.extend(checked)
            candidate.evidence = evidence
            slices.append((lo, len(sentences)))
        # Поле охвата сохраняет число оценённых предложений, включая кэш.
        coverage.sentences_scored = len(sentences)
        stage_failed = False
        try:
            missing = [sentences[index] for index in missing_indices]
            fresh = _check_verdicts(
                missing, self.components.stage_scorer.score(missing) if missing else []
            )
            verdicts = cached_verdicts
            for index, verdict in zip(missing_indices, fresh, strict=True):
                verdicts[index] = verdict
        except ModelUnavailable as exc:
            stage_failed = True
            verdicts = []
            slices = [(0, 0) for _ in candidates]
            coverage.degraded = True
            coverage.notes.append(f"классификатор стадии недоступен: {exc}")
            failed_documents.update(item.doc_id for item in sentences)

        featurizer = self.components.featurizer
        anchor = request.observation_date or self.config.observation_date
        if anchor is not None:
            bind = getattr(featurizer, "with_observation_date", None)
            if not callable(bind):
                raise ValueError("компонент признаков не поддерживает дату наблюдения")
            featurizer = bind(anchor)
        bind = getattr(featurizer, "with_documents", None)
        if callable(bind):
            featurizer = bind(by_id)
        local = copy(self)
        local.components = replace(self.components, featurizer=featurizer)
        scored: list[Candidate] = []
        for candidate, (lo, hi) in zip(candidates, slices, strict=True):
            finished = local._finish(candidate, verdicts[lo:hi], docs, by_id)
            if stage_failed:
                finished.stage.status = "failed"
            scored.append(finished)
        coverage.stage_confirmed = sum(1 for c in scored if c.stage.confirmed)
        scored.sort(key=lambda candidate: candidate.sort_key)

        coverage.documents_failed = len(failed_documents)
        if request.query.strip():
            coverage.notes.append(
                "query использован для лексического приоритета доказательств перед бюджетом; смысловая релевантность не проверялась"
            )
        response = ExtractResponse(
            candidates=scored,
            coverage=coverage,
            model_versions=self.model_versions(),
        )

        # Повреждённый или деградировавший прогон не должен попасть в кэш спанов как здоровый.
        if not coverage.degraded:
            for key, block, proofs, lo, hi in pending_stages:
                self.components.stage_cache.put(key, block, proofs, verdicts[lo:hi])
            for key, spans in pending.items():
                self.components.cache.put(*key, spans)
        return response

    def _finish(
        self,
        candidate: Candidate,
        verdicts: Sequence[SentenceStage],
        docs: Sequence[Document],
        by_id: Mapping[str, Document],
    ) -> Candidate:
        thresholds = self.config.thresholds
        features = self.components.featurizer.compute(candidate, docs)
        candidate = candidate.model_copy(
            update={
                "stage": pick_stage(
                    verdicts,
                    thresholds.stage_min_prob,
                    evidence=candidate.evidence,
                    documents=by_id,
                ),
                "trend": CandidateTrend(
                    code=trend_code(features, thresholds.trend_t1, thresholds.trend_t2),
                    features=features,
                ),
                "record_ids": sorted(
                    doc_id
                    for doc_id in candidate.doc_ids
                    if doc_id in by_id and by_id[doc_id].is_market_record
                ),
                "has_standard": detect_has_standard(
                    [evidence.sentence for evidence in candidate.evidence]
                    + [candidate.canonical_ru]
                ),
            }
        )
        ruler = self.components.ruler
        candidate = candidate.model_copy(
            update={
                "flags": compute_flags(candidate, by_id, getattr(ruler, "config", None))
            }
        )
        bind = getattr(ruler, "with_documents", None)
        if callable(bind):
            ruler = bind(by_id)
        if candidate.stage.confirmed:
            ruler = _bind_stage_calibration(ruler, self.components.stage_scorer)
        outcome = ruler.score(candidate)
        return candidate.model_copy(
            update={
                "score": outcome.score,
                "label": outcome.label,
                "value": round6(outcome.value),
                "explanation": list(outcome.explanation),
            }
        )


def _unique_documents(documents: Iterable[Document]) -> list[Document]:
    seen: dict[str, Document] = {}
    for document in documents:
        if document.doc_id in seen and seen[document.doc_id] != document:
            raise ValueError(
                f"конфликт текста или метаданных doc_id={document.doc_id!r}"
            )
        seen[document.doc_id] = document
    return [seen[doc_id] for doc_id in sorted(seen)]


class PredictService:
    """Стадия из извлечения берётся только при версии активной модели реестра, иначе оценка
    идёт по источникам. При отказе модели весь пакет получает явный отказ без подмены
    оценкой правил.
    """

    def __init__(
        self,
        components: Components | None = None,
        config: PipelineConfig | None = None,
        registry: ModelRegistry | None = None,
    ) -> None:
        self.registry = registry or default_registry()
        self.config = config or PipelineConfig.from_registry(self.registry)
        self.components = _configure_components(components or Components(), self.config)

    def _supplied_stage(self, candidate) -> CandidateStage | None:
        features = candidate.features
        # Активный компонент загружен сборкой из реестра; сравниваем полную ссылку.
        if (
            features.stage_code is None
            or features.stage_model_version != self.components.stage_scorer.version
        ):
            return None
        return CandidateStage(
            code=features.stage_code,
            quote=features.stage_quote or "",
            doc_id=features.stage_doc_id,
            prob=features.stage_prob or 0.0,
            single_source=features.stage_single_source,
            # Без stage_prob уверенность нулевая: распределение не восстанавливает множитель 0,6,
            # уже применённый к коду 5 при извлечении.
            probs=features.stage_probs if features.stage_prob is not None else None,
        )

    def _sentences(self, candidate, *, query: str = "") -> list[SentenceInput]:
        normalizer = _text_normalizer()
        names = [candidate.name, *candidate.surface_forms]
        needles = {normalizer.tokens(name) for name in names if normalizer.tokens(name)}
        out: list[SentenceInput] = []
        for source in sorted(candidate.sources, key=lambda item: item.id)[
            : self.config.max_sources_per_candidate
        ]:
            body = source.title + "\n\n" + source.abstract
            named = []
            for start, end, sentence in sentence_spans(body):
                tokens = normalizer.tokens(sentence)
                if not any(
                    tokens[i : i + len(needle)] == needle
                    for needle in needles
                    for i in range(len(tokens) - len(needle) + 1)
                ):
                    continue
                named.append(
                    SentenceInput(
                        doc_id=source.id,
                        sent_start=start,
                        sent_end=end,
                        sentence=sentence,
                        candidate_text=candidate.name,
                        role=marker_role(sentence),
                        source_type=source.source_type,
                    )
                )
            out.extend(
                _query_order(named, query)[: self.config.max_sentences_per_source]
            )
        return sorted(out, key=lambda item: item.sort_key)

    @staticmethod
    def _documents(source_candidate) -> dict[str, Document]:
        return {
            source.id: Document(
                doc_id=source.id,
                source="predict",
                title=source.title,
                abstract=source.abstract,
                url=source.url,
                source_type=source.source_type,
                trust_level=source.trust_level,
            )
            for source in source_candidate.sources
        }

    def _as_candidate(
        self, source_candidate, verdicts: Sequence[SentenceStage]
    ) -> Candidate:
        thresholds = self.config.thresholds
        features = source_candidate.features.to_trend_features()
        docs = self._documents(source_candidate)
        evidence = [
            Evidence(
                doc_id=v.doc_id,
                start=v.sent_start,
                end=v.sent_end,
                sentence=v.sentence,
                role=marker_role(v.sentence),
                in_title=v.sent_end <= docs[v.doc_id].title_end,
            )
            for v in verdicts
            if v.code > 0
            and v.quotes
            and v.prob >= thresholds.stage_min_prob
            and v.doc_id in docs
        ]
        candidate = Candidate(
            id=source_candidate.id,
            name=source_candidate.name,
            canonical_ru=source_candidate.name,
            surface_forms=source_candidate.surface_forms,
            source_trust=source_candidate.features.source_trust,
            head_prob=source_candidate.features.head_prob,
            doc_ids=sorted(docs),
            evidence=evidence,
            stage=self._supplied_stage(source_candidate)
            or pick_stage(
                verdicts, thresholds.stage_min_prob, evidence=evidence, documents=docs
            ),
            trend=CandidateTrend(
                code=trend_code(features, thresholds.trend_t1, thresholds.trend_t2),
                features=features,
            ),
            has_standard=detect_has_standard(
                [source_candidate.name, *(v.sentence for v in verdicts)]
            ),
        )
        supplied = source_candidate.features
        flags = set(compute_flags(candidate, docs, getattr(self.components.ruler, "config", None)))
        # Явные значения извлечения относятся ко всему корпусу, а не к выборке источников.
        for flag in (
            NoiseFlag.ONLY_MARKETING, NoiseFlag.NOT_A_TECHNOLOGY,
            NoiseFlag.INSUFFICIENT_EVIDENCE, NoiseFlag.HAS_STANDARD, NoiseFlag.SINGLE_SOURCE,
        ):
            value = getattr(supplied, flag.value)
            if value is not None:
                flags.discard(flag)
                if value:
                    flags.add(flag)
        return candidate.model_copy(update={
            "flags": sorted(flags, key=lambda flag: flag.value),
            "has_standard": supplied.has_standard if supplied.has_standard is not None else candidate.has_standard,
        })

    def predict(self, request: PredictRequest) -> PredictResponse:
        ruler = self.components.ruler
        sentences: list[SentenceInput] = []
        slices: list[tuple[int, int]] = []
        for candidate in request.candidates:
            block = (
                []
                if self._supplied_stage(candidate) is not None
                else self._sentences(candidate, query=request.query)
            )
            slices.append((len(sentences), len(sentences) + len(block)))
            sentences.extend(block)
        try:
            verdicts = (
                self.components.stage_scorer.score(sentences) if sentences else []
            )
            verdicts = _check_verdicts(sentences, verdicts)
        except ModelUnavailable as exc:
            return self.failed(request, str(exc))

        predictions: list[Prediction] = []
        for source_candidate, (lo, hi) in zip(request.candidates, slices, strict=True):
            candidate = self._as_candidate(source_candidate, verdicts[lo:hi])
            bound_ruler = ruler
            bind = getattr(ruler, "with_documents", None)
            if callable(bind):
                bound_ruler = bind(self._documents(source_candidate))
            try:
                if candidate.stage.confirmed:
                    bound_ruler = _bind_stage_calibration(bound_ruler, self.components.stage_scorer)
                outcome = bound_ruler.score(candidate)
            except ModelUnavailable as exc:
                return self.failed(request, str(exc))
            basis = (
                bound_ruler.basis_for(outcome.label)
                if hasattr(bound_ruler, "basis_for")
                else bound_ruler.basis
            )
            if request.query.strip():
                basis += "; query использован для лексического приоритета доказательств перед бюджетом; смысловая релевантность не проверялась"
            if source_candidate.features.stage_code is not None:
                if self._supplied_stage(source_candidate) is not None:
                    basis += "; стадия сохранена из извлечения, версия модели совпадает"
                    if source_candidate.features.stage_prob is None:
                        basis += (
                            "; вероятность стадии не передана, уверенность равна нулю"
                        )
                    elif (
                        source_candidate.features.stage_probs is None
                        and candidate.stage.code in WEAK_SIGNAL_STAGE_CODES
                    ):
                        basis += (
                            "; распределение стадии не передано: вероятность диапазона "
                            "1–4 оценена снизу вероятностью выбранного кода"
                        )
                else:
                    basis += "; стадия пересчитана по доступным источникам: версия модели стадии не совпадает"
            if source_candidate.features.growth_window == "6m/6m":
                basis += (
                    "; тренд — отношение документов за последние 6 полных месяцев к "
                    "предыдущим 6 внутри окна поиска, пороги T1/T2 условные"
                )
            ml = None
            features = list(outcome.explanation)
            if (
                source_candidate.features.stage_code is not None
                or source_candidate.features.observation_date is not None
                or source_candidate.features.growth_ratio is not None
            ):
                ml = MLAssessment.model_validate(
                    {
                        "stage_code": candidate.stage.code,
                        "stage_label": candidate.stage.label_ru,
                        "trend_code": candidate.trend.code,
                        "score": outcome.score,
                        "class": outcome.label,
                        "confidence": outcome.value,
                        "basis": basis,
                    }
                )
                features.extend(ml.feature_values())
            predictions.append(
                Prediction(
                    id=source_candidate.id,
                    label=CLASS_TO_LABEL[outcome.label],
                    value=outcome.value,
                    threshold=ruler.threshold,
                    model=ruler.model_name,
                    version=self.config.service_version,
                    feature_schema=FEATURE_SCHEMA,
                    basis=basis,
                    features=features,
                    ml=ml,
                )
            )
        response = PredictResponse(
            feature_schema=FEATURE_SCHEMA, predictions=predictions
        )
        response.check_against(request)
        return response

    def failed(self, request: PredictRequest, reason: str) -> PredictResponse:
        basis = (
            "Оценка не выполнена: модель недоступна. Числовая оценка правил не "
            f"подставляется вместо модельной. Причина: {reason}"
        )
        return PredictResponse(
            feature_schema=FEATURE_SCHEMA,
            predictions=[
                Prediction(
                    id=candidate.id,
                    label=PredictLabel.FAILED,
                    value=0.0,
                    threshold=self.components.ruler.threshold,
                    model=self.components.ruler.model_name,
                    version=self.config.service_version,
                    feature_schema=FEATURE_SCHEMA,
                    basis=basis,
                    features=[
                        FeatureContribution(
                            name="Отказ модели", value=reason[:200], contribution=0.0
                        )
                    ],
                )
                for candidate in request.candidates
            ],
        )


__all__ = [
    "Components",
    "InMemorySpanCache",
    "PipelineConfig",
    "name_tokens",
    "pick_stage",
]
