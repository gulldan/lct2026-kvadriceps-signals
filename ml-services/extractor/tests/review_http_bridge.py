"""Общий HTTP-стенд Python/Go: заменены только модели, обработчики настоящие."""

import json
import os
from tempfile import TemporaryDirectory
import sys

from fastapi.testclient import TestClient

from extractor.api import create_app
from extractor.interfaces import ModelUnavailable
from extractor.outputs import ExtractPipeline, PredictService
from extractor.pipeline import Components
from extractor.registry import default_registry
from extractor.runtime.cache import FileSpanCache
from extractor.schemas import Document
from extractor.stubs import DictionarySpanTagger, KeywordStageScorer
from extractor.tagger_projection import TaggerProjection


class ReviewStage(KeywordStageScorer):
    version = default_registry().spec("stage-v1").ref

    def __init__(self):
        self.calls = 0
        self.fail = False

    def score(self, sentences):
        self.calls += 1
        if self.fail:
            raise ModelUnavailable("временный отказ модели")
        return [item.model_copy(update={"prob": 0.99}) for item in super().score(sentences)]


class ReviewTagger(DictionarySpanTagger):
    """Словарь вместо весов, но входной контракт настоящего теггера: заголовок и
    непустая аннотация дословно входят в text, иначе документ не размечается."""

    def input_identity(self, doc):
        TaggerProjection.build(doc)
        return doc.text_sha256

    def input_notes(self, doc):
        return TaggerProjection.build(doc).audit_notes()


def review_app(*, max_response=16 << 20):
    stage = ReviewStage()
    vocabulary = {
        name: {"canonical_ru": name, "subtype": "method"}
        for name in ["Фазовый детектор", "Квантовый датчик"]
        + [f"Технология {i:03d}" for i in range(100)]
    }
    components = Components(tagger=ReviewTagger(vocabulary), stage_scorer=stage)
    return create_app(
        extract_pipeline=ExtractPipeline(components), predict_service=PredictService(components),
        max_extract_request_bytes=32 << 20, max_extract_response_bytes=max_response
    ), stage


def inject_storage_fault(app, fault, documents, directory):
    if fault == "cache":
        cache = FileSpanCache(directory)
        for raw in documents:
            doc = Document.model_validate(raw)
            cache._path(doc.text_sha256, app.state.extract_pipeline.components.tagger.version).write_text("{broken")
        app.state.extract_pipeline.components.cache = cache
    elif fault == "span_storage":
        class LostCache:
            def get(self, *args):
                from asyncpg import ConnectionDoesNotExistError
                raise ConnectionDoesNotExistError("connection lost")
        app.state.extract_pipeline.components.cache = LostCache()


def main():
    fault = os.getenv("EXTRACTOR_REVIEW_FAULT", "")
    app, stage = review_app(max_response=256 if fault == "response" else 16 << 20)
    with TestClient(app) as client, TemporaryDirectory() as directory:
        for line in sys.stdin:
            request = json.loads(line)
            stage.fail = request.get("fail_stage", False)
            inject_storage_fault(app, fault, request["body"].get("documents", []), directory)
            response = client.post(request["path"], json=request["body"])
            print(json.dumps({
                "status": response.status_code, "body": response.json(),
                "headers": dict(response.headers), "stage_calls": stage.calls,
            }, ensure_ascii=False), flush=True)


if __name__ == "__main__":
    main()
