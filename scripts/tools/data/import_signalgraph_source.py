#!/usr/bin/env python3
"""Import one local public source through the existing authenticated upload API.

No source URL is fetched or followed. The operator supplies downloaded bytes and
a sourcedoc v1 JSON manifest. Input intent is saved before the first mutation;
the response receipt adds the exact document ID for review or addressed cleanup.
"""
from __future__ import annotations

import argparse
import hashlib
import json
import os
from pathlib import Path
import urllib.error
import urllib.parse
import urllib.request
import uuid

from operator_env import add_env_argument, load_environment, normalize_api_base, public_output


class NoRedirect(urllib.request.HTTPRedirectHandler):
    def redirect_request(self, req, fp, code, msg, headers, newurl):
        raise ValueError(f"API redirect rejected ({code})")


def import_source(base_url: str, token: str, path: Path, source_path: Path, receipt: Path) -> dict:
    endpoint = urllib.parse.urlsplit(base_url)
    if endpoint.scheme not in {"http", "https"} or not endpoint.hostname or endpoint.username or endpoint.password:
        raise ValueError("absolute API URL without credentials required")
    if not token or len(token) > 8192:
        raise ValueError("authenticated token required")
    if not path.is_file() or not 0 < path.stat().st_size <= 8 << 20:
        raise ValueError("source file must contain 1..8 MiB")
    raw = path.read_bytes()
    metadata = source_path.read_bytes()
    if not 0 < len(metadata) <= 32768:
        raise ValueError("source manifest must contain 1..32768 bytes")
    source = json.loads(metadata)
    if source.get("provider") != "manual" or source.get("hash_algorithm") != "sha256":
        raise ValueError("CLI imports manual sources with SHA-256 manifests")
    if source.get("content_hash") != hashlib.sha256(raw).hexdigest():
        raise ValueError("source bytes differ from manifest hash")
    compact = json.dumps(source, ensure_ascii=True, separators=(",", ":"))
    if len(compact) > 32768:
        raise ValueError("encoded source manifest exceeds 32768 bytes")
    filename = path.name
    if any(char in filename for char in '\r\n"'):
        raise ValueError("unsupported upload filename")
    intent = {"version": 1, "operation": "manual_source_import", "budget": 1,
              "file": str(path.resolve()), "source_file": str(source_path.resolve()),
              "sha256": source["content_hash"], "source": source, "document_ids": []}
    descriptor = os.open(receipt, os.O_WRONLY | os.O_CREAT | os.O_EXCL, 0o600)
    with os.fdopen(descriptor, "w") as stream:
        json.dump(intent, stream, ensure_ascii=False, indent=2)
        stream.write("\n")
    boundary = "signalgraph-" + uuid.uuid4().hex
    body = (f'--{boundary}\r\nContent-Disposition: form-data; name="file"; filename="{filename}"\r\n'
            'Content-Type: application/octet-stream\r\n\r\n').encode() + raw + f"\r\n--{boundary}--\r\n".encode()
    request = urllib.request.Request(base_url.rstrip("/") + "/documents", body, method="POST", headers={
        "Authorization": "Bearer " + token,
        "Content-Type": "multipart/form-data; boundary=" + boundary,
        "X-Document-Source": compact,
    })
    try:
        with urllib.request.build_opener(NoRedirect()).open(request, timeout=90) as response:
            result = json.loads(response.read(1 << 20))
    except urllib.error.HTTPError as error:
        # В журнал оператора не попадают ни ключи, ни произвольный ответ сервера.
        raise RuntimeError(f"source import HTTP {error.code}; input intent retained") from None
    if not result.get("id") or result.get("source_metadata") != source:
        raise ValueError("upload response lacks matching provenance; input intent retained")
    intent["document_ids"] = [result["id"]]
    intent["document"] = result
    temporary = receipt.with_suffix(receipt.suffix + ".pending")
    descriptor = os.open(temporary, os.O_WRONLY | os.O_CREAT | os.O_EXCL, 0o600)
    with os.fdopen(descriptor, "w") as stream:
        json.dump(intent, stream, ensure_ascii=False, indent=2)
        stream.write("\n")
    temporary.replace(receipt)
    return result


def main() -> None:
    parser = argparse.ArgumentParser(description=__doc__)
    env = load_environment(parser)
    add_env_argument(parser, env, "--api", "RAG_API")
    parser.add_argument("--file", type=Path, required=True)
    parser.add_argument("--source", type=Path, required=True)
    parser.add_argument("--receipt", type=Path, required=True, help="new intent/receipt file; never overwritten")
    parser.add_argument("--token-env", default="SIGNALGRAPH_TOKEN", help="environment variable containing the token")
    args = parser.parse_args()
    token = env.get(args.token_env, "")
    try:
        base = normalize_api_base(args.api)
        result = import_source(base, token, args.file, args.source, args.receipt)
    except (OSError, ValueError, RuntimeError) as error:
        raise SystemExit(public_output(str(error), (args.api, token))) from None
    print(json.dumps({"document_id": result["id"], "status": result["status"], "duplicate": result.get("duplicate", False)}))


if __name__ == "__main__":
    main()
