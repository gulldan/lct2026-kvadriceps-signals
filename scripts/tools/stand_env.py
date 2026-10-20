"""Локальные настройки оператора и обезличивание сохраняемых доказательств."""

import ipaddress
import json
import os
from pathlib import Path
import re
import urllib.parse


ADDRESS_ENV_NAMES = {
    "VLLM_URL", "RAG_API", "RAG_API_BASE", "DEMO_FRONTEND_URL", "RABBITMQ_API",
    "S3_ENDPOINT", "S3_PUBLIC_ENDPOINT", "EMBEDDINGS_URL", "RERANKER_URL", "OCR_ENGINE_URL",
    "RENDER_URL", "OPENALEX_BASE_URL", "PUBSEARCH_BASE_URL", "STAND_URL", "STAND_HEALTH_URL", "STAND_LAN_IP",
}
PATH_ENV_NAMES = {
    "RAG_REPO_DIR", "RAG_CA_BUNDLE", "DEMO_ENV_FILE", "PRESENTATION_RUNTIME_DIR", "PRESENTATION_SKILL_DIR",
    "PRESENTATION_WORK_DIR", "PRESENTATION_FONT_DIR", "STAND_TRUST_BUNDLE_PATH",
    "FRONTEND_TLS_CERT", "FRONTEND_TLS_KEY",
}
ENV_NAMES = {
    "HOME", "RAG_USER", "RAG_PASS", "RAG_API_BASE", "RAG_API", "RAG_DATASET_USER", "RAG_DATASET_PASS",
    "ADMIN_USERNAME", "ADMIN_PASSWORD", "JWT_SECRET", "GRAFANA_ADMIN_USER", "GRAFANA_ADMIN_PASSWORD",
    "RABBITMQ_USER", "RABBITMQ_PASSWORD", "VLLM_URL", "VLLM_API_KEY", "DEMO_FRONTEND_URL", "DEMO_ENV_FILE",
} | ADDRESS_ENV_NAMES | PATH_ENV_NAMES
SECRET_FIELDS = {"password", "passwd", "access_token", "refresh_token", "token", "api_key", "secret", "authorization", "cookie"}
IPV4 = re.compile(r"(?<![\w.])(?:\d{1,3}\.){3}\d{1,3}(?![\w.])")


def load_env_file(path: Path) -> dict[str, str]:
    env = {}
    if not path.exists():
        return env
    for line in path.read_text(errors="replace").splitlines():
        stripped = line.strip()
        if not stripped or stripped.startswith("#") or "=" not in stripped:
            continue
        key, value = stripped.split("=", 1)
        env[key.strip()] = value.strip().strip('"').strip("'")
    return env


def load_stand_env() -> dict[str, str]:
    path = os.environ.get("STAND_ENV_FILE")
    try:
        if path and not Path(path).is_file():
            raise ValueError("STAND_ENV_FILE must refer to an existing private dotenv file")
        return {**(load_env_file(Path(path)) if path else {}), **os.environ}
    except OSError:
        raise ValueError("STAND_ENV_FILE could not be read") from None


def stand_origin(env: dict[str, str], *, required: bool = True) -> str:
    base = env.get("STAND_URL", "")
    if not base and not required:
        return ""
    try:
        parsed = urllib.parse.urlsplit(base)
    except ValueError:
        raise ValueError("STAND_URL contains an invalid address") from None
    if (not base or base != base.strip() or parsed.scheme not in ("http", "https") or not parsed.hostname or
            parsed.username or parsed.password or parsed.query or parsed.fragment or parsed.path not in ("", "/")):
        raise ValueError("STAND_URL must be an HTTP origin without credentials or extra URL components")
    return base.rstrip("/")


def redact(value, env: dict[str, str], extra_sensitive=()):
    replacements = {v for k, v in env.items() if v and (k.startswith("STAND_") or k in ENV_NAMES)}
    replacements.update(v for v in extra_sensitive if v)
    for item in tuple(replacements):
        if item.startswith(("http://", "https://")):
            try:
                host = urllib.parse.urlsplit(item).hostname
            except ValueError:
                continue
            if host:
                replacements.add(host)
    replacements.update(json.dumps(item, ensure_ascii=False)[1:-1] for item in tuple(replacements))
    pattern = re.compile("|".join(re.escape(item) for item in sorted(replacements, key=len, reverse=True))) if replacements else None

    def private_ip(match):
        try:
            return "[private-address]" if ipaddress.ip_address(match[0]).is_private else match[0]
        except ValueError:
            return match[0]

    def clean(item):
        if isinstance(item, dict):
            return {clean(key): "[redacted]" if any(str(key).lower() == name or str(key).lower().endswith("_" + name)
                                           for name in SECRET_FIELDS) else clean(child)
                    for key, child in item.items()}
        if isinstance(item, (list, tuple)):
            return [clean(child) for child in item]
        if isinstance(item, str):
            if pattern:
                item = pattern.sub("[redacted]", item)
            return IPV4.sub(private_ip, item)
        return item

    return clean(value)


def normalize_api_base(value: str) -> str:
    try:
        parsed = urllib.parse.urlsplit(value.strip().rstrip("/"))
    except ValueError:
        raise ValueError("API URL contains an invalid address") from None
    if (parsed.scheme not in ("http", "https") or not parsed.hostname or parsed.username or parsed.password or
            parsed.query or parsed.fragment):
        raise ValueError("API URL must use HTTP(S) without credentials, query or fragment")
    path = parsed.path.rstrip("/")
    if path in ("", "/api"):
        path = "/api/v1"
    return urllib.parse.urlunsplit(parsed._replace(path=path))


def configure_dataset_args(parser, args, env):
    if args.password is not None:
        parser.error("passwords are accepted only through RAG_DATASET_PASS")
    for name, key, default in (("api_base", "RAG_API", "http://localhost:8080/api/v1"),
                               ("username", "RAG_DATASET_USER", "")):
        value = env.get(key) or default
        if getattr(args, name) is not None and getattr(args, name) != value:
            parser.error("dataset connection settings are configured only through the environment")
        setattr(args, name, value)
    args.password = env.get("RAG_DATASET_PASS", "")
    return args
