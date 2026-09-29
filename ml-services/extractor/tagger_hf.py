from __future__ import annotations

from bisect import bisect_right
from collections.abc import Sequence

import numpy as np

from extractor.interfaces import ModelUnavailable, SpanTagger
from extractor.registry import ModelRegistry
from extractor.runtime import ModelRuntime, Prediction, RuntimeConfig
from extractor.runtime.base import softmax
from extractor.runtime.text import TextNormalizer, marker_role, sentence_spans
from extractor.schemas import (
    CURRENT_SUBTYPES,
    Document,
    Span,
    SpanRole,
    SpanSubtype,
    round6,
)
from extractor.tagger_projection import (
    TARGET_POLICY,
    InputPolicy,
    TaggerProjection,
    input_identity,
    word_bounds,
)

#: Версия раскодирования входит в версию теггера и потому в ключ кэша спанов: записи
#: без ``head_prob`` или с обрывками слов не должны выдаваться как тёплый ответ.
SPAN_DECODE_VERSION = "spans-head-prob-words@2"


class TaggedSpans(list[list[Span]]):
    """Совместим со списком, но несёт счётчики окон запроса для охвата."""

    def __init__(
        self, spans: Sequence[list[Span]], *, windows: Sequence[int | None]
    ) -> None:
        super().__init__(spans)
        self.windows = tuple(windows)


class HFSpanTagger(SpanTagger):
    """Закреплённый локальный чекпойнт с id2label = O / B-TECH / I-TECH.

    Необязательные метаданные config.json: role_id2label, role_classifier_name, tech_subtype,
    candidate_id2label/candidate_classifier_name для обученной головы кандидата, canonical_names.
    Без словаря имён берутся нормализованные русские леммы или английские поверхности:
    переводы не выдумываются.
    """

    def __init__(
        self,
        registry: ModelRegistry | None = None,
        model_name: str = "tagger-v1",
        *,
        config: RuntimeConfig | None = None,
        input_policy: InputPolicy = "title_abstract",
    ) -> None:
        if input_policy not in {"title_abstract", "full_text_diagnostic"}:
            raise ValueError(f"unknown tagger input policy: {input_policy}")
        self.input_policy = input_policy
        self.runtime = ModelRuntime(
            model_name, task="token-classification", registry=registry, config=config
        )
        self._normalizer: TextNormalizer | None = None

    @property
    def version(self) -> str:
        policy = (
            TARGET_POLICY
            if self.input_policy == "title_abstract"
            else self.input_policy
        )
        return f"{self.runtime.version}|{policy}|{SPAN_DECODE_VERSION}"

    def input_identity(self, doc: Document) -> str:
        # Проверка до обращения к кэшу: устаревшие метаданные не должны прятаться за верным
        # хэшем полного источника. Настройки проекции входят и в версию.
        TaggerProjection.build(doc, self.input_policy)
        return input_identity(doc, self.input_policy)

    def input_notes(self, doc: Document) -> list[str]:
        return TaggerProjection.build(doc, self.input_policy).audit_notes()

    def tag(self, docs: Sequence[Document]) -> list[list[Span]]:
        if not docs:
            return []
        try:
            projections = [
                TaggerProjection.build(doc, self.input_policy) for doc in docs
            ]
            predictions = self.runtime.predict([view.text for view in projections])
            config = self.runtime.hf_config
            labels = {int(key): value for key, value in config.id2label.items()}
            if set(labels) != {0, 1, 2} or set(labels.values()) != {
                "O",
                "B-TECH",
                "I-TECH",
            }:
                raise ValueError("token checkpoint must declare O, B-TECH, I-TECH")
            raw_roles = getattr(config, "role_id2label", None) or {
                0: "background",
                1: "contribution",
                2: "application",
            }
            roles = {int(key): SpanRole(value) for key, value in raw_roles.items()}
            if set(roles) != {0, 1, 2} or set(roles.values()) != set(SpanRole):
                raise ValueError("role_id2label must map three ids to all three roles")
            subtype = SpanSubtype(getattr(config, "tech_subtype", "method"))
            if subtype not in CURRENT_SUBTYPES:
                raise ValueError("tech_subtype is not an allowed TECH subtype")
            if self._normalizer is None:
                self._normalizer = TextNormalizer()
            canonical_names = {}
            for surface, canonical in getattr(config, "canonical_names", {}).items():
                if not isinstance(canonical, str) or not canonical.strip():
                    raise ValueError("canonical_names values must be nonempty strings")
                key = self._normalizer.canonical(surface)
                if key in canonical_names and canonical_names[key] != canonical:
                    raise ValueError("conflicting canonical names for the same surface")
                canonical_names[key] = canonical
            return TaggedSpans(
                [
                    self._decode(
                        doc,
                        prediction,
                        labels,
                        roles,
                        subtype,
                        canonical_names,
                        projection=view,
                    )
                    for doc, prediction, view in zip(
                        docs, predictions, projections, strict=True
                    )
                ],
                windows=[prediction.windows for prediction in predictions],
            )
        except ModelUnavailable:
            raise
        except Exception as exc:
            raise ModelUnavailable(f"теггер не смог обработать пакет: {exc}") from exc

    def _decode(
        self,
        doc: Document,
        prediction: Prediction,
        labels: dict[int, str],
        roles: dict[int, SpanRole],
        subtype: SpanSubtype,
        canonical_names: dict[str, str],
        *,
        projection: TaggerProjection | None = None,
    ) -> list[Span]:
        projection = projection or TaggerProjection.build(doc, self.input_policy)
        probabilities = softmax(prediction.logits)
        ids = probabilities.argmax(axis=-1)
        sentences = sentence_spans(projection.text)
        starts = [start for start, _, _ in sentences]
        source_sentences = sentence_spans(doc.text)
        source_starts = [start for start, _, _ in source_sentences]
        spans = []
        groups: list[tuple[int, int, list[int], int]] = []
        tokens: list[int] = []
        sentence_index = -1

        def finish() -> None:
            if not tokens:
                return
            start = min(prediction.offsets[index][0] for index in tokens)
            end = max(prediction.offsets[index][1] for index in tokens)
            start, end = word_bounds(projection.text, start, end)
            groups.append((start, end, list(tokens), sentence_index))
            tokens.clear()

        def build(start: int, end: int, tokens: list[int], sentence_index: int) -> None:
            sentence = sentences[sentence_index][2]
            source_range = projection.source_span(start, end)
            if source_range is None:
                return
            start, end = source_range
            role = marker_role(sentence)
            if prediction.role_logits is not None:
                role_probs = softmax(prediction.role_logits[tokens]).mean(axis=0)
                role = roles[int(np.argmax(role_probs))]
            text = doc.text[start:end]
            key = self._normalizer.canonical(text)
            in_title = projection.is_title(start, end)
            is_candidate = in_title or role is SpanRole.CONTRIBUTION
            head_prob = None
            if prediction.candidate_logits is not None:
                candidate_probs = softmax(prediction.candidate_logits[tokens]).mean(
                    axis=0
                )
                is_candidate = int(np.argmax(candidate_probs)) == 1
                # Та же усреднённая вероятность, по которой принято решение.
                head_prob = round6(float(candidate_probs[1]))
            spans.append(
                Span(
                    start=start,
                    end=end,
                    text=text,
                    subtype=subtype,
                    role=role,
                    is_candidate=is_candidate,
                    in_title=in_title,
                    canonical_ru=canonical_names.get(key, key),
                    confidence=round6(
                        float(np.mean([probabilities[i, ids[i]] for i in tokens]))
                    ),
                    head_prob=head_prob,
                    # Идентификаторы источника перепривязывает конвейер; ключ кэша связывает
                    # и метаданные, и политику проекции.
                    doc_id=None,
                    sentence=source_sentences[bisect_right(source_starts, start) - 1][
                        2
                    ],
                )
            )

        for index, (start, end) in enumerate(prediction.offsets):
            if start == end == 0:  # служебные токены и дополнение
                finish()
                continue
            if not 0 <= start < end <= len(projection.text):
                raise ValueError(f"invalid character offset: {start}:{end}")
            current_sentence = bisect_right(starts, start) - 1
            if current_sentence < 0 or not projection.text[start:end].strip():
                finish()
                continue
            label = labels[int(ids[index])]
            if label == "O":
                finish()
                continue
            if (
                label == "B-TECH"
                or current_sentence != sentence_index
                or (tokens and start < prediction.offsets[tokens[-1]][0])
            ):
                finish()
            # Одинокий I начинает новую сущность: детерминированная починка BIO.
            sentence_index = current_sentence
            tokens.append(index)
        finish()
        # После привязки к словам соседние сущности одного слова совпадают или
        # перекрываются: они становятся одним спаном по объединению токенов.
        merged: list[tuple[int, int, list[int], int]] = []
        for start, end, group, sentence_at in sorted(groups, key=lambda item: item[:2]):
            if merged and start < merged[-1][1]:
                first, last, previous, sentence_first = merged[-1]
                merged[-1] = (first, max(last, end), previous + group, sentence_first)
            else:
                merged.append((start, end, group, sentence_at))
        for start, end, group, sentence_at in merged:
            build(start, end, group, sentence_at)
        spans.sort(key=lambda span: (span.start, span.end, span.canonical_ru))
        for span in spans:
            span.check_against(doc)
        return spans
