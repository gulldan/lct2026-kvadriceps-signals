from __future__ import annotations

import threading
from collections.abc import Sequence
from dataclasses import replace

import numpy as np

from extractor.interfaces import ModelUnavailable
from extractor.registry import Backend, ModelRegistry, default_registry
from extractor.schemas import Document

from .torch_backend import TorchBackend, candidate_head_prefix, check_manifest
from .base import Prediction, RuntimeConfig, length_buckets
from .determinism import initialize_torch_determinism
from .tokenization import TokenizerPool


def document_window(document: Document) -> str:
    """Диагностика по полному тексту; прод-теггеры используют TaggerProjection."""
    return document.text


class ModelRuntime:
    def __init__(
        self,
        model_name: str,
        *,
        task: str,
        registry: ModelRegistry | None = None,
        config: RuntimeConfig | None = None,
        max_len: int | None = None,
    ) -> None:
        if task not in {"token-classification", "sequence-classification"}:
            raise ValueError("unsupported inference task")
        if max_len is not None and max_len < 1:
            raise ValueError("max_len must be positive")
        self.registry = registry or default_registry()
        self.model_name = model_name
        self.task = task
        self.config = config or RuntimeConfig()
        self.length_cap = max_len
        self.backend = None
        self.tokenizer = None
        self.hf_config = None
        self.max_len = 0
        self._lock = threading.RLock()

    @property
    def version(self) -> str:
        try:
            spec = self.registry.spec(self.model_name)
            return f"{spec.ref}|{self.config.torch_numerics}"
        except Exception as exc:
            raise ModelUnavailable(
                f"реестр модели {self.model_name} недоступен: {exc}"
            ) from exc

    def load(self) -> ModelRuntime:
        """Явный прогрев необязателен; обычно ошибки загрузки всплывают внутри tag/score."""
        with self._lock:
            if self.backend is not None:
                return self
            try:
                self.registry.load_model(self.model_name, self._load_verified)
            except ModelUnavailable:
                raise
            except Exception as exc:
                raise ModelUnavailable(
                    f"не удалось загрузить {self.model_name}: {exc}"
                ) from exc
        return self

    def _load_verified(self, spec, root) -> None:
        from transformers import AutoConfig, AutoTokenizer

        check_manifest(spec, root)
        if spec.max_len <= 0:
            raise ValueError("registry max_len must be positive")
        hf_config = AutoConfig.from_pretrained(
            root, local_files_only=True, trust_remote_code=False
        )
        tokenizer = AutoTokenizer.from_pretrained(
            root, local_files_only=True, trust_remote_code=False, use_fast=True
        )
        if not tokenizer.is_fast:
            raise ValueError("a fast tokenizer with offset_mapping is required")
        if tokenizer.pad_token_id is None:
            raise ValueError("tokenizer must define a pad token")
        tokenizer.padding_side = "right"
        tokenizer.truncation_side = "right"
        max_len = min(spec.max_len, self.length_cap or spec.max_len)
        if self.task == "token-classification":
            max_len = min(max_len, 512)
        if max_len <= tokenizer.num_special_tokens_to_add(pair=False):
            raise ValueError("max_len leaves no room for text tokens")
        candidate_prefix = candidate_head_prefix(hf_config)
        if candidate_prefix is not None and self.task != "token-classification":
            raise ValueError(
                "candidate head is only supported for token classification"
            )
        if spec.backend is not Backend.TORCH:
            raise ValueError(f"unsupported local backend: {spec.backend}")
        initialize_torch_determinism()
        backend = TorchBackend(spec, root, hf_config, self.task, self.config)
        self.hf_config = hf_config
        self.tokenizer = tokenizer
        self.tokenization = TokenizerPool(tokenizer, self.config.tokenization_workers, self.config.tokenization_chunk_size)
        self.max_len = max_len
        # Бэкенд публикуется, только когда загрузка удалась целиком.
        self.backend = backend

    def predict(self, texts: Sequence[str]) -> list[Prediction]:
        if not texts:
            return []
        # Состояние дополнения токенизатора и ленивая инициализация общие для запросов.
        with self._lock:
            try:
                self.load()
                return self._predict(texts)
            except ModelUnavailable:
                raise
            except Exception as exc:
                raise ModelUnavailable(
                    f"ошибка инференса {self.model_name}: {exc}"
                ) from exc

    def _predict(self, texts: Sequence[str]) -> list[Prediction]:
        if self.task == "token-classification":
            encoded, step = self._encode_windows(texts)
        else:
            encoded = self.tokenization.encode(
                list(texts),
                truncation=True,
                max_length=self.max_len,
                padding=False,
                return_offsets_mapping=True,
                return_attention_mask=True,
            )
        offsets = encoded.pop("offset_mapping")
        sources = encoded.pop("overflow_to_sample_mapping", list(range(len(texts))))
        masks = encoded.pop("special_tokens_mask", None)
        lengths = [len(ids) for ids in encoded["input_ids"]]
        result: list[Prediction | None] = [None] * len(lengths)
        for padded, indices in length_buckets(
            lengths, [texts[index] for index in sources], self.config, self.max_len
        ):
            rows = [
                {name: values[i] for name, values in encoded.items()} for i in indices
            ]
            inputs = self.tokenizer.pad(
                rows, padding="max_length", max_length=padded, return_tensors="np"
            )
            output = self.backend.infer(inputs)
            logits = np.asarray(output.logits)
            expected = (
                (len(indices), padded)
                if self.task == "token-classification"
                else (len(indices),)
            )
            if logits.ndim != len(expected) + 1 or logits.shape[:-1] != expected:
                raise ValueError(
                    f"invalid logits shape: {logits.shape}, expected {expected} + classes"
                )
            if (
                logits.shape[-1] != self.hf_config.num_labels
                or not np.isfinite(logits).all()
            ):
                raise ValueError("invalid class count or non-finite logits")
            roles = output.role_logits
            if roles is not None:
                roles = np.asarray(roles)
                if (
                    roles.shape != (len(indices), padded, 3)
                    or not np.isfinite(roles).all()
                ):
                    raise ValueError("invalid role logits")
            candidates = output.candidate_logits
            if (candidates is not None) != (
                candidate_head_prefix(self.hf_config) is not None
            ):
                raise ValueError(
                    "candidate head declaration must match candidate logits"
                )
            if candidates is not None:
                candidates = np.asarray(candidates)
                if (
                    candidates.shape != (len(indices), padded, 2)
                    or not np.isfinite(candidates).all()
                ):
                    raise ValueError("invalid candidate logits")
            for row, index in enumerate(indices):
                length = lengths[index]
                result[index] = Prediction(
                    logits=(
                        logits[row, :length] if logits.ndim == 3 else logits[row]
                    ).copy(),
                    offsets=tuple(tuple(pair) for pair in offsets[index]),
                    role_logits=None if roles is None else roles[row, :length].copy(),
                    windows=1,
                    candidate_logits=None
                    if candidates is None
                    else candidates[row, :length].copy(),
                )
        if any(item is None for item in result):
            raise ValueError("incomplete inference batch")
        if masks is not None:
            groups: list[list[int]] = [[] for _ in texts]
            for index, source in enumerate(sources):
                groups[source].append(index)
            return [
                _merge_windows(
                    [result[index] for index in group],
                    [masks[index] for index in group],
                    step,
                )
                for group in groups
            ]
        return result

    def _encode_windows(self, texts: Sequence[str]) -> tuple[dict, int]:
        """Документ токенизируется целиком до нарезки окон и добавления служебных токенов.

        Явный Encoding.truncate не теряет хвосты в версиях tokenizers, где усечение при encode
        отбрасывает переполнение на постобработке. Служебные токены добавляет постпроцессор
        самого чекпойнта, сохраняя смещения.
        """
        content_size = self.max_len - self.tokenizer.num_special_tokens_to_add(
            pair=False
        )
        # 512 включает служебные токены: шаг 448 оставляет 62 токена перекрытия при двух служебных,
        # как в бенчмарке. Меньшие пределы реестра берут пропорциональный шаг в пределах содержимого.
        step = min(448, max(1, self.max_len * 7 // 8), content_size)
        full = self.tokenization.encode(
            list(texts),
            add_special_tokens=False,
            truncation=False,
            padding=False,
            return_attention_mask=True,
            verbose=False,
        )
        fields = {
            "input_ids": "ids",
            "attention_mask": "attention_mask",
            "offset_mapping": "offsets",
            "special_tokens_mask": "special_tokens_mask",
        }
        if "token_type_ids" in full:
            fields["token_type_ids"] = "type_ids"
        encoded = {name: [] for name in fields}
        encoded["overflow_to_sample_mapping"] = []
        for source, encoding in enumerate(full.encodings):
            # stride в tokenizers — перекрытие, а не расстояние между началами окон.
            encoding.truncate(
                content_size, stride=content_size - step, direction="right"
            )
            processed = self.tokenizer.backend_tokenizer.post_process(
                encoding, add_special_tokens=True
            )
            for window in (processed, *processed.overflowing):
                for name, attribute in fields.items():
                    encoded[name].append(getattr(window, attribute))
                encoded["overflow_to_sample_mapping"].append(source)
        return encoded, step


def _merge_windows(
    predictions: Sequence[Prediction], masks: Sequence[Sequence[int]], step: int
) -> Prediction:
    """Для каждого токена берётся самое центральное вхождение до BIO-раскодирования.

    Порядковые номера токенов различают подслова даже при совпадающих смещениях (byte fallback);
    смещения в перекрывающихся окнах обязаны совпадать. Ничья — за более ранним окном, независимо
    от порядка пакетов. Выживают только внешние служебные токены, поэтому шов не рвёт спан.
    """
    if not predictions:
        raise ValueError("tokenizer returned no windows for a document")
    if len(predictions) == 1:
        return replace(predictions[0], windows=1)
    roles_present = predictions[0].role_logits is not None
    if any((item.role_logits is not None) != roles_present for item in predictions):
        raise ValueError("inconsistent role logits across windows")
    candidates_present = predictions[0].candidate_logits is not None
    if any(
        (item.candidate_logits is not None) != candidates_present
        for item in predictions
    ):
        raise ValueError("inconsistent candidate logits across windows")
    chosen: dict[int, tuple[int, int, int]] = {}
    prefix = []
    suffix = []
    for window, (prediction, mask) in enumerate(zip(predictions, masks, strict=True)):
        positions = [index for index, special in enumerate(mask) if not special]
        if not positions:
            raise ValueError("empty content in an overflowing window")
        if window == 0:
            prefix = [(window, index) for index in range(positions[0])]
        if window == len(predictions) - 1:
            suffix = [
                (window, index)
                for index in range(positions[-1] + 1, len(prediction.offsets))
            ]
        for local, position in enumerate(positions):
            token = window * step + local
            distance = min(local, len(positions) - local - 1)
            previous = chosen.get(token)
            if previous is not None:
                _, old_window, old_position = previous
                if (
                    predictions[old_window].offsets[old_position]
                    != prediction.offsets[position]
                ):
                    raise ValueError("inconsistent offsets across windows")
            if previous is None or distance > previous[0]:
                chosen[token] = (distance, window, position)
    if sorted(chosen) != list(range(len(chosen))):
        raise ValueError("gap between token windows")
    selected = prefix + [chosen[token][1:] for token in range(len(chosen))] + suffix
    return Prediction(
        logits=np.stack([predictions[w].logits[i] for w, i in selected]),
        offsets=tuple(predictions[w].offsets[i] for w, i in selected),
        role_logits=np.stack([predictions[w].role_logits[i] for w, i in selected])
        if roles_present
        else None,
        windows=len(predictions),
        candidate_logits=np.stack(
            [predictions[w].candidate_logits[i] for w, i in selected]
        )
        if candidates_present
        else None,
    )
