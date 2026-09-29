"""Связная справка из закреплённых фрагментов модели по одним источникам.

Экспериментальный компонент, в рабочий исполнитель не входит. Все три поля
предпочитают одну статью. Цитаты источников не пересказываются и не правятся.
"""

from __future__ import annotations

import hashlib
import re

from signalgraph.brief import INSTRUCTION, NEGATED, NON_RESULT, sentence_spans
from signalgraph.claim_boundaries import COMPARISON, complete_clause_end, incomplete_quantification

VERSION = "source-role-selector-v11-development"

PROBLEM = re.compile(
    r"\b(?:difficult\w*|challeng\w*|insufficient|lack\w*|limit\w*|expensive|cost\w*|"
    r"struggl\w*|suffer\w*|issue\w*|restrict\w*|shortcoming\w*|fail\w*|"
    r"cannot|unable|unclear|unhelpful|harmful|susceptible)\b",
    re.I,
)
BENEFIT = re.compile(
    r"\b(?:outperform\w*|surpass\w*|improv\w*|reduc\w*|better|faster|gain\w*|"
    r"speedup\w*|fewer|less|boost\w*|mitigat\w*|accuracy|preferred|abstain\w*|enhanc\w*|highest|superior|comparable|smaller)\b",
    re.I,
)
GOAL = re.compile(r"\b(?:we|this paper|this work|this study)\s+(?:study|aim|propose|introduce|present)\b", re.I)
SPECULATION = re.compile(r"\b(?:posit|hypothes\w*|plan|hope|expect|anticipate|could|might|will)\b", re.I)
REFERENT = re.compile(
    r"\b(?:both (?:datasets|tasks)|all (?:(?:three|four) )?(?:tasks|problems))\b|"
    r"^(?:It |This model|These models|Our framework|Our approach)",
    re.I,
)


def has_benefit(quote):
    if SPECULATION.search(quote):
        return False
    for match in BENEFIT.finditer(quote):
        prefix = quote[max(0, match.start() - 120) : match.start()]
        if not NEGATED.search(prefix) and not NON_RESULT.search(prefix):
            return True
    return False


def qualitative_benefit(text, item):
    """Целое придаточное источника или неизвестное поле.

    Несколько процентов в одной фразе обрабатываются консервативно, даже если
    соответствие можно восстановить: меняется отбор, но не значение и не единица.
    """
    for a, b in sentence_spans(item["quote"]):
        sentence = item["quote"][a:b]
        if not has_benefit(sentence):
            continue
        extent = complete_clause_end(sentence)
        if extent is None:
            continue
        sentence = sentence[:extent]
        if (
            len(sentence) < 45
            or incomplete_quantification(sentence)
            or not has_benefit(sentence)
            or INSTRUCTION.search(sentence)
        ):
            continue
        start = item["start"] + a
        end = start + len(sentence)
        if text[start:end] != sentence:
            raise ValueError("qualitative clause differs from its source")
        return {**item, "start": start, "end": end, "quote": sentence, "selection_mode": "verbatim_qualitative_clause"}
    return None


def select(sources, roles, members, field):
    role = "mechanism" if field == "example" else field
    options = []
    for native in sorted(members):
        item = roles[native][role]
        if item is None:
            continue
        text = sources[native]["text"]
        quote = item["quote"]
        if (
            item["text_sha256"] != sources[native]["text_sha256"]
            or hashlib.sha256(text.encode()).hexdigest() != item["text_sha256"]
            or type(item["start"]) is not int
            or type(item["end"]) is not int
            or not 0 <= item["start"] < item["end"] <= len(text)
            or text[item["start"] : item["end"]] != quote
        ):
            raise ValueError("source span or revision changed")
        if INSTRUCTION.search(quote):
            continue
        if field == "problem" and GOAL.search(quote) and not PROBLEM.search(quote):
            continue
        if field == "advantage" and not has_benefit(quote):
            continue
        item = {"source_id": native, **item}
        if field == "advantage" and incomplete_quantification(quote):
            item = qualitative_benefit(text, item)
            if item is None:
                continue
            quote = item["quote"]
        if field == "advantage" and REFERENT.search(quote):
            previous = [(a, b) for a, b in sentence_spans(text) if b <= item["start"]]
            if previous:
                item["start"] = previous[-1][0]
                item["quote"] = text[item["start"] : item["end"]]
                item["context_expanded"] = True
            if len(item["quote"]) > 3000:
                continue
        # Конкретное сравнение предпочтительнее общего преимущества; всё это —
        # утверждения авторов. Оценок новизны и тренда в этом отборе нет.
        priority = (
            (int(bool(COMPARISON.search(quote))), int(bool(re.search(r"\d", quote))))
            if field == "advantage"
            else (0, 0)
        )
        options.append((priority, native, item))
    options.sort(key=lambda o: (-o[0][0], -o[0][1], o[1]))
    return options[0][2] if options else None


def select_bundle(sources, roles, members):
    bundles = []
    for native in sorted(members):
        fields = {f: select(sources, roles, [native], f) for f in ("problem", "advantage", "example")}
        advantage = fields["advantage"]
        if advantage and advantage.get("selection_mode") != "verbatim_qualitative_clause":
            # Два предшествующих предложения источника сохраняют условия сравнения:
            # это контекст доказательства, а не сгенерированный или исправленный текст.
            original = roles[native]["advantage"]
            previous = [(a, b) for a, b in sentence_spans(sources[native]["text"]) if b <= original["start"]]
            if previous:
                advantage["start"] = min(advantage["start"], previous[max(0, len(previous) - 2)][0])
                advantage["quote"] = sources[native]["text"][advantage["start"] : advantage["end"]]
                advantage["context_expanded"] = True
            if len(advantage["quote"]) > 3000:
                fields["advantage"] = None
        problem = fields["problem"]
        if problem and re.search(r"\b(?:such|these|their|both|its|this|it)\b", problem["quote"][:120], re.I):
            previous = [(a, b) for a, b in sentence_spans(sources[native]["text"]) if b <= problem["start"]]
            if previous:
                problem["start"] = previous[-1][0]
                problem["quote"] = sources[native]["text"][problem["start"] : problem["end"]]
                problem["context_expanded"] = True
            if len(problem["quote"]) > 3000:
                fields["problem"] = None
        for field, evidence in fields.items():
            if evidence and (INSTRUCTION.search(evidence["quote"]) or "\x00" in evidence["quote"]):
                fields[field] = None
        n = sum(v is not None for v in fields.values())
        a = fields["advantage"]
        q = a["quote"] if a else ""
        priority = (n, int(bool(COMPARISON.search(q))), int(bool(re.search(r"\d", q))))
        bundles.append((priority, native, fields))
    bundles.sort(key=lambda o: (tuple(-v for v in o[0]), o[1]))
    return bundles[0][2] if bundles else {f: None for f in ("problem", "advantage", "example")}
