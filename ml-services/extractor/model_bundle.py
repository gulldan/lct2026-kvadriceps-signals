"""Доставка готового бандла: SHA-256, безопасная распаковка и атомарная установка."""

from __future__ import annotations

import fcntl
import hashlib
import json
import os
import re
import shutil
import stat
import tarfile
import tempfile
from contextlib import contextmanager
from pathlib import Path, PurePosixPath
from urllib.parse import parse_qs, urlsplit
from urllib.request import urlopen

import yaml

from extractor.registry import Backend, ModelsConfig


class ReleaseError(ValueError):
    """Неполный, неоднозначный или изменённый релиз не должен дойти до рантайма."""


def digest(path: Path) -> str:
    with path.open("rb") as handle:
        return hashlib.file_digest(handle, "sha256").hexdigest()


def relative_path(value: str) -> str:
    if not isinstance(value, str) or not value or "\\" in value:
        raise ReleaseError("artifact path must be a nonempty POSIX relative path")
    path = PurePosixPath(value)
    if path.is_absolute() or ".." in path.parts or path.as_posix() != value or value == ".":
        raise ReleaseError(f"unsafe artifact path: {value!r}")
    if any(ord(char) < 32 for char in value):
        raise ReleaseError("control character in artifact path")
    return value


def hf_identity(repo_id: str, revision: str) -> None:
    if (
        not isinstance(repo_id, str)
        or not re.fullmatch(r"[A-Za-z0-9][A-Za-z0-9_.-]*/[A-Za-z0-9][A-Za-z0-9_.-]*", repo_id)
        or ".." in repo_id
        or "--" in repo_id
    ):
        raise ReleaseError("repo_id must be an explicit namespace/model name")
    if not isinstance(revision, str) or not re.fullmatch(r"[0-9a-f]{40}", revision):
        raise ReleaseError("revision must be the full lowercase 40-hex commit SHA")


def _object(value, required, optional=()):
    if (
        not isinstance(value, dict)
        or not set(required) <= value.keys()
        or (value.keys() - set(required) - set(optional))
    ):
        raise ReleaseError(f"expected fields: {', '.join(sorted(required))}")


def validate_release(raw: bytes) -> dict:
    def unique_keys(pairs):
        result = {}
        for key, value in pairs:
            if key in result:
                raise ReleaseError(f"duplicate JSON key: {key}")
            result[key] = value
        return result

    def invalid_constant(value):
        raise ReleaseError(f"non-finite JSON number: {value}")

    release = json.loads(raw, object_pairs_hook=unique_keys, parse_constant=invalid_constant)
    _object(
        release,
        {
            "schema_version",
            "service_version",
            "runtime",
            "thresholds",
            "models",
            "evaluations",
        },
        {"provenance", "sharing", "preprocessing", "external_embedding_dependency"},
    )
    for name in (
        "provenance",
        "sharing",
        "preprocessing",
        "external_embedding_dependency",
    ):
        if name in release and not isinstance(release[name], dict):
            raise ReleaseError(f"{name} must be an object")
    if type(release["schema_version"]) is not int or release["schema_version"] != 1:
        raise ReleaseError("unsupported release schema_version")
    runtime = release["runtime"]
    _object(runtime, {"code_revision", "torch", "transformers", "cuda_dtype"})
    if not isinstance(runtime["code_revision"], str) or not re.fullmatch(r"[0-9a-f]{40}", runtime["code_revision"]):
        raise ReleaseError("runtime.code_revision must be a full commit SHA")
    if runtime["cuda_dtype"] not in {"fp32", "bf16", "fp16"} or any(
        not isinstance(runtime[key], str) or not runtime[key].strip() for key in ("torch", "transformers")
    ):
        raise ReleaseError("invalid runtime versions or CUDA precision")
    if not isinstance(release["evaluations"], dict):
        raise ReleaseError("evaluations must be an object")
    models = release["models"]
    if not isinstance(models, dict) or not models:
        raise ReleaseError("release must declare models")
    specs, bundle_paths = {}, set()
    for name, model in models.items():
        if not re.fullmatch(r"[A-Za-z0-9][A-Za-z0-9_-]*", name) or not isinstance(model, dict):
            raise ReleaseError("invalid model name or specification")
        spec = dict(model)
        source = spec.pop("source", None)
        if spec.get("backend") == Backend.REMOTE_HTTP.value:
            if source is not None or "url" in spec or spec.get("path") or spec.get("sha256"):
                raise ReleaseError("remote model must not bundle files, source, path or URL")
            if not spec.get("expected_revision"):
                raise ReleaseError("remote model requires expected_revision")
        else:
            if not isinstance(source, dict):
                raise ReleaseError("local model requires a source")
            if source.get("kind") == "bundle":
                _object(source, {"kind", "subdir"})
                subdir = relative_path(source["subdir"])
                if PurePosixPath(subdir).parts[0] in {"release.json", "install.json"}:
                    raise ReleaseError("bundle directory collides with release metadata")
                if spec.get("path", subdir) != subdir:
                    raise ReleaseError("bundled model path must equal source.subdir")
            elif source.get("kind") == "huggingface":
                _object(source, {"kind", "repo_id", "revision"})
                hf_identity(source["repo_id"], source["revision"])
                if spec.get("path"):
                    raise ReleaseError("upstream model must not contain a host-local path")
            else:
                raise ReleaseError("unsupported model source kind")
            pins = spec.get("sha256")
            if not isinstance(pins, dict) or not pins:
                raise ReleaseError("every local model requires complete SHA pins")
            if (
                spec.get("backend") == Backend.TORCH.value
                and not {"config.json", "model.safetensors", "tokenizer.json"} <= pins.keys()
            ):
                raise ReleaseError("release v1 Torch models require config, safetensors and tokenizer pins")
            filenames = set()
            for filename, checksum in pins.items():
                relative_path(filename)
                if any(filename.startswith(old + "/") or old.startswith(filename + "/") for old in filenames):
                    raise ReleaseError("artifact file/directory paths overlap")
                filenames.add(filename)
                if not isinstance(checksum, str) or not re.fullmatch(r"[0-9a-f]{64}", checksum):
                    raise ReleaseError("every artifact requires a lowercase SHA-256")
                if source["kind"] == "bundle":
                    full = str(PurePosixPath(source["subdir"]) / filename)
                    if any(
                        full == old or full.startswith(old + "/") or old.startswith(full + "/") for old in bundle_paths
                    ):
                        raise ReleaseError("overlapping bundled artifact paths")
                    bundle_paths.add(full)
            if spec.get("backend") == Backend.E5_LR.value and set(pins) != {
                "stage_lr.json",
                "e5_lr_coefficients.npz",
            }:
                raise ReleaseError("E5+LR requires exactly its metadata and coefficients")
            spec["path"] = f"models/{name}"
        specs[name] = spec
    ModelsConfig.model_validate(
        {
            "service_version": release["service_version"],
            "root": ".",
            "thresholds": release["thresholds"],
            "models": specs,
        }
    )
    return release


def _copy_verified(source: Path, target: Path, expected: str) -> None:
    if not source.is_file() or digest(source) != expected:
        raise ReleaseError(f"artifact SHA mismatch: {target.name}")
    target.parent.mkdir(parents=True, exist_ok=True)
    try:
        os.link(source.resolve(), target)
    except OSError:
        shutil.copyfile(source, target)
    if target.is_symlink() or digest(target) != expected:
        raise ReleaseError(f"materialized artifact SHA mismatch: {target.name}")


def _verify_tree(root: Path, expected: dict[str, str]) -> None:
    if root.is_symlink() or not root.is_dir():
        raise ReleaseError("materialized release must be a regular directory")
    directories = {
        parent.as_posix() for name in expected for parent in PurePosixPath(name).parents if parent.as_posix() != "."
    }
    actual = set()
    for path in root.rglob("*"):
        if path.is_symlink():
            raise ReleaseError("symlinks are forbidden in the materialized release")
        if path.is_dir() and path.relative_to(root).as_posix() not in directories:
            raise ReleaseError("unregistered directory in materialized release")
        if path.is_file():
            name = path.relative_to(root).as_posix()
            actual.add(name)
            if name not in expected or digest(path) != expected[name]:
                raise ReleaseError(f"cached artifact missing from manifest or changed: {name}")
        elif not path.is_dir():
            raise ReleaseError("materialized artifacts must be regular files")
    if actual != set(expected):
        raise ReleaseError("materialized release has missing artifacts")


def _check_candidate_config(release: dict, root: Path, *, materialized=False) -> None:
    """Релиз формата v1 везёт обученный теггер кандидатов, а не устаревший запасной вариант."""
    for name, model in release["models"].items():
        source = model.get("source", {})
        if name != "tagger-v1" or model["backend"] != "torch":
            continue
        if "config.json" not in model["sha256"]:
            raise ReleaseError("bundled Torch candidate checkpoint requires config.json")
        if not materialized and source.get("kind") != "bundle":
            continue
        directory = root / (f"models/{name}" if materialized else source["subdir"])
        config = json.loads((directory / "config.json").read_bytes())
        if (
            not isinstance(config, dict)
            or config.get("candidate_classifier_name") != "candidate_classifier"
            or config.get("candidate_id2label") != {"0": "not_candidate", "1": "candidate"}
        ):
            raise ReleaseError("candidate config requires the learned Boolean candidate head")


def hf_download(*, repo_id, revision, filename, cache_dir):
    from huggingface_hub import hf_hub_download

    return Path(hf_hub_download(repo_id=repo_id, revision=revision, filename=filename, cache_dir=cache_dir))


def _embedding_url(value: str) -> None:
    url = urlsplit(value or "")
    if (
        url.scheme not in {"http", "https"}
        or not url.hostname
        or url.username
        or url.password
        or url.query
        or url.fragment
    ):
        raise ReleaseError("embedding URL must be HTTP(S), without credentials/query/fragment")


def _paths(cache_dir: Path, target: Path, registry_output: Path) -> Path:
    registry_output = registry_output.expanduser().absolute()
    if registry_output.is_symlink():
        raise ReleaseError("registry output must not be a symlink")
    registry_output = registry_output.parent.resolve() / registry_output.name
    if registry_output.is_relative_to(target):
        raise ReleaseError("registry output must be outside the immutable release")
    for parent in [target, *target.parents]:
        if parent == cache_dir:
            break
        if parent.is_symlink():
            raise ReleaseError("symlink in release cache path")
    return registry_output


@contextmanager
def _process_lock(path: Path, description: str):
    """Блокировкой владеет ядро: её снимает даже SIGKILL; inode остаётся для ожидающих."""
    fd = os.open(path, os.O_CREAT | os.O_RDWR | os.O_NOFOLLOW, 0o600)
    try:
        if not stat.S_ISREG(os.fstat(fd).st_mode):
            raise ReleaseError("release lock must be a regular file")
        try:
            fcntl.flock(fd, fcntl.LOCK_EX | fcntl.LOCK_NB)
        except BlockingIOError as exc:
            raise ReleaseError(f"{description} is already locked") from exc
        yield
    finally:
        # Ожидающий процесс может уже держать этот inode открытым.
        os.close(fd)


def _install_release(raw, identity, target, cache_dir, registry_output, embedding_url, populate):
    """Публикуется только полностью проверенное дерево; путь общий для HF и архива."""
    release = validate_release(raw)
    _embedding_url(embedding_url)
    registry_output = _paths(cache_dir, target, registry_output)
    specs, expected = {}, {}
    for name, model in release["models"].items():
        spec = dict(model)
        spec.pop("source", None)
        if spec["backend"] == Backend.REMOTE_HTTP.value:
            spec["url"] = embedding_url
        else:
            spec["path"] = f"models/{name}"
            expected.update({f"models/{name}/{f}": checksum for f, checksum in spec["sha256"].items()})
        specs[name] = spec
    receipt = json.dumps({**identity, "release_sha256": hashlib.sha256(raw).hexdigest()}, sort_keys=True).encode()
    expected["release.json"] = hashlib.sha256(raw).hexdigest()
    expected["install.json"] = hashlib.sha256(receipt).hexdigest()
    config = {
        "service_version": release["service_version"],
        "root": os.path.relpath(target, registry_output.parent),
        "thresholds": release["thresholds"],
        "models": specs,
    }
    ModelsConfig.model_validate(config)
    registry_bytes = yaml.safe_dump(config, sort_keys=True).encode()
    if registry_output.exists() and registry_output.read_bytes() != registry_bytes:
        raise ReleaseError("registry output already exists with different contents")
    target.parent.mkdir(parents=True, exist_ok=True)
    lock = target.parent / f".{target.name}.lock"
    with _process_lock(lock, "release installation"):
        if target.exists():
            _verify_tree(target, expected)
        else:
            with tempfile.TemporaryDirectory(prefix=".release-install-", dir=target.parent) as temp:
                stage = Path(temp) / "release"
                stage.mkdir()
                populate(stage, release)
                (stage / "release.json").write_bytes(raw)
                (stage / "install.json").write_bytes(receipt)
                _verify_tree(stage, expected)
                _check_candidate_config(release, stage, materialized=True)
                os.rename(stage, target)
        # Независимые бандлы имеют разные блокировки; опубликованный реестр
        # принимается только при полном совпадении содержимого.
        registry_output.parent.mkdir(parents=True, exist_ok=True)
        with tempfile.NamedTemporaryFile(dir=registry_output.parent, delete=False) as handle:
            temporary = Path(handle.name)
            handle.write(registry_bytes)
        try:
            try:
                os.link(temporary, registry_output)
            except FileExistsError:
                if (
                    registry_output.is_symlink()
                    or not registry_output.is_file()
                    or registry_output.read_bytes() != registry_bytes
                ):
                    raise ReleaseError("registry output changed during release installation") from None
        finally:
            temporary.unlink(missing_ok=True)
    return registry_output


MAX_ARCHIVE_BYTES = 8 * 1024**3
MAX_UNPACKED_BYTES = 8 * 1024**3
MAX_MANIFEST_BYTES = 2 * 1024**2
MAX_ARCHIVE_MEMBERS = 10000


def archive_url(value: str) -> tuple[str, bool]:
    """Нормализуются только публичные ссылки на файл Drive; общая папка — не файл."""
    if not isinstance(value, str) or any(ord(char) <= 32 for char in value):
        raise ReleaseError("bundle URL must be HTTP(S) without whitespace")
    url = urlsplit(value)
    if url.scheme not in {"http", "https"} or not url.hostname or url.username or url.password or url.fragment:
        raise ReleaseError("bundle URL must be HTTP(S) without credentials or fragment")
    if url.hostname in {"drive.google.com", "www.drive.google.com"}:
        if "/folders/" in url.path:
            raise ReleaseError("Google Drive folder URL is not a public file URL")
        match = re.fullmatch(r"/file/d/([A-Za-z0-9_-]+)(?:/(?:view|edit|preview))?/?", url.path)
        ids = parse_qs(url.query).get("id", []) if url.path in {"/uc", "/open"} else []
        file_id = match.group(1) if match else (ids[0] if len(ids) == 1 else "")
        if not re.fullmatch(r"[A-Za-z0-9_-]+", file_id):
            raise ReleaseError("Google Drive URL must identify one public file")
        return f"https://drive.google.com/uc?id={file_id}", True
    return value, False


class _BoundedWriter:
    def __init__(self, handle, limit):
        self.handle, self.limit, self.written = handle, limit, 0

    def write(self, data):
        if self.written + len(data) > self.limit:
            raise ReleaseError("download exceeds archive byte limit")
        result = self.handle.write(data)
        self.written += result
        return result

    def flush(self):
        self.handle.flush()


def archive_download(*, url: str, destination: Path, max_bytes: int) -> None:
    normalized, drive = archive_url(url)
    with destination.open("xb") as handle:
        writer = _BoundedWriter(handle, max_bytes)
        if drive:
            # Для крупных файлов Drive требуется подтверждение скачивания.
            # Потоковая запись ограничивает размер так же, как обычный HTTP.
            try:
                import gdown
            except ImportError as exc:
                raise ReleaseError("Google Drive file download requires gdown==5.2.0") from exc
            result = gdown.download(
                url=normalized,
                output=writer,
                quiet=True,
                fuzzy=True,
                resume=False,
                use_cookies=False,
            )
            if result is None:
                raise ReleaseError("Google Drive did not return the public archive")
        else:
            with urlopen(normalized, timeout=120) as response:
                if urlsplit(response.url).scheme not in {"http", "https"}:
                    raise ReleaseError("archive redirected to a non-HTTP URL")
                length = response.headers.get("Content-Length")
                if length is not None and int(length) > max_bytes:
                    raise ReleaseError("download exceeds archive byte limit")
                while chunk := response.read(1024 * 1024):
                    writer.write(chunk)


def _archive_layout(archive: Path, max_unpacked_bytes: int):
    """Все заголовки проверяются до распаковки; tar.extract и extractall не вызываются."""
    members, total = {}, 0
    with tarfile.open(archive, "r:*") as tar:
        for member in tar:
            name = relative_path(member.name)
            if (
                name in members
                or len(members) >= MAX_ARCHIVE_MEMBERS
                or not (member.isfile() or member.isdir())
                or member.sparse is not None
            ):
                raise ReleaseError("archive contains duplicate, special, linked or too many members")
            if member.size < 0 or (member.isdir() and member.size):
                raise ReleaseError("invalid archive member size")
            total += member.size
            if total > max_unpacked_bytes:
                raise ReleaseError("archive exceeds unpacked byte limit")
            members[name] = member
        manifest = members.get("release.json")
        if manifest is None or not manifest.isfile() or manifest.size > MAX_MANIFEST_BYTES:
            raise ReleaseError("archive requires a bounded root release.json")
        raw = tar.extractfile(manifest).read(MAX_MANIFEST_BYTES + 1)
    release = validate_release(raw)
    files = {}
    for name, model in release["models"].items():
        source = model.get("source", {})
        if source.get("kind") == "bundle":
            for filename, checksum in model["sha256"].items():
                files[f"{source['subdir']}/{filename}"] = (
                    f"models/{name}/{filename}",
                    checksum,
                )
    permitted = {"release.json", "README.md", *files}
    directories = {
        parent.as_posix() for file in permitted for parent in PurePosixPath(file).parents if parent.as_posix() != "."
    }
    for name, member in members.items():
        if (member.isdir() and name not in directories) or (member.isfile() and name not in permitted):
            raise ReleaseError(f"unregistered archive member: {name}")
    if not files.keys() <= members.keys() or any(not members[f].isfile() for f in files):
        raise ReleaseError("archive is missing pinned model files")
    return raw, files


def materialize_archive(
    url: str,
    sha256: str,
    cache_dir: Path,
    registry_output: Path,
    embedding_url: str,
    *,
    download=archive_download,
    upstream_download=hf_download,
    max_archive_bytes=MAX_ARCHIVE_BYTES,
    max_unpacked_bytes=MAX_UNPACKED_BYTES,
) -> Path:
    """Архив адресуется содержимым: полный повтор обходится без сети."""
    archive_url(url)
    if not isinstance(sha256, str) or not re.fullmatch(r"[0-9a-f]{64}", sha256):
        raise ReleaseError("bundle SHA256 must be a full lowercase 64-hex digest")
    if min(max_archive_bytes, max_unpacked_bytes) <= 0:
        raise ReleaseError("archive size limits must be positive")
    _embedding_url(embedding_url)
    cache_dir = cache_dir.expanduser().resolve()
    target = cache_dir / "bundles" / sha256
    registry_output = _paths(cache_dir, target, registry_output)
    archive = cache_dir / "downloads" / f"{sha256}.tar.gz"
    _paths(cache_dir, archive, registry_output)
    if registry_output.is_relative_to(archive.parent):
        raise ReleaseError("registry must be outside the archive download directory")
    archive.parent.mkdir(parents=True, exist_ok=True)
    download_lock = archive.with_suffix(".lock")
    with _process_lock(download_lock, "archive download"):
        if not archive.exists():
            if target.exists():
                raise ReleaseError("cached release is missing its original SHA-pinned archive")
            with tempfile.TemporaryDirectory(prefix=".archive-download-", dir=archive.parent) as temp:
                partial = Path(temp) / "archive.tar.gz"
                download(url=url, destination=partial, max_bytes=max_archive_bytes)
                if (
                    partial.is_symlink()
                    or not partial.is_file()
                    or partial.stat().st_size > max_archive_bytes
                    or digest(partial) != sha256
                ):
                    raise ReleaseError("downloaded archive SHA/size mismatch")
                os.rename(partial, archive)
        if (
            archive.is_symlink()
            or not archive.is_file()
            or archive.stat().st_size > max_archive_bytes
            or digest(archive) != sha256
        ):
            raise ReleaseError("cached archive SHA/size mismatch")
        raw, files = _archive_layout(archive, max_unpacked_bytes)

        def populate(stage, release):
            with tarfile.open(archive, "r|*") as tar:
                for member in tar:
                    if member.name not in files:
                        continue
                    destination, checksum = files[member.name]
                    output = stage / destination
                    output.parent.mkdir(parents=True, exist_ok=True)
                    with tar.extractfile(member) as source, output.open("xb") as handle:
                        shutil.copyfileobj(source, handle, length=1024 * 1024)
                    if digest(output) != checksum:
                        raise ReleaseError(f"archive artifact SHA mismatch: {member.name}")
            for name, model in release["models"].items():
                source = model.get("source", {})
                if source.get("kind") == "huggingface":
                    for filename, checksum in model["sha256"].items():
                        path = upstream_download(
                            repo_id=source["repo_id"],
                            revision=source["revision"],
                            filename=filename,
                            cache_dir=str(cache_dir / "hub"),
                        )
                        _copy_verified(Path(path), stage / f"models/{name}/{filename}", checksum)

        return _install_release(
            raw,
            {"archive_sha256": sha256},
            target,
            cache_dir,
            registry_output,
            embedding_url,
            populate,
        )
