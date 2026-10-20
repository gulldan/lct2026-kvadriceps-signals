#!/usr/bin/env bash
# Проверка провайдера без передачи ключа аргументами процесса и вывода ответов с приватными данными.
set -euo pipefail
python3 - "$(dirname "$0")" "${1:-infra/.env}" <<'PY'
import json
import os
from pathlib import Path
import sys
import urllib.error
import urllib.parse
import urllib.request

sys.path.insert(0, sys.argv[1])
from tools.stand_env import load_env_file

try:
    env = {**load_env_file(Path(sys.argv[2])), **os.environ}
    base = env.get("VLLM_URL", "").rstrip("/")
    parsed = urllib.parse.urlsplit(base)
    if (parsed.scheme not in ("http", "https") or not parsed.hostname or
            parsed.username or parsed.password or parsed.query or parsed.fragment):
        raise ValueError()
except (OSError, ValueError):
    sys.exit("ERROR: configure VLLM_URL in the private environment")

headers = {"Content-Type": "application/json"}
if env.get("VLLM_API_KEY"):
    headers["Authorization"] = "Bearer " + env["VLLM_API_KEY"]


class NoRedirect(urllib.request.HTTPRedirectHandler):
    def redirect_request(self, req, fp, code, msg, response_headers, newurl):
        return None


def request(path, payload=None):
    body = json.dumps(payload).encode() if payload is not None else None
    req = urllib.request.Request(base + "/v1/" + path, data=body, headers=headers)
    with urllib.request.build_opener(NoRedirect).open(req, timeout=90 if body else 20) as response:
        return json.load(response)


print("== GET models")
try:
    models = request("models")
    print("models:", len(models.get("data", [])))
except (OSError, ValueError, TypeError, AttributeError):
    print("models endpoint unavailable (optional)")

print("== POST chat/completions")
try:
    result = request("chat/completions", {
        "model": env.get("VLLM_MODEL", ""),
        "messages": [{"role": "user", "content": "Ответь одним словом: работает?"}],
        "max_tokens": 256,
    })
    content = result.get("choices", [{}])[0].get("message", {}).get("content", "")
    if "error" in result or not isinstance(content, str) or not content.strip():
        raise ValueError()
except (OSError, ValueError, TypeError, AttributeError, IndexError):
    sys.exit("ERROR: provider check failed; response details are private")
print("OK: non-empty completion received")
PY
