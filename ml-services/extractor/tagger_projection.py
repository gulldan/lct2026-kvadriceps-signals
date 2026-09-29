"""Дословное представление заголовка и аннотации для модели с координатами в полном источнике."""

from __future__ import annotations

import json
from dataclasses import dataclass
from hashlib import sha256
from typing import Literal

from extractor.interfaces import DocumentInputError
from extractor.schemas import Document

InputPolicy = Literal["title_abstract", "full_text_diagnostic"]
TARGET_POLICY = "title-abstract-literal-v1:3000"


#: Дефисы, соединяющие части одного слова («LLM-агент», «state-of-the-art»).
_WORD_HYPHENS = "-‐‑"


def _in_word(text: str, index: int) -> bool:
    char = text[index]
    return char.isalnum() or (
        char in _WORD_HYPHENS
        and 0 < index < len(text) - 1
        and text[index - 1].isalnum()
        and text[index + 1].isalnum()
    )


def word_bounds(text: str, start: int, end: int) -> tuple[int, int]:
    """Расширяет ``[start, end)`` до целых слов ``text``, сохраняя дословность.

    Подсловные смещения XLM-R оставляют обрывки («adversar» из «adversarial»). Слово — буквы
    и цифры любого языка; дефис между ними соединяет части, пробелы и прочая пунктуация — границы.
    На dev это подняло строгий F1 спанов с 0,4972 до 0,5238 (только буквы и цифры: 0,5144).
    """
    while start < end and text[start].isspace():
        start += 1
    while end > start and text[end - 1].isspace():
        end -= 1
    while 0 < start < len(text) and _in_word(text, start - 1) and _in_word(text, start):
        start -= 1
    while 0 < end < len(text) and _in_word(text, end - 1) and _in_word(text, end):
        end += 1
    return start, end


@dataclass(frozen=True)
class TaggerProjection:
    text: str
    # У синтетического разделителя нет координаты в источнике. В целевом представлении не больше
    # 3000 записей; диагностическое тождественное не выделяет карту на весь текст.
    origins: tuple[int | None, ...] | None
    title_range: tuple[int, int] | None
    truncated: bool = False
    selections: tuple[tuple[str, int, int], ...] = ()

    @classmethod
    def build(cls, doc: Document, policy: InputPolicy = "title_abstract"):
        if policy == "full_text_diagnostic":
            return cls(doc.text, None, (0, doc.title_end))
        if policy != "title_abstract":
            raise ValueError(f"unknown tagger input policy: {policy}")
        if not doc.title.strip() and not doc.abstract.strip():
            raise DocumentInputError("пустые заголовок и аннотация для теггера")
        ranges = []
        selections = []
        title_range = None
        for field, value in (("title", doc.title), ("abstract", doc.abstract)):
            if not value.strip():
                continue
            start = doc.text.find(value)
            if start < 0:
                raise DocumentInputError(
                    f"{field}: нет дословного вхождения в полном тексте"
                )
            count = 1
            occurrence = start
            while (occurrence := doc.text.find(value, occurrence + 1)) >= 0:
                count += 1
            selections.append((field, start, count))
            selected = (start, start + len(value))
            ranges.append(selected)
            if field == "title":
                title_range = selected

        # Аннотация из метаданных может уже включать заголовок: объединение выдаётся один раз.
        # Непересекающиеся части сохраняют порядок входа «заголовок, затем аннотация».
        if len(ranges) == 2:
            first, second = ranges
            if max(first[0], second[0]) <= min(first[1], second[1]):
                ranges = [(min(first[0], second[0]), max(first[1], second[1]))]
        parts = []
        origins: list[int | None] = []
        truncated = False
        for index, (start, end) in enumerate(ranges):
            if index:
                remaining = 3000 - len(origins)
                if remaining <= 2:
                    break
                parts.append("\n\n")
                origins.extend((None, None))
            length = min(end - start, 3000 - len(origins))
            if length <= 0:
                break
            parts.append(doc.text[start : start + length])
            origins.extend(range(start, start + length))
            if length < end - start:
                truncated = True
                break
        return cls(
            "".join(parts), tuple(origins), title_range, truncated, tuple(selections)
        )

    def source_span(self, start: int, end: int) -> tuple[int, int] | None:
        """Отклоняет спаны через разрыв и возможно обрезанную последнюю сущность."""
        if not 0 <= start < end <= len(self.text):
            raise ValueError(f"invalid projection offset {start}:{end}")
        if self.origins is None:
            return start, end
        if self.truncated and end == len(self.text):
            return None
        source_start = self.origins[start]
        if source_start is None or any(
            value != source_start + offset
            for offset, value in enumerate(self.origins[start:end])
        ):
            return None
        return source_start, source_start + end - start

    def is_title(self, start: int, end: int) -> bool:
        return self.title_range is not None and (
            self.title_range[0] <= start < end <= self.title_range[1]
        )

    def audit_notes(self) -> list[str]:
        # Выбор обычно однозначен; неоднозначный выбор первого вхождения записывается в охват запроса.
        return [
            f"tagger_projection {field}: ambiguity_count={count}, selected_start={start}"
            for field, start, count in self.selections
            if count > 1
        ]


def input_identity(doc: Document, policy: InputPolicy) -> str:
    return sha256(
        json.dumps(
            {
                "text_sha256": doc.text_sha256,
                "title": doc.title,
                "abstract": doc.abstract,
                "policy": TARGET_POLICY if policy == "title_abstract" else policy,
            },
            ensure_ascii=False,
            sort_keys=True,
            separators=(",", ":"),
        ).encode("utf-8")
    ).hexdigest()
