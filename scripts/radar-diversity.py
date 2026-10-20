#!/usr/bin/env python3
"""Проверяет отрасли сохранённых выдач по предметной разметке и исходному строгому контракту.

Манифест содержит classifier_version, runs [{file, sha256}] и reviews
[{run, candidate_id, industry_id, source_id, quote, reason, reviewed}].
Пути runs считаются от манифеста; разметка ссылается на тот же file.
Отрасль подтверждается собственным источником технологии, не названием запроса.
"""

import argparse
import hashlib
import importlib.util
import json
from pathlib import Path


SCRIPT = Path(__file__).with_name("radar-acceptance.py")
SPEC = importlib.util.spec_from_file_location("radar_acceptance", SCRIPT)
RADAR = importlib.util.module_from_spec(SPEC)
SPEC.loader.exec_module(RADAR)
TAXONOMY = Path(__file__).resolve().parents[1] / "frontend/src/shared/lib/researchTaxonomy.json"
MIN_INDUSTRIES = 21


def complete(candidate):
    card = candidate.get("card") or {}
    claims = card.get("evidence") or []
    return not card.get("missing") and all(
        RADAR.russian(card.get(field)) and any(
            claim.get("field") == field and claim.get("verified") is True for claim in claims)
        for field in RADAR.FIELDS)


def confirmed(candidate):
    """Условие интерфейса isConfirmedSignal; счётчик API «выше 75 %» имеет другой смысл."""
    score = candidate.get("model_score") or {}
    return (candidate.get("shortfall") is None and score.get("label") == "weak_signal"
            and not score.get("data_gap") and RADAR.number(score.get("value"))
            and RADAR.number(score.get("threshold")) and 0 <= score["threshold"] <= score["value"] <= 1
            and RADAR.independent_sources(candidate) >= RADAR.MIN_INDEPENDENT)


def grounded(review, candidate, detail):
    source, quote = review.get("source_id"), review.get("quote")
    own = (candidate.get("paper_ids") or []) + (candidate.get("evidence_ids") or [])
    if (review.get("reviewed") is not True or not RADAR.text(review.get("reason"))
            or source not in own or not RADAR.text(quote)):
        return False
    qualification = candidate.get("qualification") or {}
    if not any(item.get("source_id") == source for item in qualification.get("evidence") or []):
        return False
    papers = {paper.get("id"): paper for paper in detail.get("papers") or []}
    paper = papers.get(source) or {}
    texts = [paper.get("title") or "", paper.get("abstract") or ""]
    texts.extend(context.get("text") or "" for context in (candidate.get("card") or {}).get("source_contexts") or []
                 if context.get("source_id") == source)
    texts.append(((detail.get("source_revisions") or {}).get(source) or {}).get("text") or "")
    return any(RADAR.normalized(quote) in RADAR.normalized(text) for text in texts)


def analyze(saved_runs, reviews, industries, classifier_version):
    runs, records, identities = {}, [], {}
    for name, saved in saved_runs.items():
        response = saved.get("response") or {}
        detail = response.get("detail") or {}
        validation = RADAR.validate(detail, require_model=True, expected_query=saved.get("query"))
        ready = saved.get("final_response_received") is True and response.get("status") == "ready"
        fresh = (saved.get("timing") or {}).get("fresh_collection_proven") is True
        current = detail.get("classifier_version") == classifier_version
        candidates = detail.get("candidates") or []
        runs[name] = {"query": saved.get("query"), "strict_passed": validation["top15_passed"],
                      "ready": ready, "fresh": fresh, "current_classifier": current,
                      "eligible": ready and fresh and current and validation["top15_passed"],
                      "candidates": len(candidates), "complete": sum(map(complete, candidates)),
                      "confirmed_complete": sum(complete(c) and confirmed(c) for c in candidates),
                      "problems": validation["problems"], "warnings": validation["warnings"]}
    seen = set()
    for review in reviews:
        name, candidate_id, industry = (review.get(key) for key in ("run", "candidate_id", "industry_id"))
        if name not in runs or industry not in industries or (name, candidate_id) in seen:
            raise ValueError("Разметка содержит неизвестную выдачу, отрасль или повторную карточку.")
        seen.add((name, candidate_id))
        detail = saved_runs[name]["response"]["detail"]
        candidate = next((c for c in detail.get("candidates") or [] if c.get("id") == candidate_id), None)
        if candidate is None:
            raise ValueError("Размеченная карточка отсутствует в выдаче.")
        qualification = candidate.get("qualification") or {}
        identity = RADAR.label_key(qualification.get("canonical_name") or candidate.get("name")
                                   or candidate.get("label") or candidate_id)
        for key in (("id", candidate_id), ("name", identity)):
            previous = identities.setdefault(key, industry)
            if previous != industry:
                raise ValueError("Одна технология размечена несколькими отраслями; выберите основную.")
        source_grounded = grounded(review, candidate, detail)
        accepted = runs[name]["eligible"] and complete(candidate) and confirmed(candidate) and source_grounded
        records.append({**review, "complete": complete(candidate), "confirmed": confirmed(candidate),
                        "grounded": source_grounded, "counted": accepted, "identity": identity,
                        "independent_sources": RADAR.independent_sources(candidate)})
    represented = sorted({record["industry_id"] for record in records if record["counted"]})
    batch_passed = bool(runs) and all(run["eligible"] for run in runs.values())
    return {"classifier_version": classifier_version, "minimum_industries": MIN_INDUSTRIES,
            "strict_batch_passed": batch_passed, "represented_industries": represented,
            "industry_count": len(represented),
            "content_coverage_passed": batch_passed and len(represented) >= MIN_INDUSTRIES,
            "runs": runs, "reviews": records,
            "limits": ["Предметная разметка требует чтения источников; дословная цитата сама не доказывает её смысл.",
                       "Классификация использует предварительные рубрики каталога; точность модели не измеряется.",
                       "Стабильность контейнеров и маршрутизатора LLM проверяется отдельными протоколами."]}


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("manifest", type=Path)
    parser.add_argument("--out", required=True, type=Path)
    args = parser.parse_args()
    if args.out.exists():
        parser.error("Выходной файл уже существует; прежнее доказательство не перезаписывается.")
    try:
        manifest = json.loads(args.manifest.read_text())
        version = manifest["classifier_version"]
        if not RADAR.text(version):
            raise ValueError("Нужна зафиксированная версия классификатора.")
        runs = {}
        for entry in manifest["runs"]:
            name = entry["file"]
            raw = (args.manifest.parent / name).read_bytes()
            if name in runs or hashlib.sha256(raw).hexdigest() != entry["sha256"]:
                raise ValueError("Повторная выдача или несовпадающая контрольная сумма.")
            runs[name] = json.loads(raw)
        catalog = json.loads(TAXONOMY.read_text())
        result = analyze(runs, manifest["reviews"], {row["id"] for row in catalog["categories"]}, version)
        result["input_manifest_sha256"] = hashlib.sha256(args.manifest.read_bytes()).hexdigest()
        result["validator_sha256"] = hashlib.sha256(SCRIPT.read_bytes()).hexdigest()
        result["taxonomy_sha256"] = hashlib.sha256(TAXONOMY.read_bytes()).hexdigest()
        with args.out.open("x") as output:
            json.dump(result, output, ensure_ascii=False, indent=2)
            output.write("\n")
        print(f"Отраслей с полными подтверждёнными сигналами: {result['industry_count']}/{MIN_INDUSTRIES}; "
              f"строгая проверка выдач: {'PASS' if result['strict_batch_passed'] else 'FAIL'}")
        return 0 if result["content_coverage_passed"] else 1
    except (OSError, ValueError, KeyError, TypeError) as error:
        parser.error(str(error))


if __name__ == "__main__":
    raise SystemExit(main())
