"""Ограниченный перенос старых jobs: сначала отчёт, запись только после остановки исполнителей.

uv run --no-project --with redis --with 'psycopg[binary]' python scripts/quality/migrate-legacy-jobs.py
Адреса передаются через VALKEY_URL и POSTGRES_DSN, в отчёт они не попадают.
"""
import argparse
import json
import os
import uuid

import psycopg
import redis
from psycopg.types.json import Jsonb

KINDS = {"generate", "verify", "enrich", "assess_trl", "competitors", "refine", "tag"}


def scan_jobs(client, maximum):
    count = 0
    total_bytes = 0
    for key in client.scan_iter("rag:hypothesis_jobs:*", count=100):
        pieces = key.decode().split(":")
        if len(pieces) != 4:
            continue
        try:
            uuid.UUID(pieces[2])
            uuid.UUID(pieces[3])
        except ValueError:
            continue
        count += 1
        if count > maximum:
            raise RuntimeError("legacy job limit exceeded; freeze a bounded batch before migration")
        raw = client.get(key)
        if raw is None:
            raise RuntimeError("legacy record expired during inventory; freeze workers and backup first")
        total_bytes += len(raw)
        if total_bytes > 64 * 1024 * 1024:
            raise RuntimeError("migration inventory exceeds 64 MiB")
        if len(raw) > 128 * 1024:
            raise RuntimeError("legacy record exceeds migration budget")
        job = json.loads(raw)
        if job.get("id") != pieces[3] or job.get("owner_id") != pieces[2] or job.get("kind") not in KINDS:
            raise RuntimeError("legacy record identity is invalid")
        if job.get("status") not in {"queued", "running", "succeeded", "failed"}:
            raise RuntimeError("legacy record state is invalid")
        if len(json.dumps(job["input"]).encode()) > 65536:
            raise RuntimeError("legacy input exceeds accepted job budget")
        yield job


def migrate(connection, job):
    state = job["status"]
    error = ""
    # Старый исполнитель не фиксировал квитанции эффектов. Его running нельзя
    # безопасно перевести в queued: сначала нужно сверить уже записанный результат.
    if state == "running":
        state, error = "failed", "legacy_outcome_unknown"
    elif state == "failed":
        error = "legacy_failed"
    row = connection.execute(
        """INSERT INTO hypothesis_jobs(id,owner_id,kind,input,state,result_ids,error,created_at,started_at,finished_at)
        VALUES(%s,%s,%s,%s,%s,%s,%s,%s,%s,%s) ON CONFLICT(id) DO NOTHING RETURNING id""",
        (job["id"], job["owner_id"], job["kind"], Jsonb(job["input"]), state,
         job.get("result_ids", []), error, job["created_at"], job.get("started_at") or None,
         job.get("finished_at") or None),
    ).fetchone()
    if row:
        return "imported"
    existing = connection.execute("SELECT owner_id,kind,input FROM hypothesis_jobs WHERE id=%s", (job["id"],)).fetchone()
    if existing != (job["owner_id"], job["kind"], job["input"]):
        raise RuntimeError("existing job has different identity; migration rolled back")
    return "already_imported"


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("--replay-trigger", action="append", choices=["eval", "itc"], default=[])
    parser.add_argument("--apply", action="store_true")
    parser.add_argument("--workers-stopped", action="store_true")
    parser.add_argument("--max-jobs", type=int, default=10000)
    args = parser.parse_args()
    if args.apply and not args.workers_stopped:
        parser.error("--apply requires --workers-stopped; stop old producers and workers first")
    if not 1 <= args.max_jobs <= 10000:
        parser.error("--max-jobs must be 1..10000")
    client = redis.Redis.from_url(os.environ["VALKEY_URL"], socket_timeout=5, socket_connect_timeout=5)
    try:
        # Весь ограниченный инвентарь валидируется до первой записи. Исходные ключи
        # сохраняются для сверки; TTL нужно заморозить резервной копией до cutover.
        jobs = list(scan_jobs(client, args.max_jobs))
        if args.apply:
            receipts = []
            with psycopg.connect(os.environ["POSTGRES_DSN"], connect_timeout=5) as connection:
                connection.execute("SET LOCAL statement_timeout='30s'")
                for job in jobs:
                    receipts.append({"id": job["id"], "result": migrate(connection, job)})
            for receipt in receipts:
                print(json.dumps(receipt))
            for worker in args.replay_trigger:
                key = f"rag:{worker}:trigger"
                value = client.get(key)
                if value is None:
                    continue
                accepted = client.eval("""
                if redis.call('GET',KEYS[1])~=ARGV[1] then return 0 end
                if redis.call('GET',KEYS[3])==ARGV[1] then return 2 end
                if redis.call('LLEN',KEYS[2])>=100 then return 0 end
                redis.call('LPUSH',KEYS[2],ARGV[1]);redis.call('SET',KEYS[3],ARGV[1]);return 1
                """, 3, key, key+":pending", key+":legacy-imported", value)
                if not accepted:
                    raise RuntimeError("legacy trigger changed or queue is full")
                print(json.dumps({"trigger": worker, "result": "imported" if accepted == 1 else "already_imported"}))
        else:
            for job in jobs:
                print(json.dumps({"id": job["id"], "owner_id": job["owner_id"], "state": job["status"],
                                  "needs_reconciliation": job["status"] == "running"}))
    finally:
        client.close()


if __name__ == "__main__":
    main()
