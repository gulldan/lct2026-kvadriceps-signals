#!/usr/bin/env python3
"""Оценка RAG на размеченном наборе вопросов (dataset.jsonl) через живой API.

Поиск: Hit@k, MRR, nDCG@k. Генерация (RAGAS и «триада» TruLens): faithfulness,
answer_relevance, context_relevance и answer_correctness ставит LLM-судья из
VLLM_URL, answer_similarity — косинус F2LLM-v2 между ответом и эталоном. Метрика,
которую не удалось посчитать, пишется как null и не обрывает прогон.

С --gate (или EVAL_GATE=1) прогон становится регрессионным порогом:
  0 — все метрики не ниже порогов;
  1 — метрика ниже порога либо судья ожидался (EVAL_SKIP_JUDGE не задан), но ничего
      не оценил: null не считается успехом;
  2 — ошибка обвязки (недоступен API или эмбеддер, нет набора), а не провал качества.
Пороги переопределяются через EVAL_MIN_<МЕТРИКА>. Без --gate разбор PASS/FAIL
печатается, но выход всегда 0.

EVAL_HYPOTHESES=1 добавляет оценку фабрики гипотез по dataset-hypotheses.jsonl,
EVAL_DISCOVERY=1 — вклад мостов в доску. Обе пишутся в results.json отдельными
ключами и на порог не влияют.
"""

import json
import math
import os
from worker_lease import guard_lease
import statistics
import time
import uuid
import urllib.error
import urllib.request

HERE = os.path.dirname(os.path.abspath(__file__))
GATEWAY = os.environ.get("RAG_GATEWAY", "http://localhost:18080").rstrip("/")
API_BASE = GATEWAY if GATEWAY.endswith("/api/v1") else GATEWAY + "/api/v1"
EMBED_URL = os.environ.get("EVAL_EMBED_URL", "http://localhost:8085/v1/embeddings")
USER = os.environ.get("RAG_USER", "admin")
PASS = os.environ.get("RAG_PASS", "")
TOPK = int(os.environ.get("EVAL_TOPK", "5"))
EVAL_HYPOTHESES = os.environ.get("EVAL_HYPOTHESES") == "1"
HYP_COUNT = int(os.environ.get("EVAL_HYP_COUNT", "5"))
EVAL_DISCOVERY = os.environ.get("EVAL_DISCOVERY") == "1"


def env(key: str) -> str:
    if os.environ.get(key, ""):
        return os.environ[key]
    env_paths = [
        os.path.join(HERE, "..", "..", "infra", ".env"),
        os.path.join(HERE, "..", ".env"),
    ]
    for path in env_paths:
        try:
            with open(path, encoding="utf-8") as f:
                for line in f:
                    if line.startswith(key + "="):
                        return line.split("=", 1)[1].strip()
        except FileNotFoundError:
            continue
    return ""


JUDGE_BASE_URL = env("VLLM_URL").rstrip("/")
JUDGE_URL = JUDGE_BASE_URL + "/v1/chat/completions" if JUDGE_BASE_URL else ""
JUDGE_KEY = env("VLLM_API_KEY")
JUDGE_MODEL = env("VLLM_MODEL")
JUDGE_MAX_TOKENS = max(128, min(8192, int(os.environ.get("EVAL_JUDGE_MAX_TOKENS", "4096"))))

GATE = "--gate" in __import__("sys").argv[1:] or os.environ.get("EVAL_GATE") == "1"

EXIT_PASS = 0
EXIT_QUALITY_FAIL = 1
EXIT_HARNESS_ERROR = 2


def _floor(key: str, default: float) -> float:
    return float(os.environ.get("EVAL_MIN_" + key.upper(), default))


# Пороги под синтетический эталонный набор: поиск на нём держится около 1,0.
# answer_similarity сравнивает свободный ответ с коротким эталоном — отсюда запас.
THRESHOLDS = {
    "hit@k": _floor("hit@k", 0.90),
    "mrr": _floor("mrr", 0.90),
    "ndcg@k": _floor("ndcg@k", 0.90),
    "answer_similarity": _floor("answer_similarity", 0.55),
    "faithfulness": _floor("faithfulness", 0.70),
    "answer_relevance": _floor("answer_relevance", 0.70),
    "context_relevance": _floor("context_relevance", 0.70),
    "answer_correctness": _floor("answer_correctness", 0.70),
}
JUDGE_METRICS = ("faithfulness", "answer_relevance", "context_relevance", "answer_correctness")

_TRANSIENT = (urllib.error.URLError, TimeoutError, OSError)


def http(url, body=None, headers=None, timeout=90, retries=5):
    data = json.dumps(body).encode() if body is not None else None
    last = None
    for attempt in range(retries):
        try:
            req = urllib.request.Request(url, data=data, headers=dict(headers or {}), method="POST" if data else "GET")
            if data:
                req.add_header("Content-Type", "application/json")
            guard_lease()
            with urllib.request.urlopen(req, timeout=timeout) as r:
                return json.loads(r.read().decode("utf-8"), strict=False)
        except urllib.error.HTTPError as e:
            last = e
            if e.code in (429, 500, 502, 503, 504):
                time.sleep(3 * (attempt + 1))
                continue
            raise
        except _TRANSIENT as e:
            last = e
            time.sleep(3 * (attempt + 1))
    raise last


def login() -> str:
    return http(f"{API_BASE}/auth/login", {"username": USER, "password": PASS})["access_token"]


def ask(token, question):
    h = {"Authorization": "Bearer " + token}
    chat = http(f"{API_BASE}/chats", {"title": "eval"}, h)["id"]
    t0 = time.time()
    resp = http(f"{API_BASE}/chats/{chat}/messages", {"content": question, "request_id": str(uuid.uuid4())}, h, timeout=150)
    return resp.get("content", ""), resp.get("sources", []), time.time() - t0


def embed(texts):
    return [d["embedding"] for d in http(EMBED_URL, {"model": "F2LLM-v2-0.6B", "input": texts})["data"]]


def cosine(a, b):
    dot = sum(x * y for x, y in zip(a, b))
    na, nb = math.sqrt(sum(x * x for x in a)), math.sqrt(sum(y * y for y in b))
    return dot / (na * nb) if na and nb else 0.0


JUDGE_SUFFIX = '\n\nОтветь СТРОГО в формате JSON без пояснений: {"score": <число от 0.0 до 1.0>}'


def judge_payload(prompt, attempt=0):
    payload = {"model": JUDGE_MODEL, "messages": [{"role": "user", "content": prompt}],
               "max_tokens": min(8192, JUDGE_MAX_TOKENS * (attempt + 1)), "temperature": 0,
               "response_format": {"type": "json_object"}}
    if effort := os.environ.get("VLLM_REASONING_EFFORT", "").strip():
        payload["reasoning_effort"] = effort
    return payload


def judge(prompt):
    if os.environ.get("EVAL_SKIP_JUDGE") or not JUDGE_URL or not JUDGE_MODEL:
        return None
    headers = {"X-Data-Logging-Enabled": "false"}
    if JUDGE_KEY:
        headers["Authorization"] = "Bearer " + JUDGE_KEY
    for attempt in range(2):
        try:
            r = http(JUDGE_URL, judge_payload(prompt + JUDGE_SUFFIX, attempt), headers)
            choice = r["choices"][0]
            # Оборванный ответ и число из рассуждения не являются оценкой качества.
            if choice.get("finish_reason") == "length":
                continue
            score = json.loads(choice["message"].get("content") or "").get("score")
            if type(score) in (int, float) and math.isfinite(score) and 0 <= score <= 1:
                return float(score)
        except Exception:
            continue
    return None


PROMPTS = {
    "faithfulness": "Контекст (фрагменты, найденные системой):\n{ctx}\n\nОтвет системы:\n{ans}\n\nЗадача: оцени, насколько ответ подтверждается ТОЛЬКО приведённым контекстом, без домыслов. 1.0 — все утверждения ответа есть в контексте; 0.0 — ответ не следует из контекста. Верни ТОЛЬКО число от 0.0 до 1.0.",
    "answer_relevance": "Вопрос: {q}\nОтвет: {ans}\n\nНасколько ответ по существу отвечает именно на этот вопрос (без воды и ухода в сторону)? Верни ТОЛЬКО число от 0.0 до 1.0.",
    "context_relevance": "Вопрос: {q}\n\nНайденный контекст:\n{ctx}\n\nНасколько найденный контекст релевантен вопросу? 1.0 — содержит нужную информацию; 0.0 — нерелевантен. Верни ТОЛЬКО число от 0.0 до 1.0.",
    "correctness": "Вопрос: {q}\nЭталонный ответ: {gt}\nОтвет системы: {ans}\n\nНасколько ответ системы фактически совпадает с эталоном? 1.0 — полностью верно; 0.0 — неверно. Верни ТОЛЬКО число от 0.0 до 1.0.",
}


def retrieval_metrics(sources, relevant_doc, k):
    # Попадания сводятся к документам (первое вхождение), чтобы метрики были
    # на уровне документа и nDCG не выходил за [0, 1].
    docs = []
    for s in sources[:k]:
        fn = s.get("filename", "") or ""
        if fn and fn not in docs:
            docs.append(fn)
    ranks = [i + 1 for i, fn in enumerate(docs) if relevant_doc.lower() in fn.lower()]
    if not ranks:
        return 0.0, 0.0, 0.0
    r = ranks[0]  # релевантный документ один, релевантность бинарная: идеальный DCG = 1
    return 1.0, 1.0 / r, (1.0 / math.log2(r + 1)) / (1.0 / math.log2(2))


def prometheus_stages(metric: str, group: str):
    import urllib.parse

    prom = os.environ.get("EVAL_PROM_URL", "http://localhost:9090")
    out: dict = {}
    for quant, label in ((0.5, "p50"), (0.95, "p95")):
        promql = f"histogram_quantile({quant}, sum by ({group}, le) ({metric}_bucket))"
        try:
            d = http(prom + "/api/v1/query?query=" + urllib.parse.quote(promql))
            for r in d.get("data", {}).get("result", []):
                k = r["metric"].get(group, "?")
                v = float(r["value"][1])
                if v == v:  # отсекает NaN
                    out.setdefault(k, {})[label] = round(v, 3)
        except Exception:
            pass
    return out


def find_or_create_kpi(token, spec):
    h = {"Authorization": "Bearer " + token}
    title = (spec.get("title") or "").strip()
    for k in http(f"{API_BASE}/kpis", None, h) or []:
        if (k.get("title") or "").strip() == title and title:
            return k["id"]
    body = {
        "title": title,
        "description": spec.get("description", ""),
        "metric": spec.get("metric", ""),
        "unit": spec.get("unit", ""),
        "direction": spec.get("direction", ""),
        "baseline": spec.get("baseline"),
        "target": spec.get("target"),
        "function_area": spec.get("material_family", ""),
        # material_family — поле предметной области; в detail оно переживает сохранение и чтение.
        "detail": {"material_family": spec.get("material_family", "")},
    }
    return http(f"{API_BASE}/kpis", body, h)["id"]


def generate_for_kpi(token, kpi_id, count):
    h = {"Authorization": "Bearer " + token}
    return http(f"{API_BASE}/hypotheses/generate", {"kpi_id": kpi_id, "count": count}, h, timeout=600) or []


def _hay(hyp):
    parts = [hyp.get("statement", ""), hyp.get("rationale", ""), hyp.get("method", "")]
    for key in ("detail", "assessment"):
        v = hyp.get(key)
        if isinstance(v, (dict, list)):
            parts.append(json.dumps(v, ensure_ascii=False))
        elif isinstance(v, str):
            parts.append(v)
    return " ".join(parts).lower()


# Ключевые слова на обоих языках: генератор пишет то по-русски, то по-английски.
# Гипотеза фальсифицируема, если называет рычаг и материал, опирается на базу и предлагает проверку.
_KW_INTERVENTION = ("intervention", "composition_change", "process_change", "lever", "вмешательств", "легирован", "допирован", "режим", "обработк", "состав")
_KW_MATERIAL = ("material_system", "material", "alloy", "coating", "electrolyte", "материал", "сплав", "покрыти", "электролит", "систем")
_KW_BASELINE = ("baseline", "база", "базов", "исходн", "относительно", "к базе")
_KW_VALIDATION = ("validation_plan", "experiment_plan", "experiment", "test_method", "characterization", "success_criteria", "валидаци", "эксперимент", "испытан", "метод проверк", "критери")


def schema_completeness(hyp):
    text = _hay(hyp)

    def has(words):
        return any(w in text for w in words)

    return has(_KW_INTERVENTION) and has(_KW_MATERIAL) and has(_KW_BASELINE) and has(_KW_VALIDATION)


def counterevidence(hyp):
    """Есть ли доказательство против или контекстное: гипотеза не подпёрта одними «за»."""
    ev = hyp.get("evidence") or []
    return any((e.get("stance") or "").lower() in ("contradicts", "context") for e in ev)


def composite_of(hyp):
    cs = hyp.get("composite_score")
    if isinstance(cs, (int, float)):
        return float(cs)
    rank = (hyp.get("assessment") or {}).get("ranking") or {}
    sc = rank.get("score")
    return float(sc) if isinstance(sc, (int, float)) else None


def separation(hyps, good, bad):
    """Доля пар «похожая на хорошую — похожая на плохую», где хорошая выше по composite доски.

    Сгенерированные гипотезы делятся по близости F2LLM-v2 к эталонным формулировкам
    expected_good и expected_bad. Без оценок или эмбеддера score = null."""
    scored = [(h, composite_of(h)) for h in hyps]
    scored = [(h, s) for h, s in scored if s is not None]
    out = {"n_generated": len(hyps), "n_scored": len(scored)}
    if not scored:
        out["score"] = None
        out["note"] = "no composite/ranking scores yet (generation stack not fully up)"
        return out

    good_txt = [g.get("description", "") for g in good if g.get("description")]
    bad_txt = [b.get("description", "") for b in bad if b.get("description")]
    try:
        if not good_txt or not bad_txt:
            raise ValueError("no reference descriptions")
        vecs = embed([h.get("statement", "") or "" for h, _ in scored] + good_txt + bad_txt)
        n = len(scored)
        hv, gv, bv = vecs[:n], vecs[n:n + len(good_txt)], vecs[n + len(good_txt):]
        good_s = [s for (_, s), v in zip(scored, hv) if max(cosine(v, g) for g in gv) >= max(cosine(v, b) for b in bv)]
        bad_s = [s for (_, s), v in zip(scored, hv) if max(cosine(v, g) for g in gv) < max(cosine(v, b) for b in bv)]
        out["n_good_like"], out["n_bad_like"] = len(good_s), len(bad_s)
        if good_s and bad_s:
            pairs = [(gs, bs) for gs in good_s for bs in bad_s]
            out["score"] = round(sum(1 for gs, bs in pairs if gs > bs) / len(pairs), 3)
            out["note"] = "fraction of good>bad composite pairs (good/bad split by F2LLM-v2 similarity)"
        else:
            out["score"] = None
            out["note"] = "could not split generated into good-like vs bad-like (need both)"
    except Exception:
        vals = [s for _, s in scored]
        out["score"] = None
        out["composite_scores"] = [round(v, 3) for v in vals]
        out["composite_mean"] = round(statistics.mean(vals), 3)
        out["note"] = "embedder unavailable; reporting generated composite scores only"
    return out


def run_hypothesis_eval(token):
    path = os.path.join(HERE, "dataset-hypotheses.jsonl")
    specs = [json.loads(ln) for ln in open(path, encoding="utf-8") if ln.strip()]
    print(f"\nHypothesis eval: {len(specs)} KPIs (count={HYP_COUNT} each)\n", flush=True)

    kpis_out, all_complete, all_counter = [], [], []
    for spec in specs:
        kpi = spec.get("kpi", {})
        title = kpi.get("title", "")
        try:
            kpi_id = find_or_create_kpi(token, kpi)
            hyps = generate_for_kpi(token, kpi_id, HYP_COUNT)
        except Exception as e:
            print(f"  ! KPI failed: {title[:44]} {e}", flush=True)
            kpis_out.append({"kpi": title, "error": str(e), "n_generated": 0})
            continue
        comp = [schema_completeness(h) for h in hyps]
        cnt = [counterevidence(h) for h in hyps]
        all_complete += comp
        all_counter += cnt
        sep = separation(hyps, spec.get("expected_good", []), spec.get("expected_bad", []))

        def rate(xs):
            return round(sum(xs) / len(xs), 3) if xs else None

        entry = {
            "kpi": title,
            "kpi_id": kpi_id,
            "n_generated": len(hyps),
            "schema_completeness": rate(comp),
            "counterevidence_rate": rate(cnt),
            "separation": sep,
        }
        kpis_out.append(entry)
        print(f"  {title[:44]:46s} n={len(hyps)} complete={entry['schema_completeness']} counter={entry['counterevidence_rate']} sep={sep.get('score')}", flush=True)

    def agg(xs):
        return {"mean": round(sum(xs) / len(xs), 3), "n": len(xs)} if xs else {"mean": None, "n": 0}

    return {
        "n_kpis": len(specs),
        "hyp_count": HYP_COUNT,
        "schema_completeness": agg(all_complete),
        "counterevidence_rate": agg(all_counter),
        "kpis": kpis_out,
    }


def _obj(value):
    if isinstance(value, dict):
        return value
    if isinstance(value, str):
        try:
            parsed = json.loads(value)
        except (ValueError, TypeError):
            return {}
        return parsed if isinstance(parsed, dict) else {}
    return {}


def _has_tag(hyp, tag):
    return tag in (hyp.get("tags") or [])


def _grounded(hyp):
    return any(e.get("document_id") for e in (hyp.get("evidence") or []))


def _doc_count(hyp):
    return len({e.get("document_id") for e in (hyp.get("evidence") or []) if e.get("document_id")})


def _cross_community_span(hyp):
    """Доказательства покрывают обе темы моста (relation theme_a и theme_b ставит discovery-worker)."""
    rels = {(e.get("relation") or "").lower() for e in (hyp.get("evidence") or [])}
    return "theme_a" in rels and "theme_b" in rels


def _novelty(hyp):
    """Новизна по корпусу (1 − top_sim), записанная discovery-worker."""
    ns = hyp.get("novelty_score")
    return float(ns) if isinstance(ns, (int, float)) else None


def run_discovery_eval(token):
    """Вклад мостов (auto_bridge) в доску относительно гипотез одного кластера (auto_cluster)."""
    h = {"Authorization": "Bearer " + token}
    board = http(f"{API_BASE}/hypotheses?limit=500", None, h) or []
    bridges = [x for x in board if _has_tag(x, "auto_bridge")]
    clusters = [x for x in board if _has_tag(x, "auto_cluster")]
    print(f"\nDiscovery eval: {len(bridges)} bridge hypotheses vs {len(clusters)} cluster (baseline)\n", flush=True)

    def rate(items):
        return round(sum(1 for x in items if x) / len(items), 3) if items else None

    def meanf(items):
        vals = [x for x in items if x is not None]
        return round(statistics.mean(vals), 3) if vals else None

    scores = [_obj(b.get("generation")).get("scores", {}) for b in bridges]

    def score_mean(key):
        return meanf([s.get(key) for s in scores if isinstance(s, dict)])

    bridge_nov = meanf([_novelty(b) for b in bridges])
    cluster_nov = meanf([_novelty(c) for c in clusters])
    block = {
        "n_bridge_hypotheses": len(bridges),
        "n_cluster_hypotheses": len(clusters),
        "falsifiability": rate([schema_completeness(b) for b in bridges]),
        "grounding": rate([_grounded(b) for b in bridges]),
        "cross_community_span": rate([_cross_community_span(b) for b in bridges]),
        "counterevidence_rate": rate([counterevidence(b) for b in bridges]),
        "novelty_mean": bridge_nov,
        "bridge_scores": {
            "maverick": score_mean("maverick"),
            "bridging_centrality": score_mean("bridging_centrality"),
            "convergence": score_mean("convergence"),
            "composite": score_mean("composite"),
        },
        "contribution_vs_cluster": {
            "bridge_cross_doc_rate": rate([_doc_count(b) >= 2 for b in bridges]),
            "cluster_cross_doc_rate": rate([_doc_count(c) >= 2 for c in clusters]),
            "bridge_novelty_mean": bridge_nov,
            "cluster_novelty_mean": cluster_nov,
            "novelty_delta": (round(bridge_nov - cluster_nov, 3) if bridge_nov is not None and cluster_nov is not None else None),
            "note": "bridges add value when they surface cross-community (theme_a×theme_b) hypotheses the per-cluster baseline cannot, at comparable-or-higher corpus novelty",
        },
    }
    print(f"  bridges: falsifiable={block['falsifiability']} span={block['cross_community_span']} novelty={block['novelty_mean']} composite={block['bridge_scores']['composite']}", flush=True)
    contrib = block["contribution_vs_cluster"]
    print(f"  contribution: cross_doc bridge={contrib['bridge_cross_doc_rate']} vs cluster={contrib['cluster_cross_doc_rate']} novelty_delta={contrib['novelty_delta']}", flush=True)
    return block


def gate(summary):
    failures, lines = [], []
    skip_judge = bool(os.environ.get("EVAL_SKIP_JUDGE"))
    flat = {**summary["retrieval"], **summary["generation"]}

    for key in ("hit@k", "mrr", "ndcg@k", "answer_similarity", *JUDGE_METRICS):
        agg = flat.get(key, {"mean": None, "n": 0})
        floor = THRESHOLDS[key]
        mean, n = agg.get("mean"), agg.get("n", 0)
        is_judge = key in JUDGE_METRICS

        if mean is None or n == 0:
            if is_judge and not skip_judge:
                msg = f"{key}: null/n=0 — judge expected but nothing was scored (not a passing state)"
                failures.append(msg)
                lines.append("  FAIL  " + msg)
            elif is_judge and skip_judge:
                lines.append(f"  skip  {key}: judge disabled (EVAL_SKIP_JUDGE)")
            else:
                msg = f"{key}: null/n=0 — no data scored"
                failures.append(msg)
                lines.append("  FAIL  " + msg)
            continue

        if mean < floor:
            msg = f"{key}: mean {mean:.3f} < floor {floor:.3f} (n={n})"
            failures.append(msg)
            lines.append("  FAIL  " + msg)
        else:
            lines.append(f"  ok    {key}: mean {mean:.3f} >= floor {floor:.3f} (n={n})")

    return (not failures), lines


def main():
    cases = [json.loads(ln) for ln in open(os.path.join(HERE, "dataset.jsonl"), encoding="utf-8") if ln.strip()]
    try:
        token = login()
    except Exception as e:
        print(f"!! harness error: cannot reach RAG API at {GATEWAY}: {e}", flush=True)
        if GATE:
            print("\n=== GATE: ERROR (harness/setup, exit 2) ===", flush=True)
        raise SystemExit(EXIT_HARNESS_ERROR)
    rows = []
    print(f"Evaluating {len(cases)} questions (top_k={TOPK}, judge={JUDGE_MODEL})\n", flush=True)
    for c in cases:
        try:
            ans, sources, dt = ask(token, c["q"])
        except Exception as e:
            print(f"  ! case failed (RAG call): {c['q'][:40]}… {e}", flush=True)
            continue
        ctx = "\n---\n".join((s.get("snippet", "") or "") for s in sources[:TOPK])[:3000]
        hit, mrr, ndcg = retrieval_metrics(sources, c["doc"], TOPK)
        try:
            sim = cosine(*embed([ans, c["gt"]]))
        except Exception:
            sim = None
        row = {
            "q": c["q"], "doc": c["doc"], "hit": hit, "mrr": mrr, "ndcg": ndcg,
            "faithfulness": judge(PROMPTS["faithfulness"].format(ctx=ctx, ans=ans)),
            "answer_relevance": judge(PROMPTS["answer_relevance"].format(q=c["q"], ans=ans)),
            "context_relevance": judge(PROMPTS["context_relevance"].format(q=c["q"], ctx=ctx)),
            "answer_correctness": judge(PROMPTS["correctness"].format(q=c["q"], gt=c["gt"], ans=ans)),
            "answer_similarity": sim, "latency_s": dt, "answer": ans[:140],
        }
        rows.append(row)

        def s(x):
            return "—" if x is None else f"{x:.2f}"
        print(f"  {c['q'][:44]:46s} hit={hit:.0f} mrr={mrr:.2f} ndcg={ndcg:.2f} | faith={s(row['faithfulness'])} rel={s(row['answer_relevance'])} ctx={s(row['context_relevance'])} corr={s(row['answer_correctness'])} sim={s(sim)} | {dt:.1f}s", flush=True)
        time.sleep(1)

    def agg(k):
        vals = [r[k] for r in rows if r.get(k) is not None]
        return {"mean": round(statistics.mean(vals), 3), "n": len(vals)} if vals else {"mean": None, "n": 0}

    lat = sorted(r["latency_s"] for r in rows) or [0]
    summary = {
        "n_questions": len(rows), "top_k": TOPK, "judge_model": JUDGE_MODEL,
        "retrieval": {"hit@k": agg("hit"), "mrr": agg("mrr"), "ndcg@k": agg("ndcg")},
        "generation": {m: agg(m) for m in ("faithfulness", "answer_relevance", "context_relevance", "answer_correctness", "answer_similarity")},
        "latency_s": {"p50": round(lat[len(lat) // 2], 2), "p95": round(lat[min(len(lat) - 1, int(0.95 * len(lat)))], 2), "mean": round(statistics.mean(lat), 2)},
    }
    time.sleep(20)  # Prometheus должен успеть снять свежие гистограммы стадий
    summary["pipeline"] = prometheus_stages("rag_stage_duration_seconds", "stage")
    summary["ingestion"] = prometheus_stages("rag_processing_duration_seconds", "queue")

    payload = {"summary": summary, "cases": rows}
    if EVAL_HYPOTHESES:
        try:
            payload["hypotheses"] = run_hypothesis_eval(token)
        except Exception as e:
            print(f"\nhypothesis eval failed (non-fatal): {e}", flush=True)
            payload["hypotheses"] = {"error": str(e)}
    if EVAL_DISCOVERY:
        try:
            payload["discovery"] = run_discovery_eval(token)
        except Exception as e:
            print(f"\ndiscovery eval failed (non-fatal): {e}", flush=True)
            payload["discovery"] = {"error": str(e)}

    json.dump(payload, open(os.path.join(HERE, "results.json"), "w", encoding="utf-8"), ensure_ascii=False, indent=2)
    print("\n=== SCORECARD ===")
    print(json.dumps(summary, ensure_ascii=False, indent=2))
    if "hypotheses" in payload:
        print("\n=== HYPOTHESES ===")
        print(json.dumps(payload["hypotheses"], ensure_ascii=False, indent=2))
    if "discovery" in payload:
        print("\n=== DISCOVERY (bridge contribution) ===")
        print(json.dumps(payload["discovery"], ensure_ascii=False, indent=2))

    # Оценку показывает страница «Метрики» в админке.
    try:
        admin_user = os.environ.get("RAG_ADMIN_USER") or os.environ.get("ADMIN_USERNAME") or USER
        admin_pass = os.environ.get("RAG_ADMIN_PASS") or os.environ.get("ADMIN_PASSWORD") or os.environ.get("RAG_PASS", "")
        if not admin_pass:
            raise RuntimeError("RAG_ADMIN_PASS or ADMIN_PASSWORD is required to publish the scorecard")
        atok = http(f"{API_BASE}/auth/login", {"username": admin_user, "password": admin_pass})["access_token"]
        http(f"{API_BASE}/admin/metrics", payload, {"Authorization": "Bearer " + atok})
        print("\npublished scorecard to /api/v1/admin/metrics")
    except Exception as e:
        print("\npublish failed:", e)

    passed, lines = gate(summary)
    print("\n=== GATE " + ("(enforced)" if GATE else "(advisory — pass --gate or EVAL_GATE=1 to enforce)") + " ===")
    for ln in lines:
        print(ln)
    verdict = "PASS" if passed else "FAIL"
    print(f"\nGATE: {verdict}")
    if GATE and not passed:
        raise SystemExit(EXIT_QUALITY_FAIL)
    raise SystemExit(EXIT_PASS)


if __name__ == "__main__":
    try:
        main()
    except SystemExit:
        raise
    except Exception as e:
        # Непойманный сбой — ошибка обвязки: трейсбек дал бы код 1, неотличимый от провала качества.
        print(f"!! harness error: {type(e).__name__}: {e}", flush=True)
        raise SystemExit(EXIT_HARNESS_ERROR)
