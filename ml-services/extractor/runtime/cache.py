from __future__ import annotations

import hashlib
import json
import os
import tempfile
import threading
from collections.abc import Sequence
from pathlib import Path

from pydantic import TypeAdapter

from extractor.interfaces import SpanCache
from extractor.schemas import Span, dumps_canonical

_SPANS = TypeAdapter(list[Span])


class CacheConflict(ValueError):
    """Один ключ текста и модели дал разные спаны; остаётся первая запись."""


class CacheCorruptionError(ValueError):
    """Сохранённая запись кэша повреждена; молча считать её промахом нельзя."""


def _payload(spans: Sequence[Span]) -> str:
    return dumps_canonical(list(spans))


def _content(raw: str) -> list[dict]:
    return [{**item, "doc_id": None} for item in json.loads(raw)]


def spans_json(spans: Sequence[Span]) -> str:
    """Без doc_id: запись кэша не привязана к источнику."""
    return dumps_canonical(_content(_payload(spans)))


def _same_spans(first: str, second: str) -> bool:
    # Первая запись сохраняется целиком; идентичность источника игнорируется только при сверке
    # повторной записи: конвейер перепривязывает doc_id при попадании в кэш.
    return _content(first) == _content(second)


class InMemorySpanCache(SpanCache):
    """Потокобезопасный кэш «первая запись побеждает»; чтение возвращает независимые копии Span."""

    def __init__(self) -> None:
        self._store: dict[tuple[str, str], str] = {}
        self._lock = threading.Lock()
        self.hits = 0
        self.misses = 0

    def get(self, text_sha256: str, model_version: str) -> list[Span] | None:
        with self._lock:
            value = self._store.get((text_sha256, model_version))
            if value is None:
                self.misses += 1
                return None
            self.hits += 1
            return _SPANS.validate_json(value)

    def put(self, text_sha256: str, model_version: str, spans: Sequence[Span]) -> None:
        key, value = (text_sha256, model_version), _payload(spans)
        with self._lock:
            previous = self._store.setdefault(key, value)
            if not _same_spans(previous, value):
                raise CacheConflict(
                    "одинаковый ключ кэша дал разные спаны"
                )

    async def health(self) -> bool:
        return True

    def clear(self) -> None:
        with self._lock:
            self._store.clear()
            self.hits = self.misses = 0


class FileSpanCache(SpanCache):
    """Один JSON-конверт на ключ, публикуется атомарно и без перезаписи.

    Читатель не видит недописанный файл; конкурирующие писатели, в том числе из разных
    процессов, сверяются с победителем. Повреждённый кэш — явная ошибка.
    """

    def __init__(self, directory: str | Path) -> None:
        self.directory = Path(directory)
        self.directory.mkdir(parents=True, exist_ok=True)
        self.hits = 0
        self.misses = 0
        self._lock = threading.Lock()

    def _path(self, text_sha256: str, model_version: str) -> Path:
        key = dumps_canonical([text_sha256, model_version]).encode("utf-8")
        return self.directory / f"{hashlib.sha256(key).hexdigest()}.json"

    def _read(self, text_sha256: str, model_version: str) -> list[Span] | None:
        path = self._path(text_sha256, model_version)
        try:
            raw = path.read_text(encoding="utf-8")
        except FileNotFoundError:
            return None
        except UnicodeError as exc:
            raise CacheCorruptionError(
                f"invalid UTF-8 cache entry: {path.name}"
            ) from exc
        try:
            entry = json.loads(raw)
            if (
                entry["text_sha256"] != text_sha256
                or entry["model_version"] != model_version
            ):
                raise ValueError("cache envelope does not match its key")
            return _SPANS.validate_python(entry["spans"])
        except (KeyError, TypeError, ValueError) as exc:
            raise CacheCorruptionError(
                f"invalid span cache entry: {path.name}"
            ) from exc

    def get(self, text_sha256: str, model_version: str) -> list[Span] | None:
        spans = self._read(text_sha256, model_version)
        with self._lock:
            if spans is None:
                self.misses += 1
            else:
                self.hits += 1
        return spans

    def put(self, text_sha256: str, model_version: str, spans: Sequence[Span]) -> None:
        value = _payload(spans)
        entry = dumps_canonical(
            {
                "text_sha256": text_sha256,
                "model_version": model_version,
                "spans": json.loads(value),
            }
        )
        path = self._path(text_sha256, model_version)
        fd, temporary = tempfile.mkstemp(
            prefix=".span-", suffix=".tmp", dir=self.directory
        )
        try:
            with os.fdopen(fd, "w", encoding="utf-8") as handle:
                handle.write(entry)
                handle.flush()
                os.fsync(handle.fileno())
            try:
                os.link(temporary, path)
            except FileExistsError:
                previous = self._read(text_sha256, model_version)
                if previous is None or not _same_spans(_payload(previous), value):
                    raise CacheConflict(
                        "одинаковый ключ кэша дал разные спаны"
                    )
        finally:
            Path(temporary).unlink(missing_ok=True)
