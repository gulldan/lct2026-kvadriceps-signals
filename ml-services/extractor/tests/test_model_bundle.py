"""Проверка доставки синтетического бандла без скачивания и загрузки моделей."""

import functools
import hashlib
import io
import json
import tarfile
from concurrent.futures import ThreadPoolExecutor
from pathlib import Path

import pytest

from extractor import container_entrypoint as entry
from extractor import model_bundle
from extractor.registry import ModelRegistry


def sha(data):
    return hashlib.sha256(data).hexdigest()


@pytest.fixture
def bundle(tmp_path, monkeypatch):
    payloads = {}
    models = {}
    for name in ("tagger-v1", "stage-encoder-e5", "stage-v1"):
        files = (
            {"stage_lr.json": b"{}", "e5_lr_coefficients.npz": b"synthetic coefficients"}
            if name == "stage-v1"
            else {
                "config.json": json.dumps(
                    {
                        "candidate_classifier_name": "candidate_classifier",
                        "candidate_id2label": {"0": "not_candidate", "1": "candidate"},
                    }
                ).encode(),
                "model.safetensors": b"synthetic weights",
                "tokenizer.json": b"{}",
            }
        )
        payloads.update({f"{name}/{file}": raw for file, raw in files.items()})
        models[name] = {
            "name": name,
            "version": "fixture",
            "backend": "e5_lr" if name == "stage-v1" else "torch",
            "path": name,
            "max_len": 512 if name == "tagger-v1" else 192,
            "source": {"kind": "bundle", "subdir": name},
            "sha256": {file: sha(raw) for file, raw in files.items()},
        }
    models["f2llm-embeddings"] = {
        "name": "f2llm-embeddings",
        "version": "fixture",
        "backend": "remote_http",
        "dim": 1024,
        "expected_revision": "fixture-revision",
    }
    payloads["release.json"] = json.dumps(
        {
            "schema_version": 1,
            "service_version": "weak-signal-2026-09-22.3",
            "runtime": {"code_revision": "a" * 40, "torch": "2.14.0", "transformers": "5.17.0", "cuda_dtype": "fp32"},
            "thresholds": {"predict": 0.5, "trend_t1": 1.2, "trend_t2": 3.0},
            "evaluations": {},
            "models": models,
        }
    ).encode()
    archive = tmp_path / "bundle.tar.gz"

    def write(*, extra=None):
        with tarfile.open(archive, "w:gz") as tar:
            for name, raw in payloads.items():
                member = tarfile.TarInfo(name)
                member.size = len(raw)
                tar.addfile(member, io.BytesIO(raw))
            if extra:
                tar.addfile(extra)
        return sha(archive.read_bytes())

    env = {
        "EXTRACTOR_MODEL_SOURCE": "archive",
        "EXTRACTOR_MODEL_BUNDLE_URL": "https://example.invalid/models.tar.gz",
        "EXTRACTOR_MODEL_BUNDLE_SHA256": write(),
        "EXTRACTOR_MODEL_CACHE_DIR": str(tmp_path / "cache"),
        "EXTRACTOR_EMBEDDING_URL": "http://f2llm-service:8085",
    }
    downloads = []

    def download(*, url, destination, max_bytes):
        downloads.append(url)
        destination.write_bytes(archive.read_bytes())

    monkeypatch.setattr(
        entry,
        "materialize_archive",
        functools.partial(
            model_bundle.materialize_archive,
            download=download,
        ),
    )
    return env, payloads, write, downloads


def test_start_verifies_all_models_and_reuses_bundle_without_downloading(bundle, monkeypatch):
    env, _, _, downloads = bundle
    monkeypatch.setattr(entry.os, "environ", env)
    started = []
    monkeypatch.setattr(entry.os, "execvp", lambda executable, command: started.append(command))
    entry.main(["python", "-m", "uvicorn", "extractor.api:app"])
    registry = ModelRegistry.load(env["EXTRACTOR_MODELS_YAML"])
    assert registry.service_version == "weak-signal-2026-09-22.3"
    for name in ("tagger-v1", "stage-v1", "stage-encoder-e5"):
        assert registry.verify(name)
    assert registry.spec("f2llm-embeddings").url == env["EXTRACTOR_EMBEDDING_URL"]
    entry.main(["python", "-m", "uvicorn", "extractor.api:app"])
    assert len(downloads) == 1 and len(started) == 2


def test_parallel_processes_install_the_same_bundle_once(bundle):
    env, _, _, downloads = bundle
    environments = [dict(env), dict(env)]
    with ThreadPoolExecutor(max_workers=2) as workers:
        list(workers.map(entry.prepare_models, environments))
    assert environments[0]["EXTRACTOR_MODELS_YAML"] == environments[1]["EXTRACTOR_MODELS_YAML"]
    assert len(downloads) == 1


@pytest.mark.parametrize(
    "missing", ["EXTRACTOR_MODEL_BUNDLE_URL", "EXTRACTOR_MODEL_BUNDLE_SHA256", "EXTRACTOR_EMBEDDING_URL"]
)
def test_missing_configuration_prevents_process_start(bundle, monkeypatch, missing):
    env, _, _, downloads = bundle
    env.pop(missing)
    monkeypatch.setattr(entry.os, "environ", env)
    monkeypatch.setattr(entry.os, "execvp", lambda *args: pytest.fail("service started"))
    assert entry.main(["python", "-m", "uvicorn", "extractor.api:app"]) == 1
    assert downloads == []


def test_wrong_archive_sha_prevents_process_start(bundle, monkeypatch):
    env, _, _, _ = bundle
    env["EXTRACTOR_MODEL_BUNDLE_SHA256"] = "f" * 64
    monkeypatch.setattr(entry.os, "environ", env)
    monkeypatch.setattr(entry.os, "execvp", lambda *args: pytest.fail("service started"))
    assert entry.main(["python", "-m", "uvicorn", "extractor.api:app"]) == 1
    assert not list(Path(env["EXTRACTOR_MODEL_CACHE_DIR"]).rglob("*.yaml"))


@pytest.mark.parametrize("tamper", ["artifact", "cached_archive", "installed_weights"])
def test_changed_content_cannot_become_a_verified_registry(bundle, tamper):
    env, payloads, write, _ = bundle
    if tamper == "artifact":
        payloads["tagger-v1/model.safetensors"] = b"changed weights"
        env["EXTRACTOR_MODEL_BUNDLE_SHA256"] = write()
    else:
        entry.prepare_models(env)
        cache = Path(env["EXTRACTOR_MODEL_CACHE_DIR"])
        target = (
            next((cache / "downloads").glob("*.tar.gz"))
            if tamper == "cached_archive"
            else ModelRegistry.load(env["EXTRACTOR_MODELS_YAML"]).root / "models/tagger-v1/model.safetensors"
        )
        target.write_bytes(b"changed")
    with pytest.raises(ValueError, match="SHA|changed"):
        entry.prepare_models(env)


@pytest.mark.parametrize("kind", ["traversal", "symlink"])
def test_archive_cannot_escape_its_model_directory(bundle, kind):
    env, _, write, _ = bundle
    member = tarfile.TarInfo("../outside" if kind == "traversal" else "linked")
    if kind == "symlink":
        member.type = tarfile.SYMTYPE
        member.linkname = "/tmp/outside"
    env["EXTRACTOR_MODEL_BUNDLE_SHA256"] = write(extra=member)
    with pytest.raises(ValueError, match="unsafe|linked"):
        entry.prepare_models(env)
    assert not list(Path(env["EXTRACTOR_MODEL_CACHE_DIR"]).rglob("*.yaml"))


def test_local_mode_requires_an_existing_explicit_registry(tmp_path):
    registry = tmp_path / "models.yaml"
    env = {"EXTRACTOR_MODEL_SOURCE": "local", "EXTRACTOR_MODELS_YAML": str(registry)}
    with pytest.raises(ValueError, match="existing"):
        entry.prepare_models(env)
    registry.write_text("local: true\n")
    entry.prepare_models(env)
    assert env["EXTRACTOR_MODELS_YAML"] == str(registry)


def test_new_bundle_gets_a_new_registry_without_overwriting_the_previous_one(bundle):
    env, payloads, write, _ = bundle
    entry.prepare_models(env)
    previous = Path(env.pop("EXTRACTOR_MODELS_YAML"))
    original = previous.read_bytes()
    manifest = json.loads(payloads["release.json"])
    manifest["service_version"] = "weak-signal-2026-09-22.4"
    payloads["release.json"] = json.dumps(manifest).encode()
    env["EXTRACTOR_MODEL_BUNDLE_SHA256"] = write()
    entry.prepare_models(env)
    assert previous.read_bytes() == original
    assert ModelRegistry.load(env["EXTRACTOR_MODELS_YAML"]).service_version == "weak-signal-2026-09-22.4"
