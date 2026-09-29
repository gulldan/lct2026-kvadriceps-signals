from __future__ import annotations

from dataclasses import replace

import numpy as np
import pytest

from extractor.interfaces import ModelUnavailable
from extractor.runtime import ModelRuntime, RuntimeConfig
from extractor.schemas import Document, SpanRole, dumps_canonical
from extractor.tagger_hf import HFSpanTagger

from . import runtime_fixtures as fixtures
from .test_runtime_models import _controlled_bio

checkpoint = fixtures.checkpoint
offline = fixtures.offline


@pytest.mark.parametrize("role", [False, True])
def test_tagger_spans_at_window_seam_boundary_and_document_end(
    checkpoint, monkeypatch, role
):
    registry, _, _, _ = checkpoint(max_len=512, role=role)
    tagger = HFSpanTagger(registry, "toy", input_policy="full_text_diagnostic")
    _controlled_bio(tagger, monkeypatch)
    # B на 478 и I на 479/480 лежат на шве выбора максимального контекста; второе упоминание
    # на 509 пересекает физическую границу первого окна.
    text = (
        "long " * 478
        + "quantum computing "
        + "long " * 28
        + "quantum computing "
        + "long " * 700
        + "quantum computing."
    )
    doc = Document(doc_id="long", source="test", text=text)
    short = Document(doc_id="short", source="test", text="Quantum computing.")
    first = tagger.tag([doc, short])
    assert isinstance(first, list)
    assert first.windows == (3, 1)
    spans = first[0]
    assert len(spans) == 3
    assert all(span.text == "quantum computing" for span in spans)
    assert [span.start for span in spans] == [
        text.index("quantum computing"),
        text.index("quantum computing", text.index("quantum computing") + 1),
        text.rindex("quantum computing"),
    ]
    assert spans[-1].end == len(text) - 1
    for span in spans:
        span.check_against(doc)
        assert span.role is (SpanRole.APPLICATION if role else SpanRole.BACKGROUND)
    assert dumps_canonical(first) == dumps_canonical(tagger.tag([doc, short]))
    assert first == list(reversed(tagger.tag([short, doc])))
    assert tagger.tag([short, doc]).windows == (1, 3)
    assert first.windows == (3, 1)  # последующие вызовы не перезаписывают метаданные


@pytest.mark.parametrize(
    "tokens,windows", [(0, 1), (510, 1), (511, 2), (958, 2), (959, 3)]
)
def test_exact_window_counts_empty_input_and_complete_offset_coverage(
    checkpoint, tokens, windows
):
    registry, _, _, tokenizer = checkpoint(max_len=512)
    runtime = ModelRuntime("toy", task="token-classification", registry=registry)
    text = "метод " * tokens
    prediction = runtime.predict([text])[0]
    assert prediction.windows == windows
    assert prediction.offsets == tuple(
        map(tuple, tokenizer(text, return_offsets_mapping=True)["offset_mapping"])
    )
    assert prediction.logits.shape == (tokens + 2, 3)


def test_failure_in_later_window_never_returns_partial_document(
    checkpoint, monkeypatch
):
    registry, _, _, _ = checkpoint(max_len=512)
    runtime = ModelRuntime(
        "toy",
        task="token-classification",
        registry=registry,
        config=RuntimeConfig(batch_size=1),
    ).load()
    calls = 0
    original = runtime.backend.infer

    def broken(inputs):
        nonlocal calls
        calls += 1
        output = original(inputs)
        return (
            replace(output, logits=np.full_like(output.logits, np.nan))
            if calls == 2
            else output
        )

    monkeypatch.setattr(runtime.backend, "infer", broken)
    with pytest.raises(ModelUnavailable, match="non-finite"):
        runtime.predict(["long " * 1023])
    assert calls == 2
