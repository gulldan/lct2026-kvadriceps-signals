"""Реестр моделей: ``models.yaml`` и сверка sha256.

Без совпадения контрольной суммы ничего не грузится. Здесь же единая версия сервиса
(``weak-signal-YYYY-MM-DD.N``, она же RADAR_CLASSIFIER_VERSION): смена порогов или весов
меняет версию, старые оценки молча не пересчитываются.
"""

from __future__ import annotations

import hashlib
import os
from collections.abc import Callable, Iterator
from enum import StrEnum
from pathlib import Path
from typing import Any, Self

import yaml
from pydantic import BaseModel, ConfigDict, Field, NonNegativeInt, model_validator

from extractor.schemas import SERVICE_VERSION_RE

DEFAULT_MODELS_YAML = Path(__file__).with_name("models.yaml")

_CHUNK = 1024 * 1024


class RegistryError(RuntimeError):
    """Базовая ошибка реестра."""


class ModelNotFound(RegistryError):
    """Модели нет в ``models.yaml``."""


class ModelNotPinned(RegistryError):
    """Модель есть, но её sha256 ещё ``null``."""


class ModelIntegrityError(RegistryError):
    """Файла нет или его sha256 не совпадает с ``models.yaml``."""


class Backend(StrEnum):
    TORCH = "torch"
    E5_LR = "e5_lr"
    #: Не локальный артефакт, а чужой HTTP-сервис.
    REMOTE_HTTP = "remote_http"


def sha256_file(path: Path) -> str:
    digest = hashlib.sha256()
    with path.open("rb") as handle:
        while chunk := handle.read(_CHUNK):
            digest.update(chunk)
    return digest.hexdigest()


class ModelSpec(BaseModel):
    model_config = ConfigDict(extra="forbid", protected_namespaces=())

    name: str = Field(min_length=1)
    version: str = Field(min_length=1)
    backend: Backend
    #: Каталог или файл; относительный путь — от ``ModelsConfig.root``.
    path: str = ""
    #: Относительное имя файла -> sha256; ``null`` — ещё не закреплён.
    sha256: dict[str, str | None] = Field(default_factory=dict)
    max_len: NonNegativeInt = 0
    #: Ревизия векторного пространства удалённого сервиса: ответ с другой ревизией отклоняется.
    #: Без неё ревизия не сверяется.
    expected_revision: str | None = Field(default=None, min_length=1)
    dim: NonNegativeInt | None = None
    notes: str = ""
    #: У ``remote_http`` вместо файлов URL.
    url: str | None = None

    @property
    def ref(self) -> str:
        """Входит в ключ кэша спанов и в model_versions."""
        return f"{self.name}@{self.version}"

    @property
    def pinned(self) -> bool:
        return bool(self.sha256) and all(value for value in self.sha256.values())

    def resolve_path(self, root: Path) -> Path:
        candidate = Path(os.path.expanduser(self.path))
        return candidate if candidate.is_absolute() else (root / candidate)

    def files(self, root: Path) -> Iterator[tuple[str, Path, str | None]]:
        base = self.resolve_path(root)
        for name in sorted(self.sha256):
            yield name, base / name, self.sha256[name]


class Thresholds(BaseModel):
    """Смена любого порога меняет версию сервиса."""

    model_config = ConfigDict(extra="forbid")

    #: T1/T2 тренда, откалиброваны на эталонной выборке. Явный null отключает соответствующее
    #: повышение, наблюдения при этом не меняются.
    trend_t1: float | None = None
    trend_t2: float | None = None
    #: Порог включения в ТОП-15, подбирается только на валидации (контракт).
    predict: float = Field(default=0.5, ge=0.0, le=1.0)
    #: Стадия с меньшей вероятностью не считается подтверждённой.
    stage_min_prob: float = Field(default=0.0, ge=0.0, le=1.0)


class ModelsConfig(BaseModel):
    model_config = ConfigDict(extra="forbid")

    service_version: str
    root: str = "."
    thresholds: Thresholds = Field(default_factory=Thresholds)
    models: dict[str, ModelSpec] = Field(default_factory=dict)

    @model_validator(mode="after")
    def _check(self) -> Self:
        if not SERVICE_VERSION_RE.match(self.service_version):
            raise ValueError(
                f"service_version must look like weak-signal-YYYY-MM-DD.N, "
                f"got {self.service_version!r}"
            )
        for key, spec in self.models.items():
            if key != spec.name:
                raise ValueError(f"models.{key} declares name {spec.name!r}")
        return self


class ModelRegistry:
    def __init__(self, config: ModelsConfig, root: Path | None = None) -> None:
        self.config = config
        self.root = Path(root) if root is not None else Path(config.root)

    @classmethod
    def load(cls, path: Path | str | None = None) -> ModelRegistry:
        source = Path(path) if path is not None else DEFAULT_MODELS_YAML
        raw: Any = yaml.safe_load(source.read_text(encoding="utf-8")) or {}
        config = ModelsConfig.model_validate(raw)
        root = Path(config.root)
        if not root.is_absolute():
            root = (source.parent / root).resolve()
        return cls(config, root)

    @property
    def service_version(self) -> str:
        return self.config.service_version

    @property
    def thresholds(self) -> Thresholds:
        return self.config.thresholds

    def spec(self, name: str) -> ModelSpec:
        try:
            return self.config.models[name]
        except KeyError as exc:
            known = ", ".join(sorted(self.config.models)) or "<empty>"
            raise ModelNotFound(f"no model {name!r} in registry; known: {known}") from exc

    def versions(self) -> dict[str, str]:
        out = {"service": self.service_version}
        out.update({name: spec.ref for name, spec in sorted(self.config.models.items())})
        return out

    def verify(self, name: str, *, require_pinned: bool = True) -> dict[str, str]:
        spec = self.spec(name)
        if spec.backend is Backend.REMOTE_HTTP:
            return {}
        if not spec.sha256:
            if require_pinned:
                raise ModelNotPinned(f"{spec.ref}: no files pinned in models.yaml")
            return {}
        seen: dict[str, str] = {}
        for filename, path, expected in spec.files(self.root):
            if expected is None:
                if require_pinned:
                    raise ModelNotPinned(f"{spec.ref}: sha256 of {filename} is null")
                continue
            if not path.is_file():
                raise ModelIntegrityError(f"{spec.ref}: missing file {path}")
            actual = sha256_file(path)
            if actual != expected:
                raise ModelIntegrityError(
                    f"{spec.ref}: sha256 mismatch for {filename}: "
                    f"expected {expected}, got {actual}"
                )
            seen[filename] = actual
        return seen

    def load_model(
        self,
        name: str,
        loader: Callable[[ModelSpec, Path], Any],
        *,
        require_pinned: bool = True,
    ) -> Any:
        """Реестр сам torch не импортирует: ``loader`` даёт рантайм."""
        self.verify(name, require_pinned=require_pinned)
        spec = self.spec(name)
        return loader(spec, spec.resolve_path(self.root))


def default_registry() -> ModelRegistry:
    return ModelRegistry.load()


__all__ = [
    "DEFAULT_MODELS_YAML",
    "Backend",
    "ModelIntegrityError",
    "ModelNotFound",
    "ModelNotPinned",
    "ModelRegistry",
    "ModelSpec",
    "ModelsConfig",
    "RegistryError",
    "Thresholds",
    "default_registry",
    "sha256_file",
]
