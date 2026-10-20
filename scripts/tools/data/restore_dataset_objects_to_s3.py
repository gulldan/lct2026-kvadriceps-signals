#!/usr/bin/env python3
"""Restore local dataset files to the configured S3 bucket using DB object keys.

Demo disaster recovery after recreating the S3 backend: it neither creates
document rows nor re-indexes anything.
"""

from __future__ import annotations

import argparse
import datetime as dt
import hashlib
import hmac
import json
import mimetypes
import subprocess
import sys
import time
from pathlib import Path
from typing import Any
from urllib import error, parse, request

from operator_env import add_env_argument, load_environment, public_output


DEFAULT_MANIFESTS = [
    Path("datasets/medical_publications/upload_manifest.jsonl"),
    Path("datasets/arxiv_mixed_1000/upload_manifest.jsonl"),
    Path("datasets/materials_hypotheses_corpus/upload_manifest.jsonl"),
]
DEFAULT_COMPOSE_FILE = Path("infra/docker-compose.yml")
USER_AGENT = "demo-rag-s3-restore/1.0"
EMPTY_SHA256 = hashlib.sha256(b"").hexdigest()


class S3HTTPError(RuntimeError):
    def __init__(self, status: int, body: bytes):
        self.status = status
        self.body = body
        super().__init__(f"HTTP {status}: {body[:500].decode('utf-8', errors='replace')}")


def load_manifests(paths: list[Path]) -> tuple[dict[str, Path], dict[str, Path]]:
    by_doc_id: dict[str, Path] = {}
    by_filename: dict[str, Path] = {}
    for path in paths:
        with path.open("r", encoding="utf-8") as fh:
            for line_no, line in enumerate(fh, start=1):
                line = line.strip()
                if not line:
                    continue
                row = json.loads(line)
                if not row.get("ok"):
                    continue
                doc_id = str(row.get("document_id") or "")
                filename = str(row.get("filename") or "")
                local_path = Path(str(row.get("local_path") or ""))
                if not local_path.is_absolute():
                    local_path = Path.cwd() / local_path
                if not filename or not local_path:
                    raise RuntimeError(f"{path}:{line_no}: missing filename/local_path")
                if doc_id:
                    by_doc_id[doc_id] = local_path
                by_filename.setdefault(filename, local_path)
    return by_doc_id, by_filename


def query_documents(compose_file: Path) -> list[dict[str, Any]]:
    sql = """
select jsonb_build_object(
  'id', id,
  'owner_id', owner_id,
  'filename', filename,
  'mime_type', mime_type,
  'object_key', object_key,
  'size', size,
  'status', status,
  'created_at', created_at
)::text
from documents
order by created_at, filename;
""".strip()
    cmd = [
        "docker",
        "compose",
        "-f",
        str(compose_file),
        "exec",
        "-T",
        "postgres",
        "psql",
        "-U",
        "rag",
        "-d",
        "rag",
        "-AtX",
        "-c",
        sql,
    ]
    proc = subprocess.run(cmd, check=True, text=True, capture_output=True)
    return [json.loads(line) for line in proc.stdout.splitlines() if line.strip()]


def sign(key: bytes, msg: str) -> bytes:
    return hmac.new(key, msg.encode("utf-8"), hashlib.sha256).digest()


def signing_key(secret_key: str, date_stamp: str, region: str, service: str = "s3") -> bytes:
    k_date = sign(("AWS4" + secret_key).encode("utf-8"), date_stamp)
    k_region = sign(k_date, region)
    k_service = sign(k_region, service)
    return sign(k_service, "aws4_request")


def canonical_uri(bucket: str, object_key: str = "") -> str:
    key = f"{bucket}/{object_key}" if object_key else bucket
    return "/" + parse.quote(key, safe="/~")


def signed_headers(
    *,
    method: str,
    endpoint: str,
    bucket: str,
    object_key: str,
    region: str,
    access_key: str,
    secret_key: str,
    payload_hash: str,
    extra_headers: dict[str, str] | None = None,
) -> dict[str, str]:
    parsed = parse.urlsplit(endpoint)
    host = parsed.netloc
    now = dt.datetime.now(dt.UTC)
    amz_date = now.strftime("%Y%m%dT%H%M%SZ")
    date_stamp = now.strftime("%Y%m%d")
    headers = {
        "host": host,
        "user-agent": USER_AGENT,
        "x-amz-content-sha256": payload_hash,
        "x-amz-date": amz_date,
    }
    if extra_headers:
        for key, value in extra_headers.items():
            headers[key.lower()] = " ".join(str(value).strip().split())

    signed_names = sorted(headers)
    canonical_headers = "".join(f"{name}:{headers[name]}\n" for name in signed_names)
    credential_scope = f"{date_stamp}/{region}/s3/aws4_request"
    canonical_request = "\n".join(
        [
            method,
            canonical_uri(bucket, object_key),
            "",
            canonical_headers,
            ";".join(signed_names),
            payload_hash,
        ]
    )
    string_to_sign = "\n".join(
        [
            "AWS4-HMAC-SHA256",
            amz_date,
            credential_scope,
            hashlib.sha256(canonical_request.encode("utf-8")).hexdigest(),
        ]
    )
    signature = hmac.new(
        signing_key(secret_key, date_stamp, region),
        string_to_sign.encode("utf-8"),
        hashlib.sha256,
    ).hexdigest()
    auth = (
        "AWS4-HMAC-SHA256 "
        f"Credential={access_key}/{credential_scope}, "
        f"SignedHeaders={';'.join(signed_names)}, "
        f"Signature={signature}"
    )
    out = {key: value for key, value in headers.items() if key != "host"}
    out["Authorization"] = auth
    out["Host"] = host
    return out


def object_url(endpoint: str, bucket: str, object_key: str) -> str:
    parsed = parse.urlsplit(endpoint.rstrip("/"))
    path = canonical_uri(bucket, object_key)
    return parse.urlunsplit((parsed.scheme, parsed.netloc, path, "", ""))


def s3_request(
    *,
    method: str,
    endpoint: str,
    bucket: str,
    object_key: str,
    region: str,
    access_key: str,
    secret_key: str,
    payload: bytes = b"",
    extra_headers: dict[str, str] | None = None,
    timeout: float,
) -> tuple[int, bytes, dict[str, str]]:
    payload_hash = hashlib.sha256(payload).hexdigest() if payload else EMPTY_SHA256
    headers = signed_headers(
        method=method,
        endpoint=endpoint,
        bucket=bucket,
        object_key=object_key,
        region=region,
        access_key=access_key,
        secret_key=secret_key,
        payload_hash=payload_hash,
        extra_headers=extra_headers,
    )
    data = payload if method not in {"HEAD", "GET"} else None
    req = request.Request(object_url(endpoint, bucket, object_key), data=data, headers=headers, method=method)
    try:
        with request.urlopen(req, timeout=timeout) as resp:
            return resp.status, resp.read(), dict(resp.headers.items())
    except error.HTTPError as exc:
        body = exc.read()
        raise S3HTTPError(exc.code, body) from exc


def head_size(args: argparse.Namespace, object_key: str) -> int | None:
    try:
        _, _, headers = s3_request(
            method="HEAD",
            endpoint=args.endpoint,
            bucket=args.bucket,
            object_key=object_key,
            region=args.region,
            access_key=args.access_key,
            secret_key=args.secret_key,
            timeout=args.timeout,
        )
    except S3HTTPError as exc:
        if exc.status == 404:
            return None
        raise
    value = headers.get("Content-Length") or headers.get("content-length")
    return int(value) if value and value.isdigit() else None


def put_file(args: argparse.Namespace, object_key: str, local_path: Path, mime_type: str) -> None:
    payload = local_path.read_bytes()
    s3_request(
        method="PUT",
        endpoint=args.endpoint,
        bucket=args.bucket,
        object_key=object_key,
        region=args.region,
        access_key=args.access_key,
        secret_key=args.secret_key,
        payload=payload,
        extra_headers={
            "Content-Length": str(len(payload)),
            "Content-Type": mime_type or mimetypes.guess_type(local_path.name)[0] or "application/octet-stream",
        },
        timeout=args.timeout,
    )


def parse_args() -> argparse.Namespace:
    parser = argparse.ArgumentParser(description=__doc__)
    env = load_environment(parser)
    parser.add_argument("--compose-file", type=Path, default=DEFAULT_COMPOSE_FILE)
    parser.add_argument("--manifest", dest="manifests", action="append", type=Path)
    add_env_argument(parser, env, "--endpoint", "S3_ENDPOINT", "http://localhost:4566")
    parser.add_argument("--bucket", default="documents")
    parser.add_argument("--region", default="us-east-1")
    add_env_argument(parser, env, "--access-key", "S3_ACCESS_KEY")
    add_env_argument(parser, env, "--secret-key", "S3_SECRET_KEY")
    parser.add_argument("--timeout", type=float, default=180.0)
    parser.add_argument("--delay", type=float, default=0.0)
    parser.add_argument("--retries", type=int, default=4)
    parser.add_argument("--limit", type=int, default=0)
    parser.add_argument("--dry-run", action="store_true")
    parser.add_argument(
        "--force-put",
        action="store_true",
        help="PUT every manifest-backed object without first checking S3 with HEAD",
    )
    parser.add_argument(
        "--failures-json",
        type=Path,
        default=Path("datasets/logs/s3_restore_failures_latest.json"),
    )
    args = parser.parse_args()
    if not args.access_key or not args.secret_key:
        parser.error("S3_ACCESS_KEY and S3_SECRET_KEY are required")
    return args


def main() -> int:
    args = parse_args()
    manifests = args.manifests or DEFAULT_MANIFESTS
    by_doc_id, by_filename = load_manifests(manifests)
    rows = query_documents(args.compose_file)
    stats = {
        "db_rows": len(rows),
        "manifest_doc_ids": len(by_doc_id),
        "manifest_filenames": len(by_filename),
        "already_ok": 0,
        "would_restore": 0,
        "restored": 0,
        "missing_local": 0,
        "size_mismatch": 0,
        "failed": 0,
    }
    failures: list[dict[str, Any]] = []
    started = time.monotonic()

    for row in rows:
        doc_id = str(row["id"])
        filename = str(row["filename"])
        object_key = str(row["object_key"])
        expected_size = int(row["size"])
        local_path = by_doc_id.get(doc_id) or by_filename.get(filename)
        if not local_path or not local_path.exists():
            stats["missing_local"] += 1
            failures.append({"row": row, "error": "local file not found in manifests"})
            continue
        local_size = local_path.stat().st_size
        if local_size != expected_size:
            stats["size_mismatch"] += 1
            failures.append({"row": row, "local_path": str(local_path), "error": f"local size {local_size} != db size {expected_size}"})
            continue

        remote_size = None
        if not args.force_put:
            try:
                remote_size = head_size(args, object_key)
            except Exception as exc:  # noqa: BLE001
                stats["failed"] += 1
                failures.append({"row": row, "local_path": str(local_path), "error": public_output(f"head: {exc}")})
                continue
        if not args.force_put and remote_size == expected_size:
            stats["already_ok"] += 1
            continue
        if args.dry_run:
            stats["would_restore"] += 1
            continue
        if args.limit and stats["restored"] >= args.limit:
            continue

        last_error: Exception | None = None
        for attempt in range(1, args.retries + 1):
            try:
                put_file(args, object_key, local_path, str(row.get("mime_type") or ""))
                actual_size = head_size(args, object_key)
                if actual_size != expected_size:
                    raise RuntimeError(f"remote size {actual_size} != expected {expected_size}")
                stats["restored"] += 1
                if stats["restored"] == 1 or stats["restored"] % 25 == 0:
                    print(f"[restore] restored={stats['restored']} file={filename}", flush=True)
                if args.delay:
                    time.sleep(args.delay)
                break
            except Exception as exc:  # noqa: BLE001
                last_error = exc
                if attempt < args.retries:
                    time.sleep(min(5.0, 0.5 * attempt))
                else:
                    stats["failed"] += 1
                    failures.append({"row": row, "local_path": str(local_path), "error": public_output(str(last_error))})

    stats["elapsed_sec"] = round(time.monotonic() - started, 1)
    args.failures_json.parent.mkdir(parents=True, exist_ok=True)
    args.failures_json.write_text(
        json.dumps({"stats": stats, "failures": failures}, ensure_ascii=False, indent=2) + "\n",
        encoding="utf-8",
    )
    print(json.dumps(stats, ensure_ascii=False, indent=2), flush=True)
    if stats["failed"] or stats["size_mismatch"]:
        print(f"[restore] failures written to {args.failures_json}", file=sys.stderr)
        return 1
    return 0


if __name__ == "__main__":
    raise SystemExit(main())
