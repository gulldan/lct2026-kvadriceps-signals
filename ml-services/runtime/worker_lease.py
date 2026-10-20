"""Один исполнитель на вид работы; намерения и исходы переживают рестарт процесса."""

import hashlib
import os
import subprocess
import time
import uuid

import redis

_CLAIM = """
local state=redis.call('HGET',KEYS[2],'state')
if state=='succeeded' then return 2 end
if state=='failed' then return 3 end
if redis.call('EXISTS',KEYS[1])==1 then return 0 end
local attempt=tonumber(redis.call('HGET',KEYS[2],'attempt') or '0')
if attempt>=3 then redis.call('HSET',KEYS[2],'state','failed','error','attempts_exhausted');return 3 end
redis.call('SET',KEYS[1],ARGV[1],'PX',120000)
redis.call('HSET',KEYS[2],'state','running','attempt',attempt+1,'request',ARGV[2],'started',ARGV[3])
return 1
"""
_RENEW = """
if redis.call('GET',KEYS[1])~=ARGV[1] then return 0 end
redis.call('PEXPIRE',KEYS[1],120000);return 1
"""
_FINISH = """
if redis.call('GET',KEYS[1])~=ARGV[1] then return 0 end
redis.call('HSET',KEYS[2],'state',ARGV[2],'error',ARGV[3],'finished',ARGV[4])
if ARGV[2]=='succeeded' and KEYS[3]~='' then redis.call('SET',KEYS[3],ARGV[5]) end
if ARGV[2]=='succeeded' then redis.call('SET',KEYS[4],KEYS[2]) end
if ARGV[2]=='queued' then redis.call('HINCRBY',KEYS[2],'attempt',-1) end
redis.call('DEL',KEYS[1]);return 1
"""
_ENQUEUE = """
if redis.call('LLEN',KEYS[2])>=100 then return 0 end
redis.call('LPUSH',KEYS[2],ARGV[1]);redis.call('SET',KEYS[1],ARGV[1]);return 1
"""
_ACK = """
if redis.call('LINDEX',KEYS[1],-1)==ARGV[1] then redis.call('RPOP',KEYS[1]);return 1 end
return 0
"""


def redis_client():
    url = os.getenv("VALKEY_URL")
    if url:
        return redis.Redis.from_url(url, socket_timeout=5, socket_connect_timeout=5)
    host, _, port = os.getenv("VALKEY_ADDR", "valkey:6379").partition(":")
    return redis.Redis(host=host, port=int(port or 6379), password=os.getenv("VALKEY_PASSWORD") or None,
                       socket_timeout=5, socket_connect_timeout=5)


def _text(value):
    return value.decode() if isinstance(value, bytes) else value


def enqueue_trigger(client, key: str, request: str):
    if not client.eval(_ENQUEUE, 2, key, key + ":pending", request):
        raise RuntimeError("maintenance queue is full")


def pending_trigger(client, key: str):
    return _text(client.lindex(key + ":pending", -1))


def acknowledge_trigger(client, key: str, request: str):
    client.eval(_ACK, 1, key + ":pending", request)


def guard_lease():
    """Дочерний процесс прекращает сетевые действия после потери владения."""
    key = os.getenv("RAG_WORK_LEASE")
    if not key:
        return
    if time.time() >= float(os.environ["RAG_WORK_DEADLINE"]):
        raise RuntimeError("maintenance deadline exceeded")
    client = redis_client()
    try:
        if _text(client.get(key)) != os.environ["RAG_WORK_TOKEN"]:
            raise RuntimeError("maintenance lease lost")
    finally:
        client.close()


def run_command(client, name: str, request: str, command: list[str], *, timeout=1800,
                completed_key="", completed_value="", status_key="", env=None):
    """Возвращает busy/succeeded/failed/queued; устаревший процесс не подтверждает исход."""
    prefix = "rag:work:{" + name + "}"
    lock = prefix + ":lease"
    record = prefix + ":" + hashlib.sha256(request.encode()).hexdigest()
    token = str(uuid.uuid4())
    claim = client.eval(_CLAIM, 2, lock, record, token, request, time.time())
    if claim != 1:
        return {0: "busy", 2: "succeeded", 3: "failed"}[claim]
    # Срок задаёт вызывающий, без потолка: полный прогон тем на 28 тыс. документов
    # идёт около двух часов.
    deadline = time.time() + max(timeout, 1)
    process = None
    state, error = "failed", "worker_failure"
    try:
        if status_key:
            client.set(status_key, f"running request={request}")
        child_env = dict(os.environ if env is None else env)
        child_env.update(RAG_WORK_LEASE=lock, RAG_WORK_TOKEN=token, RAG_WORK_DEADLINE=str(deadline))
        process = subprocess.Popen(command, env=child_env)
        while process.poll() is None:
            if time.time() >= deadline:
                raise TimeoutError("maintenance execution deadline exceeded")
            if not client.eval(_RENEW, 1, lock, token):
                raise RuntimeError("maintenance lease lost")
            try:
                process.wait(timeout=5)
            except subprocess.TimeoutExpired:
                continue
        if process.returncode == 0:
            state, error = "succeeded", ""
        elif process.returncode == 75:
            state, error = "queued", "partial_progress"
        else:
            error = f"child_exit_{process.returncode}"
    except Exception as exc:
        error = type(exc).__name__
        raise
    finally:
        if process is not None and process.poll() is None:
            process.kill()
            process.wait(timeout=10)
        saved = client.eval(_FINISH, 4, lock, record, completed_key, prefix + ":last_success", token, state, error,
                            time.time(), str(completed_value))
        if not saved:
            raise RuntimeError("maintenance result rejected after lease loss")
        if status_key:
            client.set(status_key, f"{state} request={request} error={error}")
    return state
