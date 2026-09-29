from __future__ import annotations

from abc import ABC, abstractmethod
from collections.abc import Mapping, Sequence
from dataclasses import dataclass
from typing import Literal

import numpy as np


@dataclass(frozen=True)
class RuntimeConfig:
    """Настройки развёртывания; их смена требует новой версии реестра.

    CPU torch всегда fp32 с eager-вниманием: это эталонная численность, ``cuda_*`` к нему
    не применяются. CUDA только по явному запросу, не автоматически. Ширины корзин фиксируют
    дополнение независимо от других текстов запроса.
    """

    device: str = "cpu"
    cuda_dtype: Literal["fp32", "bf16", "fp16"] = "bf16"
    #: Реализация внимания HF на CUDA. SDPA математически равна eager, но
    #: использует слитые ядра; последние знаки логитов могут отличаться.
    cuda_attention: Literal["sdpa", "eager"] = "sdpa"
    batch_size: int = 16
    bucket_width: int = 32
    tokenization_workers: int = 2
    tokenization_chunk_size: int = 64

    def __post_init__(self) -> None:
        if self.tokenization_workers < 1 or self.tokenization_chunk_size < 1:
            raise ValueError("tokenization workers and chunk size must be positive")
        if self.batch_size < 1 or self.bucket_width < 1:
            raise ValueError("batch_size and bucket_width must be positive")
        if self.cuda_dtype not in {"fp32", "bf16", "fp16"}:
            raise ValueError("cuda_dtype must be fp32, bf16 or fp16")
        if self.cuda_attention not in {"sdpa", "eager"}:
            raise ValueError("cuda_attention must be sdpa or eager")
        if self.device not in {"cpu", "cuda"} and (
            not self.device.startswith("cuda:") or not self.device[5:].isdigit()
        ):
            raise ValueError("device must be cpu, cuda or cuda:N")

    @property
    def is_cuda(self) -> bool:
        return self.device.startswith("cuda")

    @property
    def torch_dtype(self) -> str:
        return self.cuda_dtype if self.is_cuda else "fp32"

    @property
    def attention(self) -> str:
        return self.cuda_attention if self.is_cuda else "eager"

    @property
    def torch_numerics(self) -> str:
        """Входит в версию каждого torch-рантайма и потому в ключи кэшей спанов, стадий и разметки:
        результаты разной численности не делят запись кэша.
        """
        return f"{'cuda' if self.is_cuda else 'cpu'}:{self.torch_dtype}:{self.attention}"


@dataclass(frozen=True)
class BatchOutput:
    logits: np.ndarray
    role_logits: np.ndarray | None = None
    candidate_logits: np.ndarray | None = None


@dataclass(frozen=True)
class Prediction:
    """Смещения — в символах входной строки, без дополнения.

    ``windows`` — число окон модели для входа (1 для короткого); ``None`` — производитель
    без этого счётчика.
    """

    logits: np.ndarray
    offsets: tuple[tuple[int, int], ...]
    role_logits: np.ndarray | None = None
    windows: int | None = None
    candidate_logits: np.ndarray | None = None


class InferenceBackend(ABC):
    @abstractmethod
    def infer(self, inputs: Mapping[str, np.ndarray]) -> BatchOutput:
        """Полный пакет или исключение; частичный пакет не возвращается."""


def length_buckets(
    lengths: Sequence[int], texts: Sequence[str], config: RuntimeConfig, max_len: int
) -> list[tuple[int, list[int]]]:
    buckets: dict[int, list[int]] = {}
    for index, length in enumerate(lengths):
        padded = min(
            max_len,
            ((length + config.bucket_width - 1) // config.bucket_width)
            * config.bucket_width,
        )
        buckets.setdefault(padded, []).append(index)
    result = []
    for padded, indices in sorted(buckets.items()):
        indices.sort(key=lambda i: (lengths[i], texts[i], i))
        for start in range(0, len(indices), config.batch_size):
            result.append((padded, indices[start : start + config.batch_size]))
    return result


def softmax(logits: np.ndarray, temperature: float = 1.0) -> np.ndarray:
    """Устойчивый softmax; ничьи потом разрешаются меньшим id."""
    values = np.asarray(logits, dtype=np.float64) / temperature
    values = np.exp(values - np.max(values, axis=-1, keepdims=True))
    return values / values.sum(axis=-1, keepdims=True)
