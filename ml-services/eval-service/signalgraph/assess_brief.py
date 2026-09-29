"""Извлечение отдельно на замороженных эталонных группах; это не приёмка кластеризации.

Точное совпадение якорей измеряется автоматически. Другой фрагмент источника требует
смысловой проверки: одно несовпадение ещё не доказывает ложного утверждения.
"""

from __future__ import annotations

import argparse
from collections import Counter
import hashlib
import json
from pathlib import Path
import uuid

from signalgraph.brief import build, sentence_spans
from validate_benchmark import validate_split


def assess(root: Path, split: str) -> dict:
    if split not in ("development", "validation"):
        raise ValueError("holdout cannot be used to tune extraction")
    validate_split(root, split)
    topics = json.loads((root / f"annotations.{split}.json").read_text())["topics"]
    for path in sorted(root.glob(f"extensions/annotations.{split}.*.json")):
        topics.extend(json.loads(path.read_text())["topics"])
    records, works, natives = [], [], {}
    for native in sorted({n for t in topics for n in t["source_ids"]}):
        source = json.loads((root / "sources" / f"{native}.json").read_text())
        text = source["text"]
        digest = hashlib.sha256(text.encode()).hexdigest()
        if digest != source["text_sha256"]:
            raise ValueError("changed frozen source")
        doc = str(uuid.uuid5(uuid.NAMESPACE_URL, source["url"]))
        snapshot = str(uuid.uuid5(uuid.NAMESPACE_URL, source["url"] + "#" + digest))
        work = "paper:" + source["canonical_id"]
        natives[doc] = native
        records.append(
            {
                "id": doc,
                "text": text,
                "text_sha256": digest,
                "snapshot_id": snapshot,
                "representation": "abstract",
                "source": {"provider": source["provider"], "revision": native},
            }
        )
        works.append({"id": work, "kind": "paper", "document_ids": [doc]})
    corpus = {"records": records, "works": works}
    by_native = {n: r for r in records for doc, n in natives.items() if doc == r["id"]}
    rows = []
    counts = Counter()
    for topic in topics:
        if not any(topic["eligible_gold"].values()):
            continue
        members = [w["id"] for w in works if natives[w["document_ids"][0]] in topic["source_ids"]]
        fields, evidence, missing = build(corpus, {"work_ids": members})
        checks = {}
        for field in ("problem", "advantage", "example"):
            actual = next((e for e in evidence if e["field"] == field), None)
            gold = topic["brief_support"].get(field)
            if actual is None:
                status = "unknown"
            elif field == "example":
                status = "research_span_requires_review"
            elif gold and natives[actual["document_id"]] == gold["source_id"] and gold["quote"] in actual["quote"]:
                status = "contains_frozen_anchor"
            else:
                status = "different_span_requires_review"
            counts[field + ":" + status] += 1
            checks[field] = {
                "status": status,
                "source_id": natives[actual["document_id"]] if actual else None,
                "actual": actual,
                "gold": gold,
            }
            if (
                actual
                and by_native[natives[actual["document_id"]]]["text"][actual["start"] : actual["end"]]
                != actual["quote"]
            ):
                raise ValueError("extractor changed source text")
        rows.append({"topic_id": topic["id"], "label": topic["label"], "missing_fields": missing, "fields": checks})
    return {
        "split": split,
        "membership": "frozen gold oracle, extraction component only",
        "topics": rows,
        "counts": dict(counts),
        "semantic_precision": "not inferred from exact-span matching; review required",
    }


if __name__ == "__main__":
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("--output", type=Path, required=True)
    args = parser.parse_args()
    report = {split: assess(Path(__file__).parent, split) for split in ("development", "validation")}
    # Десятичная точка не должна разрывать предложение источника.
    sample = "Our method improves accuracy by 3.5% over BM25 on Dataset A and reduces latency to 1.2 ms."
    report["decimal_probe"] = {"text": sample, "spans": [sample[a:b] for a, b in sentence_spans(sample)]}
    with args.output.open("x") as stream:
        json.dump(report, stream, ensure_ascii=False, indent=2)
    print(
        json.dumps(
            {
                s: {"topics": len(report[s]["topics"]), "counts": report[s]["counts"]}
                for s in ("development", "validation")
            }
        )
    )
    print(json.dumps(report["decimal_probe"]))
