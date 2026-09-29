from dataclasses import replace

import numpy as np
import pytest
import torch

from extractor.interfaces import ModelUnavailable
from extractor.schemas import Document, SpanRole
from extractor.tagger_hf import HFSpanTagger

from . import runtime_fixtures as fixtures
from .test_runtime_models import _controlled_bio
from .runtime_fixtures import VOCAB, pin

checkpoint = fixtures.checkpoint
offline = fixtures.offline
METADATA = {
    "candidate_classifier_name": "candidate_classifier",
    "candidate_id2label": {0: "not_candidate", 1: "candidate"},
}


def candidate_checkpoint(
    checkpoint, *, positive=False, classes=2, bias=True, metadata=None, max_len=512
):
    _, root, model, tokenizer = checkpoint(
        metadata=METADATA if metadata is None else metadata, max_len=max_len
    )
    model.candidate_classifier = torch.nn.Linear(
        model.config.hidden_size, classes, bias=bias
    )
    with torch.no_grad():
        model.candidate_classifier.weight.zero_()
        if bias:
            model.candidate_classifier.bias.zero_()
            model.candidate_classifier.bias[int(positive)] = 5
    model.save_pretrained(root)
    return pin(root, max_len=max_len), root, model, tokenizer


@pytest.mark.parametrize("positive", [False, True])
def test_real_candidate_head_overrides_title_and_role_rules(
    checkpoint, monkeypatch, positive
):
    registry, _, _, _ = candidate_checkpoint(checkpoint, positive=positive)
    tagger = HFSpanTagger(registry, "toy")
    _controlled_bio(tagger, monkeypatch)
    document = Document(
        doc_id="a",
        source="test",
        title="Quantum computing",
        abstract="We propose quantum computing. Using quantum computing.",
    )
    spans = tagger.tag([document])[0]
    assert tagger.runtime.backend.candidate_head is not None
    assert tagger.runtime.backend.role_head is None
    assert [s.in_title for s in spans] == [True, False, False]
    assert [s.role for s in spans] == [
        SpanRole.BACKGROUND,
        SpanRole.CONTRIBUTION,
        SpanRole.APPLICATION,
    ]
    assert [s.is_candidate for s in spans] == [positive] * 3
    from extractor.canon import Canonicalizer
    from extractor.tests.fakes import DeterministicEmbeddingClient

    canonicalizer = Canonicalizer(DeterministicEmbeddingClient(), documents={"a": document})
    # «quantum computing» — широкая область стоп-листа; здесь проверяется только голова.
    canonicalizer.name_stoplist = frozenset()
    if positive:
        [candidate] = canonicalizer.group({"a": spans})
        assert candidate.head_prob == max(s.head_prob for s in spans) > 0.5
    else:
        assert canonicalizer.group({"a": spans}) == []


@pytest.mark.parametrize("negative_logit,expected", [(1, True), (2, False), (0, True)])
def test_candidate_decode_averages_probabilities_not_logits_or_votes(
    checkpoint, monkeypatch, negative_logit, expected
):
    registry, _, _, _ = candidate_checkpoint(checkpoint)
    tagger = HFSpanTagger(registry, "toy")
    _controlled_bio(tagger, monkeypatch)
    original = tagger.runtime.backend.infer

    def forward(inputs):
        actual = original(inputs)
        values = np.zeros_like(actual.candidate_logits)
        for row, ids in enumerate(inputs["input_ids"]):
            for col, token in enumerate(ids):
                values[row, col] = (
                    [0, 10] if token == VOCAB["quant"] else [negative_logit, 0]
                )
        return replace(actual, candidate_logits=values)

    monkeypatch.setattr(tagger.runtime.backend, "infer", forward)
    spans = tagger.tag(
        [Document(doc_id="a", source="test", title="Quantum computing.")]
    )[0]
    assert len(spans) == 1 and spans[0].is_candidate is expected
    assert (spans[0].head_prob > 0.5) is expected


def test_declared_missing_candidate_weights_fail_closed(checkpoint):
    registry, _, _, _ = checkpoint(metadata=METADATA)
    with pytest.raises(ModelUnavailable, match="candidate classifier"):
        HFSpanTagger(registry, "toy").runtime.load()
