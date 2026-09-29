#!/usr/bin/env python3
"""Автокластеризация: доска тем обновляется сама.

Эпоху корпуса в Valkey увеличивает main-service после индексации каждого документа. Пересчёт
идёт, когда эпоха простояла DEBOUNCE_SEC, — чтобы не кластеризовать посреди загрузки. Граф и
Leiden считает graph-compute, здесь только разметка тем моделью и публикация доски.
"""
import json
import os
import re
import sys
import threading
import time
import uuid
import urllib.error
import urllib.request
from collections import Counter
from concurrent.futures import ThreadPoolExecutor, as_completed
from labels import content_signals, is_generic_label, keyword_fallback, phrase_candidates

import grpc
import rpc_transport
from worker_lease import enqueue_trigger, guard_lease, redis_client, run_command
from genproto.graph.v1 import graph_pb2, graph_pb2_grpc

GW = os.environ.get("RAG_GW", "http://nginx/api/v1")
USER = os.environ.get("RAG_USER", "admin")
PASS = os.environ.get("RAG_PASS", "")
COLLECTION = os.environ.get("QDRANT_COLLECTION", "documents")
GRAPH_COMPUTE_ADDR = os.environ.get("GRAPH_COMPUTE_ADDR", "graph-compute:9093")
GRAPH_COMPUTE_TIMEOUT = float(os.environ.get("GRAPH_COMPUTE_TIMEOUT", "600"))
# Срок всего прогона: полная переразметка ~800 тем идёт десятки минут, и получасовая аренда по
# умолчанию обрывала прогон.
RUN_TIMEOUT = int(os.environ.get("RUN_TIMEOUT", "21600"))
VALKEY_URL = os.environ.get("VALKEY_URL", "redis://valkey:6379")
EPOCH_KEY = os.environ.get("EPOCH_KEY", "rag:corpus_epoch:shared")
LAST_CLUSTERED_KEY = os.environ.get("LAST_CLUSTERED_KEY", "rag:clusters:last_epoch")
ITC_TRIGGER_KEY = os.environ.get("ITC_TRIGGER_KEY", "rag:itc:trigger")
WORKER_STATUS_KEY = "rag:worker:clusters:status"
CHECK_INTERVAL = int(os.environ.get("CHECK_INTERVAL", "60"))
DEBOUNCE_SEC = int(os.environ.get("DEBOUNCE_SEC", "120"))
# Больше 500 за страницу эндпоинт списка документов не отдаёт.
DOCUMENTS_PAGE = 500
# Ответ graph-compute — состав тем всего корпуса; 64 МБ хватает примерно на четверть миллиона
# документов.
GRAPH_RESPONSE_LIMIT = 64 * 1024 * 1024
KNN_K = int(os.environ.get("KNN_K", "6"))
KNN_BLOCK_SIZE = max(64, int(os.environ.get("KNN_BLOCK_SIZE", "512")))
RESOLUTION = float(os.environ.get("RESOLUTION", "1.0"))
MIN_SIZE = max(2, int(os.environ.get("MIN_SIZE", "2")))
# Порог косинусной близости ребра: 0 оставляет все пары kNN, выше — разводит слабо связанные
# документы по разным темам.
SIM_THRESHOLD = max(0.0, min(1.0, float(os.environ.get("SIM_THRESHOLD", "0.0"))))
# Взаимный kNN (оба документа в топ-k друг друга) даёт узкие, хорошо разделённые темы, объединённый —
# плотнее граф и крупнее темы. Изолированный узел в обоих случаях цепляется к ближайшему соседу.
MUTUAL_KNN = os.environ.get("MUTUAL_KNN", "true").lower() not in {"0", "false", "no", "off"}
# При равном усреднении фрагментов литература, благодарности и шум OCR уводят вектор документа от
# темы. Поэтому начало (название, аннотация) весит CHUNK_W_LEAD, выводы — CHUNK_W_RESULTS,
# литература — CHUNK_W_REFS (около нуля — выбросить), остальное — 1.
CHUNK_WEIGHTING = os.environ.get("CHUNK_WEIGHTING", "true").lower() not in {"0", "false", "no", "off"}
CHUNK_LEAD_COUNT = max(0, int(os.environ.get("CHUNK_LEAD_COUNT", "2")))
CHUNK_W_LEAD = max(0.0, float(os.environ.get("CHUNK_W_LEAD", "2.0")))
CHUNK_W_RESULTS = max(0.0, float(os.environ.get("CHUNK_W_RESULTS", "1.5")))
CHUNK_W_REFS = max(0.0, float(os.environ.get("CHUNK_W_REFS", "0.05")))
# Доля прежней темы, перешедшая в новую, от этого порога считается связью: прежняя тема с двумя
# связями и более — распад, новая с двумя и более — слияние.
LINEAGE_OVERLAP_MIN = max(0.0, min(1.0, float(os.environ.get("LINEAGE_OVERLAP_MIN", "0.3"))))
# Фиксированное зерно — чтобы Leiden давал одно и то же разбиение на одном корпусе.
CLUSTER_ALGORITHM = "leiden"
CLUSTER_SEED = 42
VLLM_URL = os.environ.get("VLLM_URL", "")
VLLM_MODEL = os.environ.get("VLLM_MODEL", "")
VLLM_API_KEY = os.environ.get("VLLM_API_KEY", "")
VLLM_RPM = max(0, int(os.environ.get("VLLM_RPM", "18")))
# Тема с тем же составом или устойчивой преемственностью сохраняет прежнее название: иначе каждый
# пересчёт заново оплачивал по вызову модели на тему, ~1000 вызовов и часы работы.
LABEL_REUSE = os.environ.get("LABEL_REUSE", "true").lower() not in {"0", "false", "no", "off"}
LABEL_REUSE_STABILITY = max(0.0, min(1.0, float(os.environ.get("LABEL_REUSE_STABILITY", "0.9"))))
# Зависшая модель не должна держать всю доску: после этих попыток тема получает запасное название,
# а хорошее вернётся на следующем прогоне. Срок — с запасом на очередь: пока шлюз занят пересказами
# RAPTOR, ответ в 200 токенов идёт больше минуты, и при 45 с падал каждый запрос разметки.
LABEL_LLM_ATTEMPTS = max(1, int(os.environ.get("LABEL_LLM_ATTEMPTS", "2")))
LABEL_LLM_TIMEOUT = max(5, int(os.environ.get("LABEL_LLM_TIMEOUT", "180")))
# Ответ модели на тему идёт ~20 с при 7–13 ток/с на поток, а шлюз суммарно держит десятки потоков:
# по одной теме 350 новых тем размечались почти два часа.
LABEL_CONCURRENCY = max(1, int(os.environ.get("LABEL_CONCURRENCY", "8")))
AUTO_HYPOTHESES = os.environ.get("AUTO_HYPOTHESES", "true").lower() not in {"0", "false", "no", "off"}
AUTO_HYPOTHESIS_LIMIT = max(0, int(os.environ.get("AUTO_HYPOTHESIS_LIMIT", "24")))
# При частой смене эпохи каждый пересчёт заваливает модель вызовами разметки. CLUSTER_AUTORUN=false
# оставляет воркер живым, а доску — на последней публикации.
CLUSTER_AUTORUN = os.environ.get("CLUSTER_AUTORUN", "true").lower() not in {"0", "false", "no", "off"}
_LAST_VLLM_CALL = {"t": 0.0}
_VLLM_PACE = threading.Lock()

def unique_label(label, keywords, signals, used):
    if label not in used:
        used.add(label)
        return label
    for phrase in list(keywords or []) + phrase_candidates(signals):
        phrase = re.sub(r"\s+", " ", str(phrase)).strip(" .;:-")
        if len(phrase) < 8 or is_generic_label(phrase):
            continue
        suffix = readable_suffix(phrase)
        candidate = f"{label}: {suffix[:48]}"
        if candidate not in used:
            used.add(candidate)
            return candidate
    i = 2
    while f"{label} #{i}" in used:
        i += 1
    candidate = f"{label} #{i}"
    used.add(candidate)
    return candidate


def normalize_key(value):
    value = re.sub(r"[^a-zа-я0-9]+", " ", (value or "").lower())
    return re.sub(r"\s+", " ", value).strip()


def cluster_params(cluster):
    params = cluster.get("params") or {}
    if isinstance(params, str):
        try:
            params = json.loads(params)
        except Exception:  # noqa: BLE001
            params = {}
    return params if isinstance(params, dict) else {}


def stable_cluster_key(cluster):
    params = cluster_params(cluster)
    return str(params.get("fingerprint") or "").strip() or normalize_key(cluster.get("label", ""))


def first_sentence(value, limit=320):
    text = re.sub(r"\s+", " ", value or "").strip()
    if not text:
        return ""
    cut = len(text)
    for sep in (". ", "! ", "? ", "\n"):
        idx = text.find(sep)
        if idx > 24:
            cut = min(cut, idx + len(sep))
    out = text[: min(cut, limit)].strip()
    if len(text) > len(out) and not out.endswith(("...", ".", "!", "?")):
        out = out.rstrip(" ,;:") + "..."
    return out


def clamp01(value):
    return max(0.0, min(1.0, float(value)))


def corpus_stats(clusters):
    """Сводка по всей доске: новизна темы оценивается относительно неё."""
    total_docs = 0
    max_docs = 0
    count = 0
    keyword_freq = Counter()
    for c in clusters or []:
        if not isinstance(c, dict):
            continue
        count += 1
        dc = int(c.get("document_count") or 0)
        total_docs += dc
        max_docs = max(max_docs, dc)
        seen = set()
        for kw in c.get("keywords") or []:
            key = normalize_key(str(kw))
            if key and key not in seen:
                seen.add(key)
                keyword_freq[key] += 1
    return {
        "total_docs": total_docs,
        "cluster_count": count,
        "max_docs": max_docs,
        "keyword_freq": dict(keyword_freq),
    }


def cluster_novelty(cluster, stats):
    """Новизна в [0, 1]: чем меньше тема относительно крупнейшей и чем реже её ключевые слова на доске,
    тем выше. Без данных — нейтральные 0,5, а не крайность, выдающая ложный сигнал."""
    stats = stats or {}
    cluster_count = int(stats.get("cluster_count") or 0)
    max_docs = int(stats.get("max_docs") or 0)
    if cluster_count <= 1 or max_docs <= 0:
        return 0.5
    doc_count = int((cluster or {}).get("document_count") or 0)
    size_factor = 1.0 - min(doc_count, max_docs) / float(max_docs)

    freq = stats.get("keyword_freq") or {}
    keys = []
    seen = set()
    for kw in (cluster or {}).get("keywords") or []:
        key = normalize_key(str(kw))
        if key and key not in seen:
            seen.add(key)
            keys.append(key)
    if keys:
        shares = [min(int(freq.get(k, 1)), cluster_count) / float(cluster_count) for k in keys]
        rarity_factor = 1.0 - sum(shares) / len(shares)
        novelty = (size_factor + rarity_factor) / 2.0
    else:
        # Редкость без ключевых слов не оценить: вместо неё нейтральные 0,5.
        novelty = (size_factor + 0.5) / 2.0
    return round(clamp01(novelty), 4)


def auto_hypothesis_payload(cluster, epoch, stats=None):
    label = cluster.get("label") or "тематический кластер"
    summary = cluster.get("summary") or f"Кластер объединяет документы по теме: {label}."
    keywords = [str(k) for k in (cluster.get("keywords") or []) if str(k).strip()]
    reps = cluster.get("representatives") or []
    if isinstance(reps, str):
        try:
            reps = json.loads(reps)
        except Exception:  # noqa: BLE001
            reps = []

    doc_count = int(cluster.get("document_count") or 0)
    chunk_count = int(cluster.get("chunk_count") or 0)
    fingerprint = str(cluster_params(cluster).get("fingerprint") or "").strip()
    label_key = normalize_key(label)
    support = first_sentence((reps[0] or {}).get("snippet", "") if reps else "", 300)
    title = label
    statement = auto_direction_statement(label, summary, keywords, support)
    rationale_parts = [
        f"Направление создано автоматически после кластеризации {doc_count} документов и {chunk_count} фрагментов.",
        summary,
    ]
    if support:
        rationale_parts.append(f"Опорный источник: {support}")

    evidence = []
    seen_docs = set()
    for rep in reps[:4]:
        doc_id = rep.get("document_id") or ""
        if doc_id and doc_id in seen_docs:
            continue
        if doc_id:
            seen_docs.add(doc_id)
        snippet = first_sentence(rep.get("snippet", ""), 520)
        if not snippet:
            continue
        evidence.append({
            "document_id": doc_id or None,
            "filename": rep.get("filename") or "",
            "snippet": snippet,
            "stance": "context",
            "score": 0.65,
            "ord": len(evidence),
        })

    confidence = clamp01(0.35 + min(doc_count, 40) / 100.0)
    value = clamp01(0.45 + min(doc_count, 30) / 100.0)
    risk = clamp01(0.65 - min(doc_count, 30) / 150.0)
    novelty = cluster_novelty(cluster, stats)
    generation = {
        "kind": "auto_cluster",
        "semantic_kind": "research_direction",
        "cluster_id": cluster.get("id", ""),
        "cluster_label": label,
        "cluster_key": fingerprint or label_key,
        "cluster_label_key": label_key,
        "cluster_fingerprint": fingerprint,
        "epoch": epoch,
        "generated_at": time.strftime("%Y-%m-%dT%H:%M:%SZ", time.gmtime()),
    }

    return {
        "run_id": f"auto-cluster-{epoch or 'unknown'}",
        "title": title,
        "statement": statement,
        "rationale": " ".join(rationale_parts),
        "method": "cluster_kpi",
        "status": "generated",
        "primary_cluster_id": cluster.get("id"),
        "trl": 3,
        "novelty_score": novelty,
        "risk_score": risk,
        "value_score": value,
        "confidence_score": confidence,
        "composite_score": round((novelty + value + confidence + (1.0 - risk)) / 4.0, 4),
        "measurable": True,
        "function_area": label,
        "source_type": "literature",
        "tags": list(dict.fromkeys(["auto_cluster", *keywords[:6]])),
        "assessment": {
            "novelty": {"score": novelty, "rationale": "Новизна оценена по редкости темы и размеру кластера относительно корпуса."},
            "risk": {"score": risk, "rationale": "Риск снижается при большем числе источников в теме, но требует валидации."},
            "value": {"score": value, "rationale": "Ценность оценена по размеру и связности тематического кластера."},
            "confidence": {"score": confidence, "rationale": "Уверенность основана на количестве документов и representative evidence."},
            "trl": {"level": 3, "rationale": "Есть научные публикации; нужна экспериментальная проверка."},
        },
        "detail": {
            "problem_addressed": label,
            "drivers": keywords[:5] or [label],
            "cluster_document_count": doc_count,
            "cluster_chunk_count": chunk_count,
        },
        "generation": generation,
        "evidence": evidence,
        "revision": {"action": "created", "editor_id": "cluster-worker", "summary": "направление создано по кластеру"},
    }


def auto_direction_statement(label, summary, keywords, support):
    topic = f"«{label}»" if label else "этой темы"
    signal = first_sentence(summary, 260) or first_sentence(support, 260)
    clean_keywords = [k.strip() for k in keywords if k and k.strip()]
    keyword_text = ", ".join(clean_keywords[:4])
    if signal and keyword_text:
        return (
            f"Документы темы {topic} связывают признаки {keyword_text}. "
            f"Ключевой корпусный сигнал: {signal}"
        )
    if signal:
        return f"Документы темы {topic} дают корпусный сигнал: {signal}"
    if keyword_text:
        return (
            f"Тема {topic} объединяет документы по признакам {keyword_text}. "
            "Для превращения направления в гипотезу нужно выбрать KPI и проверить evidence."
        )
    return (
        f"Для темы {topic} пока недостаточно извлечённых признаков; направление требует ручной "
        "проверки перед генерацией гипотезы."
    )


def auto_hyp_key(hyp):
    try:
        generation = hyp.get("generation") or {}
        if isinstance(generation, str):
            generation = json.loads(generation)
    except Exception:  # noqa: BLE001
        generation = {}
    if generation.get("kind") != "auto_cluster":
        return ""
    return generation.get("cluster_key") or normalize_key(generation.get("cluster_label", ""))


def auto_hyp_label_key(hyp):
    try:
        generation = hyp.get("generation") or {}
        if isinstance(generation, str):
            generation = json.loads(generation)
    except Exception:  # noqa: BLE001
        generation = {}
    if generation.get("kind") != "auto_cluster":
        return ""
    return generation.get("cluster_label_key") or normalize_key(generation.get("cluster_label", ""))


def auto_hyp_rank(hyp):
    try:
        detail = hyp.get("detail") or {}
        if isinstance(detail, str):
            detail = json.loads(detail)
    except Exception:  # noqa: BLE001
        detail = {}
    return (
        int(detail.get("cluster_document_count") or 0),
        int(detail.get("cluster_chunk_count") or 0),
    )


def cluster_rank(cluster):
    return (int(cluster.get("document_count") or 0), int(cluster.get("chunk_count") or 0))


def sync_auto_hypotheses(clusters, epoch, stats=None):
    if not AUTO_HYPOTHESES or AUTO_HYPOTHESIS_LIMIT <= 0:
        return 0, 0

    clusters = sorted(clusters, key=cluster_rank, reverse=True)
    if stats is None:
        stats = corpus_stats(clusters)
    existing = gw(f"/hypotheses?limit={max(AUTO_HYPOTHESIS_LIMIT * 8, 1000)}&tags=auto_cluster")
    by_key = {}
    by_label_key = {}
    by_rank = {}
    for hyp in existing:
        key = auto_hyp_key(hyp)
        if key and key not in by_key:
            by_key[key] = hyp
        label_key = auto_hyp_label_key(hyp)
        if label_key and label_key not in by_label_key:
            by_label_key[label_key] = hyp
        rank = auto_hyp_rank(hyp)
        if rank != (0, 0) and rank not in by_rank:
            by_rank[rank] = hyp

    created = updated = skipped = 0
    used_hypotheses = set()
    fallback_existing = list(existing)
    for cluster in clusters[:AUTO_HYPOTHESIS_LIMIT]:
        key = stable_cluster_key(cluster)
        label_key = normalize_key(cluster.get("label", ""))
        if not key and not label_key:
            continue
        payload = auto_hypothesis_payload(cluster, epoch, stats)
        hyp = by_key.get(key) or by_label_key.get(label_key) or by_rank.get(cluster_rank(cluster))
        while not hyp and fallback_existing:
            candidate = fallback_existing.pop(0)
            if candidate.get("id") not in used_hypotheses:
                hyp = candidate
        if hyp and hyp.get("id") not in used_hypotheses:
            used_hypotheses.add(hyp["id"])
            if hyp.get("status") == "archived":
                # Архивное направление убрал человек: перезапись вернула бы его
                # на доску со статусом «generated».
                skipped += 1
                continue
            payload.pop("evidence", None)
            payload.pop("revision", None)
            # Запись защищена номером версии: без него PUT отвечает 409, и синхронизация обрывается
            # на первом же направлении.
            payload["version"] = hyp.get("version", 0)
            try:
                gw(f"/hypotheses/{hyp['id']}", payload, method="PUT")
                updated += 1
            except Exception as e:  # noqa: BLE001 — одно направление не стоит всей доски
                skipped += 1
                print(f"  (направление {hyp['id']} не обновлено: {e})", file=sys.stderr, flush=True)
        else:
            gw("/hypotheses", payload)
            created += 1

    print(f"auto hypotheses synced: created={created} updated={updated} skipped={skipped}", flush=True)
    return created, updated


def trigger_itc_refresh(epoch):
    if not ITC_TRIGGER_KEY:
        return
    try:
        value = f"cluster-worker:{epoch or 'unknown'}:{int(time.time())}"
        enqueue_trigger(redis_client(), ITC_TRIGGER_KEY, value)
        print(f"ITC refresh triggered via {ITC_TRIGGER_KEY}={value}", flush=True)
    except Exception as e:  # noqa: BLE001 — ITC is async; clustering must stay alive
        print(f"ITC trigger failed: {e}", file=sys.stderr, flush=True)


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


def readable_suffix(phrase):
    low = phrase.lower()
    suffixes = [
        (r"labels adapting|metadata|without labels", "metadata без ручной разметки"),
        (r"medcta|tool agents", "клинические tool agents"),
        (r"anomaly", "zero-shot anomaly detection"),
        (r"segmentation|u-net|sam", "сегментация SAM/U-Net"),
        (r"restoration|super-resolution|isotropic", "super-resolution и восстановление"),
        (r"compression|frugal|variable-rate", "экономный инференс и сжатие"),
    ]
    for pattern, suffix in suffixes:
        if re.search(pattern, low):
            return suffix
    return phrase


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


def throttle_vllm():
    if VLLM_RPM <= 0:
        return
    gap = 60.0 / float(VLLM_RPM)
    with _VLLM_PACE:
        elapsed = time.monotonic() - _LAST_VLLM_CALL["t"]
        if elapsed < gap:
            time.sleep(gap - elapsed)
        _LAST_VLLM_CALL["t"] = time.monotonic()


TOKEN = {"v": None}


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


def label_community(signals, representatives=None):
    """signals — название, ключевые слова и аннотация каждого документа темы от graph-compute."""
    signals = [str(s) for s in (signals or []) if str(s).strip()]
    if not signals:
        for rep in representatives or []:
            if not isinstance(rep, dict):
                continue
            text = "\n".join(p for p in (rep.get("filename", ""), rep.get("snippet", "")) if p).strip()
            if text:
                signals.append(text)
    signals = content_signals(signals)
    joined = "\n\n---\n\n".join(t[:1200] for t in signals[:8])
    if VLLM_URL and VLLM_API_KEY:
        prompt = (
            "Ниже названия, ключевые слова и аннотации документов одного кластера. "
            "Сформулируй прикладную, узкую тему кластера: 4–9 слов, обязательно укажи "
            "метод/технологию и материаловедческий объект или инженерную задачу. Не используй названия "
            "университетов, стран, авторов и общие ярлыки вроде 'Medical', 'University', "
            "'Learning', 'Vision', 'Healthcare'. Резюме должно объяснять, что именно "
            "объединяет документы. Все поля пиши ТОЛЬКО на русском языке. "
            'Верни СТРОГО JSON: {"label":"","summary":"","keywords":[]}\n\n' + joined
        )
        retry_prompt = ""
        for attempt in range(LABEL_LLM_ATTEMPTS):
            try:
                throttle_vllm()
                payload = {"model": VLLM_MODEL,
                           "messages": [{"role": "user", "content": retry_prompt or prompt}],
                           "temperature": 0.2, "max_tokens": 1024}
                if effort := os.environ.get("VLLM_REASONING_EFFORT", "").strip():
                    payload["reasoning_effort"] = effort
                resp = http(
                    VLLM_URL.rstrip("/") + "/v1/chat/completions",
                    payload,
                    token=VLLM_API_KEY,
                    timeout=LABEL_LLM_TIMEOUT,
                )
                if isinstance(resp, dict):
                    record_llm_usage(VLLM_MODEL, "cluster_label", resp.get("usage") or {})
                c = resp["choices"][0]["message"]["content"]
                obj = json.loads(c[c.index("{"): c.rindex("}") + 1])
                label = obj.get("label", "")
                foreign = bool(label) and not re.search(r"[А-Яа-яЁё]", label)
                if not is_generic_label(label) and not foreign:
                    return label, obj.get("summary", ""), obj.get("keywords", [])[:8], True
                print(f"  label rejected ({'не русский' if foreign else 'общий'}): {label}",
                      file=sys.stderr)
                if foreign:
                    # Модель отвечает на языке источников, и повтор с теми же документами даёт тот же
                    # ответ: китайского в запросе больше, чем просьбы. Второй запрос — только перевод.
                    retry_prompt = (
                        "Переведи на русский язык название темы и ключевые слова. "
                        "Ничего не добавляй и не сокращай, верни СТРОГО JSON "
                        '{"label":"","summary":"","keywords":[]}\n\n'
                        f'{{"label": "{label}", "summary": "{obj.get("summary", "")}", '
                        f'"keywords": {json.dumps(obj.get("keywords", [])[:8], ensure_ascii=False)}}}')
            except Exception as e:  # noqa: BLE001 — labelling is best-effort
                print(f"  label attempt {attempt + 1}/{LABEL_LLM_ATTEMPTS} failed: {e}", file=sys.stderr)
                time.sleep(1.5 * (attempt + 1))
    return *keyword_fallback(signals), False


def owner_of(docs):
    owners = {d.get("owner_id") for d in docs if d.get("owner_id")}
    return next(iter(owners)) if len(owners) == 1 else None


def reuse_label(cluster, prev_by_fp, prev_by_id):
    """Прежнее название без вызова модели, если тема та же: совпал отпечаток состава или устойчивость
    преемственности не ниже порога. Порог строгий: перестроенную тему лучше переназвать, чем оставить ей
    устаревшее имя."""
    if not LABEL_REUSE:
        return None
    fp = str(getattr(cluster, "fingerprint", "") or "").strip()
    prev = prev_by_fp.get(fp) if fp else None
    if prev is None and hasattr(cluster, "HasField") and cluster.HasField("lineage"):
        lin = cluster.lineage
        if lin.stability >= LABEL_REUSE_STABILITY:
            prev = prev_by_id.get(lin.previous_cluster_id)
    if not prev:
        return None
    label = prev.get("label") or ""
    if not label or is_generic_label(label):
        return None
    return label, prev.get("summary", ""), list(prev.get("keywords") or [])


def recluster(epoch=None):
    login()
    publication_id = str(uuid.uuid4())
    expected_head = gw("/clusters/head")["publication_id"]
    # Без limit и offset эндпоинт отдаёт только первую сотню, и кластеризация молча шла бы по ней.
    docs = []
    while True:
        page = gw(f"/documents?limit={DOCUMENTS_PAGE}&offset={len(docs)}")
        rows = page if isinstance(page, list) else (page.get("items") or [])
        docs.extend(rows)
        if len(rows) < DOCUMENTS_PAGE:
            break
    # /documents отдаёт документы одного владельца; он же уходит в graph-compute, чтобы фильтр
    # в Qdrant не смешал в темах документы разных владельцев.
    owner_id = owner_of(docs) or ""
    document_refs = [
        graph_pb2.DocumentRef(id=d["id"], filename=d.get("filename", ""))
        for d in docs if isinstance(d, dict) and d.get("id")
    ]
    if not document_refs:
        print("no documents; skip", flush=True)
        return 0

    # Прежняя доска нужна для преемственности тем и повторного использования названий; сбой её загрузки
    # лишь отключает и то и другое на этот прогон.
    previous_clusters = []
    prev_by_fp = {}
    prev_by_id = {}
    try:
        for c in gw("/clusters?view=full") or []:
            if not isinstance(c, dict):
                continue
            cid = str(c.get("id") or "")
            params = cluster_params(c)
            entry = {
                "label": str(c.get("label") or ""),
                "summary": str(c.get("summary") or ""),
                "keywords": [str(k) for k in (c.get("keywords") or []) if str(k).strip()],
            }
            # Запасное название не переносится: иначе тема с тем же составом держала бы его вечно.
            if entry["label"] and not params.get("label_fallback"):
                fp = str(params.get("fingerprint") or "").strip()
                if fp:
                    prev_by_fp[fp] = entry
                if cid:
                    prev_by_id[cid] = entry
            members = params.get("members")
            if isinstance(members, (list, tuple)) and members:
                previous_clusters.append(graph_pb2.PreviousCluster(
                    id=cid,
                    members=[str(m) for m in members if m],
                ))
    except Exception as e:  # noqa: BLE001 — lineage is best-effort, never block publishing
        print(f"  previous board load skipped: {e}", file=sys.stderr, flush=True)

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
            chunk_weighting=CHUNK_WEIGHTING,
            chunk_lead_count=CHUNK_LEAD_COUNT,
            chunk_w_lead=CHUNK_W_LEAD,
            chunk_w_results=CHUNK_W_RESULTS,
            chunk_w_refs=CHUNK_W_REFS,
            lineage_overlap_min=LINEAGE_OVERLAP_MIN,
            algorithm=CLUSTER_ALGORITHM,
            seed=CLUSTER_SEED,
        ),
        documents=document_refs,
        previous_clusters=previous_clusters,
    )
    print(
        f"clustering {len(document_refs)} docs via graph-compute "
        f"({CLUSTER_ALGORITHM}) at {GRAPH_COMPUTE_ADDR}",
        flush=True,
    )
    try:
        # gRPC по умолчанию принимает 4 МБ, а состав тем на 25 тыс. документов весит 6,7 МБ: без
        # поднятого потолка сбор доски падал на уже посчитанном ответе.
        options = (("grpc.max_receive_message_length", GRAPH_RESPONSE_LIMIT),)
        started = time.monotonic()
        with rpc_transport.channel(GRAPH_COMPUTE_ADDR, options=options) as channel:
            resp = graph_pb2_grpc.GraphComputeStub(channel).Cluster(
                request, timeout=GRAPH_COMPUTE_TIMEOUT
            )
    except grpc.RpcError as e:
        print(f"graph-compute Cluster failed: {e.code()} {e.details()}", file=sys.stderr, flush=True)
        raise

    if not resp.clusters:
        # Ни одна тема не набрала MIN_SIZE: замена стёрла бы доску, поэтому старая остаётся.
        print(
            f"no clusters from graph-compute (docs={resp.stats.document_count}); keeping existing board",
            flush=True,
        )
        return 0

    total = len(resp.clusters)
    representatives_of = [
        [{"document_id": r.document_id, "filename": r.filename, "snippet": r.snippet}
         for r in cluster.representatives]
        for cluster in resp.clusters
    ]
    labels = [reuse_label(cluster, prev_by_fp, prev_by_id) for cluster in resp.clusters]
    labels = [None if cached is None else (*cached, True) for cached in labels]
    fresh = [idx for idx, cached in enumerate(labels) if cached is None]
    reused, relabelled = total - len(fresh), len(fresh)
    # До конца разметки лог иначе молчит: по этой строке видно, что граф посчитан и сколько ждать модель.
    print(f"graph-compute: {total} clusters in {time.monotonic() - started:.0f}s; "
          f"reused {reused} labels, {relabelled} go to the model x{LABEL_CONCURRENCY}", flush=True)
    # Модель зовётся параллельно, а уникальность названий ниже проверяется в исходном порядке тем:
    # иначе при совпадении суффикс «#2» доставался бы теме, чей ответ пришёл позже.
    with ThreadPoolExecutor(LABEL_CONCURRENCY) as pool:
        futures = {
            pool.submit(label_community, list(resp.clusters[idx].signals), representatives_of[idx]): idx
            for idx in fresh
        }
        for done, future in enumerate(as_completed(futures), reused + 1):
            labels[futures[future]] = future.result()
            # По застывшему updated_at в GET /system/activity видно, что прогон завис.
            if done % 10 == 0:
                report_status("running", epoch=epoch, progress_done=done, progress_total=total)
    report_status("running", epoch=epoch, progress_done=total, progress_total=total)

    cluster_payloads = []
    used_labels = set()
    for cluster, representatives, (label, summary, keywords, from_model) in zip(
        resp.clusters, representatives_of, labels
    ):
        signals = list(cluster.signals)
        label = unique_label(label, keywords, signals, used_labels)
        members = list(cluster.members)
        metrics = cluster.metrics
        params = {
            "knn_k": KNN_K,
            "knn_block_size": KNN_BLOCK_SIZE,
            "resolution": RESOLUTION,
            "min_size": MIN_SIZE,
            "sim_threshold": SIM_THRESHOLD,
            "mutual_knn": MUTUAL_KNN,
            "algorithm": CLUSTER_ALGORITHM,
            "epoch": epoch,
            "fingerprint": cluster.fingerprint,
            "label_fallback": not from_model,
            # Полный состав — для точной преемственности по Жаккару на следующем прогоне.
            "members": members,
            "metrics": {
                "size": metrics.size,
                "avg_similarity": round(metrics.avg_similarity, 4),
                "modularity": round(metrics.modularity, 4),
                "modularity_contribution": round(metrics.modularity_contribution, 4),
            },
        }
        if cluster.HasField("lineage"):
            lin = cluster.lineage
            lineage = {
                "previous_cluster_id": lin.previous_cluster_id,
                "jaccard": round(lin.jaccard, 4),
                "stability": round(lin.stability, 4),
            }
            if lin.merged_from:
                lineage["merged_from"] = list(lin.merged_from)
            if lin.split_from:
                lineage["split_from"] = lin.split_from
            params["lineage"] = lineage
        cluster_payloads.append({
            "label": label, "summary": summary, "keywords": keywords, "method": "leiden-doc",
            "chunk_count": cluster.chunk_count, "document_count": len(members),
            "representatives": representatives, "params": params,
        })
        print(f"  prepared [{label}] docs={len(members)} chunks={cluster.chunk_count}", flush=True)

    print(f"labels: reused={reused} relabelled={relabelled} total={total} (LLM calls saved: {reused})", flush=True)
    created_clusters = gw("/clusters/replace", {"clusters": cluster_payloads, "request_id": publication_id,
                                             "expected_publication_id": expected_head})
    for cluster in created_clusters:
        print(
            f"  + [{cluster['label']}] docs={cluster['document_count']} chunks={cluster['chunk_count']}",
            flush=True,
        )
    print(f"created {len(created_clusters)} clusters", flush=True)
    # Синхронизация направлений — доводка поверх опубликованной доски: её сбой не должен отменять
    # пересчёт технологического индекса ниже, иначе на странице темы пропадают карта и таблица.
    try:
        sync_auto_hypotheses(created_clusters, epoch, corpus_stats(created_clusters))
    except Exception as e:  # noqa: BLE001 — доводка не отменяет публикацию
        print(f"auto hypotheses sync skipped: {e}", file=sys.stderr, flush=True)
    trigger_itc_refresh(epoch)
    return len(created_clusters)


def main():
    r = redis_client()
    _STATUS["r"] = r
    print(f"cluster-worker up; watching {EPOCH_KEY} (debounce {DEBOUNCE_SEC}s, every {CHECK_INTERVAL}s)", flush=True)
    try:
        last_clustered = redis_int(r, LAST_CLUSTERED_KEY)
    except Exception as e:  # noqa: BLE001 — bad state should not stop the worker
        print(f"last clustered epoch read failed: {e}", file=sys.stderr, flush=True)
        last_clustered = None
    seen_epoch = last_clustered
    seen_at = time.time() if last_clustered is not None else 0.0
    if last_clustered is not None:
        print(f"last clustered epoch={last_clustered}; waiting for a newer corpus epoch", flush=True)
    while True:
        try:
            raw = r.get(EPOCH_KEY)
            epoch = int(raw) if raw else 0
            now = time.time()
            if epoch != seen_epoch:
                seen_epoch, seen_at = epoch, now  # эпоха сдвинулась — пауза отсчитывается заново
                print(f"epoch moved to {epoch}; waiting {DEBOUNCE_SEC}s before recluster", flush=True)
            elif epoch != last_clustered and epoch > 0 and (now - seen_at) >= DEBOUNCE_SEC and not CLUSTER_AUTORUN:
                # Отметка сдвигается только в памяти: после включения автозапуска последняя эпоха
                # всё же пересчитается.
                print(f"auto-recluster disabled (CLUSTER_AUTORUN=false); skipping epoch {epoch}", flush=True)
                last_clustered = epoch
                report_status("idle", epoch=epoch)
            elif epoch != last_clustered and epoch > 0 and (now - seen_at) >= DEBOUNCE_SEC:
                try:
                    report_status("running", epoch=epoch)
                    outcome = run_command(r, "clusters", str(epoch),
                        [sys.executable, __file__, "--run-epoch", str(epoch)],
                        timeout=RUN_TIMEOUT, completed_key=LAST_CLUSTERED_KEY, completed_value=epoch)
                    if outcome == "busy":
                        time.sleep(CHECK_INTERVAL)
                        continue
                    if outcome == "failed":
                        report_status("error", epoch=epoch, last_error="worker_failed")
                    last_clustered = epoch
                    if outcome == "succeeded":
                        report_status("idle", epoch=epoch)
                except Exception as e:  # noqa: BLE001 — keep the loop alive
                    print(f"recluster failed: {e}", file=sys.stderr, flush=True)
                    report_status("error", epoch=epoch, last_error=str(e))
        except Exception as e:  # noqa: BLE001 — Valkey/transient
            print(f"loop error: {e}", file=sys.stderr, flush=True)
        time.sleep(CHECK_INTERVAL)


def _selfcheck():
    board = [
        {"document_count": 40, "keywords": ["segmentation", "ct"]},
        {"document_count": 4, "keywords": ["barren plateaus"]},
        {"document_count": 20, "keywords": ["segmentation"]},
    ]
    cs = corpus_stats(board)
    assert cs["total_docs"] == 64 and cs["cluster_count"] == 3 and cs["max_docs"] == 40
    assert cs["keyword_freq"]["segmentation"] == 2 and cs["keyword_freq"]["ct"] == 1
    nov_big, nov_small, nov_mid = (cluster_novelty(c, cs) for c in board)
    assert 0.0 <= nov_big <= nov_mid <= nov_small <= 1.0
    assert nov_small > 0.5 > nov_big
    assert cluster_novelty(board[0], None) == 0.5
    assert cluster_novelty(board[0], corpus_stats([board[0]])) == 0.5
    assert 0.0 <= cluster_novelty({"document_count": 4, "keywords": []}, cs) <= 1.0
    print("selfcheck ok", flush=True)


if __name__ == "__main__":
    if len(sys.argv) == 3 and sys.argv[1] == "--run-epoch":
        epoch = int(sys.argv[2])
        guard_lease()
        r = redis_client()
        _STATUS["r"] = r
        recluster(epoch)
    else:
        if "--selfcheck" in sys.argv:
            _selfcheck()
        else:
            main()
