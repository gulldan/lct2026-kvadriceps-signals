#!/usr/bin/env python3
"""Check that every document row has a matching S3 object.

Exit code is non-zero when a document object is missing, has the wrong size, or
cannot be checked. Intended for local/demo smoke checks after storage changes or
container recreates.
"""

from __future__ import annotations

import argparse
import json
import sys
from pathlib import Path
from typing import Any

import restore_dataset_objects_to_s3 as restore
from operator_env import add_env_argument, load_environment, public_output


def parse_args() -> argparse.Namespace:
    parser = argparse.ArgumentParser(description=__doc__)
    env = load_environment(parser)
    parser.add_argument("--compose-file", type=Path, default=restore.DEFAULT_COMPOSE_FILE)
    add_env_argument(parser, env, "--endpoint", "S3_ENDPOINT", "http://localhost:4566")
    parser.add_argument("--bucket", default="documents")
    parser.add_argument("--region", default="us-east-1")
    add_env_argument(parser, env, "--access-key", "S3_ACCESS_KEY")
    add_env_argument(parser, env, "--secret-key", "S3_SECRET_KEY")
    parser.add_argument("--timeout", type=float, default=60.0)
    parser.add_argument("--limit", type=int, default=0)
    parser.add_argument("--json", action="store_true")
    args = parser.parse_args()
    if not args.access_key or not args.secret_key:
        parser.error("S3_ACCESS_KEY and S3_SECRET_KEY are required")
    return args


def main() -> int:
    args = parse_args()
    rows = restore.query_documents(args.compose_file)
    checked = 0
    missing: list[dict[str, Any]] = []
    wrong_size: list[dict[str, Any]] = []
    failed: list[dict[str, Any]] = []

    for row in rows:
        if args.limit and checked >= args.limit:
            break
        checked += 1
        try:
            size = restore.head_size(args, str(row["object_key"]))
        except Exception as exc:  # noqa: BLE001 - diagnostic tool reports all failures.
            failed.append({"id": row["id"], "filename": row["filename"], "error": public_output(str(exc))})
            continue
        expected = int(row["size"])
        if size is None:
            missing.append({"id": row["id"], "filename": row["filename"], "object_key": row["object_key"]})
        elif size != expected:
            wrong_size.append(
                {
                    "id": row["id"],
                    "filename": row["filename"],
                    "object_key": row["object_key"],
                    "expected_size": expected,
                    "actual_size": size,
                }
            )

    result = {
        "checked": checked,
        "missing": len(missing),
        "wrong_size": len(wrong_size),
        "failed": len(failed),
        "ok": not missing and not wrong_size and not failed,
    }
    if args.json:
        print(json.dumps({**result, "missing_rows": missing, "wrong_size_rows": wrong_size, "failed_rows": failed}, ensure_ascii=False, indent=2))
    else:
        print(json.dumps(result, ensure_ascii=False, sort_keys=True))
        for label, rows_ in (("missing", missing), ("wrong_size", wrong_size), ("failed", failed)):
            for row in rows_[:20]:
                print(f"{label}: {row.get('id')} {row.get('filename')}", file=sys.stderr)
            if len(rows_) > 20:
                print(f"{label}: ... {len(rows_) - 20} more", file=sys.stderr)
    return 0 if result["ok"] else 1


if __name__ == "__main__":
    raise SystemExit(main())
