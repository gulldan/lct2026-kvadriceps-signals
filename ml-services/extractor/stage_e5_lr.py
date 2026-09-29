"""Закреплённый E5 со средним пулингом и сохранённая логистическая регрессия выбранного бейзлайна."""

from __future__ import annotations

import json
import math
import threading
from pathlib import Path

import numpy as np

from extractor.interfaces import ModelUnavailable, StageScorer
from extractor.registry import Backend, ModelRegistry
from extractor.runtime import RuntimeConfig
from extractor.runtime.base import length_buckets, softmax
from extractor.runtime.tokenization import TokenizerPool
from extractor.runtime.determinism import initialize_torch_determinism
from extractor.schemas import P14Platt, SentenceStage, round6

E5_REVISION = "274baa43b0e13e37fafa6428dbc7938e62e5c439"
INSTRUCTION = (
    "Given a sentence from a document, classify the technology readiness stage "
    "it provides evidence for, from 0 (not confirmed) to 5 (mass adoption)"
)


def linear_probabilities(features, coefficients, intercept, classes, temperature=1.0):
    """Логиты шестиклассовой sklearn-модели из сохранённых весов, без десериализации оценщика."""
    logits = features @ coefficients.T + intercept
    local = softmax(logits, temperature)
    result = np.zeros((len(features), 6), dtype=np.float64)
    result[:, classes] = local
    return result


class E5LRRuntime:
    def __init__(self, registry, model_name, encoder_model_name, config):
        self.registry = registry
        self.model_name = model_name
        self.encoder_model_name = encoder_model_name
        self.required_models = (model_name, encoder_model_name)
        self.config = config
        self.backend = None
        self.tokenizer = None
        self.coefficients = self.intercept = self.classes = None
        self.temperature = 1.0
        self.p14_platt = None
        self._lock = threading.RLock()

    @property
    def version(self):
        return (
            f"{self.registry.spec(self.model_name).ref}|"
            f"{self.registry.spec(self.encoder_model_name).ref}|"
            f"e5-mean-f32l2-max192@2|{self.config.torch_numerics}|"
            f"bucket{self.config.bucket_width}:batch{self.config.batch_size}"
        )

    def load(self):
        with self._lock:
            if self.backend is not None:
                return self
            try:
                self.registry.load_model(self.model_name, self._load_classifier)
                self.registry.load_model(self.encoder_model_name, self._load_encoder)
            except ModelUnavailable:
                raise
            except Exception as exc:
                raise ModelUnavailable(f"E5+LR недоступен: {exc}") from exc
        return self

    def load_classifier(self):
        """Только закреплённый пакет LR: калибровка нужна без загрузки кодировщика."""
        with self._lock:
            if self.coefficients is None:
                try:
                    self.registry.load_model(self.model_name, self._load_classifier)
                except ModelUnavailable:
                    raise
                except Exception as exc:
                    raise ModelUnavailable(f"E5+LR недоступен: {exc}") from exc
        return self

    def _load_classifier(self, spec, root):
        if spec.backend is not Backend.E5_LR or spec.max_len != 192:
            raise ValueError("stage model must declare e5_lr/max_len192")
        required = {"stage_lr.json", "e5_lr_coefficients.npz"}
        if set(spec.sha256) != required or {p.name for p in root.iterdir()} != required:
            raise ValueError(
                "LR package must contain exactly the pinned metadata and coefficients"
            )
        metadata = json.loads((root / "stage_lr.json").read_text())
        expected = {
            "encoder_model": self.encoder_model_name,
            "encoder_revision": E5_REVISION,
            "instruction": INSTRUCTION,
            "max_len": 192,
            "pooling": "masked-mean-native-dtype-then-float32-l2",
            "cuda_dtype": "bf16",
            "variant": "sentence",
        }
        if any(metadata.get(key) != value for key, value in expected.items()):
            raise ValueError("LR feature contract differs from the selected baseline")
        encoder = self.registry.spec(self.encoder_model_name)
        if (
            encoder.version != E5_REVISION
            or encoder.backend is not Backend.TORCH
            or encoder.max_len != 192
        ):
            raise ValueError("Wrong E5 encoder revision/backend/max_len")
        with np.load(root / "e5_lr_coefficients.npz", allow_pickle=False) as values:
            if set(values.files) != {"coefficients", "intercept", "classes"}:
                raise ValueError("Invalid LR arrays")
            coefficients = values["coefficients"]
            intercept = values["intercept"]
            classes = values["classes"]
        if (
            coefficients.shape != (6, encoder.dim)
            or intercept.shape != (6,)
            or classes.shape != (6,)
            or not np.issubdtype(classes.dtype, np.integer)
            or set(map(int, classes)) != set(range(6))
        ):
            raise ValueError("Invalid LR shapes/classes")
        if not np.isfinite(coefficients).all() or not np.isfinite(intercept).all():
            raise ValueError("Non-finite LR coefficients")
        temperature = float(metadata.get("temperature", 1.0))
        if not math.isfinite(temperature) or temperature <= 0:
            raise ValueError("Invalid LR temperature")
        # Необязательная калибровка суммы p(1..4); бандлы без неё — тождество.
        p14_platt = P14Platt.parse(metadata.get("p14_platt"))
        self.coefficients, self.intercept, self.classes = (
            coefficients,
            intercept,
            classes,
        )
        self.temperature = temperature
        self.p14_platt = p14_platt

    def _load_encoder(self, spec, root: Path):
        import torch
        from transformers import AutoModel, AutoTokenizer

        from extractor.runtime.torch_backend import check_attention, check_manifest

        check_manifest(spec, root)
        initialize_torch_determinism()
        device = self.config.device
        if device.startswith("cuda") and not torch.cuda.is_available():
            raise ValueError("CUDA unavailable; no CPU fallback")
        dtype = {"bf16": torch.bfloat16, "fp32": torch.float32, "fp16": torch.float16}[self.config.torch_dtype]
        if dtype == torch.bfloat16:
            with torch.cuda.device(device):
                if not torch.cuda.is_bf16_supported():
                    raise ValueError("CUDA bf16 unavailable; no precision fallback")
        tokenizer = AutoTokenizer.from_pretrained(
            root, local_files_only=True, trust_remote_code=False
        )
        # Внимание задаётся явно: неявный выбор HF зависел бы от версии библиотеки,
        # а версия рантайма в ключах кэша называет конкретную реализацию.
        model, info = AutoModel.from_pretrained(
            root,
            dtype=dtype,
            local_files_only=True,
            trust_remote_code=False,
            output_loading_info=True,
            attn_implementation=self.config.attention,
        )
        check_attention(model, self.config.attention)
        if any(
            info.get(key)
            for key in (
                "missing_keys",
                "unexpected_keys",
                "mismatched_keys",
                "error_msgs",
            )
        ):
            raise ValueError(f"E5 encoder weights do not match its config: {info}")
        if model.config.hidden_size != spec.dim:
            raise ValueError("E5 embedding width differs from registry")
        tokenizer.padding_side = "right"
        self.tokenizer = tokenizer
        self.tokenization = TokenizerPool(tokenizer, self.config.tokenization_workers, self.config.tokenization_chunk_size)
        self.backend = model.to(device).eval()

    def encode(self, sentences):
        import torch

        self.load()
        prompts = [
            f"Instruct: {INSTRUCTION}\nQuery: {sentence}" for sentence in sentences
        ]
        output = np.empty((len(prompts), self.coefficients.shape[1]), dtype=np.float32)
        with self._lock, torch.inference_mode():
            encoded = self.tokenization.encode(
                prompts, padding=False, truncation=True, max_length=192,
            ) if prompts else {}
            lengths = [len(row) for row in encoded.get("input_ids", [])]
            for padded, indices in length_buckets(lengths, prompts, self.config, 192):
                rows = [{name: values[index] for name, values in encoded.items()} for index in indices]
                inputs = self.tokenizer.pad(
                    rows, padding="max_length", max_length=padded, return_tensors="pt",
                ).to(self.config.device)
                hidden = self.backend(**inputs).last_hidden_state
                mask = inputs["attention_mask"].unsqueeze(-1).to(hidden.dtype)
                pooled = (hidden * mask).sum(dim=1) / mask.sum(dim=1).clamp(min=1e-9)
                pooled = torch.nn.functional.normalize(pooled.float(), p=2, dim=1)
                output[indices] = pooled.cpu().numpy()
        return output

    def probabilities(self, sentences):
        features = self.encode(sentences)
        if not np.isfinite(features).all():
            raise ValueError("Non-finite E5 features")
        return linear_probabilities(
            features, self.coefficients, self.intercept, self.classes, self.temperature
        )


class E5LRStageScorer(StageScorer):
    def __init__(
        self,
        registry: ModelRegistry,
        model_name="stage-v1",
        *,
        encoder_model_name="stage-encoder-e5",
        config=None,
    ):
        self.runtime = E5LRRuntime(
            registry, model_name, encoder_model_name, config or RuntimeConfig()
        )

    @property
    def version(self):
        return self.runtime.version

    @property
    def p14_platt(self):
        """Калибровка суммы p(1..4) из ``stage_lr.json``; ``None`` — тождество."""
        return self.runtime.load_classifier().p14_platt

    def score(self, sentences):
        if not sentences:
            return []
        try:
            probabilities = self.runtime.probabilities(
                [item.sentence for item in sentences]
            )
            if (
                probabilities.shape != (len(sentences), 6)
                or not np.isfinite(probabilities).all()
            ):
                raise ValueError("Invalid LR output")
            return [
                SentenceStage(
                    doc_id=sentence.doc_id,
                    sent_start=sentence.sent_start,
                    sent_end=sentence.sent_end,
                    sentence=sentence.sentence,
                    code=int(probs.argmax()),
                    quotes=[sentence.sentence],
                    prob=round6(float(probs.max())),
                    probs=[round6(float(value)) for value in probs],
                )
                for sentence, probs in zip(sentences, probabilities, strict=True)
            ]
        except ModelUnavailable:
            raise
        except Exception as exc:
            raise ModelUnavailable(f"E5+LR не смог обработать пакет: {exc}") from exc
