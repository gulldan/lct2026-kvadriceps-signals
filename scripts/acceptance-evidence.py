#!/usr/bin/env python3
"""Снимок условий ручной приёмки без запуска контейнеров, поиска и генерации.

python3 scripts/acceptance-evidence.py > conditions.json

STAND_URL задаёт адрес проверяемого стенда; без него сеть не опрашивается.

Читает версии, доступные ресурсы и три общедоступных HTTP-маршрута. Не выводит
секреты, не принимает пароль и не заявляет проверку качества по ответу healthz.
"""

import argparse
import datetime
import hashlib
import json
import os
import platform
import re
import shutil
import subprocess
import sys
import time
import urllib.error
import urllib.request
from pathlib import Path

from tools.stand_env import load_stand_env, redact, stand_origin


ROOT = Path(__file__).resolve().parents[1]


def command(*arguments):
    try:
        result = subprocess.run(
            arguments, cwd=ROOT, capture_output=True, text=True, timeout=10,
            env={**os.environ, "GOTOOLCHAIN": "local"}, check=False,
        )
    except (OSError, subprocess.TimeoutExpired):
        return None
    return result.stdout.strip() if result.returncode == 0 else None


def memory_bytes():
    if platform.system() == "Darwin":
        raw = command("sysctl", "-n", "hw.memsize")
        return int(raw) if raw and raw.isdigit() else None
    path = Path("/proc/meminfo")
    if path.exists():
        match = re.search(r"^MemTotal:\s+(\d+)\s+kB", path.read_text(), re.MULTILINE)
        return int(match.group(1)) * 1024 if match else None
    return None


def probe(base, path, expected):
    started = time.monotonic()
    row = {"path": path, "expected_status": expected}
    try:
        request = urllib.request.Request(base + path, headers={"Accept": "*/*"}, method="GET")
        with urllib.request.urlopen(request, timeout=15) as response:
            body = response.read(1024 * 1024)
            row.update(status=response.status, content_type=response.headers.get_content_type())
            if path == "/":
                row["index_sha256"] = hashlib.sha256(body).hexdigest()
                row["assets"] = sorted(set(re.findall(rb'(?:src|href)="(/assets/[^"?#]+)', body)))
                row["assets"] = [asset.decode("utf-8", errors="replace") for asset in row["assets"]]
            if path == "/healthz":
                row["health_body_ok"] = body.strip() == b"ok"
    except urllib.error.HTTPError as error:
        row["status"] = error.code
        error.close()
    except (OSError, urllib.error.URLError):
        row["status"] = None
        row["error"] = "connection_or_tls_failure"
    row["seconds"] = round(time.monotonic() - started, 3)
    row["passed"] = row["status"] == expected and row.get("health_body_ok", True)
    return row


def main():
    parser = argparse.ArgumentParser(description=__doc__, formatter_class=argparse.RawDescriptionHelpFormatter)
    try:
        env = load_stand_env()
    except ValueError as failure:
        parser.error(str(failure))
    parser.add_argument("--base", default=env.get("STAND_URL"),
                        help="совместимость: только значение STAND_URL; без переменной сеть не опрашивается")
    args = parser.parse_args()
    if args.base != env.get("STAND_URL"):
        parser.error("адрес задаётся только через STAND_URL")
    try:
        base = stand_origin(env, required=False)
    except ValueError as failure:
        parser.error(str(failure))
    status = command("git", "status", "--porcelain=v1", "--untracked-files=normal")
    report = {
        "schema": "manual-acceptance-conditions-v1",
        "observed_at_utc": datetime.datetime.now(datetime.timezone.utc).isoformat(),
        "coverage": "conditions_and_unauthenticated_reachability_only",
        "repository": {
            "commit": command("git", "rev-parse", "HEAD"),
            "branch": command("git", "branch", "--show-current"),
            "working_tree_clean": status == "" if status is not None else None,
            "changed_entries": len(status.splitlines()) if status is not None else None,
        },
        "machine": {
            "os": platform.system(), "os_release": platform.release(), "arch": platform.machine(),
            "logical_cpus": os.cpu_count(), "memory_bytes": memory_bytes(),
            "checkout_volume_free_bytes": shutil.disk_usage(ROOT).free,
        },
        "tools": {
            "python": platform.python_version(),
            "git": command("git", "--version"), "bun": command("bun", "--version"),
            "go_local": command("go", "version"), "docker_client": command("docker", "--version"),
            "compose_client": command("docker", "compose", "version"),
        },
        "checks": [],
        "not_verified": [
            "fresh_clone_install", "backend_revision_matches_checkout", "model_route_and_approval",
            "authenticated_workflow", "search_quality", "training_metrics", "fresh_server_start",
        ],
    }
    if args.base:
        report["base"] = "[configured via STAND_URL]"
        report["checks"] = [
            probe(base, "/healthz", 200),
            probe(base, "/", 200),
            probe(base, "/api/v1/trends/live", 401),
        ]
    print(json.dumps(redact(report, env), ensure_ascii=False, indent=2))
    return 1 if any(not check["passed"] for check in report["checks"]) else 0


if __name__ == "__main__":
    sys.exit(main())
