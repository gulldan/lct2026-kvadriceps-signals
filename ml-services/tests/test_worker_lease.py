"""Регрессии конкурентного владения и сохранности принятых trigger на реальном Valkey."""

import concurrent.futures
import os
import sys
import time
import uuid

import pytest
import redis

from worker_lease import acknowledge_trigger, enqueue_trigger, pending_trigger, run_command


def test_worker_lease_serializes_replicas_and_keeps_triggers(tmp_path, monkeypatch):
    address = os.getenv("VALKEY_TEST_URL")
    if not address:
        pytest.skip("VALKEY_TEST_URL is required")
    client = redis.Redis.from_url(address, socket_timeout=5, socket_connect_timeout=5)
    monkeypatch.setenv("VALKEY_URL", address)
    name = "quality-" + uuid.uuid4().hex
    trigger = f"quality:{name}:trigger"
    marker = tmp_path / "executions"
    command = [sys.executable, "-c", f"import time;time.sleep(.5);open({str(marker)!r},'a').write('done\\n')"]
    try:
        enqueue_trigger(client, trigger, "first")
        enqueue_trigger(client, trigger, "second")
        assert pending_trigger(client, trigger) == "first"
        with concurrent.futures.ThreadPoolExecutor(max_workers=2) as pool:
            results = list(pool.map(lambda _: run_command(client, name, "first", command), range(2)))
        assert sorted(results) == ["busy", "succeeded"]
        # Новый клиент имитирует восстановление процесса: ни запуск, ни второй trigger не потеряны.
        restarted = redis.Redis.from_url(address)
        assert run_command(restarted, name, "first", command) == "succeeded"
        assert marker.read_text() == "done\n"
        acknowledge_trigger(restarted, trigger, "first")
        assert pending_trigger(restarted, trigger) == "second"
        assert run_command(client, name, "second", [sys.executable, "-c", "raise SystemExit(2)"]) == "failed"
        acknowledge_trigger(client, trigger, "second")
        assert pending_trigger(client, trigger) is None
        restarted.close()
    finally:
        keys = list(client.scan_iter(f"rag:work:{{{name}}}*")) + list(client.scan_iter(f"quality:{name}:*"))
        if keys:
            client.delete(*keys)
        client.close()


def test_lost_lease_terminates_old_child(tmp_path, monkeypatch):
    address = os.getenv("VALKEY_TEST_URL")
    if not address:
        pytest.skip("VALKEY_TEST_URL is required")
    monkeypatch.setenv("VALKEY_URL", address)
    client = redis.Redis.from_url(address)
    name = "quality-" + uuid.uuid4().hex
    lock = f"rag:work:{{{name}}}:lease"
    marker = tmp_path / "stale-write"
    command = [sys.executable, "-c", f"import time;time.sleep(7);open({str(marker)!r},'w').write('late')"]
    try:
        with concurrent.futures.ThreadPoolExecutor(max_workers=1) as pool:
            future = pool.submit(run_command, client, name, "request", command)
            deadline = time.monotonic() + 3
            while not client.exists(lock) and time.monotonic() < deadline:
                time.sleep(.01)
            assert client.exists(lock)
            client.set(lock, "new-owner", px=120000)
            with pytest.raises(RuntimeError, match="lease"):
                future.result(timeout=10)
        assert not marker.exists()
        assert client.get(lock) == b"new-owner"
    finally:
        keys = list(client.scan_iter(f"rag:work:{{{name}}}*"))
        if keys:
            client.delete(*keys)
        client.close()
