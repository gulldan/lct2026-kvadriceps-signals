#!/usr/bin/env python3
"""RAPTOR: слой сводок над фрагментами в той же коллекции Qdrant, чтобы поиск брал нужный уровень —
сводку на широкий вопрос, фрагмент на точный.

Режим incremental строит сводку на документ небольшими пакетами. Полный режим кластеризует фрагменты
через graph-compute и рекурсивно сводит сводки до RAPTOR_MAX_LEVEL. Идентификатор узла — UUIDv5 от
уровня и состава, поэтому повторный прогон перезаписывает точки, а не дублирует их; всё состояние —
в Qdrant и Valkey. Без модели воркер простаивает: шаблонная сводка — шум, а не сигнал.
"""
import hashlib
import json
import math
import os
import sys
import threading
import time
import urllib.error
import urllib.request
import uuid
from concurrent.futures import ThreadPoolExecutor, as_completed

import grpc
import rpc_transport
from worker_lease import guard_lease, redis_client, run_command
from genproto.graph.v1 import graph_pb2, graph_pb2_grpc

GW = os.environ.get("RAG_GW", "http://nginx/api/v1")
USER = os.environ.get("RAG_USER", "admin")
PASS = os.environ.get("RAG_PASS", "")
COLLECTION = os.environ.get("QDRANT_COLLECTION", "documents")
QDRANT_URL = os.environ.get("QDRANT_URL", "http://qdrant:6333")
GRAPH_COMPUTE_ADDR = os.environ.get("GRAPH_COMPUTE_ADDR", "graph-compute:9093")
GRAPH_COMPUTE_TIMEOUT = float(os.environ.get("GRAPH_COMPUTE_TIMEOUT", "600"))
GRPC_MAX_RECV_MB = max(4, int(os.environ.get("GRPC_MAX_RECV_MB", "128")))
VALKEY_URL = os.environ.get("VALKEY_URL", "redis://valkey:6379")
EPOCH_KEY = os.environ.get("EPOCH_KEY", "rag:corpus_epoch:shared")
LAST_KEY = os.environ.get("RAPTOR_LAST_KEY", "rag:raptor:last_epoch")
DOC_SIG_KEY = os.environ.get("RAPTOR_DOC_SIG_KEY", "rag:raptor:doc_signatures")
DOC_META_KEY = os.environ.get("RAPTOR_DOC_META_KEY", "rag:raptor:doc_meta")
WORKER_STATUS_KEY = "rag:worker:raptor:status"
CHECK_INTERVAL = int(os.environ.get("CHECK_INTERVAL", "60"))
DEBOUNCE_SEC = int(os.environ.get("DEBOUNCE_SEC", "120"))

# Умолчания как у cluster-worker, но граф строится по фрагментам (granularity="chunk"), а не по
# усреднённым документам.
KNN_K = int(os.environ.get("KNN_K", "6"))
KNN_BLOCK_SIZE = max(64, int(os.environ.get("KNN_BLOCK_SIZE", "512")))
RESOLUTION = float(os.environ.get("RESOLUTION", "1.0"))
# Одиночный фрагмент сам себе сводка: группы меньше порога не сводятся ни на первом уровне, ни выше.
MIN_SIZE = max(2, int(os.environ.get("MIN_SIZE", "2")))
SIM_THRESHOLD = max(0.0, min(1.0, float(os.environ.get("SIM_THRESHOLD", "0.0"))))
MUTUAL_KNN = os.environ.get("MUTUAL_KNN", "true").lower() not in {"0", "false", "no", "off"}
CLUSTER_ALGORITHM = "leiden"
CLUSTER_SEED = 42

# 1 — только сводки фрагментов, 2 — ещё и сводки сводок.
RAPTOR_MAX_LEVEL = max(1, int(os.environ.get("RAPTOR_MAX_LEVEL", "2")))
RAPTOR_MAX_MEMBERS_FOR_SUMMARY = max(2, int(os.environ.get("RAPTOR_MAX_MEMBERS_FOR_SUMMARY", "24")))
# Столько исходных фрагментов копируется в сводку: llm-service раскрывает в них найденную сводку для цитат.
RAPTOR_SUMMARY_MEMBERS = max(1, int(os.environ.get("RAPTOR_SUMMARY_MEMBERS", "8")))
# Хватает и на контекст генерации, и на цитату, а полезная нагрузка сводки остаётся ограниченной.
RAPTOR_MEMBER_TEXT_MAX = max(200, int(os.environ.get("RAPTOR_MEMBER_TEXT_MAX", "1200")))
# Верхние уровни кластеризуются на чистом Python за O(n²): выше потолка уровень пропускается, чтобы
# воркер не встал надолго.
RAPTOR_INPROC_MAX_NODES = max(0, int(os.environ.get("RAPTOR_INPROC_MAX_NODES", "1500")))
QDRANT_UPSERT_BATCH = max(1, int(os.environ.get("RAPTOR_UPSERT_BATCH", "64")))
RAPTOR_MODE = os.environ.get("RAPTOR_MODE", "incremental").strip().lower()
RAPTOR_INCREMENTAL_BATCH = max(1, int(os.environ.get("RAPTOR_INCREMENTAL_BATCH", "8")))
RAPTOR_INCREMENTAL_WORKERS = max(1, int(os.environ.get("RAPTOR_INCREMENTAL_WORKERS", "4")))
RAPTOR_DOC_MAX_CHUNKS = max(2, int(os.environ.get("RAPTOR_DOC_MAX_CHUNKS", "16")))
RAPTOR_SUMMARY_MAX_TOKENS = max(64, int(os.environ.get("RAPTOR_SUMMARY_MAX_TOKENS", "220")))

EMBEDDINGS_URL = os.environ.get("EMBEDDINGS_URL", "http://f2llm-service:8085/v1/embeddings")
EMBEDDINGS_MODEL = os.environ.get("EMBEDDINGS_MODEL", "codefuse-ai/F2LLM-v2-0.6B")
VLLM_URL = os.environ.get("VLLM_URL", "")
VLLM_MODEL = os.environ.get("VLLM_MODEL", "")
VLLM_API_KEY = os.environ.get("VLLM_API_KEY", "")
VLLM_RPM = max(0, int(os.environ.get("VLLM_RPM", "18")))
AUTO_RAPTOR = os.environ.get("AUTO_RAPTOR", "true").lower() not in {"0", "false", "no", "off"}

# По node_type поиск отличает сводки от фрагментов, которым chunk-splitter ставит "chunk".
NODE_TYPE_SUMMARY = "raptor_summary"
# Менять нельзя: от пространства имён зависят id сводок, и повторный прогон начнёт дублировать точки
# вместо перезаписи.
RAPTOR_ID_NAMESPACE = uuid.UUID("7d3f1a9c-2e6b-4c8a-bf21-0a5d9e4c7b10")

_LAST_VLLM_CALL = {"t": 0.0}
_VLLM_LOCK = threading.Lock()
TOKEN = {"v": None}


def http(url, payload=None, token=None, timeout=180, method=None):
    data = json.dumps(payload).encode() if payload is not None else None
    m = method or ("POST" if data is not None else "GET")
    req = urllib.request.Request(url, data=data, method=m)
    req.add_header("Content-Type", "application/json")
    if token:
        req.add_header("Authorization", "Bearer " + token)
    guard_lease()
    with urllib.request.urlopen(req, timeout=timeout) as r:
        body = r.read().decode()
        return json.loads(body) if body else {}


def qdrant_scroll(filter_body=None, with_vector=False, with_payload=True, limit=256):
    url = f"{QDRANT_URL.rstrip('/')}/collections/{COLLECTION}/points/scroll"
    points, offset = [], None
    while True:
        body = {
            "limit": max(1, int(limit)),
            "with_vector": bool(with_vector),
            "with_payload": bool(with_payload),
        }
        if filter_body:
            body["filter"] = filter_body
        if offset is not None:
            body["offset"] = offset
        resp = http(url, body, timeout=120)
        result = resp.get("result") or {}
        batch = result.get("points") or []
        if isinstance(batch, list):
            points.extend(batch)
        offset = result.get("next_page_offset")
        if offset is None:
            return points


def throttle_vllm():
    if VLLM_RPM <= 0:
        return
    with _VLLM_LOCK:
        gap = 60.0 / float(VLLM_RPM)
        elapsed = time.monotonic() - _LAST_VLLM_CALL["t"]
        if elapsed < gap:
            time.sleep(gap - elapsed)
        _LAST_VLLM_CALL["t"] = time.monotonic()


def vllm_payload(prompt, **extra):
    payload = {"model": VLLM_MODEL, "messages": [{"role": "user", "content": prompt}], **extra}
    if effort := os.environ.get("VLLM_REASONING_EFFORT", "").strip():
        payload["reasoning_effort"] = effort
    return payload


def login():
    TOKEN["v"] = http(f"{GW}/auth/login", {"username": USER, "password": PASS})["access_token"]


def gw(path, payload=None, method=None):
    url = f"{GW}{path}"
    try:
        return http(url, payload, token=TOKEN["v"], method=method)
    except urllib.error.HTTPError as e:
        if e.code == 401:
            login()
            return http(url, payload, token=TOKEN["v"], method=method)
        raise


def redis_int(r, key):
    raw = r.get(key)
    return int(raw) if raw else None


# Копия есть в каждом воркере (clusters, discovery, raptor, itc): общей библиотеки нет, править во всех.
_STATUS = {"r": None, "last_success_at": None, "run_started": None, "last_run_seconds": None}


USAGE_SCRIPT = """
local limit = 9007199254740991
local next = {}
for i = 1, 4 do
  local field = ARGV[1] .. ({'req','pt','ct','cost'})[i]
  local old = tonumber(redis.call('HGET', KEYS[1], field) or '0')
  local delta = tonumber(ARGV[i+1])
  if not old or not delta or old < 0 or delta < 0 or old % 1 ~= 0 or delta % 1 ~= 0 or old + delta > limit then
    return redis.error_reply('invalid usage counter')
  end
  next[i] = {field, string.format('%.0f', old + delta)}
end
local minute = tonumber(redis.call('GET', KEYS[2]) or '0')
if not minute or minute < 0 or minute % 1 ~= 0 or minute >= limit then
  return redis.error_reply('invalid minute counter')
end
for i = 1, 4 do redis.call('HSET', KEYS[1], next[i][1], next[i][2]) end
redis.call('EXPIRE', KEYS[1], ARGV[6])
redis.call('SET', KEYS[2], string.format('%.0f', minute + 1), 'EX', ARGV[7])
return 1
"""


def record_llm_usage(model, operation, usage):
    """Токены и стоимость в общий журнал Valkey, который читает main-service; сбой учёта воркер не роняет."""
    r = _STATUS["r"]
    if r is None or not isinstance(usage, dict):
        return
    try:
        pt = int(usage.get("prompt_tokens") or 0)
        ct = int(usage.get("completion_tokens") or 0)
        cost = round(float(usage.get("cost") or 0) * 1e9)
        day = time.strftime("%Y%m%d", time.gmtime())
        key = "rag:llm:usage:" + day
        sep = "\x1f"
        if min(pt, ct, cost) < 0 or max(pt, ct, cost) > 2**53 - 1:
            raise ValueError("invalid usage counters")
        if not model or not operation or sep in model + operation:
            raise ValueError("invalid usage dimensions")
        mkey = "rag:llm:reqmin:" + str(int(time.time() // 60))
        r.eval(USAGE_SCRIPT, 2, key, mkey, f"{model}{sep}{operation}{sep}", 1, pt, ct, cost, 3024000, 120)
    except Exception as e:  # noqa: BLE001 — usage telemetry, never break the worker
        print(f"llm usage record failed: {e}", file=sys.stderr, flush=True)


def report_status(state, **fields):
    """Снимок состояния для GET /system/activity; сбой записи воркер не роняет."""
    now = time.strftime("%Y-%m-%dT%H:%M:%SZ", time.gmtime())
    if state == "running":
        if _STATUS["run_started"] is None:  # отчёт о прогрессе не сбрасывает начало прогона
            _STATUS["run_started"] = time.monotonic()
    elif state == "idle":
        _STATUS["last_success_at"] = now
        if _STATUS["run_started"] is not None:
            _STATUS["last_run_seconds"] = round(time.monotonic() - _STATUS["run_started"], 3)
        _STATUS["run_started"] = None
    else:  # ошибка: длительность прошлого удачного прогона остаётся
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


def owner_of(docs):
    owners = {d.get("owner_id") for d in docs if d.get("owner_id")}
    return next(iter(owners)) if len(owners) == 1 else None


def fingerprint(ids):
    joined = "\n".join(sorted(str(i) for i in ids))
    return hashlib.sha1(joined.encode("utf-8")).hexdigest()[:16]


def summary_point_id(level, fp):
    """Уровень входит в имя, чтобы узлы с одинаковым составом на разных уровнях не совпали по id."""
    return str(uuid.uuid5(RAPTOR_ID_NAMESPACE, f"{level}:{fp}"))


def embed(text):
    resp = http(EMBEDDINGS_URL, {"model": EMBEDDINGS_MODEL, "input": [text]}, timeout=60)
    data = resp.get("data")
    if isinstance(data, list) and data and isinstance(data[0], dict):
        vec = data[0].get("embedding")
        if isinstance(vec, list) and vec:
            return vec
    embeddings = resp.get("embeddings")  # формат Triton и text-embeddings-inference
    if isinstance(embeddings, list) and embeddings and isinstance(embeddings[0], list):
        return embeddings[0]
    return None


def summarize(texts):
    """Без модели узел пропускается: шаблонная сводка засорила бы поиск."""
    texts = [t.strip() for t in texts if t and t.strip()]
    if not VLLM_URL or len(texts) < 2:
        return ""
    joined = "\n\n---\n\n".join(
        t[:RAPTOR_MEMBER_TEXT_MAX] for t in texts[:RAPTOR_MAX_MEMBERS_FOR_SUMMARY]
    )
    prompt = (
        "Ниже фрагменты из набора документов одного тематического кластера. "
        "Напиши ПЛОТНОЕ фактологическое резюме на русском языке (4–8 предложений), "
        "которое охватывает ключевые факты, методы, объекты и выводы всех фрагментов. "
        "Не добавляй вступлений, оценок и общих слов — только содержательная выжимка, "
        "по которой этот кластер можно найти семантическим поиском. "
        "Верни ТОЛЬКО текст резюме, без заголовков и списков.\n\n" + joined
    )
    for attempt in range(3):
        try:
            throttle_vllm()
            resp = http(
                VLLM_URL.rstrip("/") + "/v1/chat/completions",
                vllm_payload(prompt, temperature=0.2, max_tokens=RAPTOR_SUMMARY_MAX_TOKENS),
                token=VLLM_API_KEY,
            )
            if isinstance(resp, dict):
                record_llm_usage(VLLM_MODEL, "raptor_summary", resp.get("usage") or {})
            text = str(resp["choices"][0]["message"]["content"]).strip()
            if len(text) >= 40:
                return text
            print(f"  summary too short, retrying (attempt {attempt + 1}/3)", file=sys.stderr)
        except Exception as e:  # noqa: BLE001 — summarisation is best-effort; retried then skipped
            print(f"  summary attempt {attempt + 1}/3 failed: {e}", file=sys.stderr)
            time.sleep(1.5 * (attempt + 1))
    return ""


def qdrant_retrieve(ids):
    ids = [i for i in ids if i]
    if not ids:
        return []
    url = f"{QDRANT_URL.rstrip('/')}/collections/{COLLECTION}/points"
    resp = http(url, {"ids": ids, "with_payload": True, "with_vector": False}, timeout=120)
    result = resp.get("result")
    return result if isinstance(result, list) else []


def qdrant_upsert(points):
    if not points:
        return
    url = f"{QDRANT_URL.rstrip('/')}/collections/{COLLECTION}/points?wait=true"
    for start in range(0, len(points), QDRANT_UPSERT_BATCH):
        batch = points[start:start + QDRANT_UPSERT_BATCH]
        http(url, {"points": batch}, method="PUT", timeout=180)


def member_view(point):
    payload = point.get("payload") or {}
    text = str(payload.get("text") or "").strip()
    if not text:
        return None
    return {
        "id": str(point.get("id") or ""),
        "document_id": str(payload.get("document_id") or ""),
        "filename": str(payload.get("filename") or ""),
        "text": text[:RAPTOR_MEMBER_TEXT_MAX],
        "chunk_index": int(payload.get("chunk_index") or 0),
        "char_start": int(payload.get("char_start") or 0),
        "char_end": int(payload.get("char_end") or 0),
        "page_start": int(payload.get("page_start") or 0),
        "page_end": int(payload.get("page_end") or 0),
        "section_heading": str(payload.get("section_heading") or ""),
    }


def chunk_sort_key(point):
    payload = point.get("payload") or {}
    try:
        idx = int(payload.get("chunk_index") or 0)
    except (TypeError, ValueError):
        idx = 1_000_000
    return idx, str(point.get("id") or "")


def fetch_doc_chunks(document_id):
    if not document_id:
        return []
    points = qdrant_scroll(
        {
            "must": [
                {"key": "document_id", "match": {"value": document_id}},
                {"key": "node_type", "match": {"value": "chunk"}},
            ]
        },
        with_vector=False,
        with_payload=True,
    )
    points = [p for p in points if str(p.get("id") or "")]
    points.sort(key=chunk_sort_key)
    return points


def doc_signature(doc, chunks):
    h = hashlib.sha1()
    for value in (
        str(doc.get("id") or ""),
        str(doc.get("updated_at") or ""),
        str(doc.get("chunk_count") or ""),
        str(len(chunks)),
    ):
        h.update(value.encode("utf-8"))
        h.update(b"\0")
    for point in chunks:
        h.update(str(point.get("id") or "").encode("utf-8"))
        h.update(b"\0")
    return h.hexdigest()


def doc_meta_signature(doc):
    """Дешёвая подпись без обхода фрагментов: неизменённый документ не приходится заново читать из Qdrant."""
    h = hashlib.sha1()
    for value in (
        str(doc.get("id") or ""),
        str(doc.get("updated_at") or ""),
        str(doc.get("chunk_count") or ""),
    ):
        h.update(value.encode("utf-8"))
        h.update(b"\0")
    return h.hexdigest()


def select_doc_summary_points(chunks):
    if len(chunks) <= RAPTOR_DOC_MAX_CHUNKS:
        return chunks
    lead = max(1, RAPTOR_DOC_MAX_CHUNKS // 2)
    tail = RAPTOR_DOC_MAX_CHUNKS - lead
    picked = chunks[:lead] + (chunks[-tail:] if tail else [])
    seen, out = set(), []
    for point in picked:
        pid = str(point.get("id") or "")
        if pid and pid not in seen:
            seen.add(pid)
            out.append(point)
    return out


def build_doc_summary(doc, chunks, epoch):
    selected = select_doc_summary_points(chunks)
    members = [member_view(point) for point in selected]
    members = [m for m in members if m]
    if len(members) < MIN_SIZE:
        return None
    prompt_texts = [m["text"] for m in members[:RAPTOR_MAX_MEMBERS_FOR_SUMMARY]]
    summary = summarize(prompt_texts)
    if not summary:
        return None
    vector = embed(summary)
    if not vector:
        print("  embedding failed; skipping document summary", file=sys.stderr)
        return None

    child_ids = [m["id"] for m in members]
    fp = fingerprint([str(doc.get("id") or ""), *child_ids])
    point_id = summary_point_id(1, "doc:" + fp)
    owner_id = str(doc.get("owner_id") or "")
    payload = {
        "node_type": NODE_TYPE_SUMMARY,
        "node_level": 1,
        "owner_id": owner_id,
        "source_document_id": str(doc.get("id") or ""),
        "source_filename": str(doc.get("filename") or ""),
        "text": summary,
        "fingerprint": fp,
        "member_ids": child_ids,
        "member_count": len(chunks),
        "members": members[:RAPTOR_SUMMARY_MEMBERS],
        "epoch": epoch,
        "incremental": True,
    }
    return {"id": point_id, "vector": vector, "payload": payload}


def build_doc_summary_candidate(doc, chunks, sig, meta_sig, epoch):
    point = build_doc_summary(doc, chunks, epoch)
    return str(doc.get("id") or ""), sig, meta_sig, len(chunks), point


def fetch_members(leaf_ids):
    points = {str(p.get("id")): p for p in qdrant_retrieve(leaf_ids[:RAPTOR_SUMMARY_MEMBERS])}
    members = []
    for leaf_id in leaf_ids[:RAPTOR_SUMMARY_MEMBERS]:
        point = points.get(str(leaf_id))
        view = member_view(point) if point else None
        if view:
            members.append(view)
    return members


def build_summary_node(level, child_ids, leaf_ids, prompt_texts, owner_id, epoch):
    """child_ids — прямые потомки, из них идентичность узла; leaf_ids — исходные фрагменты под ним,
    их узел цитирует. На уровне 1 они совпадают, и prompt_texts=None: берутся тексты самих фрагментов."""
    if prompt_texts is None:
        wide = max(RAPTOR_MAX_MEMBERS_FOR_SUMMARY, RAPTOR_SUMMARY_MEMBERS)
        points = {str(p.get("id")): p for p in qdrant_retrieve(leaf_ids[:wide])}
        ordered = [member_view(points[str(i)]) for i in leaf_ids[:wide] if str(i) in points]
        ordered = [m for m in ordered if m]
        prompt_texts = [m["text"] for m in ordered[:RAPTOR_MAX_MEMBERS_FOR_SUMMARY]]
        members = ordered[:RAPTOR_SUMMARY_MEMBERS]
    else:
        members = fetch_members(leaf_ids)

    summary = summarize(prompt_texts)
    if not summary:
        return None, None
    vector = embed(summary)
    if not vector:
        print("  embedding failed; skipping summary node", file=sys.stderr)
        return None, None

    fp = fingerprint(child_ids)
    point_id = summary_point_id(level, fp)
    payload = {
        "node_type": NODE_TYPE_SUMMARY,
        "node_level": level,
        "owner_id": owner_id,
        # document_id нет намеренно: сводка охватывает много документов, а без этого поля graph-compute
        # не берёт сводки в граф фрагментов при следующем прогоне.
        "text": summary,
        "fingerprint": fp,
        "member_ids": [str(i) for i in child_ids],
        "member_count": len(leaf_ids),
        # Даже выше уровня 1 здесь исходные фрагменты: найденная сводка раскрывается в настоящие отрывки.
        "members": members,
        "epoch": epoch,
    }
    point = {"id": point_id, "vector": vector, "payload": payload}
    node = {"id": point_id, "vector": vector, "text": summary, "leaf_ids": list(leaf_ids)}
    return node, point


def _normalize(vec):
    norm = math.sqrt(sum(float(x) * float(x) for x in vec))
    if norm == 0.0 or not math.isfinite(norm):
        return [float(x) for x in vec]
    return [float(x) / norm for x in vec]


def _cosine(a, b):
    return sum(x * y for x, y in zip(a, b))


def cluster_vectors(nodes, k=None):
    """graph-compute видит только слой фрагментов, а верхние уровни малы: здесь взаимный kNN
    и компоненты связности."""
    n = len(nodes)
    if n < MIN_SIZE or (RAPTOR_INPROC_MAX_NODES and n > RAPTOR_INPROC_MAX_NODES):
        return []
    norm = [_normalize(node["vector"]) for node in nodes]
    k = max(1, min(KNN_K if k is None else k, n - 1))
    topk = []
    for i in range(n):
        sims = [(_cosine(norm[i], norm[j]), j) for j in range(n) if j != i]
        sims = [(s, j) for s, j in sims if s >= SIM_THRESHOLD]
        sims.sort(key=lambda pair: (-pair[0], pair[1]))
        topk.append({j for _, j in sims[:k]})

    parent = list(range(n))

    def find(x):
        while parent[x] != x:
            parent[x] = parent[parent[x]]
            x = parent[x]
        return x

    for i in range(n):
        for j in topk[i]:
            if not MUTUAL_KNN or i in topk[j]:
                ri, rj = find(i), find(j)
                if ri != rj:
                    parent[ri] = rj

    groups = {}
    for i in range(n):
        groups.setdefault(find(i), []).append(i)
    out = [sorted(g) for g in groups.values() if len(g) >= MIN_SIZE]
    out.sort(key=lambda g: (-len(g), g[0]))
    return out


def cluster_chunks(owner_id, document_refs):
    request = graph_pb2.ClusterRequest(
        owner_id=owner_id,
        collection=COLLECTION,
        config=graph_pb2.ClusterConfig(
            knn_k=KNN_K,
            knn_block_size=KNN_BLOCK_SIZE,
            resolution=RESOLUTION,
            min_size=MIN_SIZE,
            sim_threshold=SIM_THRESHOLD,
            mutual_knn=MUTUAL_KNN,
            algorithm=CLUSTER_ALGORITHM,
            seed=CLUSTER_SEED,
            granularity="chunk",
        ),
        documents=document_refs,
    )
    try:
        grpc_limit = GRPC_MAX_RECV_MB * 1024 * 1024
        with rpc_transport.channel(
            GRAPH_COMPUTE_ADDR,
            options=[
                ("grpc.max_receive_message_length", grpc_limit),
                ("grpc.max_send_message_length", grpc_limit),
            ],
        ) as channel:
            return graph_pb2_grpc.GraphComputeStub(channel).Cluster(
                request, timeout=GRAPH_COMPUTE_TIMEOUT
            )
    except grpc.RpcError as e:
        print(f"graph-compute Cluster failed: {e.code()} {e.details()}", file=sys.stderr, flush=True)
        raise


def build_level1(clusters, owner_id, epoch):
    nodes, points = [], []
    for cluster in clusters:
        members = [str(m) for m in cluster.members if m]
        if len(members) < MIN_SIZE:
            continue
        node, point = build_summary_node(1, members, members, None, owner_id, epoch)
        if node:
            nodes.append(node)
            points.append(point)
            print(f"  L1 summary over {len(members)} chunks -> {node['id']}", flush=True)
    return nodes, points


def build_upper_level(level, prev_nodes, owner_id, epoch):
    groups = cluster_vectors(prev_nodes)
    nodes, points = [], []
    for group in groups:
        children = [prev_nodes[i] for i in group]
        child_ids = [c["id"] for c in children]
        leaf_ids = _dedup([leaf for c in children for leaf in c["leaf_ids"]])
        prompt_texts = [c["text"] for c in children]
        node, point = build_summary_node(level, child_ids, leaf_ids, prompt_texts, owner_id, epoch)
        if node:
            nodes.append(node)
            points.append(point)
            print(
                f"  L{level} summary over {len(child_ids)} nodes / {len(leaf_ids)} leaves "
                f"-> {node['id']}",
                flush=True,
            )
    return nodes, points


def incremental_raptor(r, epoch=None):
    """Безопасно для живого стенда: без глобального графа и попарного kNN, за такт — ограниченный пакет
    изменившихся документов."""
    if not VLLM_URL:
        print("VLLM_URL is empty — incremental RAPTOR needs an LLM; skipping", flush=True)
        return True
    login()
    docs = [
        d for d in gw("/documents")
        if isinstance(d, dict) and d.get("id") and d.get("status") == "indexed"
    ]
    # Свежие документы идут вперёд догоняющей очереди, чтобы сводка к загрузке появлялась быстро.
    docs.sort(key=lambda d: (str(d.get("updated_at") or ""), str(d.get("id") or "")), reverse=True)

    skipped, pending = 0, 0
    candidates = []
    for doc in docs:
        if len(candidates) >= RAPTOR_INCREMENTAL_BATCH:
            pending += 1
            continue
        doc_id = str(doc.get("id") or "")
        meta_sig = doc_meta_signature(doc)
        old = r.hget(DOC_SIG_KEY, doc_id)
        old = old.decode("utf-8") if isinstance(old, bytes) else old
        old_meta = r.hget(DOC_META_KEY, doc_id)
        old_meta = old_meta.decode("utf-8") if isinstance(old_meta, bytes) else old_meta
        if old and old_meta == meta_sig:
            skipped += 1
            continue

        chunks = fetch_doc_chunks(doc_id)
        sig = doc_signature(doc, chunks)
        if old == sig:
            r.hset(DOC_META_KEY, doc_id, meta_sig)
            skipped += 1
            continue

        candidates.append((doc, chunks, sig, meta_sig))

    processed, failed = 0, 0
    backlog = len(candidates) + pending
    workers = min(RAPTOR_INCREMENTAL_WORKERS, len(candidates))
    if workers > 1:
        print(
            f"incremental raptor: building {len(candidates)} candidates with {workers} workers",
            flush=True,
        )
    if candidates:
        report_status("running", epoch=epoch, progress_done=0, progress_total=backlog)
        with ThreadPoolExecutor(max_workers=max(1, workers)) as pool:
            futures = [
                pool.submit(build_doc_summary_candidate, doc, chunks, sig, meta_sig, epoch)
                for doc, chunks, sig, meta_sig in candidates
            ]
            for future in as_completed(futures):
                try:
                    doc_id, sig, meta_sig, chunk_count, point = future.result()
                except Exception as e:  # noqa: BLE001 — leave it unsigned so the next tick retries
                    failed += 1
                    print(f"incremental summary failed: {e}", file=sys.stderr, flush=True)
                    report_status("running", epoch=epoch, progress_done=processed + failed, progress_total=backlog)
                    continue
                if point:
                    qdrant_upsert([point])
                    print(
                        f"incremental summary: {doc_id} chunks={chunk_count} -> {point['id']}",
                        flush=True,
                    )
                else:
                    print(f"incremental summary skipped: {doc_id} chunks={chunk_count}", flush=True)
                r.hset(DOC_SIG_KEY, doc_id, sig)
                r.hset(DOC_META_KEY, doc_id, meta_sig)
                processed += 1
                report_status("running", epoch=epoch, progress_done=processed + failed, progress_total=backlog)

    remaining = max(0, len(docs) - skipped - processed)
    print(
        f"incremental raptor tick: processed={processed} skipped={skipped} "
        f"failed={failed} pending={pending} remaining={remaining}",
        flush=True,
    )
    return remaining == 0


def _dedup(items):
    seen, out = set(), []
    for item in items:
        if item not in seen:
            seen.add(item)
            out.append(item)
    return out


def raptor(epoch=None):
    if not VLLM_URL:
        print("VLLM_URL is empty — RAPTOR needs an LLM to summarise; skipping", flush=True)
        return 0
    login()
    docs = gw("/documents")
    # /documents отдаёт документы одного владельца; он уходит в graph-compute для фильтра в Qdrant,
    # как в cluster-worker. Пустой владелец — общий корпус.
    owner_id = owner_of(docs) or ""
    document_refs = [
        graph_pb2.DocumentRef(id=d["id"], filename=d.get("filename", ""))
        for d in docs if isinstance(d, dict) and d.get("id")
    ]
    if not document_refs:
        print("no documents; skip", flush=True)
        return 0

    print(
        f"raptor: clustering chunks of {len(document_refs)} docs via graph-compute "
        f"(granularity=chunk) at {GRAPH_COMPUTE_ADDR}",
        flush=True,
    )
    resp = cluster_chunks(owner_id, document_refs)
    if not resp.clusters:
        print(f"no chunk clusters (docs={resp.stats.document_count}); nothing to summarise", flush=True)
        return 0

    total = 0
    nodes, points = build_level1(list(resp.clusters), owner_id, epoch)
    qdrant_upsert(points)
    total += len(points)
    print(f"level 1: {len(points)} summary nodes", flush=True)

    level = 2
    while level <= RAPTOR_MAX_LEVEL and len(nodes) >= MIN_SIZE:
        nodes, points = build_upper_level(level, nodes, owner_id, epoch)
        if not points:
            break
        qdrant_upsert(points)
        total += len(points)
        print(f"level {level}: {len(points)} summary nodes", flush=True)
        level += 1

    print(f"raptor done: {total} summary nodes across {min(level - 1, RAPTOR_MAX_LEVEL)} levels", flush=True)
    return total


def main():
    r = redis_client()
    _STATUS["r"] = r
    print(
        f"raptor-worker up; mode={RAPTOR_MODE}; watching {EPOCH_KEY} "
        f"(debounce {DEBOUNCE_SEC}s, every {CHECK_INTERVAL}s)",
        flush=True,
    )
    if not AUTO_RAPTOR:
        print("AUTO_RAPTOR disabled; idling", flush=True)
    try:
        last = redis_int(r, LAST_KEY)
    except Exception as e:  # noqa: BLE001 — bad state should not stop the worker
        print(f"last raptor epoch read failed: {e}", file=sys.stderr, flush=True)
        last = None
    seen_epoch = last
    seen_at = time.time() if last is not None else 0.0
    if last is not None:
        print(f"last raptor epoch={last}; waiting for a newer corpus epoch", flush=True)
    while True:
        try:
            if not AUTO_RAPTOR:
                time.sleep(CHECK_INTERVAL)
                continue
            raw = r.get(EPOCH_KEY)
            epoch = int(raw) if raw else 0
            now = time.time()
            if epoch != seen_epoch:
                seen_epoch, seen_at = epoch, now  # эпоха сдвинулась — пауза отсчитывается заново
                print(f"epoch moved to {epoch}; waiting {DEBOUNCE_SEC}s before raptor", flush=True)
            elif epoch != last and epoch > 0 and (now - seen_at) >= DEBOUNCE_SEC:
                try:
                    report_status("running", epoch=epoch)
                    outcome = run_command(r, "raptor", str(epoch),
                        [sys.executable, __file__, "--run-epoch", str(epoch)],
                        completed_key=LAST_KEY, completed_value=epoch)
                    if outcome in ("succeeded", "failed"):
                        last = epoch
                        report_status("idle" if outcome == "succeeded" else "error", epoch=epoch)
                except Exception as e:  # noqa: BLE001 — keep the loop alive
                    print(f"raptor failed: {e}", file=sys.stderr, flush=True)
                    report_status("error", epoch=epoch, last_error=str(e))
        except Exception as e:  # noqa: BLE001 — Valkey/transient
            print(f"loop error: {e}", file=sys.stderr, flush=True)
        time.sleep(CHECK_INTERVAL)


def _selfcheck():
    req = graph_pb2.ClusterRequest(
        owner_id="o", collection="documents",
        config=graph_pb2.ClusterConfig(granularity="chunk"),
    )
    assert req.config.granularity == "chunk"

    assert fingerprint(["b", "a"]) == fingerprint(["a", "b"])
    assert fingerprint(["a"]) != fingerprint(["b"])
    pid1 = summary_point_id(1, fingerprint(["a", "b"]))
    assert pid1 == summary_point_id(1, fingerprint(["b", "a"]))
    assert pid1 != summary_point_id(2, fingerprint(["a", "b"]))
    uuid.UUID(pid1)  # бросит исключение, если id не годится для Qdrant

    assert abs(_cosine(_normalize([1.0, 0.0]), _normalize([2.0, 0.0])) - 1.0) < 1e-9
    assert abs(_cosine(_normalize([1.0, 0.0]), _normalize([0.0, 1.0]))) < 1e-9
    assert _normalize([0.0, 0.0]) == [0.0, 0.0]

    # k=2: каждый узел связан только с двумя соседями своей группы, и группы расходятся чисто.
    nodes = [
        {"vector": [1.0, 0.0]}, {"vector": [0.98, 0.02]}, {"vector": [0.95, 0.05]},
        {"vector": [0.0, 1.0]}, {"vector": [0.02, 0.98]}, {"vector": [0.05, 0.95]},
    ]
    groups = cluster_vectors(nodes, k=2)
    assert len(groups) == 2, groups
    assert all(len(g) == 3 for g in groups)
    assert sorted(i for g in groups for i in g) == [0, 1, 2, 3, 4, 5]
    assert cluster_vectors(nodes[:1]) == []

    view = member_view({"id": "c1", "payload": {"text": " hello ", "document_id": "d1", "filename": "a.pdf", "chunk_index": 3}})
    assert view["id"] == "c1" and view["text"] == "hello" and view["chunk_index"] == 3
    assert member_view({"id": "c2", "payload": {"text": "  "}}) is None

    chunks = [
        {"id": f"c{i}", "payload": {"text": f"text {i}", "document_id": "d1", "filename": "a.pdf", "chunk_index": i}}
        for i in range(20)
    ]
    picked = select_doc_summary_points(chunks)
    assert len(picked) == RAPTOR_DOC_MAX_CHUNKS
    assert [p["id"] for p in picked] == [f"c{i}" for i in range(8)] + [f"c{i}" for i in range(12, 20)]
    doc = {"id": "d1", "owner_id": "u1", "filename": "a.pdf", "updated_at": "t1", "chunk_count": 20}
    sig1 = doc_signature(doc, chunks)
    sig2 = doc_signature(doc, chunks[:-1] + [{"id": "cx", "payload": chunks[-1]["payload"]}])
    assert sig1 != sig2
    assert doc_meta_signature(doc) == doc_meta_signature(dict(doc))
    assert build_doc_summary(doc, chunks[:1], 1080) is None

    old_summarize, old_embed = globals()["summarize"], globals()["embed"]
    try:
        globals()["summarize"] = lambda texts: "summary text over " + str(len(texts)) + " chunks"
        globals()["embed"] = lambda text: [0.1, 0.2, 0.3]
        point = build_doc_summary(doc, chunks, 1080)
        assert point and point["vector"] == [0.1, 0.2, 0.3]
        payload = point["payload"]
        assert payload["node_type"] == NODE_TYPE_SUMMARY
        assert payload["source_document_id"] == "d1"
        assert "document_id" not in payload
        assert payload["member_count"] == 20
        assert len(payload["members"]) == min(RAPTOR_SUMMARY_MEMBERS, RAPTOR_DOC_MAX_CHUNKS)
        uuid.UUID(point["id"])
    finally:
        globals()["summarize"], globals()["embed"] = old_summarize, old_embed

    assert _dedup(["a", "b", "a", "c", "b"]) == ["a", "b", "c"]
    print("selfcheck ok", flush=True)


if __name__ == "__main__":
    if len(sys.argv) == 3 and sys.argv[1] == "--run-epoch":
        epoch = int(sys.argv[2])
        guard_lease()
        r = redis_client()
        _STATUS["r"] = r
        complete = incremental_raptor(r, epoch) if RAPTOR_MODE == "incremental" else (raptor(epoch) is not None)
        if not complete:
            raise SystemExit(75)
    else:
        if "--selfcheck" in sys.argv:
            _selfcheck()
        else:
            main()
