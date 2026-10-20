#!/usr/bin/env python3
"""Смоук готовности демо на локальном compose-стеке.

По умолчанию без трат на генерацию: здоровье сервисов, состояние корпуса, счётчики RAPTOR,
батч реранкера, поиск по векторам сводок. --full-rag перед живым показом ещё создаёт чат
и генерирует ответ; строгие пороги для корпуса — в `make demo-readiness`.
"""

from __future__ import annotations

import argparse
import json
import os
import re
import subprocess
import sys
import time
import urllib.error
import urllib.parse
import urllib.request
from pathlib import Path


ROOT = Path(__file__).resolve().parents[3]
sys.path.insert(0, str(ROOT / "scripts"))
from tools.stand_env import load_env_file, load_stand_env, redact, stand_origin
DEFAULT_QUERY = "сверхпроводящие высокоэнтропийные сплавы критическая температура"
EXTRACTIVE_FALLBACK_MARKER = "Генерация ответа сейчас недоступна"
COMPOSE_ENV_DEFAULTS = {
    "EMBEDDINGS_URL": "http://f2llm-service:8085/v1/embeddings",
    "RERANKER_URL": "http://reranker-service:8086/v1/rerank",
    "OCR_ENGINE_URL": "http://paddleocr-vl-service:8088",
}

def run(cmd: list[str], *, input_text: str | None = None, timeout: int = 120) -> str:
    proc = subprocess.run(
        cmd,
        cwd=ROOT,
        input=input_text,
        text=True,
        stdout=subprocess.PIPE,
        stderr=subprocess.PIPE,
        timeout=timeout,
        check=False,
    )
    if proc.returncode != 0:
        raise RuntimeError(f"operator command failed with exit status {proc.returncode}")
    return proc.stdout.strip()


def compose_exec(compose_file: str, service: str, argv: list[str], *, input_text: str | None = None, timeout: int = 120) -> str:
    return run(
        ["docker", "compose", "-f", compose_file, "exec", "-T", service, *argv],
        input_text=input_text,
        timeout=timeout,
    )


def http_json(method: str, url: str, payload: dict | None = None, token: str | None = None, timeout: int = 60):
    data = json.dumps(payload).encode("utf-8") if payload is not None else None
    req = urllib.request.Request(url, data=data, method=method)
    req.add_header("Content-Type", "application/json")
    if token:
        req.add_header("Authorization", "Bearer " + token)
    with urllib.request.urlopen(req, timeout=timeout) as resp:
        body = resp.read().decode("utf-8")
        return json.loads(body) if body else {}


def http_text(url: str, *, timeout: int = 30) -> tuple[str, str]:
    req = urllib.request.Request(url, method="GET")
    req.add_header("Accept", "text/html,application/xhtml+xml,application/javascript,text/css,*/*")
    with urllib.request.urlopen(req, timeout=timeout) as resp:
        charset = resp.headers.get_content_charset() or "utf-8"
        return resp.read().decode(charset, errors="replace"), resp.headers.get("Content-Type", "")


def ok(label: str, detail: str = "") -> None:
    print(f"[ok] {label}{': ' + report_value(detail) if detail else ''}")


def warn(label: str, detail: str) -> None:
    print(f"[warn] {label}: {report_value(detail)}", file=sys.stderr)


def fail(label: str, detail: str) -> None:
    print(f"[fail] {label}: {report_value(detail)}", file=sys.stderr)


def effective_demo_env(env_file: Path) -> dict[str, str]:
    return {**COMPOSE_ENV_DEFAULTS, **load_env_file(env_file), **load_stand_env()}


def report_value(value):
    env_file = Path(os.environ.get("DEMO_ENV_FILE", "infra/.env"))
    return redact(value, effective_demo_env(ROOT / env_file))


def configure_operator_args(parser, args, env):
    if args.password is not None:
        parser.error("passwords are accepted only through environment variables")
    user = env.get("RAG_USER") or env.get("STAND_USER") or env.get("ADMIN_USERNAME")
    if args.user is not None and args.user != user:
        parser.error("user is configured through RAG_USER, STAND_USER or ADMIN_USERNAME")
    args.user = user
    args.password = env.get("RAG_PASS") or env.get("STAND_PASSWORD") or env.get("ADMIN_PASSWORD")
    if not getattr(args, "env_only", False) and (not args.user or not args.password):
        parser.error("configure an operator user and password in the private environment")
    base = stand_origin(env, required=False)
    configured = {
        "api_base": env.get("RAG_API_BASE") or (base + "/api/v1" if base else "http://localhost:8080/api/v1"),
        "frontend_url": env.get("DEMO_FRONTEND_URL") or base or "http://localhost:5173",
    }
    for name, value in configured.items():
        if getattr(args, name) is not None and getattr(args, name) != value:
            parser.error("API and frontend addresses are configured through the environment")
        setattr(args, name, value)
    expected_file = os.environ.get("DEMO_ENV_FILE", os.environ.get("STAND_ENV_FILE", "infra/.env"))
    if args.env_file != expected_file:
        parser.error("env file is configured through DEMO_ENV_FILE or STAND_ENV_FILE")


def looks_external_url(url: str) -> bool:
    parsed = urllib.parse.urlparse(url)
    host = (parsed.hostname or "").lower()
    return parsed.scheme in {"http", "https"} and host not in {"", "localhost", "127.0.0.1", "0.0.0.0"} and not host.endswith(".local")


def demo_env_check(env_file: Path, *, require_generation: bool, strict_secrets: bool) -> dict[str, int]:
    env = effective_demo_env(env_file)
    failures: list[str] = []
    warnings: list[str] = []

    if not env:
        failures.append("configured private environment is missing or empty")

    for name in ("ADMIN_USERNAME", "ADMIN_PASSWORD", "JWT_SECRET", "GRAFANA_ADMIN_USER",
                 "GRAFANA_ADMIN_PASSWORD", "RABBITMQ_USER", "RABBITMQ_PASSWORD"):
        if not env.get(name):
            failures.append(name + " is empty")

    jwt_secret = env.get("JWT_SECRET", "")
    if jwt_secret in {"", "dev-secret-change-me", "change-me", "changeme"}:
        (failures if strict_secrets else warnings).append("JWT_SECRET is a known development default")
    elif len(jwt_secret) < 32:
        (failures if strict_secrets else warnings).append("JWT_SECRET is shorter than 32 characters")

    admin_user = env.get("ADMIN_USERNAME", "")
    admin_password = env.get("ADMIN_PASSWORD", "")
    if (admin_user, admin_password) == ("admin", "admin"):
        (failures if strict_secrets else warnings).append("admin credentials use the public demo default")
    elif admin_password and admin_password == admin_user:
        (failures if strict_secrets else warnings).append("admin password equals username")
    elif admin_password and len(admin_password) < 12:
        (failures if strict_secrets else warnings).append("admin password is shorter than 12 characters")

    grafana_user = env.get("GRAFANA_ADMIN_USER", "")
    grafana_password = env.get("GRAFANA_ADMIN_PASSWORD", "")
    if (grafana_user, grafana_password) == ("admin", "admin"):
        (failures if strict_secrets else warnings).append("Grafana admin credentials use the public default")
    elif grafana_password and grafana_password == grafana_user:
        (failures if strict_secrets else warnings).append("Grafana admin password equals username")
    elif grafana_password and len(grafana_password) < 12:
        (failures if strict_secrets else warnings).append("Grafana admin password is shorter than 12 characters")

    rabbitmq_user = env.get("RABBITMQ_USER", "")
    rabbitmq_password = env.get("RABBITMQ_PASSWORD", "")
    if (rabbitmq_user, rabbitmq_password) in {("rag", "ragpass"), ("guest", "guest")}:
        (failures if strict_secrets else warnings).append("RabbitMQ credentials use a public development default")
    elif rabbitmq_password and rabbitmq_password == rabbitmq_user:
        (failures if strict_secrets else warnings).append("RabbitMQ password equals username")
    elif rabbitmq_password and len(rabbitmq_password) < 12:
        (failures if strict_secrets else warnings).append("RabbitMQ password is shorter than 12 characters")

    vllm_url = env.get("VLLM_URL", "")
    vllm_key = env.get("VLLM_API_KEY", "")
    if require_generation and not vllm_url:
        failures.append("VLLM_URL is empty, generation would use the stub backend")
    if vllm_url and looks_external_url(vllm_url) and not vllm_key:
        failures.append("external VLLM_URL is configured without VLLM_API_KEY")

    for key in ("EMBEDDINGS_URL", "RERANKER_URL", "OCR_ENGINE_URL"):
        if not env.get(key):
            failures.append(f"{key} is empty")

    if failures:
        raise RuntimeError("; ".join(failures))
    for item in warnings:
        warn("demo env", item)
    return {"warnings": len(warnings), "checked": 12}


def valkey_raw(compose_file: str, *args: str) -> str:
    return compose_exec(compose_file, "valkey", ["valkey-cli", "--raw", *args], timeout=30).strip()


def compose_ps(compose_file: str, services: list[str]) -> list[dict]:
    out = run(["docker", "compose", "-f", compose_file, "ps", "--format", "json", *services], timeout=60)
    rows: list[dict] = []
    for line in out.splitlines():
        line = line.strip()
        if line:
            rows.append(json.loads(line))
    return rows


def required_service_status(compose_file: str, services: list[str]) -> dict:
    rows = compose_ps(compose_file, services)
    by_service: dict[str, list[dict]] = {}
    for row in rows:
        service = str(row.get("Service") or "")
        if service:
            by_service.setdefault(service, []).append(row)

    problems: list[str] = []
    for service in services:
        containers = by_service.get(service) or []
        if not containers:
            problems.append(f"{service}: missing")
            continue
        running = [c for c in containers if c.get("State") == "running"]
        if not running:
            status = ", ".join(str(c.get("Status") or c.get("State") or "unknown") for c in containers)
            problems.append(f"{service}: not running ({status})")
            continue
        bad_health = [
            str(c.get("Health") or "")
            for c in running
            if str(c.get("Health") or "").lower() not in {"", "healthy"}
        ]
        if bad_health:
            problems.append(f"{service}: health={','.join(bad_health)}")

    return {"services": len(services), "containers": len(rows), "problems": problems}


def check_required_services(compose_file: str, services: list[str]) -> dict:
    status = required_service_status(compose_file, services)
    problems = status["problems"]
    if problems:
        raise RuntimeError("; ".join(problems))
    return status


def wait_required_services(compose_file: str, services: list[str], seconds: int) -> dict:
    deadline = time.time() + seconds
    last = ""
    while True:
        status = required_service_status(compose_file, services)
        if not status["problems"]:
            return status
        last = "; ".join(status["problems"])
        if time.time() >= deadline:
            raise RuntimeError(f"services not ready after {seconds}s: {last}")
        time.sleep(2)


def frontend_bundle_check(frontend_url: str) -> dict:
    base = frontend_url.rstrip("/") + "/"
    health_url = urllib.parse.urljoin(base, "healthz")
    health, _ = http_text(health_url, timeout=15)
    if not health.strip():
        raise RuntimeError(f"{health_url} returned an empty body")

    html, content_type = http_text(base, timeout=30)
    if "text/html" not in content_type.lower():
        raise RuntimeError(f"{base} returned content-type {content_type!r}, want text/html")
    if 'id="root"' not in html and "id='root'" not in html:
        raise RuntimeError("frontend index does not contain the React root")

    assets = re.findall(r"""(?:src|href)=["']([^"']+\.(?:js|css))["']""", html)
    js_assets = [asset for asset in assets if asset.split("?", 1)[0].endswith(".js")]
    if not js_assets:
        raise RuntimeError("frontend index has no JS bundle reference")

    js_url = urllib.parse.urljoin(base, js_assets[0])
    js, js_type = http_text(js_url, timeout=30)
    if len(js) < 1024:
        raise RuntimeError(f"frontend JS bundle is unexpectedly small: {len(js)} bytes")
    if "javascript" not in js_type.lower():
        raise RuntimeError(f"{js_url} returned content-type {js_type!r}, want javascript")

    return {"assets": len(assets), "js": urllib.parse.urlparse(js_url).path}


def qdrant_counts(compose_file: str) -> dict:
    code = """
import json
import worker

def count(node_type=None):
    payload = {"exact": True}
    if node_type:
        payload["filter"] = {"must": [{"key": "node_type", "match": {"value": node_type}}]}
    return worker.http(
        worker.QDRANT_URL.rstrip("/") + f"/collections/{worker.COLLECTION}/points/count",
        payload,
        timeout=60,
    )["result"]["count"]

print(json.dumps({
    "total": count(),
    "chunks": count("chunk"),
    "summaries": count("raptor_summary"),
}))
"""
    return json.loads(compose_exec(compose_file, "raptor-worker", ["python", "-"], input_text=code, timeout=90))


def summary_search(compose_file: str, query: str) -> dict:
    code = f"""
import json
import worker

query = {query!r}
vec = worker.embed(query)
resp = worker.http(
    worker.QDRANT_URL.rstrip("/") + f"/collections/{{worker.COLLECTION}}/points/search",
    {{
        "vector": vec,
        "limit": 3,
        "with_payload": True,
        "with_vector": False,
        "filter": {{"must": [{{"key": "node_type", "match": {{"value": "raptor_summary"}}}}]}},
    }},
    timeout=120,
)
hits = resp.get("result") or []
out = []
for hit in hits:
    payload = hit.get("payload") or {{}}
    out.append({{
        "score": hit.get("score"),
        "file": payload.get("source_filename"),
        "members": payload.get("member_count"),
        "preview": str(payload.get("text") or "")[:160],
    }})
print(json.dumps(out, ensure_ascii=False))
"""
    hits = json.loads(compose_exec(compose_file, "raptor-worker", ["python", "-"], input_text=code, timeout=180))
    return {"hits": hits}


def reranker_batch_check(compose_file: str) -> dict:
    code = """
import json
import urllib.request

payload = {
    "query": "superconducting high entropy alloys critical temperature",
    "documents": [
        "A scientific paper about superconducting high entropy alloys, critical temperature, niobium and hafnium systems.",
        "A cooking recipe for apple pie with cinnamon and sugar.",
    ],
}
req = urllib.request.Request("http://127.0.0.1:8086/v1/rerank", data=json.dumps(payload).encode(), method="POST")
req.add_header("Content-Type", "application/json")
with urllib.request.urlopen(req, timeout=90) as resp:
    body = json.loads(resp.read().decode())
print(json.dumps({"scores": body.get("scores"), "results": len(body.get("results") or [])}))
"""
    return json.loads(compose_exec(compose_file, "reranker-service", ["python", "-"], input_text=code, timeout=120))


def fetch_list(api_base: str, token: str, path: str, timeout: int = 60) -> list[dict]:
    obj = http_json("GET", api_base + path, token=token, timeout=timeout)
    if not isinstance(obj, list):
        raise RuntimeError(f"{path} returned {type(obj).__name__}, want list")
    return obj


def fetch_hypotheses(api_base: str, token: str, *, min_count: int, tag: str | None = None) -> list[dict]:
    limit = max(10, min_count)
    suffix = f"&tags={tag}" if tag else ""
    return fetch_list(api_base, token, f"/hypotheses?limit={limit}{suffix}")


def check_nonempty_hypothesis_shape(items: list[dict], label: str) -> str:
    sample = next((h for h in items if isinstance(h, dict)), None)
    if not sample:
        raise RuntimeError(f"{label} returned no object sample")
    title = str(sample.get("title") or "").strip()
    statement = str(sample.get("statement") or "").strip()
    if len(title) < 8 or len(statement) < 40:
        raise RuntimeError(f"{label} sample is too thin: title={title!r} statement_len={len(statement)}")
    return title


def hypothesis_generation(h: dict) -> dict:
	gen = h.get("generation")
	return gen if isinstance(gen, dict) else {}


def is_research_direction(h: dict) -> bool:
    gen = hypothesis_generation(h)
    tags = set(h.get("tags") or [])
    return gen.get("semantic_kind") == "research_direction" or gen.get("kind") == "auto_cluster" or "auto_cluster" in tags


def is_fallback_draft(h: dict) -> bool:
    gen = hypothesis_generation(h)
    return gen.get("kind") == "fallback_evidence" or gen.get("model") == "cluster-fallback" or isinstance(gen.get("fallback_reason"), str)


def hypothesis_priority_score(h: dict) -> float:
    assessment = h.get("assessment") if isinstance(h.get("assessment"), dict) else {}
    ranking = assessment.get("ranking") if isinstance(assessment.get("ranking"), dict) else {}
    score = ranking.get("score", h.get("composite_score"))
    try:
        return float(score or 0.0)
    except (TypeError, ValueError):
        return 0.0


def visible_hypotheses(api_base: str, token: str) -> list[dict]:
    board = http_json("GET", api_base + "/hypotheses/board?limit=200", token=token, timeout=60)
    items = board.get("items") if isinstance(board, dict) else None
    if not isinstance(items, list):
        raise RuntimeError("/hypotheses/board did not return an items list")
    visible = [
        h for h in items
        if isinstance(h, dict) and not is_research_direction(h) and not is_fallback_draft(h)
    ]
    return sorted(visible, key=hypothesis_priority_score, reverse=True)


def check_visible_hypothesis_quality(
    api_base: str,
    token: str,
    items: list[dict],
    top_n: int,
    clusters_by_id: dict[str, dict],
) -> str:
    _ = clusters_by_id
    for h in items[:top_n]:
        title = str(h.get("title") or "")
        statement = str(h.get("statement") or "")
        if is_fallback_draft(h):
            raise RuntimeError(f"fallback draft leaked into visible board: {title!r}")
        if len(title.strip()) < 8 or len(statement.strip()) < 80:
            raise RuntimeError(f"visible hypothesis is too thin: {title!r}")
        generation = hypothesis_generation(h)
        if not generation.get("kind"):
            raise RuntimeError(f"visible hypothesis has no generation kind: {title!r}")
        if hypothesis_priority_score(h) <= 0:
            raise RuntimeError(f"visible hypothesis has no priority score: {title!r}")
        evidence = h.get("evidence") if isinstance(h.get("evidence"), list) else []
        if not evidence and h.get("id"):
            evidence = fetch_list(api_base, token, f"/hypotheses/{h['id']}/evidence")
        grounded = [
            e for e in evidence
            if isinstance(e, dict) and (e.get("document_id") or e.get("chunk_id") or e.get("filename"))
        ]
        if not grounded:
            raise RuntimeError(f"visible hypothesis has no grounded evidence in board payload: {title!r}")
    sample = str((items[0] if items else {}).get("title") or "")
    return sample


def bridge_evidence_check(api_base: str, token: str, bridges: list[dict]) -> dict:
    for bridge in bridges[:5]:
        hid = bridge.get("id")
        if not hid:
            continue
        evidence = fetch_list(api_base, token, f"/hypotheses/{hid}/evidence")
        if evidence:
            grounded = [e for e in evidence if (e.get("document_id") or e.get("filename"))]
            if grounded:
                return {
                    "hypothesis_id": hid,
                    "title": bridge.get("title"),
                    "evidence": len(evidence),
                    "grounded": len(grounded),
                }
    raise RuntimeError("no bridge hypothesis among top 5 has grounded evidence")


def run_full_rag(api_base: str, token: str, query: str, timeout: int) -> dict:
    started = time.time()
    cache_busted_query = (
        query
        + f"\n\nIgnore this smoke-test nonce in the answer: demo-smoke-{int(started)}"
    )
    chat = http_json("POST", api_base + "/chats", {"title": "demo-smoke"}, token=token, timeout=30)
    msg = http_json(
        "POST",
        api_base + f"/chats/{chat['id']}/messages",
        {"content": cache_busted_query},
        token=token,
        timeout=timeout,
    )
    return {
        "seconds": time.time() - started,
        "chat_id": chat["id"],
        "answer_len": len(msg.get("content") or ""),
        "extractive_fallback": EXTRACTIVE_FALLBACK_MARKER in str(msg.get("content") or ""),
        "sources": len(msg.get("sources") or []),
    }


def main() -> int:
    parser = argparse.ArgumentParser()
    parser.add_argument("--compose-file", default=os.environ.get("COMPOSE_FILE", "infra/docker-compose.yml"))
    parser.add_argument("--api-base", help="compatibility: must match RAG_API_BASE or STAND_URL")
    parser.add_argument("--user", help="compatibility: must match the configured environment user")
    parser.add_argument("--password", help=argparse.SUPPRESS)
    parser.add_argument("--query", default=os.environ.get("DEMO_QUERY", DEFAULT_QUERY))
    parser.add_argument("--env-file", default=os.environ.get("DEMO_ENV_FILE", os.environ.get("STAND_ENV_FILE", "infra/.env")))
    parser.add_argument(
        "--check-env",
        action="store_true",
        default=os.environ.get("DEMO_CHECK_ENV", "").lower() in {"1", "true", "yes"},
    )
    parser.add_argument(
        "--env-only",
        action="store_true",
        default=os.environ.get("DEMO_ENV_ONLY", "").lower() in {"1", "true", "yes"},
    )
    parser.add_argument(
        "--require-generation-env",
        action="store_true",
        default=os.environ.get("DEMO_REQUIRE_GENERATION_ENV", "").lower() in {"1", "true", "yes"},
    )
    parser.add_argument(
        "--strict-secrets",
        action="store_true",
        default=os.environ.get("DEMO_STRICT_SECRETS", "").lower() in {"1", "true", "yes"},
    )
    parser.add_argument(
        "--require-services",
        default=os.environ.get("DEMO_REQUIRE_SERVICES", ""),
        help="Comma-separated compose service names that must be running and healthy if they define a healthcheck.",
    )
    parser.add_argument("--wait-services-seconds", type=int, default=int(os.environ.get("DEMO_WAIT_SERVICES_SECONDS", "0")))
    parser.add_argument("--frontend-url", help="compatibility: must match DEMO_FRONTEND_URL or STAND_URL")
    parser.add_argument(
        "--check-frontend",
        action="store_true",
        default=os.environ.get("DEMO_CHECK_FRONTEND", "").lower() in {"1", "true", "yes"},
    )
    parser.add_argument("--min-docs", type=int, default=int(os.environ.get("DEMO_MIN_DOCS", "1")))
    parser.add_argument("--min-clusters", type=int, default=int(os.environ.get("DEMO_MIN_CLUSTERS", "0")))
    parser.add_argument("--min-hypotheses", type=int, default=int(os.environ.get("DEMO_MIN_HYPOTHESES", "0")))
    parser.add_argument(
        "--min-auto-cluster-hypotheses",
        type=int,
        default=int(os.environ.get("DEMO_MIN_AUTO_CLUSTER_HYPOTHESES", "0")),
    )
    parser.add_argument(
        "--min-auto-bridge-hypotheses",
        type=int,
        default=int(os.environ.get("DEMO_MIN_AUTO_BRIDGE_HYPOTHESES", "0")),
    )
    parser.add_argument(
        "--require-bridge-evidence",
        action="store_true",
        default=os.environ.get("DEMO_REQUIRE_BRIDGE_EVIDENCE", "").lower() in {"1", "true", "yes"},
    )
    parser.add_argument("--min-visible-hypotheses", type=int, default=int(os.environ.get("DEMO_MIN_VISIBLE_HYPOTHESES", "0")))
    parser.add_argument(
        "--check-visible-hypothesis-quality",
        action="store_true",
        default=os.environ.get("DEMO_CHECK_VISIBLE_HYPOTHESIS_QUALITY", "").lower() in {"1", "true", "yes"},
    )
    parser.add_argument("--visible-quality-top", type=int, default=int(os.environ.get("DEMO_VISIBLE_QUALITY_TOP", "10")))
    parser.add_argument("--full-rag", action="store_true", default=os.environ.get("DEMO_FULL_RAG", "").lower() in {"1", "true", "yes"})
    parser.add_argument("--full-rag-timeout", type=int, default=int(os.environ.get("DEMO_FULL_RAG_TIMEOUT", "240")))
    parser.add_argument("--full-rag-max-seconds", type=float, default=float(os.environ.get("DEMO_FULL_RAG_MAX_SECONDS", "60")))
    parser.add_argument("--min-full-rag-sources", type=int, default=int(os.environ.get("DEMO_MIN_FULL_RAG_SOURCES", "1")))
    parser.add_argument("--min-full-rag-answer-chars", type=int, default=int(os.environ.get("DEMO_MIN_FULL_RAG_ANSWER_CHARS", "80")))
    parser.add_argument(
        "--allow-extractive-fallback",
        action="store_true",
        default=os.environ.get("DEMO_ALLOW_EXTRACTIVE_FALLBACK", "").lower() in {"1", "true", "yes"},
        help="Allow full RAG to pass when the answer used the no-generator extractive fallback.",
    )
    args = parser.parse_args()
    try:
        env = effective_demo_env(ROOT / os.environ.get("DEMO_ENV_FILE", os.environ.get("STAND_ENV_FILE", "infra/.env")))
        configure_operator_args(parser, args, env)
    except (OSError, ValueError):
        parser.error("configured private environment could not be read or contains an invalid address")

    failures = 0
    try:
        run(["docker", "compose", "-f", args.compose_file, "config", "-q"], timeout=60)
        ok("compose config")
    except Exception as exc:  # noqa: BLE001
        fail("compose config", type(exc).__name__)
        return 1

    if args.check_env:
        try:
            env_status = demo_env_check(
                ROOT / args.env_file,
                require_generation=args.require_generation_env,
                strict_secrets=args.strict_secrets,
            )
            ok("demo env", f"{env_status['checked']} settings checked; warnings={env_status['warnings']}")
        except Exception as exc:  # noqa: BLE001
            failures += 1
            fail("demo env", type(exc).__name__)
        if args.env_only:
            if failures:
                print(f"demo smoke failed: {failures} check(s)", file=sys.stderr)
                return 1
            print("demo env ok")
            return 0

    try:
        token = http_json(
            "POST",
            args.api_base + "/auth/login",
            {"username": args.user, "password": args.password},
            timeout=30,
        )["access_token"]
        ok("api login")
    except Exception as exc:  # noqa: BLE001
        fail("api login", type(exc).__name__)
        return 1

    required_services = [s.strip() for s in args.require_services.split(",") if s.strip()]
    if required_services:
        try:
            if args.wait_services_seconds > 0:
                service_status = wait_required_services(args.compose_file, required_services, args.wait_services_seconds)
            else:
                service_status = check_required_services(args.compose_file, required_services)
            ok(
                "compose services",
                f"{service_status['services']} services; {service_status['containers']} containers",
            )
        except Exception as exc:  # noqa: BLE001
            failures += 1
            fail("compose services", type(exc).__name__)

    if args.check_frontend:
        try:
            frontend = frontend_bundle_check(args.frontend_url)
            ok("frontend bundle", f"assets={frontend['assets']}; js={frontend['js']}")
        except Exception as exc:  # noqa: BLE001
            failures += 1
            fail("frontend bundle", type(exc).__name__)

    try:
        docs = http_json("GET", args.api_base + "/documents", token=token, timeout=60)
        indexed = [d for d in docs if isinstance(d, dict) and d.get("status") == "indexed"]
        single = [d for d in indexed if int(d.get("chunk_count") or 0) <= 1]
        if len(indexed) < args.min_docs:
            failures += 1
            fail("indexed documents", f"{len(indexed)} < {args.min_docs}")
        else:
            ok("indexed documents", f"{len(indexed)} indexed; {len(single)} single-chunk")
    except Exception as exc:  # noqa: BLE001
        fail("document list", type(exc).__name__)
        return 1

    try:
        corpus_epoch = valkey_raw(args.compose_file, "get", "rag:corpus_epoch:shared")
        raptor_epoch = valkey_raw(args.compose_file, "get", "rag:raptor:last_epoch")
        signatures = int(valkey_raw(args.compose_file, "hlen", "rag:raptor:doc_signatures") or "0")
        meta = int(valkey_raw(args.compose_file, "hlen", "rag:raptor:doc_meta") or "0")
        if corpus_epoch != raptor_epoch:
            failures += 1
            fail("raptor epoch", f"corpus={corpus_epoch!r} raptor={raptor_epoch!r}")
        else:
            ok("raptor epoch", raptor_epoch)
        if signatures < len(indexed) or meta < len(indexed):
            failures += 1
            fail("raptor signatures", f"signatures={signatures} meta={meta} indexed={len(indexed)}")
        else:
            ok("raptor signatures", f"signatures={signatures}; meta={meta}")
    except Exception as exc:  # noqa: BLE001
        failures += 1
        fail("raptor valkey counters", type(exc).__name__)

    try:
        counts = qdrant_counts(args.compose_file)
        expected_summaries = max(0, len(indexed) - len(single))
        if counts["summaries"] < expected_summaries:
            failures += 1
            fail("qdrant summaries", f"{counts['summaries']} < expected {expected_summaries}")
        else:
            ok(
                "qdrant points",
                f"chunks={counts['chunks']} summaries={counts['summaries']} total={counts['total']}",
            )
    except Exception as exc:  # noqa: BLE001
        failures += 1
        fail("qdrant counts", type(exc).__name__)

    try:
        rr = reranker_batch_check(args.compose_file)
        scores = rr.get("scores") or []
        if len(scores) != 2 or rr.get("results") != 2:
            failures += 1
            fail("reranker batch", json.dumps(rr))
        elif float(scores[0]) <= float(scores[1]):
            failures += 1
            fail("reranker relevance", f"relevant_score={scores[0]} <= distractor_score={scores[1]}")
        else:
            ok("reranker batch", f"2 documents scored; relevant {scores[0]:.3f} > distractor {scores[1]:.3f}")
    except Exception as exc:  # noqa: BLE001
        failures += 1
        fail("reranker batch", type(exc).__name__)

    try:
        search = summary_search(args.compose_file, args.query)
        hits = search["hits"]
        if not hits:
            failures += 1
            fail("summary search", "no raptor_summary hits")
        else:
            top = hits[0]
            ok("summary search", f"top_score={top.get('score'):.4f}; file={top.get('file')}")
    except Exception as exc:  # noqa: BLE001
        failures += 1
        fail("summary search", type(exc).__name__)

    if args.min_clusters > 0:
        try:
            clusters = fetch_list(args.api_base, token, "/clusters")
            if len(clusters) < args.min_clusters:
                failures += 1
                fail("clusters", f"{len(clusters)} < {args.min_clusters}")
            else:
                sample = next((str(c.get("label") or "").strip() for c in clusters if isinstance(c, dict)), "")
                ok("clusters", f"{len(clusters)} clusters; sample={sample[:80]}")
        except Exception as exc:  # noqa: BLE001
            failures += 1
            fail("clusters", type(exc).__name__)

    if args.min_hypotheses > 0:
        try:
            hypotheses = fetch_hypotheses(args.api_base, token, min_count=args.min_hypotheses)
            if len(hypotheses) < args.min_hypotheses:
                failures += 1
                fail("hypotheses", f"{len(hypotheses)} < {args.min_hypotheses}")
            else:
                title = check_nonempty_hypothesis_shape(hypotheses, "hypotheses")
                ok("hypotheses", f"{len(hypotheses)} returned; sample={title[:80]}")
        except Exception as exc:  # noqa: BLE001
            failures += 1
            fail("hypotheses", type(exc).__name__)

    if args.min_auto_cluster_hypotheses > 0:
        try:
            auto_cluster = fetch_hypotheses(
                args.api_base,
                token,
                min_count=args.min_auto_cluster_hypotheses,
                tag="auto_cluster",
            )
            if len(auto_cluster) < args.min_auto_cluster_hypotheses:
                failures += 1
                fail("auto-cluster hypotheses", f"{len(auto_cluster)} < {args.min_auto_cluster_hypotheses}")
            else:
                title = check_nonempty_hypothesis_shape(auto_cluster, "auto-cluster hypotheses")
                ok("auto-cluster hypotheses", f"{len(auto_cluster)} returned; sample={title[:80]}")
        except Exception as exc:  # noqa: BLE001
            failures += 1
            fail("auto-cluster hypotheses", type(exc).__name__)

    bridge_hypotheses: list[dict] = []
    if args.min_auto_bridge_hypotheses > 0 or args.require_bridge_evidence:
        try:
            bridge_hypotheses = fetch_hypotheses(
                args.api_base,
                token,
                min_count=max(5, args.min_auto_bridge_hypotheses),
                tag="auto_bridge",
            )
            if len(bridge_hypotheses) < args.min_auto_bridge_hypotheses:
                failures += 1
                fail("auto-bridge hypotheses", f"{len(bridge_hypotheses)} < {args.min_auto_bridge_hypotheses}")
            else:
                title = check_nonempty_hypothesis_shape(bridge_hypotheses, "auto-bridge hypotheses")
                ok("auto-bridge hypotheses", f"{len(bridge_hypotheses)} returned; sample={title[:80]}")
        except Exception as exc:  # noqa: BLE001
            failures += 1
            fail("auto-bridge hypotheses", type(exc).__name__)

    if args.require_bridge_evidence:
        try:
            evidence = bridge_evidence_check(args.api_base, token, bridge_hypotheses)
            ok("bridge evidence", f"{evidence['evidence']} evidence rows; hypothesis={evidence['hypothesis_id']}")
        except Exception as exc:  # noqa: BLE001
            failures += 1
            fail("bridge evidence", type(exc).__name__)

    if args.min_visible_hypotheses > 0 or args.check_visible_hypothesis_quality:
        try:
            visible = visible_hypotheses(args.api_base, token)
            if len(visible) < args.min_visible_hypotheses:
                failures += 1
                fail("visible hypotheses", f"{len(visible)} < {args.min_visible_hypotheses}")
            elif args.check_visible_hypothesis_quality:
                clusters_by_id = {str(c.get("id")): c for c in fetch_list(args.api_base, token, "/clusters")}
                sample = check_visible_hypothesis_quality(
                    args.api_base, token, visible, args.visible_quality_top, clusters_by_id
                )
                ok("visible hypotheses", f"{len(visible)} visible; top quality checked; sample={sample[:80]}")
            else:
                ok("visible hypotheses", f"{len(visible)} visible")
        except Exception as exc:  # noqa: BLE001
            failures += 1
            fail("visible hypotheses", type(exc).__name__)

    if args.full_rag:
        try:
            rag = run_full_rag(args.api_base, token, args.query, args.full_rag_timeout)
            if rag["answer_len"] < args.min_full_rag_answer_chars or rag["sources"] < args.min_full_rag_sources:
                failures += 1
                fail("full rag", json.dumps(rag, ensure_ascii=False))
            elif rag.get("extractive_fallback") and not args.allow_extractive_fallback:
                failures += 1
                fail("full rag", "used extractive fallback; generation backend is degraded")
            elif args.full_rag_max_seconds > 0 and rag["seconds"] > args.full_rag_max_seconds:
                failures += 1
                fail("full rag latency", f"{rag['seconds']:.1f}s > {args.full_rag_max_seconds:.1f}s")
            else:
                ok("full rag", f"{rag['seconds']:.1f}s; sources={rag['sources']}; chat={rag['chat_id']}")
        except urllib.error.HTTPError as exc:
            failures += 1
            fail("full rag", f"HTTP {exc.code}")
        except Exception as exc:  # noqa: BLE001
            failures += 1
            fail("full rag", type(exc).__name__)

    if failures:
        print(f"demo smoke failed: {failures} check(s)", file=sys.stderr)
        return 1
    print("demo smoke ok")
    return 0


if __name__ == "__main__":
    raise SystemExit(main())
