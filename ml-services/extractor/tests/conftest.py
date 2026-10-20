"""Общие фикстуры на демо-разметке: без сети, весов и часов."""

from __future__ import annotations

import json
from pathlib import Path

import pytest

from extractor.api import create_app
from extractor.outputs import ExtractPipeline, PredictService
from extractor.pipeline import Components
from extractor.registry import default_registry
from extractor.schemas import Document, SpanAnnotation
from extractor.stubs import DictionarySpanTagger

FIXTURES = Path(__file__).parent / "fixtures"


def load(name: str) -> dict:
    return json.loads((FIXTURES / name).read_text(encoding="utf-8"))


@pytest.fixture(scope="session")
def documents() -> list[Document]:
    return [Document.model_validate(item) for item in load("documents.json")["documents"]]


@pytest.fixture(scope="session")
def annotations() -> list[SpanAnnotation]:
    return [
        SpanAnnotation.model_validate(item)
        for item in load("spans_final.json")["annotations"]
    ]




@pytest.fixture(scope="session")
def handoff_request() -> dict:
    """Пример /predict из README.md (раздел «Стык с радаром») байт в байт."""
    return load("predict_request.json")["request"]


@pytest.fixture(scope="session")
def demo_predict_request() -> dict:
    request = load("predict_request_demo.json")["request"]
    # В сохранённой демонстрации чужая версия: подставляем текущую, не трогая
    # неизменяемый пример контракта.
    request["model_version"] = default_registry().service_version
    return request


@pytest.fixture(scope="session")
def service_version() -> str:
    return default_registry().service_version


@pytest.fixture
def components(annotations: list[SpanAnnotation]) -> Components:
    return Components(tagger=DictionarySpanTagger.from_annotations(annotations))


@pytest.fixture
def pipeline(components: Components) -> ExtractPipeline:
    return ExtractPipeline(components)


@pytest.fixture
def predict_service() -> PredictService:
    return PredictService()


@pytest.fixture
def app(pipeline: ExtractPipeline, predict_service: PredictService):
    return create_app(extract_pipeline=pipeline, predict_service=predict_service)


@pytest.fixture
def client(app):
    from fastapi.testclient import TestClient

    with TestClient(app) as test_client:
        yield test_client
