"""Проверяет перенос реальных записей и неизменность повторного cutover."""
import json
import os
from pathlib import Path
import subprocess
import sys
import uuid

import psycopg
import redis


def test_legacy_jobs_survive_cutover():
    client = redis.Redis.from_url(os.environ["VALKEY_TEST_URL"])
    connection = psycopg.connect(os.environ["DB_TEST_DSN"], autocommit=True)
    owner = str(uuid.uuid4())
    records = [{"id": str(uuid.uuid4()), "owner_id": owner, "kind": "generate", "status": status,
                "input": {"count": 1}, "created_at": "2026-09-17T00:00:00Z", "result_ids": []}
               for status in ("queued", "running", "succeeded")]
    keys = [f"rag:hypothesis_jobs:{owner}:{record['id']}" for record in records]
    try:
        for key, record in zip(keys, records, strict=True):
            client.set(key, json.dumps(record))
        env = {**os.environ, "VALKEY_URL": os.environ["VALKEY_TEST_URL"], "POSTGRES_DSN": os.environ["DB_TEST_DSN"]}
        command = [sys.executable, str(Path(__file__).with_name("migrate-legacy-jobs.py")), "--apply", "--workers-stopped"]
        for _ in range(2):
            subprocess.run(command, env=env, check=True, capture_output=True)
        rows = connection.execute("SELECT state,error FROM hypothesis_jobs WHERE owner_id=%s", (owner,)).fetchall()
        assert sorted(rows) == [("failed", "legacy_outcome_unknown"), ("queued", ""), ("succeeded", "")]
        assert all(client.exists(key) for key in keys)
    finally:
        connection.execute("DELETE FROM hypothesis_jobs WHERE owner_id=%s", (owner,))
        connection.close()
        client.delete(*keys)
        client.close()
