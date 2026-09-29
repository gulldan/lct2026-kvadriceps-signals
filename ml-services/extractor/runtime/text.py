from __future__ import annotations

import re
from functools import lru_cache

from extractor.schemas import SpanRole, normalize_key

_BOUNDARY = re.compile(r'(?<!\b[A-ZА-ЯЁ])[.!?…]+[»"\)\]]*(?=\s|$)|\n+')
_WORDS = re.compile(r"[^\W_]+", re.UNICODE)
_CYRILLIC = re.compile(r"[а-яё]")
_CONTRIBUTION = re.compile(
    r"\bwe\s+(?:propose|present|introduce|develop(?:ed)?)\b|"
    r"\b(?:предложен\w*|предлагается|разработан\w*)\b|"
    r"\bв (?:данной|настоящей) работе\b",
    re.IGNORECASE,
)
_APPLICATION = re.compile(
    r"\b(?:using|we used)\b|\b(?:применял\w*|использовал\w*)\b|\bс помощью\b",
    re.IGNORECASE,
)


def sentence_spans(text: str) -> list[tuple[int, int, str]]:
    """Точные срезы источника с пунктуацией, без нормализации пробелов."""
    result = []
    cursor = 0
    for boundary in [*(match.end() for match in _BOUNDARY.finditer(text)), len(text)]:
        chunk = text[cursor:boundary]
        stripped = chunk.strip()
        if stripped:
            start = cursor + len(chunk) - len(chunk.lstrip())
            result.append((start, start + len(stripped), stripped))
        cursor = boundary
    return result


def marker_role(sentence: str) -> SpanRole:
    if _CONTRIBUTION.search(sentence):
        return SpanRole.CONTRIBUTION
    if _APPLICATION.search(sentence):
        return SpanRole.APPLICATION
    return SpanRole.BACKGROUND


class TextNormalizer:
    """Детерминированная словарная лемматизация без скачиваний и внешних сервисов."""

    def __init__(self) -> None:
        import pymorphy3

        self._morph = pymorphy3.MorphAnalyzer()
        self._lemma = lru_cache(maxsize=16384)(self._parse)

    def _parse(self, word: str) -> str:
        if not _CYRILLIC.search(word):
            return word
        analyses = self._morph.parse(word)
        # Оценки pymorphy бывают равны: ничья разрешается независимо от порядка в контейнере.
        best = min(analyses, key=lambda p: (-p.score, p.normal_form, str(p.tag)))
        return best.normal_form.replace("ё", "е")

    def tokens(self, text: str) -> tuple[str, ...]:
        return tuple(self._lemma(word) for word in _WORDS.findall(normalize_key(text)))

    def canonical(self, text: str) -> str:
        return " ".join(self.tokens(text)) or normalize_key(text)
