from __future__ import annotations


import pytest
import torch

from extractor.evidence import EvidenceFinder
from extractor.pipeline import Components, ExtractPipeline
from extractor.runtime import (
    CacheConflict,
    CacheCorruptionError,
    FileSpanCache,
    InMemorySpanCache,
)
from extractor.schemas import Candidate, Document, ExtractRequest, Span, dumps_canonical
from extractor.stage_hf import HFStageScorer
from extractor.tagger_hf import HFSpanTagger

from . import runtime_fixtures as fixtures
from .runtime_fixtures import pin

checkpoint = fixtures.checkpoint
offline = fixtures.offline


@pytest.fixture(params=["memory", "file"])
def cache(request, tmp_path):
    return (
        InMemorySpanCache()
        if request.param == "memory"
        else FileSpanCache(tmp_path / "cache")
    )


def span():
    return Span(
        start=0,
        end=5,
        text="метод",
        subtype="method",
        role="contribution",
        canonical_ru="метод",
        confidence=0.9,
    )


def test_cache_keys_copies_empty_values_and_conflicts(cache):
    key = ("a" * 64, "model@1")
    assert cache.get(*key) is None
    original = span()
    cache.put(*key, [original])
    original.confidence = 0.1
    first = cache.get(*key)
    assert first == [span()]
    first[0].text = "wrong"
    assert cache.get(*key) == [span()]
    assert cache.get("b" * 64, key[1]) is None
    assert cache.get(key[0], "model@2") is None
    cache.put(*key, [span()])
    with pytest.raises(CacheConflict):
        cache.put(*key, [])
    assert cache.get(*key) == [span()]
    cache.put("b" * 64, "model@1", [])
    assert cache.get("b" * 64, "model@1") == []
    assert set(cache.get_many([key, (key[0], "model@2")])) == {key}


def test_file_cache_survives_new_instance_and_uses_safe_paths(tmp_path):
    directory = tmp_path / "cache"
    key = ("a" * 64, "../../версия/1")
    FileSpanCache(directory).put(*key, [span()])
    assert FileSpanCache(directory).get(*key) == [span()]
    files = list(directory.iterdir())
    assert len(files) == 1 and files[0].suffix == ".json"
    assert "метод" in files[0].read_text(encoding="utf-8")


def test_file_cache_corruption_is_not_a_false_cache_miss(tmp_path):
    cache = FileSpanCache(tmp_path)
    cache.put("a", "version", [span()])
    next(tmp_path.glob("*.json")).write_text("{", encoding="utf-8")
    with pytest.raises(CacheCorruptionError):
        cache.get("a", "version")


def test_cold_warm_pipeline_and_duplicate_texts_are_identical(checkpoint, cache):
    registry, root, model, _ = checkpoint()
    with torch.no_grad():
        model.classifier.weight.zero_()
        model.classifier.bias.copy_(torch.tensor([-5.0, 5.0, -5.0]))
    model.save_pretrained(root)
    registry = pin(root)
    stage_registry, _, _, _ = checkpoint(task="stage")
    tagger = HFSpanTagger(registry, "toy")
    scorer = HFStageScorer(stage_registry, "toy")
    pipeline = ExtractPipeline(
        Components(tagger=tagger, stage_scorer=scorer, cache=cache)
    )
    docs = [
        Document(doc_id=name, source="test", title="метод", abstract="Предложен метод.")
        for name in ("b", "a")
    ]
    cold = pipeline.run(ExtractRequest(documents=docs))
    warm = pipeline.run(ExtractRequest(documents=list(reversed(docs))))
    assert cold.candidates
    assert dumps_canonical(cold.candidates) == dumps_canonical(warm.candidates)
    assert cold.coverage.cache_misses == 2 and warm.coverage.cache_hits == 2
    assert not cold.coverage.degraded and not warm.coverage.degraded
    assert {e.doc_id for c in warm.candidates for e in c.evidence} == {"a", "b"}


def test_evidence_finds_ru_inflections_aliases_and_body_after_tagger_window():
    prefix = "Заголовок\n\nКороткая аннотация."
    full = (
        prefix
        + "\n\nПроверены нейронными сетями результаты.\nПрименялась ИНС.\nДругое."
    )
    doc = Document(
        doc_id="d",
        source="test",
        title="Заголовок",
        abstract="Короткая аннотация.",
        text=full,
    )
    candidate = Candidate(name="Нейронная сеть", canonical_ru="нейронная сеть")
    result = EvidenceFinder().find(candidate, doc, ["ИНС"])
    assert len(result) == 2
    assert all(s.sent_start > len(prefix) for s in result)
    assert [s.sentence for s in result] == [
        "Проверены нейронными сетями результаты.",
        "Применялась ИНС.",
    ]
    for item in result:
        assert doc.text[item.sent_start : item.sent_end] == item.sentence
        assert item.candidate_text == "нейронная сеть"


def test_evidence_limit_is_global_deterministic_and_matches_whole_phrases():
    docs = [
        Document(
            doc_id=name,
            source="test",
            title="Обзор",
            abstract="Ии применяется. В России развитие. ИИ.",
        )
        for name in ("b", "a")
    ]
    finder = EvidenceFinder(max_sentences=3)
    found = finder.find("ИИ", docs)
    assert [(s.doc_id, s.sentence) for s in found] == [
        ("a", "Ии применяется."),
        ("a", "ИИ."),
        ("b", "Ии применяется."),
    ]
    assert found == finder.find("ИИ", docs[::-1])
    assert (
        EvidenceFinder().find(
            "цифровой двойник",
            Document(
                doc_id="x", source="s", abstract="Цифровой датчик. Двойник человека."
            ),
        )
        == []
    )
