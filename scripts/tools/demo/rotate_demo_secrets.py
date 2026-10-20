#!/usr/bin/env python3
"""Смена секретов локального демо без печати значений.

Compose заводит первого админа только на пустой базе, поэтому на живой базе обновляется
и bcrypt-хэш админа в Postgres: смоук и воркеры переходят на новый пароль без сброса
томов и переиндексации.
"""

from __future__ import annotations

import argparse
import os
import secrets
import shlex
import subprocess
import sys
import tempfile
import textwrap
import uuid
from pathlib import Path


ROOT = Path(__file__).resolve().parents[3]
DEFAULT_ENV_FILE = ROOT / "infra/.env"
DEFAULT_COMPOSE_FILE = ROOT / "infra/docker-compose.yml"
DB_SERVICE_DIR = ROOT / "backend/db-service"


def load_env(path: Path) -> dict[str, str]:
    env: dict[str, str] = {}
    if not path.exists():
        return env
    for line in path.read_text(errors="replace").splitlines():
        stripped = line.strip()
        if not stripped or stripped.startswith("#") or "=" not in stripped:
            continue
        key, value = stripped.split("=", 1)
        env[key.strip()] = value.strip().strip('"').strip("'")
    return env


def upsert_env(path: Path, updates: dict[str, str]) -> None:
    lines = path.read_text(errors="replace").splitlines() if path.exists() else []
    seen: set[str] = set()
    out: list[str] = []
    for line in lines:
        stripped = line.strip()
        if not stripped or stripped.startswith("#") or "=" not in stripped:
            out.append(line)
            continue
        key, _ = stripped.split("=", 1)
        key = key.strip()
        if key in updates:
            out.append(f"{key}={updates[key]}")
            seen.add(key)
        else:
            out.append(line)
    missing = [key for key in updates if key not in seen]
    if missing and out and out[-1].strip():
        out.append("")
    for key in missing:
        out.append(f"{key}={updates[key]}")
    path.write_text("\n".join(out) + "\n")


def run(
    cmd: list[str],
    *,
    cwd: Path = ROOT,
    input_text: str | None = None,
    env: dict[str, str] | None = None,
) -> str:
    proc = subprocess.run(
        cmd,
        cwd=cwd,
        input=input_text,
        text=True,
        stdout=subprocess.PIPE,
        stderr=subprocess.PIPE,
        env=env,
        check=False,
    )
    if proc.returncode != 0:
        raise RuntimeError(
            f"{' '.join(cmd)} failed with {proc.returncode}\nSTDOUT:\n{proc.stdout}\nSTDERR:\n{proc.stderr}"
        )
    return proc.stdout.strip()


def bcrypt_hash(password: str) -> str:
    source = textwrap.dedent(
        """
        package main

        import (
            "fmt"
            "io"
            "os"

            "golang.org/x/crypto/bcrypt"
        )

        func main() {
            raw, err := io.ReadAll(os.Stdin)
            if err != nil {
                panic(err)
            }
            hash, err := bcrypt.GenerateFromPassword(raw, bcrypt.DefaultCost)
            if err != nil {
                panic(err)
            }
            fmt.Print(string(hash))
        }
        """
    ).strip()
    with tempfile.NamedTemporaryFile("w", suffix=".go", delete=False) as tmp:
        tmp.write(source)
        tmp_path = tmp.name
    try:
        return run(["go", "run", tmp_path], cwd=DB_SERVICE_DIR, input_text=password)
    finally:
        try:
            os.unlink(tmp_path)
        except FileNotFoundError:
            pass


def sql_literal(value: str) -> str:
    return "'" + value.replace("'", "''") + "'"


def sync_admin_db(compose_file: Path, username: str, password: str) -> None:
    hash_value = bcrypt_hash(password)
    user_id = str(uuid.uuid4())
    sql = f"""
WITH upsert AS (
    UPDATE users
       SET password_hash = {sql_literal(hash_value)},
           roles = ARRAY['admin','user']
     WHERE username = {sql_literal(username)}
 RETURNING id
)
INSERT INTO users (id, username, password_hash, roles, created_at)
SELECT {sql_literal(user_id)}, {sql_literal(username)}, {sql_literal(hash_value)}, ARRAY['admin','user'], now()
WHERE NOT EXISTS (SELECT 1 FROM upsert);
"""
    run(
        [
            "docker",
            "compose",
            "-f",
            str(compose_file),
            "exec",
            "-T",
            "postgres",
            "psql",
            "-U",
            "rag",
            "-d",
            "rag",
            "-v",
            "ON_ERROR_STOP=1",
        ],
        input_text=sql,
    )


def sync_grafana(compose_file: Path, password: str) -> None:
    env = os.environ.copy()
    env["DEMO_GRAFANA_PASSWORD"] = password
    run(
        [
            "docker",
            "compose",
            "-f",
            str(compose_file),
            "exec",
            "-T",
            "-e",
            "DEMO_GRAFANA_PASSWORD",
            "grafana",
            "sh",
            "-lc",
            "/usr/share/grafana/bin/grafana-cli admin reset-admin-password \"$DEMO_GRAFANA_PASSWORD\" >/dev/null",
        ],
        env=env,
    )


def sync_rabbitmq(compose_file: Path, username: str, password: str) -> None:
    env = os.environ.copy()
    env["DEMO_RABBITMQ_PASSWORD"] = password
    run(
        [
            "docker",
            "compose",
            "-f",
            str(compose_file),
            "exec",
            "-T",
            "-e",
            "DEMO_RABBITMQ_PASSWORD",
            "rabbitmq",
            "sh",
            "-lc",
            "PATH=/opt/erlang/lib/erlang/bin:/opt/rabbitmq/sbin:$PATH "
            f"/opt/rabbitmq/sbin/rabbitmqctl change_password {shlex.quote(username)} \"$DEMO_RABBITMQ_PASSWORD\" >/dev/null",
        ],
        env=env,
    )


def main() -> int:
    parser = argparse.ArgumentParser()
    parser.add_argument("--env-file", default=str(DEFAULT_ENV_FILE))
    parser.add_argument("--compose-file", default=str(DEFAULT_COMPOSE_FILE))
    parser.add_argument("--admin-username", default="")
    parser.add_argument("--skip-db", action="store_true", help="Only update the env file, do not touch Postgres.")
    parser.add_argument("--skip-grafana", action="store_true", help="Do not reset the running Grafana admin password.")
    parser.add_argument("--skip-rabbitmq", action="store_true", help="Do not reset the running RabbitMQ password.")
    args = parser.parse_args()

    env_file = Path(args.env_file)
    compose_file = Path(args.compose_file)
    env = load_env(env_file)
    username = args.admin_username or env.get("ADMIN_USERNAME") or "admin"
    grafana_user = env.get("GRAFANA_ADMIN_USER") or "admin"
    rabbitmq_user = env.get("RABBITMQ_USER") or "rag"
    password = secrets.token_urlsafe(24)
    jwt_secret = secrets.token_urlsafe(48)
    grafana_password = secrets.token_urlsafe(24)
    rabbitmq_password = secrets.token_urlsafe(24)

    upsert_env(env_file, {
        "JWT_SECRET": jwt_secret,
        "ADMIN_USERNAME": username,
        "ADMIN_PASSWORD": password,
        "GRAFANA_ADMIN_USER": grafana_user,
        "GRAFANA_ADMIN_PASSWORD": grafana_password,
        "RABBITMQ_USER": rabbitmq_user,
        "RABBITMQ_PASSWORD": rabbitmq_password,
    })
    if not args.skip_db:
        sync_admin_db(compose_file, username, password)
    if not args.skip_grafana:
        sync_grafana(compose_file, grafana_password)
    if not args.skip_rabbitmq:
        sync_rabbitmq(compose_file, rabbitmq_user, rabbitmq_password)

    print(
        f"[ok] env rotated: ADMIN_USERNAME={username}; password_len={len(password)}; "
        f"jwt_len={len(jwt_secret)}; grafana_user={grafana_user}; grafana_password_len={len(grafana_password)}; "
        f"rabbitmq_user={rabbitmq_user}; rabbitmq_password_len={len(rabbitmq_password)}"
    )
    print("[ok] secret values were not printed")
    return 0


if __name__ == "__main__":
    try:
        raise SystemExit(main())
    except Exception as exc:  # noqa: BLE001
        print(f"[fail] rotate demo secrets: {exc}", file=sys.stderr)
        raise SystemExit(1)
