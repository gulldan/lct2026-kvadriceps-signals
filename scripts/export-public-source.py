#!/usr/bin/env python3
"""Export the current working source tree without history or local runtime state.

Checks known local dotenv credentials, the configured stand host and private
paths, including Office XML. This is not an arbitrary-secret detector or media
OCR: presentation/video review remains a separate publication check.
"""

import argparse
import hashlib
import html
import io
import ipaddress
import json
import os
from pathlib import Path, PurePosixPath
import re
import stat
import subprocess
import tempfile
import urllib.parse
import xml.etree.ElementTree as ET
import zipfile

from tools.stand_env import ADDRESS_ENV_NAMES, PATH_ENV_NAMES, load_env_file, load_stand_env


MANIFEST = "PUBLIC_SOURCE_MANIFEST.json"
PRESENTATIONS = PurePosixPath("docs/presentation")
PRIVATE_ROOTS = {".data", ".codex", ".agents", "datasets", "dataset", "models", "reports", "artifacts", "output", "out", "tmp", "cache", "bin"}
PRIVATE_PARTS = {".git", ".cache", "node_modules", ".venv", "venv", "__pycache__", ".pytest_cache", ".mypy_cache",
                 ".ruff_cache", ".eggs", ".idea", ".vscode", ".playwright-cli", "dist", "target", "playwright-report", "test-results"}
PUBLIC_HOSTS = {
    "localhost", "host.docker.internal", "gateway.docker.internal", "host.containers.internal",
    "api.openai.com", "api.anthropic.com", "api.voyageai.com", "huggingface.co", "openrouter.ai",
    "api.deepseek.com", "api.openalex.org", "api.crossref.org", "api.github.com", "export.arxiv.org",
    "llm.api.cloud.yandex.net", "llm.api.cloud.yandex.ru",
    "page-renderer", "llama-server", "llm-service", "f2llm-service", "embeddings", "reranker-service", "reranker",
    "ocr-service", "paddleocr", "localstack", "seaweedfs", "rabbitmq", "postgres", "valkey", "qdrant", "opensearch",
    "main-service", "radar-analysis", "radar-collector", "frontend",
}
CREDENTIAL_NAME = re.compile(r"(?:^|_)(?:PASSWORD|PASSWD|PASS|TOKEN|SECRET|API_KEY|APIKEY|KEY)$")
OFFICE_SUFFIXES = {".pptx", ".docx", ".xlsx", ".pptm", ".docm", ".xlsm"}


class ExportError(Exception):
    pass


def dotenv(path: PurePosixPath) -> bool:
    name = path.name
    return (name == ".env" or name.startswith(".env.") or name.endswith(".env")) and not name.endswith((".example", ".sample", ".template"))


def excluded(path: PurePosixPath) -> bool:
    return (not path.parts or path.parts[0] in PRIVATE_ROOTS or any(part in PRIVATE_PARTS for part in path.parts) or
            (path.parts[0] == ".claude" and (len(path.parts) < 2 or path.parts[1] != "skills")) or
            dotenv(path) or path.name == ".DS_Store" or path.suffix in {".pyc", ".pyo", ".swp", ".tmp", ".bak"})


def git_files(root: Path, *options: str) -> set[str]:
    result = subprocess.run(["git", "ls-files", "-z", *options], cwd=root, capture_output=True, check=False)
    if result.returncode:
        raise ExportError("Git could not enumerate the current source tree")
    return {os.fsdecode(name) for name in result.stdout.split(b"\0") if name}


def known_private_values(root: Path) -> tuple[bytes, ...]:
    values = set()
    env = {}
    for relative in (".env", "infra/.env"):
        path = root / relative
        if not path.exists():
            continue
        if not path.resolve().is_relative_to(root):
            raise ExportError(relative + ": private environment symlink escapes the repository")
        local_env = load_env_file(path)
        values.update(value for key, value in local_env.items() if CREDENTIAL_NAME.search(key.upper()) and len(value) >= 8)
        env.update(local_env)
    try:
        env.update(load_stand_env())
    except ValueError as error:
        raise ExportError(str(error)) from None
    values.update(value for key, value in env.items() if CREDENTIAL_NAME.search(key.upper()) and len(value) >= 8)
    values.update(value for key, value in env.items() if len(value) >= 8 and
                  (key in {"PUBSEARCH_MAILTO", "OPENALEX_MAILTO", "STAND_CONTROL_ANALYSIS_ID"} or key.startswith("STAND_PREVIEW_")))
    for key in ADDRESS_ENV_NAMES:
        value = env.get(key, "")
        if not value:
            continue
        try:
            endpoint = urllib.parse.urlsplit(value if "://" in value else "//" + value)
            host = endpoint.hostname
        except ValueError:
            raise ExportError("invalid deployment address in " + key) from None
        if not host:
            continue
        if endpoint.username or endpoint.password or endpoint.query or endpoint.fragment:
            values.add(value)
        if host not in PUBLIC_HOSTS and not host.endswith(".localhost"):
            try:
                local = ipaddress.ip_address(host).is_loopback
            except ValueError:
                local = False
            if not local:
                values.update((host, value))
    values.update(value for key, value in env.items() if
                  (key in PATH_ENV_NAMES or (key.startswith("STAND_") and key.endswith(("_DIR", "_FILE", "_PATH")))) and
                  Path(value).is_absolute())
    home = str(Path.home())
    if home not in ("/", "/root", "/home/user", "/Users/user"):
        values.add(home)
    values = {value for value in values if value and not re.fullmatch(r"\$\{[^}]+\}", value)}
    forms = set(values)
    for value in values:
        forms.update((html.escape(value, quote=True), urllib.parse.quote(value, safe=""),
                      json.dumps(value, ensure_ascii=False)[1:-1], json.dumps(value)[1:-1]))
    return tuple(sorted({value.encode(encoding) for value in forms for encoding in ("utf-8", "utf-16-le", "utf-16-be")}, key=len, reverse=True))


def inspect_content(path: PurePosixPath, data: bytes, private: tuple[bytes, ...]) -> None:
    if any(value in data for value in private):
        raise ExportError(str(path) + ": contains a configured private value")
    if path.suffix.lower() not in OFFICE_SUFFIXES:
        return
    try:
        with zipfile.ZipFile(io.BytesIO(data)) as package:
            for member in package.infolist():
                if member.filename.lower().endswith((".xml", ".rels")):
                    if member.file_size > 64 * 1024 * 1024:
                        raise ExportError(str(path) + ": Office XML exceeds the inspection limit")
                    xml = package.read(member)
                    tree = ET.fromstring(xml)
                    decoded = "".join([*tree.itertext(), *(value for node in tree.iter() for value in node.attrib.values())]).encode()
                    if any(value in xml or value in decoded for value in private):
                        raise ExportError(str(path) + ": Office XML contains a configured private value")
    except (zipfile.BadZipFile, RuntimeError, OSError, ET.ParseError):
        raise ExportError(str(path) + ": Office package could not be inspected") from None


def zip_entry(archive, path: str, data: bytes, executable: bool = False) -> None:
    entry = zipfile.ZipInfo(path, date_time=(1980, 1, 1, 0, 0, 0))
    entry.create_system = 3
    entry.external_attr = (stat.S_IFREG | (0o755 if executable else 0o644)) << 16
    entry.compress_type = zipfile.ZIP_DEFLATED
    archive.writestr(entry, data, compresslevel=6)


def export(root: Path, destination: Path) -> int:
    root, destination = root.resolve(), destination.absolute()
    if destination.exists() or destination.is_symlink():
        raise ExportError("destination already exists; choose a new output path")
    tracked = git_files(root, "--cached")
    candidates = git_files(root, "--cached", "--others", "--exclude-standard")
    candidates.update(str(PurePosixPath(path).with_suffix(".pdf")) for path in tracked
                      if PurePosixPath(path).parent == PRESENTATIONS and path.endswith(".pptx") and
                      (root / path).is_file() and (root / PurePosixPath(path).with_suffix(".pdf")).is_file())
    private = known_private_values(root)
    rows, problems = [], []
    destination.parent.mkdir(parents=True, exist_ok=True)
    temporary = None
    try:
        with tempfile.NamedTemporaryFile(prefix=".public-source-", suffix=".zip", dir=destination.parent, delete=False) as handle:
            temporary = Path(handle.name)
        with zipfile.ZipFile(temporary, "w") as archive:
            for relative in sorted(candidates):
                path = PurePosixPath(relative)
                if path.is_absolute() or ".." in path.parts:
                    raise ExportError("Git returned a path outside the source tree")
                source = root / path
                try:
                    source.lstat()
                except FileNotFoundError:
                    continue
                if relative in tracked and dotenv(path):
                    problems.append(relative + ": tracked private environment file")
                    continue
                if excluded(path) or source.absolute() == destination:
                    continue
                if any(value in relative.encode("utf-8") for value in private):
                    problems.append("a source filename contains a configured private value")
                    continue
                if relative == MANIFEST:
                    problems.append(relative + ": reserved export manifest path")
                    continue
                try:
                    resolved = source.resolve(strict=True)
                    if not resolved.is_relative_to(root) or excluded(PurePosixPath(resolved.relative_to(root).as_posix())):
                        raise ExportError(relative + ": symlink escapes the source tree or targets private state")
                    if not resolved.is_file():
                        raise ExportError(relative + ": source is not a regular file")
                    data = source.read_bytes()
                    inspect_content(path, data, private)
                    zip_entry(archive, relative, data, bool(source.stat().st_mode & 0o111))
                    rows.append({"path": relative, "sha256": hashlib.sha256(data).hexdigest(), "size": len(data)})
                except ExportError as problem:
                    problems.append(str(problem))
                except (OSError, RuntimeError):
                    problems.append(relative + ": source could not be read")
            if problems:
                raise ExportError("Export blocked:\n" + "\n".join(problems))
            zip_entry(archive, MANIFEST, (json.dumps(rows, ensure_ascii=False, indent=2, sort_keys=True) + "\n").encode())
        # Готовый архив становится видимым целиком; существующий файл не заменяется.
        os.link(temporary, destination)
        return len(rows)
    finally:
        if temporary is not None:
            temporary.unlink(missing_ok=True)


def main() -> int:
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("--output", type=Path, default=Path("output/public-source/rag-test-public-source.zip"))
    args = parser.parse_args()
    root = Path(__file__).resolve().parents[1]
    destination = root / args.output
    try:
        count = export(root, destination)
    except ExportError as problem:
        parser.exit(1, str(problem) + "\n")
    except (OSError, UnicodeError):
        parser.exit(1, "Export could not read source files or write the new destination\n")
    label = destination.relative_to(root) if destination.is_relative_to(root) else Path(destination.name)
    print(f"Exported {count} current source files to {label}; no Git history included.")
    return 0


if __name__ == "__main__":
    raise SystemExit(main())
