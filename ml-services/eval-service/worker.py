#!/usr/bin/env python3
"""Периодический прогон evaluate.py: по таймеру и по POST /admin/metrics/run.

Вынесен из main-service: оценка долгая и зависит от моделей.
"""

import os
import sys
import time

import redis
from worker_lease import acknowledge_trigger, pending_trigger, redis_client, run_command


def _bool(name: str, default: bool) -> bool:
    v = os.environ.get(name)
    if v is None:
        return default
    return v.strip().lower() not in ("", "0", "false", "no")


def _redis() -> redis.Redis:
    host, _, port = os.environ.get("VALKEY_ADDR", "valkey:6379").partition(":")
    return redis.Redis(
        host=host or "valkey",
        port=int(port or 6379),
        password=os.environ.get("VALKEY_PASSWORD") or None,
        decode_responses=True,
    )


INTERVAL = int(os.environ.get("EVAL_INTERVAL_SEC", "3600"))
CHECK_INTERVAL = int(os.environ.get("EVAL_CHECK_INTERVAL_SEC", "10"))
STARTUP_DELAY = int(os.environ.get("EVAL_STARTUP_DELAY_SEC", "120"))
TRIGGER_KEY = os.environ.get("EVAL_TRIGGER_KEY", "rag:eval:trigger")
STATUS_KEY = os.environ.get("EVAL_STATUS_KEY", "rag:eval:last_status")
TIMEOUT = int(os.environ.get("EVAL_TIMEOUT_SEC", "1800"))


def run_once(rdb: redis.Redis, request: str):
    env = os.environ.copy()
    env.setdefault("RAG_GATEWAY", "http://nginx/api/v1")
    env.setdefault("EVAL_EMBED_URL", "http://f2llm-service:8085/v1/embeddings")
    env.setdefault("EVAL_PROM_URL", "http://prometheus:9090")
    return run_command(rdb, "eval", request,
                       [sys.executable, "/app/evaluate.py"], timeout=TIMEOUT,
                       status_key=STATUS_KEY, env=env)


def main() -> None:
    rdb = redis_client()
    scheduled = INTERVAL > 0
    delay = STARTUP_DELAY if _bool("EVAL_RUN_ON_START", True) else INTERVAL
    next_due = time.monotonic() + delay if scheduled else float("inf")
    while True:
        try:
            request = pending_trigger(rdb, TRIGGER_KEY)
            if request:
                state = run_once(rdb, "trigger:" + request)
                if state in ("succeeded", "failed"):
                    acknowledge_trigger(rdb, TRIGGER_KEY, request)
            elif scheduled and time.monotonic() >= next_due:
                run_once(rdb, f"schedule:{int(time.time()) // INTERVAL}")
                next_due = time.monotonic() + INTERVAL
        except Exception as exc:
            print(f"maintenance attempt failed: {type(exc).__name__}", flush=True)
        time.sleep(CHECK_INTERVAL)


if __name__ == "__main__":
    main()
