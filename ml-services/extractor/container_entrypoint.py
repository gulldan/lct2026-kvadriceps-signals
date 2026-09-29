"""Подготовка закреплённого бандла моделей до запуска процесса контейнера."""

from __future__ import annotations

import fcntl
import os
import re
import sys
from pathlib import Path

from extractor.model_bundle import materialize_archive

DEFAULT_CACHE = "/var/lib/extractor/models"


def prepare_models(env):
    mode = env.get("EXTRACTOR_MODEL_SOURCE", "archive").strip()
    url = env.get("EXTRACTOR_MODEL_BUNDLE_URL", "").strip()
    checksum = env.get("EXTRACTOR_MODEL_BUNDLE_SHA256", "").strip()
    if mode == "local":
        if url or checksum:
            raise ValueError("Local model mode cannot also configure an archive URL/SHA")
        registry = env.get("EXTRACTOR_MODELS_YAML", "").strip()
        if not registry or not Path(registry).is_file():
            raise ValueError("Local model mode requires an explicit existing EXTRACTOR_MODELS_YAML")
        return
    if mode != "archive":
        raise ValueError("EXTRACTOR_MODEL_SOURCE must be archive or local")
    if not url or not re.fullmatch(r"[0-9a-f]{64}", checksum):
        raise ValueError("Set both EXTRACTOR_MODEL_BUNDLE_URL and a full lowercase SHA256")
    if not env.get("EXTRACTOR_EMBEDDING_URL", "").strip():
        raise ValueError("Set EXTRACTOR_EMBEDDING_URL to the reachable F2 embedding service")
    cache = Path(env.get("EXTRACTOR_MODEL_CACHE_DIR") or DEFAULT_CACHE).resolve()
    registry = (
        Path(env.get("EXTRACTOR_MODELS_YAML") or cache / "registries" / f"{checksum}.yaml").expanduser().absolute()
    )
    # Последний компонент сохраняется, чтобы проверка видела символическую ссылку.
    registry = registry.parent.resolve() / registry.name
    if registry.is_symlink():
        raise ValueError("Model registry output must not be a symlink")
    cache.mkdir(parents=True, exist_ok=True)
    # Несколько процессов могут делить том: блокировка сериализует подготовку весов.
    with (cache / ".bootstrap.lock").open("a") as lock:
        fcntl.flock(lock, fcntl.LOCK_EX)
        if registry.is_symlink():
            raise ValueError("Model registry output must not be a symlink")
        materialize_archive(
            url=url,
            sha256=checksum,
            cache_dir=cache,
            registry_output=registry,
            embedding_url=env["EXTRACTOR_EMBEDDING_URL"],
        )
        if not registry.is_file() or registry.stat().st_size == 0:
            raise ValueError("Model materialization did not produce a registry")
    env["EXTRACTOR_MODELS_YAML"] = str(registry)


def main(argv=None):
    command = list(sys.argv[1:] if argv is None else argv)
    try:
        if not command:
            raise ValueError("A service command is required")
        prepare_models(os.environ)
    except (OSError, ValueError) as exc:
        print(f"Model bootstrap failed; service was not started: {exc}", file=sys.stderr)
        return 1
    os.execvp(command[0], command)


if __name__ == "__main__":
    raise SystemExit(main())
