"""Объединение упоминаний технологий: морфология, явные алиасы, плотная и символьная близость.

Модели здесь не грузятся: EmbeddingClient передаёт вызывающий. Символьная близость — косинус
TF-IDF символьных 3–5-грамм, а не редакционное расстояние.
"""

from __future__ import annotations

import re
from collections import Counter, defaultdict, deque
from collections.abc import Mapping, Sequence
from copy import copy
from dataclasses import dataclass
from functools import lru_cache
from pathlib import Path

import numpy as np
import pymorphy3
from sklearn.feature_extraction.text import ENGLISH_STOP_WORDS, TfidfVectorizer

from extractor.interfaces import Canonicalizer as CanonicalizerInterface
from extractor.interfaces import EmbeddingClient, EmbeddingUnavailable
from extractor.schemas import (
    CURRENT_SUBTYPES,
    Candidate,
    Document,
    Span,
    SpanSubtype,
    candidate_id,
    normalize_key,
)
from extractor.evidence import SentenceBoundsIndex, evidence_from_span

_TOKEN = re.compile(r"[^\W_]+", re.UNICODE)
_RU = re.compile(r"[а-я]+\Z")
_ABBREVIATION = re.compile(r"\(([A-ZА-ЯЁ][A-ZА-ЯЁ0-9-]{1,15})\)")


@lru_cache(maxsize=1)
def _morph() -> pymorphy3.MorphAnalyzer:
    return pymorphy3.MorphAnalyzer()


@lru_cache(maxsize=65536)
def _lemma(token: str) -> str:
    return (_morph().parse(token)[0].normal_form.replace("ё", "е")
            if _RU.fullmatch(token) else token)


@lru_cache(maxsize=32768)
def normalize_mention(text: str) -> str:
    tokens = _TOKEN.findall(normalize_key(text).replace("ё", "е"))
    return " ".join(
        _lemma(token)
        for token in tokens
    )


@lru_cache(maxsize=1)
def _name_stoplist() -> frozenset[str]:
    path = Path(__file__).with_name("resources") / "name_stoplist.txt"
    return frozenset(normalize_mention(line) for line in path.read_text(encoding="utf-8").splitlines() if line.strip())


@lru_cache(maxsize=1)
def _name_allowlist() -> frozenset[str]:
    path = Path(__file__).with_name("resources") / "name_allowlist.txt"
    return frozenset(normalize_mention(line) for line in path.read_text(encoding="utf-8").splitlines() if line.strip())


def name_filter_reason(
    name: str, stoplist: frozenset[str], *, confidence: float = 0.0, in_title: bool = False,
) -> str | None:
    key = normalize_mention(name)
    tokens = key.split()
    if not tokens or all(token.isdecimal() for token in tokens):
        return "пустое имя или число"
    if key in ENGLISH_STOP_WORDS or key in {"это", "для", "или", "как", "что", "при", "из", "на", "и"}:
        return "стоп-слово, широкая область или нетехнология"
    if name_stoplisted(name, stoplist):
        return "стоп-слово, широкая область или нетехнология"
    if re.search(r"[a-z]", key) and not re.search(r"[а-я]", key) and len(tokens) < 2 and len(key) < 4:
        if key not in _name_allowlist() and not (confidence >= 0.8 and in_title):
            return "неизвестное короткое латинское имя без уверенного упоминания в заголовке"
    return None


def filter_view(original: str, expansion: str) -> str:
    """Для фильтра сокращение из белого списка представляет само себя.

    Его раскрытие может быть широкой областью из стоп-листа (PQC -> post quantum cryptography),
    и сокращение не должно из-за этого выпасть.
    """
    return original if normalize_mention(original) in _name_allowlist() else expansion


def name_stoplisted(name: str, stoplist: frozenset[str]) -> bool:
    """Только стоп-лист имён, в том числе для поверхностей и алиасов; белый список важнее.

    Английский не лемматизируется, поэтому в списке обе формы (network/networks). Правило
    коротких имён из ``name_filter_reason`` к поверхностям не применяется.
    """
    key = normalize_mention(name)
    return key in stoplist and key not in _name_allowlist()


@dataclass(frozen=True, slots=True)
class CanonicalizerConfig:
    dense_threshold: float = 0.85
    hybrid_dense_threshold: float = 0.75
    string_threshold: float = 0.6
    skip_quarantined: bool = True
    allowed_subtypes: frozenset[SpanSubtype] = CURRENT_SUBTYPES

    def __post_init__(self) -> None:
        if not 0 <= self.hybrid_dense_threshold <= self.dense_threshold <= 1:
            raise ValueError(
                "expected 0 <= hybrid_dense_threshold <= dense_threshold <= 1"
            )
        if not 0 <= self.string_threshold <= 1:
            raise ValueError("string_threshold must be in [0, 1]")


def _definitions(text: str, *, whole_name: bool) -> list[tuple[str, str]]:
    """Кратчайшее предшествующее имя с совпадающими инициалами: «Имя (ABC)».

    Для целого спана или имени допустимы и не начальные буквы (БПЛА); в предложении это
    опасно — захватило бы предшествующие глаголы и другие имена.
    """
    found = []
    recent: deque[re.Match[str]] = deque(maxlen=16)
    cursor = 0
    for match in _ABBREVIATION.finditer(text):
        abbreviation = normalize_key(match[1]).replace("ё", "е").replace("-", "")
        # Отрезок кончается на «(», которая не разрезает слово; слова предшествующих сокращений
        # тоже остаются в окне.
        recent.extend(_TOKEN.finditer(text, cursor, match.start()))
        cursor = match.start()
        words = list(recent)
        end = match.start()
        while end and text[end - 1].isspace():
            end -= 1
        full = None
        for count in range(1, len(words) + 1):
            suffix = words[-count:]
            initials = (
                "".join(word[0][0] for word in suffix).casefold().replace("ё", "е")
            )
            letters = "".join(word[0] for word in suffix).casefold().replace("ё", "е")
            tail = iter(letters[1:])
            # Некоторые сокращения берут несколько букв из одного слова: ВЭЖХ, БПЛА.
            subsequence = letters[0] == abbreviation[0] and all(
                any(letter == wanted for letter in tail) for wanted in abbreviation[1:]
            )
            if initials == abbreviation or subsequence:
                full = text[suffix[0].start() : end]
                break
        if full is None and whole_name and len(words) <= 12:
            full = text[:end].lstrip()
        if full and normalize_mention(full):
            found.append((normalize_mention(match[1]), normalize_mention(full)))
    return found


class _UnionFind:
    def __init__(self, size: int) -> None:
        self.parent = list(range(size))
        self.size = [1] * size

    def find(self, item: int) -> int:
        while self.parent[item] != item:
            self.parent[item] = self.parent[self.parent[item]]
            item = self.parent[item]
        return item

    def union(self, left: int, right: int) -> None:
        left, right = self.find(left), self.find(right)
        if left == right:
            return
        if (self.size[left], -left) < (self.size[right], -right):
            left, right = right, left
        self.parent[right] = left
        self.size[left] += self.size[right]


class Canonicalizer(CanonicalizerInterface):
    """Union-find по упоминаниям; одинаковые канонические имена — явные алиасы.

    ``documents`` нужен только для record_ids, которых нет у Span; конвейер после группировки
    всё равно заполняет их заново. Словари сокращений живут в пределах прогона и предпочитают
    определения из самого документа; неоднозначные глобальные сокращения не раскрываются наугад.
    """

    version = "morph-dense-char-canon@3"

    def __init__(
        self,
        embedding_client: EmbeddingClient,
        config: CanonicalizerConfig | None = None,
        *,
        documents: Mapping[str, Document] | None = None,
    ) -> None:
        self.embedding_client = embedding_client
        self.config = config or CanonicalizerConfig()
        self.documents = dict(documents or {})
        self.name_stoplist = _name_stoplist()
        self.notes: list[str] | None = None

    def with_notes(self, notes: list[str]) -> Canonicalizer:
        bound = copy(self)
        bound.notes = notes
        return bound

    def with_documents(self, documents: Mapping[str, Document]) -> Canonicalizer:
        bound = copy(self)
        bound.documents = dict(documents)
        return bound

    def keep(self, span: Span) -> bool:
        return (
            not (self.config.skip_quarantined and span.quarantined)
            and span.subtype in self.config.allowed_subtypes
            and bool(normalize_mention(span.canonical_ru))
            and bool(normalize_mention(span.text))
        )

    def group(self, spans_by_doc: Mapping[str, Sequence[Span]]) -> list[Candidate]:
        # Один голос на вхождение: разрешение конфликтов не должно зависеть от порядка входа.
        mentions: list[tuple[str, Span]] = []
        for doc_id in sorted(spans_by_doc):
            winners = {}
            for span in spans_by_doc[doc_id]:
                if not self.keep(span):
                    continue
                key = (span.start, span.end)
                previous = winners.get(key)
                if previous is None or span.confidence > previous.confidence or (
                    span.confidence == previous.confidence
                    and span.model_dump_json() < previous.model_dump_json()
                ):
                    winners[key] = span
            mentions.extend((doc_id, winners[key]) for key in sorted(winners))
        if not mentions:
            return []

        local: dict[str, dict[str, set[str]]] = defaultdict(lambda: defaultdict(set))
        global_aliases: dict[str, set[str]] = defaultdict(set)
        # Определения документа одинаковы для всех его упоминаний: документ читается один раз.
        for doc_id in sorted({doc_id for doc_id, _ in mentions}):
            if doc_id in self.documents:
                for abbreviation, full in _definitions(
                    self.documents[doc_id].text, whole_name=False
                ):
                    local[doc_id][abbreviation].add(full)
                    global_aliases[abbreviation].add(full)
        definitions = lru_cache(maxsize=None)(_definitions)
        for doc_id, span in mentions:
            for text, whole in (
                (span.text, True),
                (span.canonical_ru, True),
                (span.sentence or "", False),
            ):
                for abbreviation, full in definitions(text, whole_name=whole):
                    local[doc_id][abbreviation].add(full)
                    global_aliases[abbreviation].add(full)

        def expanded(text: str, doc_id: str) -> str:
            text = _ABBREVIATION.sub("", text)
            key = normalize_mention(text)
            choices = local[doc_id].get(key) or global_aliases.get(key, set())
            return next(iter(choices)) if len(choices) == 1 else key

        surfaces = [expanded(span.text, doc_id) for doc_id, span in mentions]
        canonical = [expanded(span.canonical_ru, doc_id) for doc_id, span in mentions]
        retained = []
        for index, (doc_id, span) in enumerate(mentions):
            reason = name_filter_reason(
                filter_view(span.canonical_ru, canonical[index]), self.name_stoplist,
                confidence=span.confidence, in_title=span.in_title,
            )
            # Теггер отдаёт английские поверхности при русском каноническом имени:
            # поверхность и её алиас сверяются со стоп-листом отдельно.
            if reason is None and any(
                name_stoplisted(text, self.name_stoplist)
                for text in (span.text, filter_view(span.text, surfaces[index]))
            ):
                reason = "поверхность или алиас из стоп-листа имён"
            if reason is None:
                retained.append(index)
            elif self.notes is not None:
                self.notes.append(f"имя отфильтровано: {span.text!r} ({span.canonical_ru!r}); документ {doc_id}: {reason}")
        mentions = [mentions[index] for index in retained]
        surfaces = [surfaces[index] for index in retained]
        canonical = [canonical[index] for index in retained]
        if not mentions:
            return []
        texts = sorted(set(surfaces))
        try:
            # Уникальные нормализованные имена служат кэшем в пределах запроса.
            batches = []
            size = max(1, self.embedding_client.config.max_batch)
            for start in range(0, len(texts), size):
                batch = texts[start:start + size]
                vectors = np.asarray(self.embedding_client.embed(batch), dtype=np.float64)
                if vectors.shape != (len(batch), self.embedding_client.config.dim):
                    raise EmbeddingUnavailable("неверная форма пакета эмбеддингов")
                batches.append(vectors)
            dense = np.concatenate(batches)
        except (ValueError, TypeError) as exc:
            raise EmbeddingUnavailable(
                "некорректный ответ клиента эмбеддингов"
            ) from exc
        expected = (len(texts), self.embedding_client.config.dim)
        if dense.shape != expected or not np.isfinite(dense).all():
            raise EmbeddingUnavailable(
                "неверное число, размерность или значения эмбеддингов"
            )
        norms = np.linalg.norm(dense, axis=1)
        if not np.isfinite(norms).all() or np.any(norms == 0):
            raise EmbeddingUnavailable("нулевой или некорректный вектор эмбеддинга")
        dense = dense / norms[:, None]
        # Пробелы по краям дают 3-граммы коротким именам вроде AI без униграмм.
        sparse = TfidfVectorizer(
            analyzer="char", ngram_range=(3, 5), lowercase=False
        ).fit_transform([f" {text} " for text in texts])
        union = _UnionFind(len(mentions))
        representatives: dict[str, int] = {}
        canonical_representatives: dict[str, int] = {}
        for index, (surface, key) in enumerate(zip(surfaces, canonical, strict=True)):
            if surface in representatives:
                union.union(index, representatives[surface])
            else:
                representatives[surface] = index
            if key in canonical_representatives:
                union.union(index, canonical_representatives[key])
            else:
                canonical_representatives[key] = index

        # Построчное сравнение не выделяет плотную матрицу близости N × N.
        for left, text in enumerate(texts):
            similarities = dense[left + 1 :] @ dense[left]
            left_mention = representatives[text]
            sparse_left = None
            for offset in np.flatnonzero(
                similarities >= self.config.hybrid_dense_threshold
            ):
                right = left + 1 + int(offset)
                right_mention = representatives[texts[right]]
                if union.find(left_mention) == union.find(right_mention):
                    continue
                cosine = similarities[offset]
                if cosine >= self.config.dense_threshold:
                    union.union(left_mention, right_mention)
                    continue
                if sparse_left is None:
                    sparse_left = sparse[left]
                lexical = float(sparse_left.multiply(sparse[right]).sum())
                if lexical >= self.config.string_threshold:
                    union.union(left_mention, right_mention)

        buckets: dict[int, list[tuple[str, Span]]] = defaultdict(list)
        keys_by_root: dict[int, set[str]] = defaultdict(set)
        for index, mention in enumerate(mentions):
            root = union.find(index)
            buckets[root].append(mention)
            keys_by_root[root].add(canonical[index])
        bounds = {doc_id: SentenceBoundsIndex(doc.text) for doc_id, doc in self.documents.items()
                  if sum(span.start for span in spans_by_doc.get(doc_id, ())) > len(doc.text)}
        candidates = []
        for root, bucket in buckets.items():
            # Фоновые упоминания TECH участвуют в алиасах и происхождении, но кандидата порождает
            # только явно допущенное.
            if not any(span.is_candidate for _, span in bucket):
                continue
            bucket_keys = sorted(keys_by_root[root])
            # Короткое сокращение сохраняет доказательства, но отображается
            # полным именем, только если оно явно раскрыто в документе.
            names = Counter(span.text for _, span in bucket if name_filter_reason(
                span.text, self.name_stoplist, confidence=span.confidence, in_title=span.in_title,
            ) is None)
            canonical_names = Counter(span.canonical_ru for _, span in bucket if name_filter_reason(
                span.canonical_ru, self.name_stoplist, confidence=span.confidence, in_title=span.in_title,
            ) is None)
            canonical_names = canonical_names or Counter(bucket_keys)
            names = names or canonical_names
            unambiguous = {
                text: count
                for text, count in canonical_names.items()
                if len(global_aliases.get(normalize_mention(text), set())) <= 1
            }
            canonical_names = unambiguous or canonical_names
            name = min(
                names, key=lambda text: (-names[text], normalize_key(text), text)
            )
            canonical_ru = min(
                canonical_names,
                key=lambda text: (-canonical_names[text], normalize_key(text), text),
            )
            doc_ids = sorted({doc_id for doc_id, _ in bucket})
            # Кандидат появляется, если хоть одно упоминание признано головой (p > 0,5); максимум —
            # монотонная оценка того же правила. Прочие упоминания той же технологии получают p около нуля,
            # и среднее наказывало бы кандидата за число упоминаний.
            heads = [span.head_prob for _, span in bucket if span.head_prob is not None]
            candidates.append(
                Candidate(
                    id=candidate_id(bucket_keys[0]),
                    name=name,
                    surface_forms=sorted({span.text for _, span in bucket}),
                    canonical_ru=canonical_ru,
                    doc_ids=doc_ids,
                    record_ids=[
                        doc_id
                        for doc_id in doc_ids
                        if doc_id in self.documents
                        and self.documents[doc_id].is_market_record
                    ],
                    evidence=[
                        evidence_from_span(span, doc_id, self.documents.get(doc_id), bounds=bounds.get(doc_id))
                        for doc_id, span in bucket
                    ],
                    head_prob=max(heads, default=None),
                )
            )
        return sorted(
            candidates, key=lambda item: (normalize_key(item.canonical_ru), item.id)
        )
