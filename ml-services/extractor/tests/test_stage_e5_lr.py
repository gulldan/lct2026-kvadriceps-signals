import json

import numpy as np
import pytest
from transformers import AutoModel

from extractor.interfaces import ModelUnavailable
from extractor.registry import ModelRegistry, ModelsConfig, ModelSpec, sha256_file
from extractor.schemas import P14Platt, SentenceInput
from extractor.stage_e5_lr import (
    E5_REVISION,
    INSTRUCTION,
    E5LRStageScorer,
    linear_probabilities,
)

from . import runtime_fixtures as fixtures

checkpoint = fixtures.checkpoint
offline = fixtures.offline


def pins(root):
    return {p.name: sha256_file(p) for p in root.iterdir() if p.is_file()}


@pytest.fixture
def lr_registry(checkpoint, tmp_path):
    _, encoder_root, classifier, _ = checkpoint()
    encoder = AutoModel.from_config(classifier.config)
    encoder.save_pretrained(encoder_root)
    width = encoder.config.hidden_size
    lr_root = tmp_path / "lr"
    lr_root.mkdir()
    random = np.random.default_rng(3)
    np.savez(
        lr_root / "e5_lr_coefficients.npz",
        coefficients=random.normal(size=(6, width)),
        intercept=random.normal(size=6),
        classes=np.arange(6),
    )
    metadata = {
        "encoder_model": "stage-encoder-e5",
        "encoder_revision": E5_REVISION,
        "instruction": INSTRUCTION,
        "max_len": 192,
        "pooling": "masked-mean-native-dtype-then-float32-l2",
        "cuda_dtype": "bf16",
        "variant": "sentence",
        "temperature": 1.0,
        "calibration_status": "unidentified",
    }
    (lr_root / "stage_lr.json").write_text(json.dumps(metadata))
    registry = ModelRegistry(
        ModelsConfig(
            service_version="weak-signal-2026-09-21.3",
            models={
                "stage-v1": ModelSpec(
                    name="stage-v1",
                    version="lr-test",
                    backend="e5_lr",
                    path=str(lr_root),
                    sha256=pins(lr_root),
                    max_len=192,
                ),
                "stage-encoder-e5": ModelSpec(
                    name="stage-encoder-e5",
                    version=E5_REVISION,
                    backend="torch",
                    path=str(encoder_root),
                    sha256=pins(encoder_root),
                    max_len=192,
                    dim=width,
                ),
            },
        )
    )
    return registry, lr_root, encoder_root


@pytest.mark.parametrize(
    "fault",
    [
        "instruction",
        "revision",
        "nan",
        "shape",
        "classes",
        "tamper_encoder",
        "extra_file",
    ],
)
def test_bad_lr_or_encoder_package_fails_closed(lr_registry, fault):
    registry, lr_root, encoder_root = lr_registry
    if fault in ("instruction", "revision"):
        path = lr_root / "stage_lr.json"
        metadata = json.loads(path.read_text())
        metadata["instruction" if fault == "instruction" else "encoder_revision"] = (
            "wrong"
        )
        path.write_text(json.dumps(metadata))
        registry.spec("stage-v1").sha256 = pins(lr_root)
    elif fault == "tamper_encoder":
        (encoder_root / "config.json").write_text("{}")
    elif fault == "extra_file":
        (lr_root / "unexpected.json").write_text("{}")
    else:
        path = lr_root / "e5_lr_coefficients.npz"
        with np.load(path) as data:
            values = {key: data[key].copy() for key in data.files}
        if fault == "nan":
            values["coefficients"][0, 0] = np.nan
        if fault == "shape":
            values["coefficients"] = values["coefficients"][:5]
        if fault == "classes":
            values["classes"][0] = 7
        np.savez(path, **values)
        registry.spec("stage-v1").sha256 = pins(lr_root)
    scorer = E5LRStageScorer(registry)
    with pytest.raises(ModelUnavailable):
        scorer.score(
            [SentenceInput(doc_id="d", sent_start=0, sent_end=6, sentence="method")]
        )
    assert scorer.runtime.backend is None


@pytest.mark.parametrize("value", [None, {"a": 0.8435, "b": -1.1996}, {"a": -1, "b": 0}])
def test_p14_platt_is_optional_beside_the_temperature(lr_registry, value):
    registry, lr_root, _ = lr_registry
    if value is not None:
        path = lr_root / "stage_lr.json"
        path.write_text(json.dumps({**json.loads(path.read_text()), "p14_platt": value}))
        registry.spec("stage-v1").sha256 = pins(lr_root)
    scorer = E5LRStageScorer(registry)
    if value is None:
        assert scorer.p14_platt is None  # бандл v1.1: тождественное преобразование
    elif value["a"] > 0:
        assert scorer.p14_platt == P14Platt(0.8435, -1.1996)
    else:
        with pytest.raises(ModelUnavailable, match="p14_platt"):
            _ = scorer.p14_platt
    # Калибровка читается из пакета LR без загрузки кодировщика E5.
    assert scorer.runtime.backend is None


def test_linear_class_order_and_temperature_are_explicit():
    features = np.array([[1.0, 0.0], [0.0, 1.0]])
    coefficients = np.array(
        [[0.0, 0.0], [1.0, 0.0], [0.0, 2.0], [0.0, 0.0], [0.0, 0.0], [0.0, 0.0]]
    )
    classes = np.array([5, 4, 3, 2, 1, 0])
    probs = linear_probabilities(features, coefficients, np.zeros(6), classes)
    hot = linear_probabilities(features, coefficients, np.zeros(6), classes, 2.0)
    assert probs.argmax(axis=1).tolist() == [4, 3]
    assert hot.argmax(axis=1).tolist() == [4, 3]
    assert (hot.max(axis=1) < probs.max(axis=1)).all()


def test_encoder_and_lr_return_normalized_stages_with_verbatim_quotes(lr_registry):
    registry, _, _ = lr_registry
    scorer = E5LRStageScorer(registry)
    sentences = [SentenceInput(doc_id="d", sent_start=0, sent_end=len(text), sentence=text)
                 for text in ["We propose a method.", "A prototype was demonstrated."]]
    first = scorer.score(sentences)
    assert first == scorer.score(sentences)
    assert len(first) == 2
    for verdict, source in zip(first, sentences, strict=True):
        assert verdict.doc_id == source.doc_id
        assert verdict.sentence == source.sentence
        assert 0 <= verdict.code <= 5 and 0 <= verdict.prob <= 1
        assert len(verdict.probs) == 6 and verdict.probs[verdict.code] == verdict.prob
        assert sum(verdict.probs) == pytest.approx(1, abs=1e-5)
        assert all(quote in source.sentence for quote in verdict.quotes)
        if verdict.code:
            assert verdict.quotes
