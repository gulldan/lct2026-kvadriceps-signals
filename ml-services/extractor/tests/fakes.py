"""Детерминированные эмбеддинги для тестов без внешнего сервиса."""
import hashlib
import struct
import unicodedata
from collections.abc import Sequence
from extractor.interfaces import EmbeddingClient, EmbeddingClientConfig
from extractor.schemas import round6

class DeterministicEmbeddingClient(EmbeddingClient):
    """Единичные векторы из хэша с настоящей геометрией, без сети.

    Одинаковый текст — одинаковый вектор; годится для проверки кода, но не для измерения качества.
    """

    def __init__(self, config: EmbeddingClientConfig | None = None) -> None:
        self.config = config or EmbeddingClientConfig()

    def embed(self, texts: Sequence[str]) -> list[list[float]]:
        return [self._vector(text) for batch in self.batches(texts) for text in batch]

    def health(self) -> bool:
        return True

    def _vector(self, text: str) -> list[float]:
        dim = self.config.dim
        seed = unicodedata.normalize("NFKC", text).encode("utf-8")
        raw = bytearray()
        counter = 0
        while len(raw) < dim * 4:
            raw += hashlib.sha256(seed + counter.to_bytes(4, "big")).digest()
            counter += 1
        values = [
            (struct.unpack_from(">I", raw, i * 4)[0] / 0xFFFFFFFF) * 2.0 - 1.0
            for i in range(dim)
        ]
        norm = sum(value * value for value in values) ** 0.5 or 1.0
        return [round6(value / norm) for value in values]
