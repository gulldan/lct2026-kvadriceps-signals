"""Только локальный torch, без запасного провайдера или модели."""

from __future__ import annotations

import inspect
import json
from collections.abc import Mapping
from pathlib import Path

import numpy as np

from extractor.interfaces import ModelUnavailable
from extractor.registry import ModelSpec

from .base import BatchOutput, InferenceBackend, RuntimeConfig


def check_manifest(spec: ModelSpec, root: Path) -> None:
    """HF не должен подхватить незакреплённый вспомогательный файл или другие веса.

    Суммы перечисленных файлов уже сверил registry.load_model; здесь проверяется полнота
    манифеста, потому что HF сам находит файлы токенизатора и шарды весов.
    """
    if not root.is_dir():
        raise ValueError("the checkpoint path must be a local directory")
    for name in spec.sha256:
        path = Path(name)
        if path.is_absolute() or ".." in path.parts:
            raise ValueError(
                f"artifact must be inside the checkpoint directory: {name}"
            )
        if not (root / path).resolve().is_relative_to(root.resolve()):
            raise ValueError(f"artifact leaves the checkpoint directory: {name}")
    for path in sorted(root.rglob("*")):
        if not path.is_file() or path.name in {"README.md", ".gitattributes"}:
            continue
        name = path.relative_to(root).as_posix()
        if name not in spec.sha256:
            raise ValueError(f"unregistered checkpoint file: {name}")
    if "config.json" not in spec.sha256:
        raise ValueError("config.json must be pinned (label semantics are model data)")
    for name in spec.sha256:
        if name.endswith(".index.json"):
            index = json.loads((root / name).read_text(encoding="utf-8"))
            for shard in index.get("weight_map", {}).values():
                if shard not in spec.sha256:
                    raise ValueError(f"unregistered weight shard: {shard}")


def _role_state(root: Path, spec: ModelSpec, prefix: str) -> dict:
    import torch
    from safetensors import safe_open

    wanted = {f"{prefix}.weight", f"{prefix}.bias"}
    state = {}
    safes = sorted(name for name in spec.sha256 if name.endswith(".safetensors"))
    if safes:
        for name in safes:
            with safe_open(root / name, framework="pt", device="cpu") as handle:
                for key in wanted.intersection(handle.keys()):
                    if key in state:
                        raise ValueError(f"duplicate role parameter: {key}")
                    state[key] = handle.get_tensor(key)
    else:
        for name in sorted(spec.sha256):
            if name == "pytorch_model.bin" or name.startswith("pytorch_model-"):
                weights = torch.load(root / name, map_location="cpu", weights_only=True)
                for key in wanted.intersection(weights):
                    state[key] = weights[key]
    return state


def check_attention(model, expected: str) -> None:
    """Версия рантайма называет реализацию внимания; HF не должен подменить её другой."""
    actual = getattr(model.config, "_attn_implementation", None)
    if actual != expected:
        raise ValueError(
            f"attention implementation {actual!r} differs from requested {expected!r}"
        )


def candidate_head_prefix(hf_config) -> str | None:
    labels = getattr(hf_config, "candidate_id2label", None)
    prefix = getattr(hf_config, "candidate_classifier_name", None)
    if labels is None and prefix is None:
        return None
    if not isinstance(labels, dict) or {int(k): v for k, v in labels.items()} != {
        0: "not_candidate",
        1: "candidate",
    }:
        raise ValueError("candidate_id2label must map 0=not_candidate, 1=candidate")
    if not isinstance(prefix, str) or not prefix.strip():
        raise ValueError("declared candidate head requires candidate_classifier_name")
    return prefix


class TorchBackend(InferenceBackend):
    def __init__(
        self, spec: ModelSpec, root: Path, hf_config, task: str, config: RuntimeConfig
    ) -> None:
        import torch
        from transformers import (
            AutoModelForSequenceClassification,
            AutoModelForTokenClassification,
        )

        self.device = torch.device(config.device)
        if self.device.type == "cuda" and not torch.cuda.is_available():
            raise ModelUnavailable("запрошена CUDA, но CUDA недоступна")
        self.dtype = {
            "fp32": torch.float32,
            "bf16": torch.bfloat16,
            "fp16": torch.float16,
        }[config.torch_dtype]
        if self.dtype == torch.bfloat16:
            with torch.cuda.device(self.device):
                if not torch.cuda.is_bf16_supported():
                    raise ModelUnavailable("устройство CUDA не поддерживает bf16")
        auto = (
            AutoModelForTokenClassification
            if task == "token-classification"
            else AutoModelForSequenceClassification
        )
        self.model, info = auto.from_pretrained(
            root,
            config=hf_config,
            local_files_only=True,
            trust_remote_code=False,
            output_loading_info=True,
            dtype=self.dtype,
            attn_implementation=config.attention,
        )
        check_attention(self.model, config.attention)
        # Иначе HF молча инициализирует отсутствующую голову классификации.
        if (
            info.get("missing_keys")
            or info.get("mismatched_keys")
            or info.get("error_msgs")
        ):
            raise ValueError(f"incomplete checkpoint: {info}")
        self.role_head = None
        self.candidate_head = None
        candidate_prefix = candidate_head_prefix(hf_config)
        if candidate_prefix is not None and task != "token-classification":
            raise ValueError(
                "candidate head is only supported for token classification"
            )
        prefix = getattr(hf_config, "role_classifier_name", "role_classifier")
        unexpected = set(info.get("unexpected_keys", []))
        if task == "token-classification":
            state = _role_state(root, spec, prefix)
            if state:
                weight = state.get(f"{prefix}.weight")
                bias = state.get(f"{prefix}.bias")
                if weight is None or weight.ndim != 2 or weight.shape[0] != 3:
                    raise ValueError("role classifier must have three output classes")
                hidden_size = getattr(hf_config, "hidden_size", weight.shape[1])
                if weight.shape[1] != hidden_size:
                    raise ValueError(
                        "role classifier hidden size does not match encoder"
                    )
                self.role_head = torch.nn.Linear(
                    weight.shape[1], 3, bias=bias is not None
                )
                self.role_head.load_state_dict(
                    {k.rsplit(".", 1)[1]: v for k, v in state.items()}
                )
                self.role_head.to(device=self.device, dtype=self.dtype).eval()
                unexpected -= set(state)
            elif getattr(hf_config, "role_id2label", None):
                raise ValueError(
                    "role labels declared but role classifier weights are absent"
                )
            if candidate_prefix is not None:
                state = _role_state(root, spec, candidate_prefix)
                weight = state.get(f"{candidate_prefix}.weight")
                bias = state.get(f"{candidate_prefix}.bias")
                hidden_size = getattr(hf_config, "hidden_size", None)
                if weight is None or tuple(weight.shape) != (2, hidden_size):
                    raise ValueError(
                        "candidate classifier must have shape (2, hidden_size)"
                    )
                if bias is None or tuple(bias.shape) != (2,):
                    raise ValueError("candidate classifier bias must have shape (2,)")
                self.candidate_head = torch.nn.Linear(hidden_size, 2)
                self.candidate_head.load_state_dict({"weight": weight, "bias": bias})
                self.candidate_head.to(device=self.device, dtype=self.dtype).eval()
                unexpected -= set(state)
        if unexpected:
            raise ValueError(f"unsupported checkpoint parameters: {sorted(unexpected)}")
        self.model.to(self.device).eval()
        self.input_names = set(inspect.signature(self.model.forward).parameters)

    def infer(self, inputs: Mapping[str, np.ndarray]) -> BatchOutput:
        import torch

        tensors = {
            name: torch.as_tensor(value, device=self.device)
            for name, value in inputs.items()
            if name in self.input_names
        }
        with torch.inference_mode():
            output = self.model(
                **tensors,
                return_dict=True,
                output_hidden_states=self.role_head is not None
                or self.candidate_head is not None,
            )
            role_logits = getattr(output, "role_logits", None)
            if self.role_head is not None:
                role_logits = self.role_head(output.hidden_states[-1])
            candidate_logits = None
            if self.candidate_head is not None:
                candidate_logits = self.candidate_head(output.hidden_states[-1])
            return BatchOutput(
                logits=output.logits.float().cpu().numpy(),
                role_logits=None
                if role_logits is None
                else role_logits.float().cpu().numpy(),
                candidate_logits=None
                if candidate_logits is None
                else candidate_logits.float().cpu().numpy(),
            )
