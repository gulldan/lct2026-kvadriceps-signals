"""Поиск доказательств по полному тексту, независимо от окна входа теггера."""

from __future__ import annotations

import re
from bisect import bisect_right

from extractor.schemas import Evidence, Span

from collections.abc import Mapping, Sequence
from copy import copy

from extractor.runtime.text import TextNormalizer, marker_role, sentence_spans
from extractor.schemas import Candidate, Document, SentenceInput

NeedleIndex = dict[str, dict[int, set[tuple[str, ...]]]]


def _contains(haystack: tuple[str, ...], needles: NeedleIndex) -> bool:
    """Точное вхождение подпоследовательности токенов, без приближения подстрокой."""
    for start, first in enumerate(haystack):
        for length, choices in needles.get(first, {}).items():
            if (
                start + length <= len(haystack)
                and haystack[start : start + length] in choices
            ):
                return True
    return False


class EvidenceFinder:
    """Дословные предложения по каноническому имени или переданным поверхностям.

    Предел — на кандидата по всем документам, и порядок документов его не меняет.
    """

    version = "literal-evidence@1"

    def __init__(self, max_sentences: int = 8) -> None:
        if max_sentences < 1:
            raise ValueError("max_sentences must be positive")
        self.max_sentences = max_sentences
        self._normalizer = TextNormalizer()
        # Подготовленный полный текст и индексы алиасов живут только на копии, привязанной к запросу.
        self._prepared = None
        self._needles = None

    def with_documents(self, _documents: Mapping[str, Document]) -> EvidenceFinder:
        bound = copy(self)
        bound._prepared = {}
        bound._needles = {}
        return bound

    def _index(self, names: Sequence[str]) -> NeedleIndex:
        key = tuple(names)
        if self._needles is not None and key in self._needles:
            return self._needles[key]
        index: NeedleIndex = {}
        for name in names:
            tokens = self._normalizer.tokens(name)
            if tokens:
                index.setdefault(tokens[0], {}).setdefault(len(tokens), set()).add(
                    tokens
                )
        if self._needles is not None:
            self._needles[key] = index
        return index

    def _sentences(self, doc: Document):
        # Вызывающий в том же процессе может изменить Document, не пересчитав text_sha256:
        # устаревшие недословные срезы не возвращаются.
        key = (doc.text_sha256, doc.text)
        if self._prepared is not None and key in self._prepared:
            return self._prepared[key]
        sentences = sentence_spans(doc.text)
        # Индекс слов позволяет проверять только предложения с началом алиаса.
        tokenized = [self._normalizer.tokens(sentence) for _, _, sentence in sentences]
        postings = {}
        for index, tokens in enumerate(tokenized):
            for token in set(tokens):
                postings.setdefault(token, []).append(index)
        prepared = (sentences, tokenized, postings)
        if self._prepared is not None:
            self._prepared[key] = prepared
        return prepared

    def find(
        self,
        candidate: Candidate | str,
        docs: Document | Sequence[Document],
        surface_forms: Sequence[str] = (),
    ) -> list[SentenceInput]:
        """Поиск по всему Document.text по именам и алиасам с русской лемматизацией.

        Смещения указывают на исходное предложение, а не на нормализованный текст:
        doc.text[sent_start:sent_end] == sentence. Строковый кандидат (или пустая строка
        с surface_forms) позволяет искать до сборки Candidate.
        """
        canonical = (
            candidate.canonical_ru if isinstance(candidate, Candidate) else candidate
        )
        names = [canonical, *surface_forms]
        if isinstance(candidate, Candidate):
            names.append(candidate.name)
        needles = self._index(names)
        if not needles:
            return []
        documents = [docs] if isinstance(docs, Document) else docs
        result = []
        seen = set()
        for doc in sorted(documents, key=lambda d: (d.doc_id, d.text_sha256)):
            sentences, tokenized, postings = self._sentences(doc)
            possible = sorted({index for first in needles for index in postings.get(first, ())})
            for index in possible:
                start, end, sentence = sentences[index]
                key = (doc.doc_id, start, end)
                if key in seen:
                    continue
                haystack = tokenized[index]
                if not _contains(haystack, needles):
                    continue
                seen.add(key)
                result.append(
                    SentenceInput(
                        doc_id=doc.doc_id,
                        sent_start=start,
                        sent_end=end,
                        sentence=sentence,
                        context_before=sentences[index - 1][2] if index else "",
                        context_after=sentences[index + 1][2]
                        if index + 1 < len(sentences)
                        else "",
                        candidate_text=canonical,
                        role=marker_role(sentence),
                        source_type=doc.source_type,
                        date=doc.date,
                    )
                )
                if len(result) >= self.max_sentences:
                    return result
        return result


SENTENCE_BREAK = re.compile(r"(?<![.\s][A-ZА-ЯЁ])[.!?…]+\s")

SENTENCE_END = re.compile(SENTENCE_BREAK.pattern + r"|[.!?…]+$")


class SentenceBoundsIndex:
    """Префикс документа просматривается один раз для всех упоминаний."""

    def __init__(self, text):
        self.ends = [match.end() for match in SENTENCE_BREAK.finditer(text)]


def sentence_bounds(text: str, start: int, end: int, index=None) -> tuple[int, int]:
    """Границы — :data:`SENTENCE_BREAK` и пустая строка, которая отделяет и заголовок от аннотации."""
    blank_before = text.rfind("\n\n", 0, start)
    left = blank_before + 2 if blank_before >= 0 else 0
    if index is None:
        for match in SENTENCE_BREAK.finditer(text[:start]):
            left = max(left, match.end())
    else:
        pos = bisect_right(index.ends, start)
        if pos:
            left = max(left, index.ends[pos - 1])
    right = len(text)
    blank = text.find("\n\n", end)
    if blank >= 0:
        right = blank
    match = SENTENCE_END.search(text[end:])
    if match:
        right = min(right, end + match.end())
    right = min(right, len(text))
    while left < right and text[left].isspace():
        left += 1
    while right > left and text[right - 1].isspace():
        right -= 1
    return left, right

def evidence_from_span(
    span: Span, doc_id: str, document: Document | None = None, *, bounds=None
) -> Evidence:
    """Смещения предложения, если их можно доказать; иначе — точный срез упоминания."""
    start, end, sentence = span.start, span.end, span.text
    if document is not None:
        start, end = sentence_bounds(document.text, span.start, span.end, bounds)
        sentence = document.text[start:end]
    elif span.sentence and span.sentence.count(span.text) == 1:
        offset = span.sentence.index(span.text)
        if span.start >= offset:
            start = span.start - offset
            sentence = span.sentence
            end = start + len(sentence)
    return Evidence(
        doc_id=doc_id,
        start=start,
        end=end,
        sentence=sentence,
        role=span.role,
        in_title=span.in_title,
    )
