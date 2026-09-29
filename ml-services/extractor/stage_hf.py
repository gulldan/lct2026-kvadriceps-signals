"""Шестиклассовый HF-классификатор стадии; отказ модели не заменяется оценкой правил."""

from __future__ import annotations

import math
from collections.abc import Sequence

from extractor.interfaces import ModelUnavailable, StageScorer
from extractor.registry import ModelRegistry
from extractor.runtime import ModelRuntime, RuntimeConfig
from extractor.runtime.base import softmax
from extractor.schemas import P14Platt, SentenceInput, SentenceStage, round6


class HFStageScorer(StageScorer):
    """Классы 0..5; id2label = '0'..'5' может переупорядочить их, LABEL_0..LABEL_5 — порядок HF.

    Положительная stage_temperature в закреплённом config.json — температура с валидации;
    без неё prob — сырой softmax, а не эмпирически калиброванная вероятность.
    """

    def __init__(
        self,
        registry: ModelRegistry | None = None,
        model_name: str = "stage-v1",
        *,
        config: RuntimeConfig | None = None,
    ) -> None:
        self.runtime = ModelRuntime(
            model_name,
            task="sequence-classification",
            registry=registry,
            config=config,
            max_len=256,
        )

    @property
    def version(self) -> str:
        return self.runtime.version

    @property
    def p14_platt(self) -> P14Platt | None:
        """Необязательный ``stage_p14_platt`` рядом с ``stage_temperature`` в config.json."""
        try:
            return P14Platt.parse(getattr(self.runtime.load().hf_config, "stage_p14_platt", None))
        except ValueError as exc:
            raise ModelUnavailable(f"классификатор стадии: {exc}") from exc

    def score(self, sentences: Sequence[SentenceInput]) -> list[SentenceStage]:
        if not sentences:
            return []
        try:
            predictions = self.runtime.predict([item.sentence for item in sentences])
            config = self.runtime.hf_config
            if config.num_labels != 6:
                raise ValueError(
                    "stage checkpoint must have exactly six classes (0..5)"
                )
            labels = {int(key): value for key, value in config.id2label.items()}
            if labels == {i: f"LABEL_{i}" for i in range(6)}:
                codes = list(range(6))
            else:
                codes = [int(labels[i]) for i in range(6)]
                if set(codes) != set(range(6)):
                    raise ValueError("stage id2label must identify codes 0..5")
            temperature = float(getattr(config, "stage_temperature", 1.0))
            if not math.isfinite(temperature) or temperature <= 0:
                raise ValueError("stage_temperature must be finite and positive")
            # Неверная калибровка — отказ модели, а не молчаливое тождество.
            P14Platt.parse(getattr(config, "stage_p14_platt", None))
            results = []
            for item, prediction in zip(sentences, predictions, strict=True):
                probs = softmax(prediction.logits, temperature)
                index = int(probs.argmax())
                # Распределение переупорядочено по кодам 0..5, а не по индексам
                # логитов: потребитель суммирует вероятности кодов диапазона.
                by_code = [0.0] * 6
                for position, code in enumerate(codes):
                    by_code[code] = round6(float(probs[position]))
                results.append(
                    SentenceStage(
                        doc_id=item.doc_id,
                        sent_start=item.sent_start,
                        sent_end=item.sent_end,
                        sentence=item.sentence,
                        code=codes[index],
                        quotes=[item.sentence],
                        prob=round6(float(probs[index])),
                        probs=by_code,
                    )
                )
            return results
        except ModelUnavailable:
            raise
        except Exception as exc:
            raise ModelUnavailable(
                f"классификатор стадии не смог обработать пакет: {exc}"
            ) from exc
