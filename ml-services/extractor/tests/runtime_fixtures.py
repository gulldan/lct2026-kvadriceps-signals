from __future__ import annotations

import socket
from pathlib import Path

import pytest
import torch
import transformers
import yaml
from transformers import (
    AutoConfig,
    AutoModelForSequenceClassification,
    AutoModelForTokenClassification,
    BertTokenizerFast,
)

from extractor.registry import ModelRegistry, sha256_file

VOCAB = {
    token: index
    for index, token in enumerate(
        [
            "[PAD]",
            "[UNK]",
            "[CLS]",
            "[SEP]",
            "[MASK]",
            ".",
            ",",
            "!",
            "?",
            "we",
            "propose",
            "using",
            "quant",
            "##um",
            "computing",
            "a",
            "method",
            "background",
            "prototype",
            "pilot",
            "long",
            "text",
            "title",
            "abstract",
            "предложен",
            "применялась",
            "квантовый",
            "метод",
            "метода",
            "методом",
            "нейронные",
            "сети",
            "цифровой",
            "двойник",
            "ё",
            "технология",
        ]
    )
}


def pin(root: Path, *, name: str = "toy", backend: str = "torch", max_len: int = 64):
    config = {
        "service_version": "weak-signal-2026-09-21.1",
        "root": ".",
        "models": {
            name: {
                "name": name,
                "version": "test-1",
                "backend": backend,
                "path": root.name,
                "max_len": max_len,
                "sha256": {
                    p.relative_to(root).as_posix(): sha256_file(p)
                    for p in sorted(root.rglob("*"))
                    if p.is_file()
                },
            }
        },
    }
    path = root.parent / f"{name}-models.yaml"
    path.write_text(yaml.safe_dump(config), encoding="utf-8")
    return ModelRegistry.load(path)


def _toy_tokenizer(tmp_path):
    # Образ закрепляет transformers 4.57 (vocab_file), локальная среда — 5.x (vocab).
    if int(transformers.__version__.split(".")[0]) >= 5:
        return BertTokenizerFast(vocab=VOCAB, do_lower_case=True, strip_accents=False)
    vocab = tmp_path / "toy-vocab.txt"
    vocab.write_text("\n".join(sorted(VOCAB, key=VOCAB.get)) + "\n", encoding="utf-8")
    return BertTokenizerFast(vocab_file=str(vocab), do_lower_case=True, strip_accents=False)


@pytest.fixture
def offline(monkeypatch):
    def deny_network(*args, **kwargs):
        raise AssertionError("runtime tests must never access the network")

    monkeypatch.setattr(socket.socket, "connect", deny_network)
    monkeypatch.setattr(socket, "create_connection", deny_network)


@pytest.fixture
def checkpoint(tmp_path, offline):
    def build(task="token", *, max_len=64, labels=None, role=False, metadata=None):
        root = tmp_path / f"model-{len(list(tmp_path.glob('model-*')))}"
        root.mkdir()
        tokenizer = _toy_tokenizer(tmp_path)
        tokenizer.save_pretrained(root)
        id2label = labels or (
            {0: "O", 1: "B-TECH", 2: "I-TECH"}
            if task == "token"
            else {i: str(i) for i in range(6)}
        )
        config = AutoConfig.for_model(
            "bert",
            vocab_size=len(VOCAB),
            hidden_size=16,
            num_hidden_layers=1,
            num_attention_heads=2,
            intermediate_size=24,
            max_position_embeddings=512,
            hidden_dropout_prob=0.4,
            attention_probs_dropout_prob=0.4,
            id2label=id2label,
            label2id={v: k for k, v in id2label.items()},
            **(metadata or {}),
        )
        auto = (
            AutoModelForTokenClassification
            if task == "token"
            else AutoModelForSequenceClassification
        )
        with torch.random.fork_rng():
            torch.manual_seed(31)
            model = auto.from_config(config)
            if role:
                model.role_classifier = torch.nn.Linear(config.hidden_size, 3)
                with torch.no_grad():
                    model.role_classifier.weight.zero_()
                    model.role_classifier.bias.copy_(torch.tensor([0.0, -3.0, 3.0]))
            model.save_pretrained(root)
        return pin(root, max_len=max_len), root, model, tokenizer

    old_threads = torch.get_num_threads()
    torch.set_num_threads(1)
    try:
        yield build
    finally:
        torch.set_num_threads(old_threads)
