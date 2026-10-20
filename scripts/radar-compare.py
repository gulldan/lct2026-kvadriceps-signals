#!/usr/bin/env python3
"""Сопоставляет сохранённые ответы на одинаковые запросы, не подменяя предметное ревью счётчиками.

python3 scripts/radar-compare.py --before before-1.json before-2.json \
    --after after-1.json after-2.json --out comparison.json
"""

import argparse
import collections
import json
from pathlib import Path


def metrics(saved):
    response = saved.get("response") or {}
    detail = response.get("detail") or {}
    candidates = detail.get("candidates") or []
    contexts, prior_works, models, cited_fulltexts = {}, set(), set(), set()
    verified, complete, prior_checked, prior_found, prior_unavailable = 0, 0, 0, 0, 0
    limitations, core_claims, prior_budget_failures = 0, 0, 0
    for candidate in candidates:
        card = candidate.get("card") or {}
        if card.get("model"):
            models.add(card["model"])
        claims = card.get("evidence") or []
        verified += sum(claim.get("verified") is True for claim in claims)
        core_claims += sum(claim.get("verified") is True and claim.get("field") in ("summary", "advantage", "example")
                           for claim in claims)
        limitations += bool(card.get("study_limitations")) and any(
            claim.get("field") == "study_limitations" and claim.get("verified") is True for claim in claims)
        complete += all(card.get(field) and any(claim.get("field") == field and claim.get("verified")
                        for claim in claims) for field in ("summary", "advantage", "example"))
        for source in card.get("source_contexts") or []:
            # Одна статья может быть основанием нескольких карточек: сохраняем наибольшее фактическое покрытие.
            key = source.get("doi") or source.get("arxiv_id") or source.get("source_id")
            if source.get("coverage") in ("fulltext", "excerpted_fulltext") and any(
                    claim.get("verified") is True and claim.get("source_id") == source.get("source_id")
                    and claim.get("quote") and claim["quote"] in source.get("text", "") for claim in claims):
                cited_fulltexts.add(key)
            rank = {"abstract": 0, "excerpted_fulltext": 1, "fulltext": 2}
            if key not in contexts or rank.get(source.get("coverage"), 0) > rank.get(contexts[key].get("coverage"), 0):
                contexts[key] = source
        prior = card.get("prior_art") or {}
        prior_checked += prior.get("status") in ("candidates", "not_found")
        prior_found += prior.get("status") == "candidates"
        prior_unavailable += prior.get("status") == "unavailable"
        prior_budget_failures += "бюджет запросов" in prior.get("note", "")
        prior_works.update(work["id"] for work in prior.get("works", []) if work.get("id"))
    coverage = collections.Counter(source.get("coverage") for source in contexts.values())
    providers = collections.Counter(source.get("provider") or "arxiv" for source in contexts.values()
                                    if source.get("coverage") in ("fulltext", "excerpted_fulltext"))
    ready = (saved.get("final_response_received") is True and response.get("status") == "ready"
             and not detail.get("digests_pending") and not detail.get("rescoring"))
    return {"ready": ready, "contract": detail.get("search_contract"), "models": sorted(models),
            "seconds": (saved.get("timing") or {}).get("seconds"), "candidates": len(candidates),
            "complete_cards": complete, "verified_claims": verified,
            "verified_core_claims": core_claims, "study_limitations_cards": limitations,
            "cited_fulltext_sources": len(cited_fulltexts), "prior_art_budget_failures": prior_budget_failures,
            "fulltext_sources": coverage["fulltext"] + coverage["excerpted_fulltext"],
            "fulltext_providers": dict(providers), "abstract_contexts": coverage["abstract"],
            "prior_art_checked": prior_checked, "prior_art_found": prior_found,
            "prior_art_unavailable": prior_unavailable, "earlier_works": len(prior_works),
            "top15_passed": (saved.get("validation") or {}).get("top15_passed", False)}


def compare(before, after):
    query = before.get("query")
    if not query or query != after.get("query"):
        raise ValueError("Для пары нужны ответы на один и тот же запрос.")
    left, right = metrics(before), metrics(after)
    comparable = left["ready"] and right["ready"] and left["models"] == right["models"]
    measured = ("complete_cards", "verified_claims", "verified_core_claims", "study_limitations_cards",
                "fulltext_sources", "cited_fulltext_sources", "prior_art_checked", "prior_art_found",
                "prior_art_budget_failures", "earlier_works")
    return {"query": query, "comparable": comparable, "before": left, "after": right,
            "delta": {key: right[key] - left[key] for key in measured} if comparable else None,
            "semantic_accuracy_checked": False}


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("--before", type=Path, nargs="+", required=True)
    parser.add_argument("--after", type=Path, nargs="+", required=True)
    parser.add_argument("--out", type=Path, required=True)
    args = parser.parse_args()
    if len(args.before) != len(args.after):
        parser.error("Число ответов до и после должно совпадать.")
    pairs = [compare(json.loads(left.read_text()), json.loads(right.read_text()))
             for left, right in zip(args.before, args.after)]
    result = {"pairs": pairs, "semantic_accuracy_checked": False,
              "limitation": "Проверенные цитаты — вердикт модели. Полные тексты и ранние работы измеряют покрытие; "
                            "релевантность, тождество механизма и фактическую точность нужно оценить по первоисточникам. "
                            "Одиночный прогон не отделяет эффект источников от вариативности модели и поиска."}
    args.out.write_text(json.dumps(result, ensure_ascii=False, indent=2) + "\n")
    print(json.dumps(result, ensure_ascii=False, indent=2))
    return 0 if all(pair["comparable"] for pair in pairs) else 1


if __name__ == "__main__":
    raise SystemExit(main())
