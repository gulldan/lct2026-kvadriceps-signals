"""Извлечение технологий, доказательств и оценок по методологии слабых сигналов."""

from extractor.schemas import (
    FEATURE_SCHEMA,
    Candidate,
    Document,
    ExtractRequest,
    ExtractResponse,
    PredictRequest,
    PredictResponse,
    SentenceStage,
    Span,
)

__all__ = [
    "FEATURE_SCHEMA",
    "Candidate",
    "Document",
    "ExtractRequest",
    "ExtractResponse",
    "PredictRequest",
    "PredictResponse",
    "SentenceStage",
    "Span",
]
