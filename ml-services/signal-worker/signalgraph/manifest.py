"""Verify and materialize exactly one authorized immutable analysis input.

No live index lookup, alias substitution, discovery or model generation occurs
here. The db-service must recheck current reader rights before and after use.
"""
from __future__ import annotations

from dataclasses import asdict
from datetime import datetime, time, timedelta, timezone
import hashlib
import json
import math
from typing import Callable

from .identity import canonical_works
from .coverage import MAX_COVERAGE_OBJECT, MAX_COVERAGE_PAGES, enrich
from .relations import paper_repository_links
from .temporal import date_interval

MAX_TEXT = 16 << 20
MAX_INPUT = 64 << 20
MAX_SNAPSHOT = 128 << 20
# Cumulative sequential reads, not retained RAM. A verified 1,604-paper input
# contains 2.14 GB of pinned text/vector artifacts within the 2,000-work bound.
MAX_TOTAL = 4 << 30
VERSIONS = {"identity": "canonical-works-v1", "relations": "explicit-reference-v1",
            "pooling": "selected-verified-body-mean-l2-v2", "loader": "analysis-input-v2",
            "measurements": "retrospective-quarters-v4-shared-coverage-enrollment", "coverage": "enrolled-scientific-scopes-v2"}


def digest(raw: bytes) -> str:
    return hashlib.sha256(raw).hexdigest()


def canonical(value) -> bytes:
    return json.dumps(value, sort_keys=True, ensure_ascii=False, separators=(",", ":"), allow_nan=False).encode()


def timestamp(value: str) -> datetime:
    result = datetime.fromisoformat(value.replace("Z", "+00:00"))
    if result.tzinfo is None:
        raise ValueError("source time requires an explicit timezone")
    return result.astimezone(timezone.utc)


def _verify_source(record: dict, request: dict) -> None:
    source = record["source"]
    test_source = source.get("test_run_id")
    if (source["source_kind"] == "hypothesis" or
            (test_source and (test_source != request.get("test_run_id") or
                              record["index"]["owner_id"] != request["owner_id"]))):
        raise ValueError("non-product source in analysis membership")
    allowed = (record["index"]["owner_id"] == request["owner_id"]
               or source["access_scope"] == "public"
               or (source["access_scope"] == "shared" and request["shared_corpus"]))
    if not allowed:
        raise ValueError("foreign source in frozen input")
    cutoff = timestamp(request["cutoff"])
    if source.get("publication"):
        _, end = date_interval(source["publication"])
        if datetime.combine(end, time(), timezone.utc) > cutoff + timedelta(seconds=1):
            raise ValueError("future publication interval in frozen input")
    observed = timestamp(source["observed_at"])
    available = timestamp(source.get("available_at") or source["observed_at"])
    if available > cutoff or (request["mode"] == "as_observed" and observed > cutoff):
        raise ValueError("future source revision in frozen input")
    if arxiv := source.get("arxiv"):
        if timestamp(arxiv["version_updated"]) > cutoff:
            raise ValueError("future arXiv version in frozen input")
        if observed > cutoff and (arxiv.get("doi") or arxiv.get("journal_ref")):
            raise ValueError("unproven late annotations in historical input")


def _mean_vector(chunks: list[dict], dimension: int) -> list[float]:
    for chunk in chunks:
        vector = chunk["vector"]
        if len(vector) != dimension or not all(isinstance(v, (int, float)) and math.isfinite(v) for v in vector):
            raise ValueError("incompatible or nonfinite source vector")
        if not any(vector):
            raise ValueError("zero source vector")
    mean = [math.fsum(c["vector"][i] for c in chunks) / len(chunks) for i in range(dimension)]
    norm = math.sqrt(math.fsum(v * v for v in mean))
    if not norm or not math.isfinite(norm):
        raise ValueError("invalid canonical work centroid")
    return [v / norm for v in mean]


def representation_preference(record: dict) -> tuple:
    status = record['source'].get('text_status', '')
    if status in ('metadata_only', 'title_only'):
        rank = 3
    elif record['representation'] == 'full_text' or status in ('article_text', 'pdf', 'full_text'):
        rank = 0
    elif record['representation'] == 'abstract':
        rank = 1
    else:
        rank = 2
    return rank, record['source']['provider'] != 'arxiv', record['id']


def _snapshot(record: dict, model: dict, read: Callable[[str, int], bytes]) -> dict:
    index = record["index"]
    if index["model"] != model:
        raise ValueError("mixed embedding model in analysis input")
    raw = read(index["data"]["key"], MAX_SNAPSHOT)
    if digest(raw) != index["data"]["sha256"]:
        raise ValueError("immutable source snapshot hash mismatch")
    snapshot = json.loads(raw)
    if any(snapshot[key] != index[key] for key in (
        "document_id", "owner_id", "snapshot_id", "model", "parser_revision", "pipeline_revision"
    )):
        raise ValueError("source generation differs from frozen manifest")
    if (snapshot["document_id"] != record["id"] or snapshot["source"]["revision"] != index["source_revision"]
            or snapshot["source"]["content_hash"] != index["raw_hash"]
            # Go's typed Source serializes fields in declaration order. Preserve
            # that exact order from the hash-pinned snapshot; sorting here would
            # hash a different serialization of otherwise equal metadata.
            or digest(json.dumps(snapshot["source"], ensure_ascii=False, separators=(",", ":"),
                                 allow_nan=False).encode()) != record["source_hash"]):
        raise ValueError("original source provenance differs from input")
    # Compare the permitted copy after removing only the annotations whose
    # historical availability was explicitly rejected at freeze time.
    source = json.loads(json.dumps(snapshot["source"]))
    if "undated_later_doi_and_journal_annotations_removed" in record["limitations"]:
        source["arxiv"].pop("doi", None)
        source["arxiv"].pop("journal_ref", None)
    if source != record["source"]:
        raise ValueError("analysis source metadata was silently substituted")
    text_raw = read(index["text"]["key"], MAX_TEXT)
    if digest(text_raw) != index["text"]["sha256"] or digest(text_raw) != snapshot["text_hash"]:
        raise ValueError("source text hash mismatch")
    text = text_raw.decode("utf-8", errors="strict")
    chunks = snapshot["chunks"]
    if not 1 <= len(chunks) == index["chunk_count"] <= 8192:
        raise ValueError("source fragment count mismatch")
    seen = set()
    for number, chunk in enumerate(chunks):
        start, end = chunk["char_start"], chunk["char_end"]
        if (chunk["index"] != number or chunk["id"] in seen or not 0 <= start < end <= len(text)
                or text[start:end] != chunk["text"]):
            raise ValueError("source fragment differs from exact immutable text")
        seen.add(chunk["id"])
    return {**record, "text": text, "text_sha256": snapshot["text_hash"], "snapshot_id": snapshot["snapshot_id"],
            "vector": _mean_vector(chunks, model["dimension"]),
            "chunks": [{k: v for k, v in chunk.items() if k not in ("vector", "lexical_text", "metadata")}
                       for chunk in chunks]}


def load_input(raw: bytes, sha256: str, read_object: Callable[[str, int], bytes]) -> dict:
    if len(raw) > MAX_INPUT or digest(raw) != sha256:
        raise ValueError("analysis input hash mismatch or size limit exceeded")
    manifest = json.loads(raw)
    if manifest["version"] != "analysis-input-v2" or len(manifest["records"]) > 2000:
        raise ValueError("unsupported or oversized input")
    if any(timestamp(page["captured_at"]) > timestamp(manifest["request"]["cutoff"])
           for page in manifest["pages"]):
        raise ValueError("future collection coverage in frozen input")
    used = 0

    def read(key: str, maximum: int) -> bytes:
        nonlocal used
        remaining = MAX_TOTAL - used
        if remaining <= 0:
            raise ValueError("bounded analysis artifact budget exceeded")
        data = read_object(key, min(maximum, remaining))
        used += len(data)
        if len(data) > maximum or used > MAX_TOTAL:
            raise ValueError("bounded analysis artifact budget exceeded")
        return data

    records = []
    for record in manifest["records"]:
        _verify_source(record, manifest["request"])
        records.append(_snapshot(record, manifest["model"], read))
    works = canonical_works(records)
    links = paper_repository_links(records, works, manifest["request"]["cutoff"])
    by_id = {r["id"]: r for r in records}
    vectors = []
    for work in works:
        # One representation per work prevents v1/v2, abstract+PDF and duplicate
        # provider records from giving a scientific work extra graph weight.
        representative = min((by_id[doc] for doc in work.document_ids), key=representation_preference)
        vectors.append({"canonical_id": work.id, "vector": representative["vector"],
                        "title": representative["title"][:1000],
                        "representative_text": representative["text"][:3000],
                        "document_id": representative["id"], "snapshot_id": representative["snapshot_id"]})
    corpus = {"input": manifest, "input_sha256": sha256, "versions": VERSIONS,
              "records": records, "works": [asdict(work) for work in works],
              "relations": [asdict(link) for link in links], "vectors": vectors}
    page_limit = min(MAX_COVERAGE_PAGES, (MAX_TOTAL - used) // (2 * MAX_COVERAGE_OBJECT))
    enrich(corpus, read, page_limit=page_limit)
    corpus["read_bytes"] = used
    return corpus
