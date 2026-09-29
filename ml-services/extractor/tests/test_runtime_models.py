from __future__ import annotations


import numpy as np

from extractor.runtime import BatchOutput
from extractor.schemas import Document, SentenceInput, SpanRole
from extractor.stage_hf import HFStageScorer
from extractor.tagger_hf import HFSpanTagger
from extractor.tagger_projection import word_bounds

from . import runtime_fixtures as fixtures
from .runtime_fixtures import VOCAB

checkpoint = fixtures.checkpoint
offline = fixtures.offline


def sentence(text, doc_id="d", start=7):
    return SentenceInput(
        doc_id=doc_id, sent_start=start, sent_end=start + len(text), sentence=text
    )


def _controlled_bio(tagger, monkeypatch, *, all_inside=False, labels=None):
    tagger.runtime.load()
    original = tagger.runtime.backend.infer
    labels = labels or {
        VOCAB["quant"]: 1,
        VOCAB["##um"]: 2,
        VOCAB["computing"]: 2,
        VOCAB["метод"]: 1,
        VOCAB["метода"]: 1,
        VOCAB["методом"]: 1,
    }

    def forward(inputs):
        actual = original(inputs)
        logits = np.full((*inputs["input_ids"].shape, 3), -10.0)
        for row, values in enumerate(inputs["input_ids"]):
            for col, token in enumerate(values):
                label = 2 if all_inside else labels.get(int(token), 0)
                logits[row, col, label] = 10.0
        return BatchOutput(logits, actual.role_logits, actual.candidate_logits)

    monkeypatch.setattr(tagger.runtime.backend, "infer", forward)


def test_bio_offsets_subwords_roles_candidates_and_canonical_names(
    checkpoint, monkeypatch
):
    registry, _, _, _ = checkpoint(
        metadata={"canonical_names": {"quantum computing": "квантовые вычисления"}}
    )
    tagger = HFSpanTagger(registry, "toy")
    _controlled_bio(tagger, monkeypatch)
    doc = Document(
        doc_id="x",
        source="s",
        title="Quantum computing",
        abstract="We propose quantum computing. Using quantum computing! Quantum computing.",
    )
    spans = tagger.tag([doc])[0]
    assert [s.text for s in spans] == [
        "Quantum computing",
        "quantum computing",
        "quantum computing",
        "Quantum computing",
    ]
    assert [s.role for s in spans] == [
        SpanRole.BACKGROUND,
        SpanRole.CONTRIBUTION,
        SpanRole.APPLICATION,
        SpanRole.BACKGROUND,
    ]
    assert [s.is_candidate for s in spans] == [True, True, False, False]
    assert [s.in_title for s in spans] == [True, False, False, False]
    assert {s.canonical_ru for s in spans} == {"квантовые вычисления"}
    assert [s.start for s in spans] == sorted(s.start for s in spans)
    assert all(s.confidence == round(s.confidence, 6) for s in spans)
    for span in spans:
        span.check_against(doc)
        assert span.text in span.sentence


def test_word_bounds_complete_fragments_and_hyphenated_words():
    text = "An adversarial LLM-агент, нейросетевой (LoRA) и A–B."

    def snapped(fragment, occurrence=0):
        start = text.index(fragment) + occurrence
        return text[slice(*word_bounds(text, start, start + len(fragment) - occurrence))]

    assert snapped("adversar") == "adversarial"
    assert snapped("LLM") == snapped("агент") == snapped("M-аг") == "LLM-агент"
    assert snapped("нейросет") == snapped("сетевой") == "нейросетевой"
    assert snapped("LoRA") == "LoRA"  # скобки и запятые — границы слова
    assert snapped("A–B")[:1] == "A" and snapped("B.")[:1] == "B"  # тире не соединяет слово
    assert word_bounds(text, 2, 15) == (3, 14)  # пробелы по краям обрезаются


def test_decoder_snaps_fragments_to_words_and_merges_the_same_word(checkpoint, monkeypatch):
    registry, _, _, _ = checkpoint()
    doc = Document(doc_id="x", source="s", title="Quantum computing.")
    for labels in ({VOCAB["quant"]: 1}, {VOCAB["quant"]: 1, VOCAB["##um"]: 1}):
        tagger = HFSpanTagger(registry, "toy")
        _controlled_bio(tagger, monkeypatch, labels=labels)
        spans = tagger.tag([doc])[0]
        # «Quant» без «##um» — обрывок; два начала в одном слове — один спан.
        assert [(span.text, span.start, span.end) for span in spans] == [("Quantum", 0, 7)]
        spans[0].check_against(doc)


def test_role_classifier_loaded_from_checkpoint_overrides_markers(
    checkpoint, monkeypatch
):
    registry, _, _, _ = checkpoint(role=True)
    tagger = HFSpanTagger(registry, "toy")
    _controlled_bio(tagger, monkeypatch)
    doc = Document(
        doc_id="x",
        source="s",
        title="Quantum computing",
        abstract="We propose quantum computing.",
    )
    spans = tagger.tag([doc])[0]
    assert tagger.runtime.backend.role_head is not None
    assert [s.role for s in spans] == [SpanRole.APPLICATION, SpanRole.APPLICATION]
    assert [s.is_candidate for s in spans] == [True, False]


def test_orphan_i_and_sentence_boundaries_do_not_merge_documents(
    checkpoint, monkeypatch
):
    registry, _, _, _ = checkpoint()
    tagger = HFSpanTagger(registry, "toy")
    _controlled_bio(tagger, monkeypatch, all_inside=True)
    doc = Document(
        doc_id="x",
        source="s",
        title="Метод",
        abstract="Предложен метод. Using quantum computing.",
    )
    spans = tagger.tag([doc])[0]
    assert [s.text for s in spans] == [
        "Метод",
        "Предложен метод.",
        "Using quantum computing.",
    ]
    assert all("\n" not in s.text for s in spans)
    assert tagger.tag([doc, doc]) == [spans, spans]


def test_stage_temperature_and_reordered_class_ids(checkpoint, monkeypatch):
    registry, _, _, _ = checkpoint(
        task="stage",
        labels={i: str(5 - i) for i in range(6)},
        metadata={"stage_temperature": 2.0},
    )
    scorer = HFStageScorer(registry, "toy")
    scorer.runtime.load()
    monkeypatch.setattr(
        scorer.runtime.backend,
        "infer",
        lambda inputs: BatchOutput(
            np.array([[2.0, 0.0, 0.0, 0.0, 0.0, 0.0]] * len(inputs["input_ids"]))
        ),
    )
    result = scorer.score([sentence("prototype")])[0]
    assert result.code == 5
    assert result.prob == round(float(np.e / (np.e + 5)), 6)
    # Распределение хранится по кодам, а не по индексам логитов.
    assert result.probs == [round(float(1 / (np.e + 5)), 6)] * 5 + [result.prob]
