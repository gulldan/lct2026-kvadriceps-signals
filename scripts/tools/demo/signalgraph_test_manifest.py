#!/usr/bin/env python3
"""Bounded manifests and transactional cleanup for metadata-only tests.

Cleanup prints SQL by default. --apply runs it against the current Compose DB.
Indexed documents, foreign owners/keys and unlisted IDs are never removed.
"""

import argparse
import json
from pathlib import Path
import subprocess
import uuid

MAX_RECORDS = 50


def validate(manifest: dict) -> dict:
    run = manifest.get("run_id", "")
    if manifest.get("version") != 1 or not run.startswith("tr-test-"):
        raise ValueError("expected version 1 and a tr-test- run_id")
    uuid.UUID(run.removeprefix("tr-test-"))
    ids = manifest.get("document_ids")
    if not isinstance(ids, list) or not 1 <= len(ids) <= MAX_RECORDS or len(set(ids)) != len(ids):
        raise ValueError("expected 1..50 unique document IDs")
    for record_id in ids:
        if str(uuid.UUID(record_id)) != record_id:
            raise ValueError("document IDs must be canonical UUIDs")
    if manifest.get("owner_id") != run or manifest.get("object_prefix") != "tests/" + run + "/":
        raise ValueError("foreign manifest owner or object prefix")
    return manifest


def cleanup_sql(manifest: dict) -> str:
    m = validate(manifest)
    # Подставляемые значения уже прошли строгую проверку UUID и производного префикса.
    ids = ",".join("'" + record_id + "'" for record_id in m["document_ids"])
    return f"""BEGIN;
SET LOCAL statement_timeout = '15s';
SELECT id FROM documents WHERE id IN ({ids}) FOR UPDATE;
DO $$ BEGIN
  IF EXISTS (SELECT 1 FROM documents WHERE id IN ({ids}) AND (
      owner_id <> '{m['owner_id']}' OR object_key NOT LIKE '{m['object_prefix']}%'
      OR status <> 'uploaded' OR chunk_count <> 0
      OR (source_metadata IS NOT NULL AND source_metadata->>'test_run_id' IS DISTINCT FROM '{m['run_id']}')
  )) THEN RAISE EXCEPTION 'foreign or indexed test record; cleanup rejected'; END IF;
END $$;
DELETE FROM documents WHERE id IN ({ids});
COMMIT;
"""


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("--manifest", type=Path, required=True)
    parser.add_argument("--create", type=int, metavar="COUNT")
    parser.add_argument("--apply", action="store_true")
    args = parser.parse_args()
    try:
        if args.create is not None:
            if args.apply or not 1 <= args.create <= MAX_RECORDS:
                raise ValueError("create needs 1..50 records and cannot be combined with apply")
            run = "tr-test-" + str(uuid.uuid4())
            m = validate(dict(version=1, run_id=run, owner_id=run, object_prefix="tests/"+run+"/", document_ids=[str(uuid.uuid4()) for _ in range(args.create)]))
            with args.manifest.open("x") as output:
                output.write(json.dumps(m, indent=2)+"\n")
            print(f"created manifest for {len(m['document_ids'])} metadata-only records")
            return
        sql = cleanup_sql(json.loads(args.manifest.read_text()))
        if args.apply:
            subprocess.run(["docker", "exec", "-i", "rag-platform-postgres-1", "psql", "-Xq", "-v", "ON_ERROR_STOP=1", "-U", "rag", "-d", "rag"], input=sql, text=True, check=True, timeout=20)
        else:
            print(sql, end="")
    except (ValueError, OSError) as error:
        parser.error(str(error))


if __name__ == "__main__":
    main()
