#!/usr/bin/env python3
"""Build and upload a mixed arXiv PDF corpus for load-testing the RAG system.

The loader is intentionally standard-library only. It collects metadata from
arXiv, downloads PDFs, uploads them through the same /documents API used by the
frontend, and keeps a resumable manifest on disk.
"""

from __future__ import annotations

import argparse
import json
import mimetypes
import re
import sys
import time
import uuid
import xml.etree.ElementTree as ET
from dataclasses import dataclass
from http import HTTPStatus
from pathlib import Path
from typing import Any
from urllib import error, parse, request

sys.path.insert(0, str(Path(__file__).resolve().parents[2]))
from tools.stand_env import configure_dataset_args, load_stand_env, normalize_api_base, redact


ARXIV_API_URL = "https://export.arxiv.org/api/query"
USER_AGENT = "demo-rag-arxiv-corpus-loader/1.0 (contact: local-dev)"

TOPIC_QUERIES: dict[str, list[str]] = {
    "medicine": [
        "cat:cs.CV AND all:medical AND all:imaging",
        "cat:eess.IV AND all:medical",
        "cat:cs.LG AND all:clinical",
        "cat:q-bio.PE AND all:clinical",
        "cat:q-bio.QM AND all:cancer",
        "cat:q-bio.BM AND all:disease",
        "cat:q-bio.TO AND all:medicine",
        "cat:stat.AP AND all:clinical",
        "cat:q-bio.NC AND all:diagnosis",
        "cat:cs.AI AND all:healthcare",
        "cat:q-bio.CB AND all:cancer",
        "cat:eess.SP AND all:medical",
    ],
    "petrochemistry": [
        "all:petrochemical",
        "all:petroleum",
        "all:refinery",
        "all:oil AND all:gas",
        "all:reservoir AND all:petroleum",
        "all:seismic AND all:reservoir",
        "all:hydrocarbon",
        "all:hydrocarbon AND all:catalysis",
        "all:zeolite",
        "all:zeolite AND all:adsorption",
        "all:zeolite AND all:catalysis",
        "all:catalysis",
        "all:catalyst AND all:hydrocarbon",
        "all:polyolefin",
        "all:polymer AND all:process",
        "all:chemical AND all:process AND all:optimization",
        "all:process AND all:control AND all:chemical",
        "all:distillation",
        "all:desulfurization",
        "all:membrane AND all:separation",
        "all:CO2 AND all:reservoir",
    ],
    "machine_learning": [
        "cat:cs.LG",
        "cat:stat.ML",
        "cat:cs.AI AND all:learning",
        "cat:cs.CV AND all:transformer",
        "cat:cs.CL AND all:language AND all:model",
        "cat:cs.LG AND all:reinforcement",
        "cat:cs.LG AND all:graph AND all:neural",
        "cat:cs.LG AND all:diffusion",
        "cat:cs.LG AND all:foundation AND all:model",
        "cat:cs.AI AND all:agent",
        "cat:cs.CV AND all:segmentation",
        "cat:cs.CL AND all:retrieval",
    ],
}


class ApiError(RuntimeError):
    def __init__(self, status: int, body: bytes, headers: dict[str, str] | None = None):
        super().__init__(f"HTTP {status}")
        self.status = status
        self.body = body
        self.headers = headers or {}


@dataclass
class Paths:
    root: Path
    pdfs: Path
    logs: Path
    metadata: Path
    summary: Path
    download_failures: Path
    upload_manifest: Path
    upload_failures: Path


def build_paths(dataset_dir: Path) -> Paths:
    return Paths(
        root=dataset_dir,
        pdfs=dataset_dir / "pdfs",
        logs=dataset_dir / "logs",
        metadata=dataset_dir / "metadata.jsonl",
        summary=dataset_dir / "dataset_summary.json",
        download_failures=dataset_dir / "logs" / "download_failures.json",
        upload_manifest=dataset_dir / "upload_manifest.jsonl",
        upload_failures=dataset_dir / "logs" / "upload_failures.json",
    )


def ensure_dirs(paths: Paths) -> None:
    paths.pdfs.mkdir(parents=True, exist_ok=True)
    paths.logs.mkdir(parents=True, exist_ok=True)


def compact_text(value: str) -> str:
    return re.sub(r"\s+", " ", value).strip()


def safe_filename(value: str, max_len: int = 82) -> str:
    value = compact_text(value)
    value = re.sub(r"[^A-Za-z0-9._-]+", "_", value).strip("._-")
    return value[:max_len].strip("._-") or "paper"


def arxiv_pdf_url(arxiv_id: str) -> str:
    return f"https://export.arxiv.org/pdf/{arxiv_id}"


def load_jsonl(path: Path) -> list[dict[str, Any]]:
    if not path.exists():
        return []
    rows: list[dict[str, Any]] = []
    with path.open("r", encoding="utf-8") as fh:
        for line_no, line in enumerate(fh, start=1):
            line = line.strip()
            if not line:
                continue
            try:
                rows.append(json.loads(line))
            except json.JSONDecodeError as exc:
                raise RuntimeError(f"{path}:{line_no}: invalid JSONL: {exc}") from exc
    return rows


def write_jsonl(path: Path, rows: list[dict[str, Any]]) -> None:
    tmp = path.with_suffix(path.suffix + ".tmp")
    with tmp.open("w", encoding="utf-8") as fh:
        for row in rows:
            fh.write(json.dumps(row, ensure_ascii=False, sort_keys=True) + "\n")
    tmp.replace(path)


def write_json(path: Path, value: Any) -> None:
    tmp = path.with_suffix(path.suffix + ".tmp")
    tmp.write_text(json.dumps(redact(value, load_stand_env()), ensure_ascii=False, indent=2, sort_keys=True) + "\n", encoding="utf-8")
    tmp.replace(path)


def http_bytes(
    url: str,
    *,
    method: str = "GET",
    headers: dict[str, str] | None = None,
    data: bytes | None = None,
    timeout: float = 120,
) -> tuple[int, bytes, dict[str, str]]:
    req_headers = {"User-Agent": USER_AGENT}
    if headers:
        req_headers.update(headers)
    req = request.Request(url, data=data, headers=req_headers, method=method)
    try:
        with request.urlopen(req, timeout=timeout) as resp:
            return resp.status, resp.read(), dict(resp.headers.items())
    except error.HTTPError as exc:
        body = exc.read()
        raise ApiError(exc.code, body, dict(exc.headers.items())) from exc


def http_bytes_retry(
    url: str,
    *,
    method: str = "GET",
    headers: dict[str, str] | None = None,
    data: bytes | None = None,
    timeout: float = 120,
    retries: int = 4,
    sleep_seconds: float = 3.2,
) -> tuple[int, bytes, dict[str, str]]:
    for attempt in range(1, retries + 1):
        try:
            return http_bytes(url, method=method, headers=headers, data=data, timeout=timeout)
        except ApiError as exc:
            if exc.status != HTTPStatus.TOO_MANY_REQUESTS or attempt >= retries:
                raise
            wait = max(90.0, sleep_seconds * attempt)
            retry_after = exc.headers.get("Retry-After")
            if retry_after:
                try:
                    wait = max(wait, float(retry_after))
                except ValueError:
                    pass
            print(f"[http] 429 retry {attempt + 1}/{retries} wait={wait:.1f}s url=[source address omitted]", flush=True)
            time.sleep(wait)
        except Exception:
            if attempt >= retries:
                raise
            wait = sleep_seconds * attempt
            print(f"[http] retry {attempt + 1}/{retries} wait={wait:.1f}s url=[source address omitted]", flush=True)
            time.sleep(wait)
    raise RuntimeError("unreachable retry loop")


def json_request(
    url: str,
    *,
    method: str = "GET",
    token: str | None = None,
    payload: Any | None = None,
    timeout: float = 120,
) -> Any:
    headers = {"Accept": "application/json"}
    data = None
    if payload is not None:
        data = json.dumps(payload).encode("utf-8")
        headers["Content-Type"] = "application/json"
    if token:
        headers["Authorization"] = f"Bearer {token}"
    status, body, _ = http_bytes(url, method=method, headers=headers, data=data, timeout=timeout)
    if status == HTTPStatus.NO_CONTENT:
        return None
    return json.loads(body.decode("utf-8"))


def parse_arxiv_feed(xml_body: bytes, topic: str, source_query: str) -> list[dict[str, Any]]:
    ns = {
        "atom": "http://www.w3.org/2005/Atom",
        "arxiv": "http://arxiv.org/schemas/atom",
    }
    root = ET.fromstring(xml_body)
    rows: list[dict[str, Any]] = []
    for entry in root.findall("atom:entry", ns):
        id_url = entry.findtext("atom:id", default="", namespaces=ns)
        arxiv_id = id_url.rstrip("/").split("/")[-1]
        title = compact_text(entry.findtext("atom:title", default="", namespaces=ns))
        abstract = compact_text(entry.findtext("atom:summary", default="", namespaces=ns))
        authors = [
            compact_text(author.findtext("atom:name", default="", namespaces=ns))
            for author in entry.findall("atom:author", ns)
        ]
        authors = [author for author in authors if author]
        categories = [
            cat.attrib.get("term", "")
            for cat in entry.findall("atom:category", ns)
            if cat.attrib.get("term")
        ]
        primary = entry.find("arxiv:primary_category", ns)
        primary_category = primary.attrib.get("term", "") if primary is not None else ""
        pdf_url = ""
        abs_url = id_url
        for link in entry.findall("atom:link", ns):
            href = link.attrib.get("href", "")
            title_attr = link.attrib.get("title", "")
            rel = link.attrib.get("rel", "")
            if title_attr == "pdf" or "/pdf/" in href:
                pdf_url = href
            if rel == "alternate" and href:
                abs_url = href
        if arxiv_id and not pdf_url:
            pdf_url = arxiv_pdf_url(arxiv_id)
        if not arxiv_id:
            continue
        rows.append(
            {
                "source": "arxiv",
                "topic": topic,
                "source_query": source_query,
                "arxiv_id": arxiv_id,
                "title": title,
                "abstract": abstract,
                "authors": authors,
                "categories": categories,
                "primary_category": primary_category,
                "published": entry.findtext("atom:published", default="", namespaces=ns),
                "updated": entry.findtext("atom:updated", default="", namespaces=ns),
                "abs_url": abs_url,
                "pdf_url": pdf_url,
            }
        )
    return rows


def topic_counts(rows: list[dict[str, Any]], field: str = "topic") -> dict[str, int]:
    counts: dict[str, int] = {}
    for row in rows:
        key = str(row.get(field) or "unknown")
        counts[key] = counts.get(key, 0) + 1
    return counts


def parse_quotas(value: str, target: int) -> dict[str, int]:
    if value:
        quotas: dict[str, int] = {}
        for part in value.split(","):
            name, _, raw = part.strip().partition("=")
            if not name or not raw:
                raise ValueError("quota must look like medicine=334,petrochemistry=333,machine_learning=333")
            if name not in TOPIC_QUERIES:
                raise ValueError(f"unknown topic {name!r}; known: {', '.join(TOPIC_QUERIES)}")
            quotas[name] = int(raw)
        if sum(quotas.values()) != target:
            raise ValueError(f"quota sum {sum(quotas.values())} does not match target {target}")
        return quotas
    topics = list(TOPIC_QUERIES)
    base, extra = divmod(target, len(topics))
    return {topic: base + (1 if i < extra else 0) for i, topic in enumerate(topics)}


def collect_metadata(
    paths: Paths,
    quotas: dict[str, int],
    metadata_factor: float,
    page_size: int,
    max_pages_per_query: int,
    sleep_seconds: float,
) -> list[dict[str, Any]]:
    existing = load_jsonl(paths.metadata)
    by_id = {row.get("arxiv_id"): row for row in existing if row.get("arxiv_id")}
    rows = list(existing)

    for topic, quota in quotas.items():
        target_rows = max(quota, int(quota * metadata_factor))
        current = sum(1 for row in rows if row.get("topic") == topic)
        print(f"[metadata] topic={topic} have={current} target_rows={target_rows}", flush=True)
        if current >= target_rows:
            continue
        for query in TOPIC_QUERIES[topic]:
            if current >= target_rows:
                break
            for page in range(max_pages_per_query):
                if current >= target_rows:
                    break
                start = page * page_size
                params = parse.urlencode(
                    {
                        "search_query": query,
                        "start": start,
                        "max_results": page_size,
                        "sortBy": "submittedDate",
                        "sortOrder": "descending",
                    }
                )
                url = f"{ARXIV_API_URL}?{params}"
                print(f"[metadata] topic={topic} query={query!r} start={start}", flush=True)
                _, body, _ = http_bytes_retry(url, timeout=120, retries=5, sleep_seconds=sleep_seconds)
                parsed = parse_arxiv_feed(body, topic, query)
                if not parsed:
                    break
                added = 0
                for row in parsed:
                    arxiv_id = row["arxiv_id"]
                    if arxiv_id in by_id:
                        old = by_id[arxiv_id]
                        topics = set(old.get("topics") or [old.get("topic", topic)])
                        topics.add(topic)
                        old["topics"] = sorted(t for t in topics if t)
                        continue
                    by_id[arxiv_id] = row
                    rows.append(row)
                    current += 1
                    added += 1
                    if current >= target_rows:
                        break
                write_jsonl(paths.metadata, rows)
                print(f"[metadata] topic={topic} added={added} have={current}/{target_rows}", flush=True)
                time.sleep(sleep_seconds)

        if current < quota:
            print(f"[metadata] warning: topic={topic} collected only {current}, quota={quota}", flush=True)

    write_jsonl(paths.metadata, rows)
    return rows


def is_pdf(path: Path) -> bool:
    try:
        with path.open("rb") as fh:
            return fh.read(5).startswith(b"%PDF")
    except OSError:
        return False


def record_filename(index: int, row: dict[str, Any]) -> str:
    topic = safe_filename(str(row.get("topic", "topic")), 32)
    arxiv_id = safe_filename(str(row.get("arxiv_id", "paper")), 32)
    title = safe_filename(str(row.get("title", "paper")), 72)
    return f"{topic}_{index:04d}_{arxiv_id}_{title}.pdf"


def download_one(
    url: str,
    dest: Path,
    retries: int,
    timeout: float,
    sleep_seconds: float,
    max_pdf_mb: int,
) -> None:
    tmp = dest.with_suffix(dest.suffix + ".part")
    for attempt in range(1, retries + 1):
        try:
            status, body, _ = http_bytes_retry(url, timeout=timeout, retries=1, sleep_seconds=sleep_seconds)
            if status != HTTPStatus.OK:
                raise RuntimeError(f"unexpected HTTP status {status}")
            if max_pdf_mb > 0 and len(body) > max_pdf_mb * 1024 * 1024:
                raise RuntimeError(f"PDF exceeds max size {max_pdf_mb} MiB")
            tmp.write_bytes(body)
            if not is_pdf(tmp):
                raise RuntimeError("downloaded file is not a PDF")
            tmp.replace(dest)
            return
        except Exception as exc:
            tmp.unlink(missing_ok=True)
            if attempt >= retries:
                raise
            wait = sleep_seconds * attempt
            if isinstance(exc, ApiError) and exc.status == HTTPStatus.TOO_MANY_REQUESTS:
                retry_after = exc.headers.get("Retry-After")
                if retry_after:
                    try:
                        wait = max(wait, float(retry_after))
                    except ValueError:
                        wait = max(wait, 90.0)
                else:
                    wait = max(wait, 90.0)
            print(f"[download] retry attempt={attempt + 1}/{retries} wait={wait:.1f}s url=[source address omitted]", flush=True)
            time.sleep(wait)


def download_pdfs(
    paths: Paths,
    rows: list[dict[str, Any]],
    quotas: dict[str, int],
    retries: int,
    timeout: float,
    sleep_seconds: float,
    max_pdf_mb: int,
) -> list[dict[str, Any]]:
    failures = load_jsonl(paths.download_failures) if paths.download_failures.exists() else []
    selected_counts = {topic: 0 for topic in quotas}

    for row in rows:
        row["selected_for_upload"] = False

    index = 0
    for topic, quota in quotas.items():
        candidates = [row for row in rows if row.get("topic") == topic]
        for row in candidates:
            if selected_counts[topic] >= quota:
                break
            index += 1
            filename = row.get("filename") or record_filename(index, row)
            row["filename"] = filename
            local_path = paths.pdfs / filename
            row["local_path"] = str(local_path)

            if is_pdf(local_path):
                selected_counts[topic] += 1
                row["selected_for_upload"] = True
                print(f"[download] cached topic={topic} {selected_counts[topic]}/{quota} {row.get('arxiv_id')}", flush=True)
                continue

            url = str(row.get("pdf_url") or arxiv_pdf_url(str(row["arxiv_id"])))
            try:
                download_one(url, local_path, retries, timeout, sleep_seconds, max_pdf_mb)
                selected_counts[topic] += 1
                row["selected_for_upload"] = True
                row["downloaded_at"] = time.strftime("%Y-%m-%dT%H:%M:%SZ", time.gmtime())
                size_kib = local_path.stat().st_size // 1024
                print(
                    f"[download] ok topic={topic} {selected_counts[topic]}/{quota} "
                    f"{row.get('arxiv_id')} {size_kib} KiB",
                    flush=True,
                )
                write_jsonl(paths.metadata, rows)
                time.sleep(sleep_seconds)
            except Exception as exc:
                failure = {
                    "topic": topic,
                    "arxiv_id": row.get("arxiv_id"),
                    "pdf_url": url,
                    "error": type(exc).__name__,
                    "timestamp": time.strftime("%Y-%m-%dT%H:%M:%SZ", time.gmtime()),
                }
                failures.append(failure)
                print(f"[download] skip topic={topic} {row.get('arxiv_id')}: {type(exc).__name__}", flush=True)

    write_jsonl(paths.metadata, rows)
    write_json(paths.download_failures, failures)
    missing = {topic: quotas[topic] - selected_counts.get(topic, 0) for topic in quotas if selected_counts.get(topic, 0) < quotas[topic]}
    if missing:
        raise RuntimeError(f"not enough PDFs downloaded: {missing}")
    return rows


def auth_token(api_base: str, username: str, password: str, skip_register: bool) -> str:
    login_url = f"{api_base}/auth/login"
    register_url = f"{api_base}/auth/register"
    if not skip_register:
        try:
            token = json_request(register_url, method="POST", payload={"username": username, "password": password})
            print("[auth] registered", flush=True)
            return str(token["access_token"])
        except ApiError as exc:
            if exc.status != HTTPStatus.CONFLICT:
                raise
            print("[auth] user exists, logging in", flush=True)

    token = json_request(login_url, method="POST", payload={"username": username, "password": password})
    print("[auth] logged in", flush=True)
    return str(token["access_token"])


def multipart_body(field: str, file_path: Path, filename: str) -> tuple[bytes, str]:
    boundary = f"----demo-rag-{uuid.uuid4().hex}"
    content_type = mimetypes.guess_type(filename)[0] or "application/pdf"
    head = (
        f"--{boundary}\r\n"
        f'Content-Disposition: form-data; name="{field}"; filename="{filename}"\r\n'
        f"Content-Type: {content_type}\r\n\r\n"
    ).encode("utf-8")
    tail = f"\r\n--{boundary}--\r\n".encode("utf-8")
    return head + file_path.read_bytes() + tail, f"multipart/form-data; boundary={boundary}"


def upload_pdf(api_base: str, token: str, file_path: Path, filename: str, timeout: float) -> dict[str, Any]:
    body, content_type = multipart_body("file", file_path, filename)
    status, raw, _ = http_bytes(
        f"{api_base}/documents",
        method="POST",
        headers={
            "Authorization": f"Bearer {token}",
            "Accept": "application/json",
            "Content-Type": content_type,
        },
        data=body,
        timeout=timeout,
    )
    if status not in (HTTPStatus.OK, HTTPStatus.CREATED):
        raise ApiError(status, raw)
    return json.loads(raw.decode("utf-8"))


def load_uploaded_ids(path: Path) -> set[str]:
    uploaded: set[str] = set()
    for row in load_jsonl(path):
        if row.get("ok") and row.get("arxiv_id"):
            uploaded.add(str(row["arxiv_id"]))
    return uploaded


def upload_dataset(
    paths: Paths,
    rows: list[dict[str, Any]],
    api_base: str,
    username: str,
    password: str,
    skip_register: bool,
    force_upload: bool,
    timeout: float,
    sleep_seconds: float,
) -> None:
    token = auth_token(api_base, username, password, skip_register)
    already_uploaded = set() if force_upload else load_uploaded_ids(paths.upload_manifest)
    manifest = load_jsonl(paths.upload_manifest)
    failures: list[dict[str, Any]] = []

    candidates = [row for row in rows if row.get("selected_for_upload") and row.get("local_path")]
    for idx, row in enumerate(candidates, start=1):
        arxiv_id = str(row.get("arxiv_id", ""))
        topic = str(row.get("topic", ""))
        if arxiv_id in already_uploaded:
            print(f"[upload] skip already uploaded {idx}/{len(candidates)} topic={topic} {arxiv_id}", flush=True)
            continue
        file_path = Path(str(row["local_path"]))
        if not is_pdf(file_path):
            failures.append({"topic": topic, "arxiv_id": arxiv_id, "error": "missing or invalid PDF"})
            continue
        filename = str(row.get("filename") or file_path.name)
        try:
            doc = upload_pdf(api_base, token, file_path, filename, timeout)
            entry = {
                "ok": True,
                "topic": topic,
                "arxiv_id": arxiv_id,
                "filename": filename,
                "local_path": str(file_path),
                "document_id": doc.get("id"),
                "document_status": doc.get("status"),
                "uploaded_at": time.strftime("%Y-%m-%dT%H:%M:%SZ", time.gmtime()),
            }
            manifest.append(entry)
            already_uploaded.add(arxiv_id)
            write_jsonl(paths.upload_manifest, manifest)
            print(f"[upload] ok {idx}/{len(candidates)} topic={topic} {arxiv_id} doc={doc.get('id')}", flush=True)
            time.sleep(sleep_seconds)
        except Exception as exc:
            failure = {
                "topic": topic,
                "arxiv_id": arxiv_id,
                "filename": filename,
                "local_path": str(file_path),
                "error": type(exc).__name__,
                "timestamp": time.strftime("%Y-%m-%dT%H:%M:%SZ", time.gmtime()),
            }
            failures.append(failure)
            print(f"[upload] failed {idx}/{len(candidates)} topic={topic} {arxiv_id}: {type(exc).__name__}", flush=True)

    write_json(paths.upload_failures, failures)


def document_status_counts(api_base: str, username: str, password: str, skip_register: bool) -> dict[str, int]:
    token = auth_token(api_base, username, password, skip_register)
    docs = json_request(f"{api_base}/documents", token=token)
    counts: dict[str, int] = {}
    for doc in docs:
        status = str(doc.get("status", "unknown"))
        counts[status] = counts.get(status, 0) + 1
    counts["total"] = len(docs)
    return counts


def poll_documents(
    api_base: str,
    username: str,
    password: str,
    skip_register: bool,
    timeout_seconds: int,
    interval_seconds: int,
) -> dict[str, int]:
    deadline = time.time() + timeout_seconds
    last_counts: dict[str, int] = {}
    while True:
        last_counts = document_status_counts(api_base, username, password, skip_register)
        print(f"[poll] document statuses: {last_counts}", flush=True)
        if time.time() >= deadline:
            return last_counts
        active = sum(last_counts.get(status, 0) for status in ("uploaded", "queued", "parsing", "parsed", "chunking"))
        if last_counts.get("total", 0) > 0 and active == 0:
            return last_counts
        time.sleep(interval_seconds)


def summarize(paths: Paths, rows: list[dict[str, Any]], quotas: dict[str, int], final_counts: dict[str, int] | None = None) -> None:
    selected = [row for row in rows if row.get("selected_for_upload") and is_pdf(Path(str(row.get("local_path", ""))))]
    manifest = load_jsonl(paths.upload_manifest)
    upload_ok = [row for row in manifest if row.get("ok")]
    value = {
        "source": "arxiv",
        "metadata_rows": len(rows),
        "metadata_by_topic": topic_counts(rows),
        "pdfs_selected": len(selected),
        "pdfs_selected_by_topic": topic_counts(selected),
        "uploaded_ok": len(upload_ok),
        "uploaded_ok_by_topic": topic_counts(upload_ok),
        "api_document_status_counts": final_counts or {},
        "quotas": quotas,
        "metadata_path": str(paths.metadata),
        "pdf_dir": str(paths.pdfs),
        "upload_manifest_path": str(paths.upload_manifest),
        "generated_at": time.strftime("%Y-%m-%dT%H:%M:%SZ", time.gmtime()),
        "queries": TOPIC_QUERIES,
    }
    write_json(paths.summary, value)


def parse_args() -> argparse.Namespace:
    parser = argparse.ArgumentParser(description=__doc__)
    try:
        env = load_stand_env()
    except ValueError as failure:
        parser.error(str(failure))
    parser.add_argument("--dataset-dir", default="datasets/arxiv_mixed_1000", type=Path)
    parser.add_argument("--target", default=1000, type=int, help="PDFs to download/select/upload")
    parser.add_argument("--quotas", default="", help="topic quotas, e.g. medicine=334,petrochemistry=333,machine_learning=333")
    parser.add_argument("--metadata-factor", default=1.35, type=float, help="metadata rows to collect per quota")
    parser.add_argument("--page-size", default=100, type=int, help="arXiv API page size")
    parser.add_argument("--max-pages-per-query", default=8, type=int)
    parser.add_argument("--api-base", help="compatibility: must match RAG_API")
    parser.add_argument("--username", help="compatibility: must match RAG_DATASET_USER")
    parser.add_argument("--password", help=argparse.SUPPRESS)
    parser.add_argument("--collect-only", action="store_true", help="collect metadata and stop")
    parser.add_argument("--download-only", action="store_true", help="collect/download PDFs and stop before API upload")
    parser.add_argument("--upload-only", action="store_true", help="reuse existing metadata/PDFs and only upload")
    parser.add_argument("--skip-register", action="store_true", help="do not call /auth/register before login")
    parser.add_argument("--force-upload", action="store_true", help="upload even if upload_manifest already has success rows")
    parser.add_argument("--metadata-sleep", default=3.2, type=float, help="pause between arXiv API requests")
    parser.add_argument("--pdf-sleep", default=1.2, type=float, help="pause between PDF downloads/retries")
    parser.add_argument("--upload-sleep", default=0.05, type=float, help="pause between API uploads")
    parser.add_argument("--download-retries", default=3, type=int)
    parser.add_argument("--download-timeout", default=180, type=float)
    parser.add_argument("--upload-timeout", default=240, type=float)
    parser.add_argument("--max-pdf-mb", default=80, type=int, help="skip unusually large PDFs; 0 disables")
    parser.add_argument("--poll-seconds", default=0, type=int, help="poll document statuses after upload; 0 disables")
    parser.add_argument("--poll-interval", default=30, type=int)
    return configure_dataset_args(parser, parser.parse_args(), env)


def main() -> int:
    args = parse_args()
    if not args.upload_only:
        raise SystemExit(
            "Live arXiv collection moved to source-harvester --ingest, which owns the shared "
            "provider lease and rate limit. This legacy tool only supports --upload-only "
            "for already downloaded files."
        )
    if not (args.collect_only or args.download_only) and (not args.username or not args.password):
        raise SystemExit("RAG_DATASET_USER/RAG_DATASET_PASS environment variables are required for upload")
    api_base = normalize_api_base(args.api_base)
    quotas = parse_quotas(args.quotas, args.target)
    paths = build_paths(args.dataset_dir)
    ensure_dirs(paths)

    if args.upload_only:
        rows = load_jsonl(paths.metadata)
    else:
        rows = collect_metadata(
            paths,
            quotas,
            args.metadata_factor,
            args.page_size,
            args.max_pages_per_query,
            args.metadata_sleep,
        )
        if args.collect_only:
            summarize(paths, rows, quotas)
            return 0
        rows = download_pdfs(
            paths,
            rows,
            quotas,
            args.download_retries,
            args.download_timeout,
            args.pdf_sleep,
            args.max_pdf_mb,
        )
        if args.download_only:
            summarize(paths, rows, quotas)
            return 0

    upload_dataset(
        paths,
        rows,
        api_base,
        args.username,
        args.password,
        args.skip_register,
        args.force_upload,
        args.upload_timeout,
        args.upload_sleep,
    )
    counts = None
    if args.poll_seconds > 0:
        counts = poll_documents(api_base, args.username, args.password, True, args.poll_seconds, args.poll_interval)
    summarize(paths, rows, quotas, counts)
    return 0


if __name__ == "__main__":
    try:
        raise SystemExit(main())
    except KeyboardInterrupt:
        print("interrupted", file=sys.stderr)
        raise SystemExit(130)
    except Exception as failure:
        raise SystemExit("operation failed: " + type(failure).__name__) from None
