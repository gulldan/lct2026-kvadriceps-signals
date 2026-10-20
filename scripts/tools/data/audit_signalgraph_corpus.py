#!/usr/bin/env python3
"""Read-only, reproducible audit of a saved document/manifest snapshot.

Capture on the operator host with --capture; STAND_DATASET_DIR sets the dataset root.
No provider requests, data repairs, model calls, or writes to the running service.
"""

import argparse
from collections import Counter
from datetime import date, datetime, timezone
import hashlib
import json
from pathlib import Path
import re
import subprocess
import sys

sys.path.insert(0, str(Path(__file__).resolve().parents[2]))
from tools.stand_env import load_stand_env, redact


DIRECTIONS = {
    "ai": r"\b(?:artificial intelligence|machine learning|deep learning|neural|transformer|language model|reinforcement learning|retrieval.augmented|RAG|LLM)\b|искусственн|нейрон|машинн.*обуч",
    "rag": r"retrieval.augmented|\bRAG\b|\bгенераци(?:я|и|ю|ей)\s+(?:с\s+)?(?:поиск|извлеч)\w*",
}


def capture(dataset_root: Path) -> dict:
    sql = """BEGIN TRANSACTION ISOLATION LEVEL REPEATABLE READ READ ONLY;
    SET LOCAL statement_timeout = '15s';
    SELECT coalesce(json_agg(d ORDER BY id), '[]') FROM (
      SELECT id, filename, title, kind, status, chunk_count, content_hash,
             published_at, source_ref, created_at FROM documents
    ) d;
    COMMIT;"""
    raw = subprocess.check_output(
        ["docker", "exec", "-i", "rag-platform-postgres-1", "psql", "-XAtq", "-v", "ON_ERROR_STOP=1", "-U", "rag", "-d", "rag"],
        input=sql, text=True, timeout=20, stderr=subprocess.PIPE,
    )
    manifests = []
    for p in sorted(dataset_root.glob("*/upload_manifest.jsonl")):
        metadata = p.with_name("metadata.jsonl")
        manifests.append({
            "path": p.relative_to(dataset_root).as_posix(), "sha256": hashlib.sha256(p.read_bytes()).hexdigest(),
            "uploads": [json.loads(line) for line in p.read_text().splitlines() if line.strip()],
            "metadata": [json.loads(line) for line in metadata.read_text().splitlines() if line.strip()] if metadata.exists() else [],
            "metadata_sha256": hashlib.sha256(metadata.read_bytes()).hexdigest() if metadata.exists() else None,
        })
    return {"version": 1, "captured_at": datetime.now(timezone.utc).isoformat(), "documents": json.loads(raw), "manifests": manifests}


def publication_day(value: str, cutoff: date) -> str | None:
    # Метаданные парсера, год в имени файла и время загрузки датой публикации не считаются.
    try:
        day = date.fromisoformat(value[:10])
    except (ValueError, TypeError):
        return None
    return day.isoformat() if date(1900, 1, 1) <= day <= cutoff else None


def audit(snapshot: dict, cutoff: date) -> dict:
    recovered = {}
    by_filename = {}
    for manifest in snapshot["manifests"]:
        metadata = {str(m.get("arxiv_id") or m["source_id"]): m for m in manifest["metadata"] if m.get("arxiv_id") or m.get("source_id")}
        for upload in manifest["uploads"]:
            m = metadata.get(str(upload.get("arxiv_id") or upload.get("source_id")))
            if upload.get("ok") and upload.get("document_id") and m:
                recovered.setdefault(upload["document_id"], []).append((upload, m, manifest["path"]))
                by_filename.setdefault(upload.get("filename"), []).append((upload, m, manifest["path"]))
    result = {"version": 1, "cutoff": cutoff.isoformat(), "documents": len(snapshot["documents"]),
              "statuses": dict(sorted(Counter(d["status"] for d in snapshot["documents"]).items())),
              "legacy_publication_values": dict(sorted(Counter(d["published_at"] for d in snapshot["documents"] if d["published_at"]).items())),
              "directions": {}}
    for name, pattern in DIRECTIONS.items():
        selected = []
        for d in sorted(snapshot["documents"], key=lambda d: d["id"]):
            exact_identity = d["id"] in recovered
            links = recovered.get(d["id"], by_filename.get(d["filename"], []))
            search_text = " ".join([d["title"], d["filename"], *(m.get("title", "") + " " + (m.get("abstract") or m.get("summary", "")) for _, m, _ in links)])
            if not re.search(pattern, search_text, re.I):
                continue
            reasons = []
            if d["kind"] == "hypotheses":
                reasons.append("generated_hypothesis")
            if d["status"] != "indexed" or d["chunk_count"] <= 0:
                reasons.append("no_indexed_text")
            dated = [(u, m, p) for u, m, p in links if publication_day(m.get("published") or m.get("publication_date", ""), cutoff)]
            identities = {(m.get("arxiv_id") or m.get("source_id"), m.get("published") or m.get("publication_date"), m.get("updated")) for _, m, _ in dated}
            if len(identities) > 1:
                reasons.append("conflicting_manifest_metadata")
            if not dated:
                reasons.append("no_supported_publication_date")
            u, m, p = dated[0] if dated else ({}, {}, None)
            if m.get("updated") and not publication_day(m["updated"], cutoff):
                reasons.append("revision_after_cutoff")
            selected.append({"id": d["id"], "title": d["title"] or d["filename"],
                "classification": "ineligible" if reasons else "recoverable",
                "reasons": reasons, "native_id": m.get("arxiv_id") or m.get("source_id"),
                "published": m.get("published") or m.get("publication_date"), "updated": m.get("updated"),
                "identity_basis": "document_id" if exact_identity else "filename_requires_content_verification",
                "date_basis": p, "text_status": "indexed_indicator_only" if d["chunk_count"] > 0 else "unavailable",
                "content_hash": d["content_hash"]})
        result["directions"][name] = {"selection": "lexical audit; not semantic recall or trend eligibility",
            "matched": len(selected), "classifications": dict(sorted(Counter(d["classification"] for d in selected).items())),
            "reasons": dict(sorted(Counter(r for d in selected for r in d["reasons"]).items())), "records": selected}
    return result


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    try:
        env = load_stand_env()
    except ValueError as failure:
        parser.error(str(failure))
    parser.add_argument("--capture", action="store_true")
    parser.add_argument("--dataset-root", type=Path, default=env.get("STAND_DATASET_DIR"),
                        help="compatibility: only the value of STAND_DATASET_DIR")
    parser.add_argument("--snapshot", type=Path)
    parser.add_argument("--cutoff", type=date.fromisoformat, default=date(2026, 8, 31))
    args = parser.parse_args()
    if args.capture:
        configured_root = env.get("STAND_DATASET_DIR")
        if not configured_root or args.dataset_root != Path(configured_root):
            parser.error("--capture requires STAND_DATASET_DIR; CLI overrides are not accepted")
        if not args.dataset_root.is_dir():
            parser.error("STAND_DATASET_DIR must refer to an existing directory")
        result = capture(args.dataset_root)
    else:
        if not args.snapshot:
            parser.error("--snapshot is required")
        raw = args.snapshot.read_bytes()
        result = audit(json.loads(raw), args.cutoff)
        result["snapshot_sha256"] = hashlib.sha256(raw).hexdigest()
    print(json.dumps(redact(result, env), ensure_ascii=False, indent=2, sort_keys=True))


if __name__ == "__main__":
    try:
        main()
    except Exception as failure:
        raise SystemExit("audit failed: " + type(failure).__name__) from None
