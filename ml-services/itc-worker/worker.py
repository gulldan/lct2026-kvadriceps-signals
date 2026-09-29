#!/usr/bin/env python3
"""Пересчёт ИТЦ по таймеру и по запросу из админки, чтобы интерфейс не ждал ручного запуска."""

import json
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


INTERVAL = int(os.environ.get("ITC_INTERVAL_SEC", "3600"))
CHECK_INTERVAL = int(os.environ.get("ITC_CHECK_INTERVAL_SEC", "10"))
STARTUP_DELAY = int(os.environ.get("ITC_STARTUP_DELAY_SEC", "180"))
TRIGGER_KEY = os.environ.get("ITC_TRIGGER_KEY", "rag:itc:trigger")
STATUS_KEY = os.environ.get("ITC_STATUS_KEY", "rag:itc:last_status")
TIMEOUT = int(os.environ.get("ITC_TIMEOUT_SEC", "1800"))
WORKER_STATUS_KEY = "rag:worker:itc:status"

# Такой же помощник есть в каждом исполнителе (clusters/discovery/raptor/itc): правка нужна во всех.
_STATUS = {"r": None, "last_success_at": None, "run_started": None, "last_run_seconds": None}


def report_status(state, **fields):
    """Снимок состояния для GET /system/activity."""
    now = time.strftime("%Y-%m-%dT%H:%M:%SZ", time.gmtime())
    if state == "running":
        if _STATUS["run_started"] is None:  # повторные отчёты о ходе сохраняют исходное начало
            _STATUS["run_started"] = time.monotonic()
    elif state == "idle":
        _STATUS["last_success_at"] = now
        if _STATUS["run_started"] is not None:
            _STATUS["last_run_seconds"] = round(time.monotonic() - _STATUS["run_started"], 3)
        _STATUS["run_started"] = None
    else:  # ошибка: прошлая длительность остаётся, начало прерванного прогона сбрасывается
        _STATUS["run_started"] = None
    status = {
        "state": state,
        "epoch": None,
        "progress_done": None,
        "progress_total": None,
        "updated_at": now,
        "last_error": None,
        "last_success_at": _STATUS["last_success_at"],
        "last_run_seconds": _STATUS["last_run_seconds"],
    }
    status.update(fields)
    if _STATUS["r"] is None:
        return
    try:
        _STATUS["r"].set(WORKER_STATUS_KEY, json.dumps(status))
    except Exception as e:  # noqa: BLE001 — status is telemetry, never break the worker
        print(f"status report failed: {e}", file=sys.stderr, flush=True)


def run_once(rdb: redis.Redis, request: str):
    env = os.environ.copy()
    env.setdefault("RAG_GW", "http://nginx/api/v1")
    env.setdefault("EMB_URL", "http://f2llm-service:8085/v1/embeddings")
    return run_command(rdb, "itc", request,
                       [sys.executable, "/app/itc.py"], timeout=TIMEOUT,
                       status_key=STATUS_KEY, env=env)


def main() -> None:
    rdb = redis_client()
    _STATUS["r"] = rdb
    scheduled = INTERVAL > 0
    delay = STARTUP_DELAY if _bool("ITC_RUN_ON_START", True) else INTERVAL
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
