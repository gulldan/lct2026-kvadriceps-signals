"""Эмбеддинги F2LLM-v2 в контракте OpenAI /v1/embeddings.

F2LLM-v2 — каузальный эмбеддер на Qwen3: пулинг по последнему токену (EOS) и
L2-нормировка. У 0.6B размерность 1024, под неё создана коллекция Qdrant. Модель
задаёт пространство векторов: её смена требует переэмбеддинга всего корпуса.
"""

import os
import threading
from importlib.metadata import version
from contextlib import asynccontextmanager

import torch
import torch.nn.functional as F
from fastapi import FastAPI
from pydantic import BaseModel, Field, field_validator
from inference_limits import InferenceGate, inference_slot
from transformers import AutoModel, AutoTokenizer

MODEL_NAME = os.environ.get("F2LLM_MODEL", "codefuse-ai/F2LLM-v2-0.6B")
MODEL_REVISION = os.environ.get("F2LLM_REVISION") or None
MAX_TOKENS = min(4096, max(1, int(os.environ.get("F2LLM_MAX_TOKENS", "512"))))
BATCH_SIZE = min(32, max(1, int(os.environ.get("F2LLM_BATCH", "8"))))
_INFER_LOCK = threading.Lock()


def _device() -> str:
    configured = os.environ.get("TORCH_DEVICE", "auto").strip().lower()
    if configured and configured != "auto":
        return configured
    if torch.cuda.is_available():
        return "cuda"
    if getattr(torch.backends, "mps", None) and torch.backends.mps.is_available():
        return "mps"
    return "cpu"


DEVICE = _device()
DTYPE = torch.bfloat16 if DEVICE == "cuda" else torch.float32

tokenizer = AutoTokenizer.from_pretrained(MODEL_NAME, revision=MODEL_REVISION, trust_remote_code=True)
model = AutoModel.from_pretrained(
    MODEL_NAME, revision=MODEL_REVISION, dtype=DTYPE, trust_remote_code=True
).to(DEVICE).eval()
WEIGHTS_REVISION = getattr(model.config, "_commit_hash", None) or "unversioned"
if MODEL_REVISION and WEIGHTS_REVISION != MODEL_REVISION:
    raise RuntimeError("loaded embedding weights differ from F2LLM_REVISION")
EMBEDDING_REVISION = (
    f"{WEIGHTS_REVISION}/last-token-l2-v1/max{MAX_TOKENS}/{DTYPE}"
    f"/torch{version('torch')}/transformers{version('transformers')}"
)


class EmbeddingRequest(BaseModel):
    input: str | list[str]
    model: str | None = Field(default=None, max_length=256)

    @field_validator("input")
    @classmethod
    def input_budget(cls, value):
        texts = [value] if isinstance(value, str) else value
        if not 1 <= len(texts) <= 128 or any(len(text.encode()) > 65536 for text in texts):
            raise ValueError("input count or per-text byte budget exceeded")
        if sum(len(text.encode()) for text in texts) > 512 * 1024:
            raise ValueError("input byte budget exceeded")
        return value


def _last_token_pool(last_hidden: torch.Tensor, attention_mask: torch.Tensor) -> torch.Tensor:
    """Последний непаддинговый токен каждой последовательности; рассчитан на паддинг справа."""
    lengths = attention_mask.sum(dim=1) - 1
    idx = lengths.clamp(min=0)
    return last_hidden[torch.arange(last_hidden.size(0), device=last_hidden.device), idx]


@torch.no_grad()
def _embed(texts: list[str]) -> list[list[float]]:
    batch = tokenizer(
        texts, padding=True, truncation=True, max_length=MAX_TOKENS, return_tensors="pt"
    ).to(DEVICE)
    out = model(**batch)
    pooled = _last_token_pool(out.last_hidden_state, batch["attention_mask"])
    pooled = F.normalize(pooled, p=2, dim=1)
    return pooled.cpu().float().tolist()


@asynccontextmanager
async def lifespan(_app: FastAPI):
    # Прогон настоящей модели до зелёного /health; заодно разовая компиляция
    # Triton проходит, пока Compose ещё ждёт.
    _embed(["startup readiness probe"])
    yield


app = FastAPI(lifespan=lifespan, title="F2LLM-v2 embeddings")
app.add_middleware(InferenceGate, max_bytes=4 << 20)


@app.get("/health")
def health() -> dict:
    return {"status": "ok", "model": MODEL_NAME, "device": DEVICE, "revision": EMBEDDING_REVISION}


@app.post("/v1/embeddings")
def embeddings(req: EmbeddingRequest) -> dict:
    texts = [req.input] if isinstance(req.input, str) else list(req.input)
    with inference_slot(_INFER_LOCK):
        vectors = []
        for start in range(0, len(texts), BATCH_SIZE):
            vectors.extend(_embed(texts[start : start + BATCH_SIZE]))
    data = [
        {"object": "embedding", "index": i, "embedding": v} for i, v in enumerate(vectors)
    ]
    return {"object": "list", "data": data, "model": MODEL_NAME, "revision": EMBEDDING_REVISION}
