"""Регрессии кэша, координат, ограниченного транспорта и численного паритета."""

from dataclasses import replace

from fastapi.testclient import TestClient

from extractor.api import BodyLimit, create_app
from extractor.canon import Canonicalizer
from extractor.evidence import EvidenceFinder, SentenceBoundsIndex, sentence_bounds
from extractor.interfaces import EmbeddingClientConfig, ModelUnavailable
from extractor.outputs import ExtractPipeline, PredictService
from extractor.pipeline import Components
from extractor.schemas import Document, ExtractRequest, SentenceInput, Span
from extractor.stage_cache import EvidenceStageCache
from extractor.stubs import KeywordStageScorer
from extractor.tests.fakes import DeterministicEmbeddingClient



class CountingStage(KeywordStageScorer):
    def __init__(self):
        self.calls = 0
        self.fail = False

    def score(self, sentences):
        self.calls += 1
        if self.fail:
            raise ModelUnavailable("синтетический отказ")
        return super().score(sentences)


class CountingFinder(EvidenceFinder):
    calls = []

    def find(self, *args, **kwargs):
        self.calls.append(1)
        return super().find(*args, **kwargs)


def test_stage_and_evidence_cache_hit_miss_versions_and_isolation():
    finder, scorer = CountingFinder(), CountingStage()
    finder.calls = []
    components = Components(stage_scorer=scorer, evidence_finder=finder)
    pipeline = ExtractPipeline(components)
    def request(text="Разработан цифровой двойник. Испытан прототип.", **metadata):
        return ExtractRequest(documents=[Document(doc_id="d", source="synthetic", text=text, **metadata)])
    source = request()
    cold = pipeline.run(source)
    warm = pipeline.run(source)
    assert cold.candidates == warm.candidates
    assert scorer.calls == len(finder.calls) == 1
    warm.candidates[0].evidence[0].sentence = "повреждено вызывающим кодом"
    assert pipeline.run(source).candidates == cold.candidates
    for changed in [request(text=source.documents[0].text + " Новый текст."),
                    request(source_type="journal_article"), source.model_copy(update={"query": "прототип"})]:
        pipeline.run(changed)
    assert scorer.calls == len(finder.calls) == 4
    scorer.version = "stub-keywords@2"
    pipeline.run(source)
    assert scorer.calls == len(finder.calls) == 5
    pipeline.config = replace(pipeline.config, service_version="weak-signal-2026-09-22.1")
    pipeline.run(source)
    assert scorer.calls == len(finder.calls) == 6
    # Одинаковый текст другого источника получает его собственные координаты.
    renamed = ExtractRequest(documents=[source.documents[0].model_copy(update={"doc_id": "renamed"})])
    rebound = pipeline.run(renamed)
    assert scorer.calls == 6
    assert all(e.doc_id == "renamed" for c in rebound.candidates for e in c.evidence)
    components.stage_cache.clear()
    scorer.fail = True
    assert pipeline.run(source).coverage.degraded
    scorer.fail = False
    assert not pipeline.run(source).coverage.degraded
    assert scorer.calls == 8
    bounded = EvidenceStageCache(max_entries=1)
    block = [SentenceInput(doc_id="d", sent_start=0, sent_end=5, sentence="метод")]
    bounded.put("one", block, [], scorer.score(block))
    bounded.put("two", block, [], scorer.score(block))
    assert bounded.get("one", "d") is None and bounded.get("two", "d") is not None


def test_sentence_index_matches_original_at_every_position():
    text = 'А. Б. тест!  Метод (ABC).\n\nТекст?!… Ещё строка\n\nКонец.'
    index = SentenceBoundsIndex(text)
    for start in range(len(text)):
        for end in range(start + 1, len(text) + 1):
            assert sentence_bounds(text, start, end, index) == sentence_bounds(text, start, end)


def test_canonical_embedding_batches_cache_names_and_order():
    class Embeddings(DeterministicEmbeddingClient):
        def __init__(self):
            super().__init__(EmbeddingClientConfig(dim=32))
            self.calls = []
        def embed(self, texts):
            self.calls.append(list(texts))
            return super().embed(texts)
    client = Embeddings()
    spans = [Span(text=f"метод{i}", canonical_ru=f"метод{i}", start=i*20, end=i*20+len(f"метод{i}"),
                  subtype="method", role="contribution", is_candidate=True, confidence=0.9) for i in range(140)]
    canon = Canonicalizer(client)
    first = canon.group({"a": spans, "b": spans})
    assert [len(batch) for batch in client.calls] == [64, 64, 12]
    assert len({name for batch in client.calls for name in batch}) == 140
    assert first == canon.group({"b": list(reversed(spans)), "a": list(reversed(spans))})


def test_http_compact_response_literal_bounded_and_byte_stable():
    components = Components()
    pipeline = ExtractPipeline(components)
    app = create_app(registry=pipeline.registry, extract_pipeline=pipeline,
                     predict_service=PredictService(components, registry=pipeline.registry),
                     max_extract_request_bytes=16 << 20)
    docs = [dict(doc_id=f"d{i:03}", source="synthetic", text="Разработан цифровой двойник. " * 90) for i in range(40)]
    body = {"documents": docs}
    with TestClient(app) as client:
        cold = client.post("/extract", json=body)
        assert cold.status_code == 200, cold.text
        warm = client.post("/extract", json=body)
        assert warm.content == client.post("/extract", json=body).content
        assert cold.json()["candidates"] == warm.json()["candidates"]
        assert "documents" not in warm.json()
        sources = {item["doc_id"]: item["text"] for item in docs}
        for candidate in warm.json()["candidates"]:
            assert len(candidate["evidence"]) <= 24
            assert len(candidate["stage"]["quote"]) <= 2000
            assert sum(len(item["sentence"]) for item in candidate["evidence"]) <= 24000
            for item in candidate["evidence"]:
                assert sources[item["doc_id"]][item["start"]:item["end"]] == item["sentence"]
        assert client.post("/extract", json=docs).status_code == 200
        assert client.post("/extract", content=b'{"documents":[').status_code == 422
        assert client.post("/extract", json={"documents": [{**docs[0], "text_sha256": "0" * 64}]}).status_code == 422
    document = Document.model_validate(docs[0])
    request = ExtractRequest(documents=[document])
    assert request.documents[0] is document and request.documents[0].text is document.text


async def test_body_gate_reuses_bytes_and_rejects_chunked_overflow():
    received = []
    async def app(scope, receive, send):
        received.append((await receive())["body"])
    async def send(message):
        sent.append(message)
    gate = BodyLimit(app, max_bytes=8)
    raw = b"12345678"
    messages = [{"type": "http.request", "body": raw, "more_body": True},
                {"type": "http.request", "body": b""}]
    async def receive():
        return messages.pop(0)
    sent = []
    scope = {"type": "http", "path": "/extract", "headers": []}
    await gate(scope, receive, send)
    assert received == [raw] and received[0] is raw
    messages = [{"type": "http.request", "body": raw, "more_body": True},
                {"type": "http.request", "body": b"9"}]
    await gate(scope, receive, send)
    assert len(received) == 1 and sent[0]["status"] == 413


def test_long_quote_excerpt_keeps_stage_support_and_exact_offsets():
    from extractor.outputs import build_extract_response
    from extractor.schemas import Candidate, CandidateStage, Evidence, ExtractCoverage

    text = "Начало " * 700 + "Прототип испытан. " + "Подробности " * 500
    candidate = Candidate(name="метод", canonical_ru="метод", doc_ids=["d"],
                          stage=CandidateStage(code=2, doc_id="d", quote="Прототип испытан.", prob=0.75),
                          evidence=[Evidence(doc_id="d", start=0, end=len(text), sentence=text, role="contribution")])
    response = build_extract_response([candidate], ExtractCoverage(), {})
    proof = response.candidates[0].evidence[0]
    assert len(proof.sentence) == 2000
    assert proof.sentence == text[proof.start:proof.end]
    assert candidate.stage.quote in proof.sentence
    assert response.candidates[0].stage == candidate.stage
