"""Derive coverage/material/release facts only from hash-pinned collection pages."""
from __future__ import annotations

from collections import defaultdict
from datetime import date, datetime, timedelta
import hashlib
import json

VERSION = 'contiguous-provider-cursor-sweep-v1'
MAX_COVERAGE_PAGES = 64
MAX_COVERAGE_OBJECT = 16 << 20


def _key(source: dict) -> tuple:
    return source["provider"], source["native_id"], source["revision"], source["content_hash"]


def enrich(corpus: dict, read, *, page_limit: int = MAX_COVERAGE_PAGES) -> None:
    records = corpus["records"]
    by_source = defaultdict(list)
    work_by_doc = {doc: work for work in corpus["works"] for doc in work["document_ids"]}
    for record in records:
        by_source[_key(record["source"])].append(record)
        record["collection_scope_ids"] = []
        record["collection_run_keys"] = []
        record["text_status"] = "full_text" if record["representation"] == "full_text" else "unclassified_text"
    groups = defaultdict(list)
    releases = {}
    refs = corpus["input"]["pages"]
    page_limit = max(0, min(MAX_COVERAGE_PAGES, page_limit))
    skipped = len(refs) > page_limit
    corpus["coverage_read_status"] = {
        "status": "not_read_page_budget" if skipped else "read",
        "total_pages": len(refs), "read_pages": 0 if skipped else len(refs),
        "page_limit": page_limit, "object_byte_limit": MAX_COVERAGE_OBJECT,
    }
    corpus["coverage_limitations"] = ["collection_coverage_not_read_page_budget"] if skipped else []
    cutoff = datetime.fromisoformat(corpus["input"]["request"]["cutoff"].replace("Z", "+00:00"))
    # Частичный обход может ложно подтвердить полноту одного из исходных поисков.
    for ref in (() if skipped else refs):
        raw = read(ref["manifest"]["key"], MAX_COVERAGE_OBJECT)
        if hashlib.sha256(raw).hexdigest() != ref["manifest"]["sha256"]:
            raise ValueError("collection page artifact hash mismatch")
        stored = json.loads(raw)
        page = stored["page"]
        if (page["provider"] != ref["provider"] or page["scope"] != ref["scope"]
                or page["raw_sha256"] != ref["raw"]["sha256"]
                or page["received"] != ref["received"] or page["exhausted"] != ref["exhausted"]
                or datetime.fromisoformat(page["observed_at"].replace("Z", "+00:00")) > cutoff):
            raise ValueError("collection coverage differs from immutable input")
        raw_response = read(ref["raw"]["key"], MAX_COVERAGE_OBJECT)
        if hashlib.sha256(raw_response).hexdigest() != ref["raw"]["sha256"]:
            raise ValueError("raw collection response hash mismatch")
        page["all_prepared_in_input"] = all(_key(item["source"]) in by_source for item in stored["prepared"])
        # Preparation may replace metadata bytes with a PDF. Native identity
        # and revision must still cover every received provider record exactly.
        page_ids = [(item["source"]["provider"], item["source"]["native_id"], item["source"]["revision"])
                    for item in page["records"]]
        prepared_ids = [(item["source"]["provider"], item["source"]["native_id"], item["source"]["revision"])
                        for item in stored["prepared"]]
        page["all_records_prepared"] = (len(page_ids) == len(set(page_ids)) == page["received"]
                                         and sorted(page_ids) == sorted(prepared_ids))
        source_records = {(r["source"]["native_id"], r["source"]["revision"]): r for r in page["records"]}
        for prepared in stored["prepared"]:
            for record in by_source.get(_key(prepared["source"]), []):
                record["collection_scope_ids"].append(ref["scope_id"])
                record["collection_run_keys"].append(ref["scope_id"] + "/" + ref["run_id"])
                record["text_status"] = prepared["text_status"]
                record["limitations"] = sorted(set(record["limitations"] + (prepared.get("limitations") or [])))
                source = source_records.get((prepared["source"]["native_id"], prepared["source"]["revision"]))
                if source and record["source"]["source_kind"] == "software_repository":
                    _releases(record, source, ref, work_by_doc, cutoff, releases)
        # После извлечения релизов тексты страниц больше не нужны для проверки обхода.
        summary = {key: value for key, value in page.items() if key in (
            "provider", "scope", "provider_total", "limitations", "cursor", "next_cursor",
            "observed_at", "received", "exhausted", "all_prepared_in_input", "all_records_prepared",
        )}
        summary["records"] = [{"source": {"provider": item["source"]["provider"],
                                            "native_id": item["source"]["native_id"]}}
                              for item in page["records"]]
        groups[ref["scope_id"], ref["run_id"]].append((ref, summary))
    # Provider metadata may confirm one release even when this reader cannot
    # access collection manifests. Retain that lower bound with its own basis.
    for record in records:
        record["collection_scope_ids"] = sorted(set(record["collection_scope_ids"]))
        record["collection_run_keys"] = sorted(set(record["collection_run_keys"]))
        github = record["source"].get("github")
        if github and github.get("release_id") and github.get("released_at"):
            _metadata_release(record, work_by_doc, cutoff, releases)
    corpus["releases"] = sorted(releases.values(), key=lambda item: (item["repository_id"], item["id"]))
    corpus["coverage_windows"] = _complete_windows(groups, corpus["works"], records)
    # Enroll all scientific search scopes before measuring candidates. A newly
    # exhausted search for one paper must not certify a wider, capped search.
    by_id = {record["id"]: record for record in records}
    corpus["coverage_enrollment"] = {
        "scientific_scope_ids": sorted({ref["scope_id"] for ref in refs
                                         if ref["provider"] in ("arxiv", "crossref")}),
        "unscoped_scientific_work_ids": sorted(work["id"] for work in corpus["works"]
                                               if work["kind"] == "paper" and not any(
                                                   by_id[doc]["collection_run_keys"]
                                                   for doc in work["document_ids"])),
    }


def _releases(record, source, ref, work_by_doc, cutoff, releases):
    repository = record["source"]["github"]["repository_id"]
    work = work_by_doc[record["id"]]
    for release in source.get("releases") or []:
        if datetime.fromisoformat(release["published_at"].replace("Z", "+00:00")) > cutoff:
            continue
        key = repository, release["id"]
        releases.setdefault(key, {"repository_id": repository, "repository_work_id": work["id"],
                                 "id": release["id"], "published_at": release["published_at"],
                                 "url": release["url"], "tag": release["tag"],
                                 "document_id": record["id"], "snapshot_id": record["snapshot_id"],
                                 "basis": {"kind": "collection_page", "artifact": ref["manifest"],
                                           "scope_id": ref["scope_id"], "run_id": ref["run_id"]}})


def _metadata_release(record, work_by_doc, cutoff, releases):
    github = record["source"]["github"]
    if datetime.fromisoformat(github["released_at"].replace("Z", "+00:00")) > cutoff:
        return
    key = github["repository_id"], github["release_id"]
    releases.setdefault(key, {"repository_id": key[0], "id": key[1],
                             "repository_work_id": work_by_doc[record["id"]]["id"],
                             "published_at": github["released_at"], "url": None, "tag": None,
                             "document_id": record["id"], "snapshot_id": record["snapshot_id"],
                             "basis": {"kind": "source_metadata", "field": "github.released_at",
                                       "limitation": "only_one_recorded_release; full_history_unknown"}})


def _verified_sweep(pages):
    """A batch/run boundary is irrelevant; a cursor or population break is not."""
    first = pages[0][1]
    if first["provider"] not in ("arxiv", "crossref"):
        return None
    by_cursor, source_ids = {}, set()
    for ref, page in pages:
        if (page["provider"] != first["provider"] or page["scope"] != first["scope"]
                or type(page["provider_total"]) is not int or page["provider_total"] < 0
                or page["provider_total"] != first["provider_total"]
                or page.get("limitations") or ref["rejected"] or not page["all_prepared_in_input"]
                or not page["all_records_prepared"] or page["cursor"] in by_cursor):
            return None
        by_cursor[page["cursor"]] = ref, page
        for record in page["records"]:
            source = record["source"]
            key = source["provider"], source["native_id"]
            # Repeated native records, including changed revisions or bytes,
            # cannot establish an unchanged completely enumerated population.
            if source["provider"] != first["provider"] or key in source_ids:
                return None
            source_ids.add(key)
    starts = [c for c in ("", "0" if first["provider"] == "arxiv" else "*") if c in by_cursor]
    if len(starts) != 1:
        return None
    cursor, visited, chain = starts[0], set(), []
    while cursor in by_cursor and cursor not in visited:
        ref, page = by_cursor[cursor]
        if chain and datetime.fromisoformat(page["observed_at"].replace("Z", "+00:00")) < \
                datetime.fromisoformat(chain[-1][1]["observed_at"].replace("Z", "+00:00")):
            return None
        chain.append((ref, page))
        visited.add(cursor)
        if page["exhausted"]:
            if page["next_cursor"] or len(chain) != len(pages) or \
                    sum(p["received"] for _, p in chain) != page["provider_total"]:
                return None
            return chain
        cursor = page["next_cursor"]
    return None


def _complete_windows(groups, works, records):
    by_id = {r["id"]: r for r in records}
    scopes = defaultdict(list)
    for (scope_id, _), pages in groups.items():
        scopes[scope_id].extend(pages)
    windows = []
    for scope_id, pages in sorted(scopes.items()):
        pages = _verified_sweep(pages)
        if pages is None:
            continue
        first = pages[0][1]
        run_ids = list(dict.fromkeys(ref["run_id"] for ref, _ in pages))
        run_keys = {scope_id + "/" + run_id for run_id in run_ids}
        population = [work["id"] for work in works if work["kind"] == "paper"
                      and any(run_keys.intersection(by_id[doc]["collection_run_keys"]) for doc in work["document_ids"])]
        if any(not by_id[doc]["source"].get("publication")
               or by_id[doc]["source"]["publication"]["precision"] == "year"
               for work in works if work["id"] in population for doc in work["document_ids"]):
            continue
        windows.append({"start": first["scope"]["from"],
                        "end_exclusive": (date.fromisoformat(first["scope"]["through"]) + timedelta(days=1)).isoformat(),
                        "work_ids": population, "complete": True,
                        "basis": {"scope_id": scope_id, "run_id": run_ids[0], "run_ids": run_ids,
                                  "proof_version": VERSION,
                                  "page_manifests": [ref["manifest"] for ref, _ in pages],
                                  "query": first["scope"]["query"],
                                  "meaning": "exhausted_bounded_provider_search; not_semantic_or_global_census"}})
    return windows
