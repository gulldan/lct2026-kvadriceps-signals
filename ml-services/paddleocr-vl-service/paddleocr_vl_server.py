"""Структурный OCR: PaddleOCR-VL за контрактом ocr-service.

Вход {"model", "image_b64", "mime"}, выход {"text", "pages"}: pages — тексты страниц
в порядке чтения, из них ocr-service строит page_offsets для нарезки. PaddleOCR-VL
за проход распознаёт разметку, порядок чтения, таблицы и формулы и отдаёт Markdown —
структура для нарезки сохраняется лучше, чем в плоском тексте OCR. Для кириллицы
у модели есть замеренная оценка.
"""

import base64
import binascii
import os
import tempfile
import threading
from contextlib import asynccontextmanager

from fastapi import FastAPI, HTTPException
from pydantic import BaseModel, Field
from inference_limits import InferenceGate, inference_slot, validate_image

_pipeline: dict = {}
_predict_lock = threading.Lock()

# PaddleOCR-VL.predict() читает путь и выбирает декодер по расширению, поэтому
# байты пишутся во временный файл с верным суффиксом.
_SUFFIX = {
    "image/png": ".png",
    "image/jpeg": ".jpg",
    "image/jpg": ".jpg",
    "image/tiff": ".tiff",
    "image/bmp": ".bmp",
    "image/webp": ".webp",
    "image/gif": ".gif",
}


def _device() -> str:
    """'auto' — устройство не передаётся: Paddle сам берёт GPU 0 с откатом на CPU."""
    configured = os.environ.get("PADDLEOCR_VL_DEVICE", "auto").strip().lower()
    return configured or "auto"


def _suffix(mime: str) -> str:
    mime = (mime or "").lower()
    if "pdf" in mime:
        return ".pdf"
    return _SUFFIX.get(mime, ".png")


def _pipeline_kwargs() -> dict:
    device = _device()
    kwargs = {} if device == "auto" else {"device": device}

    backend = os.environ.get("PADDLEOCR_VL_REC_BACKEND", "").strip()
    server_url = os.environ.get("PADDLEOCR_VL_REC_SERVER_URL", "").strip()
    if not backend and not server_url:
        return kwargs
    if not backend:
        raise RuntimeError(
            "PADDLEOCR_VL_REC_BACKEND is required when "
            "PADDLEOCR_VL_REC_SERVER_URL is set"
        )
    if not server_url:
        raise RuntimeError(
            "PADDLEOCR_VL_REC_SERVER_URL is required when "
            "PADDLEOCR_VL_REC_BACKEND is set"
        )

    kwargs.update(vl_rec_backend=backend, vl_rec_server_url=server_url)
    api_model = os.environ.get("PADDLEOCR_VL_REC_API_MODEL_NAME", "").strip()
    if api_model:
        kwargs["vl_rec_api_model_name"] = api_model

    raw_concurrency = os.environ.get("PADDLEOCR_VL_REC_MAX_CONCURRENCY", "").strip()
    if raw_concurrency:
        try:
            concurrency = int(raw_concurrency)
        except ValueError as exc:
            raise RuntimeError(
                "PADDLEOCR_VL_REC_MAX_CONCURRENCY must be a positive integer"
            ) from exc
        if concurrency < 1:
            raise RuntimeError(
                "PADDLEOCR_VL_REC_MAX_CONCURRENCY must be a positive integer"
            )
        kwargs["vl_rec_max_concurrency"] = concurrency
    return kwargs


def _markdown(res) -> str:
    md = getattr(res, "markdown", None)
    if isinstance(md, dict):
        return (md.get("markdown_texts") or "").strip()
    return ("" if md is None else str(md)).strip()


def _load() -> None:
    from paddleocr import PaddleOCRVL

    _pipeline["vl"] = PaddleOCRVL(**_pipeline_kwargs())


def _recognize(path: str) -> list[str]:
    """Markdown по страницам; пустые не выбрасываются, чтобы число страниц сошлось."""
    pages = []
    size = 0
    for res in _pipeline["vl"].predict(input=path):
        text = _markdown(res)
        size += len(text.encode())
        if len(pages) >= 128 or size > 16 << 20:
            raise HTTPException(413, "OCR result budget exceeded")
        pages.append(text)
    return pages


class OCRRequest(BaseModel):
    model: str | None = None
    image_b64: str = Field(min_length=1, max_length=24 << 20)
    mime: str | None = None


@asynccontextmanager
async def lifespan(_app: FastAPI):
    _load()
    from PIL import Image

    with tempfile.NamedTemporaryFile(suffix=".png") as probe:
        Image.new("RGB", (64, 64), "white").save(probe.name)
        _recognize(probe.name)
    yield


app = FastAPI(lifespan=lifespan, title="paddleocr-vl-ocr-adapter")
app.add_middleware(InferenceGate, max_bytes=25 << 20)


@app.get("/health")
def health() -> dict:
    return {
        "status": "ok",
        "ready": "vl" in _pipeline,
        "device": _device(),
        "recognition_backend": os.environ.get(
            "PADDLEOCR_VL_REC_BACKEND", "paddle_dynamic"
        )
        or "paddle_dynamic",
    }


@app.post("/")
def ocr(req: OCRRequest) -> dict:
    with inference_slot(_predict_lock):
        try:
            data = base64.b64decode(req.image_b64, validate=True)
        except binascii.Error as exc:
            raise HTTPException(400, "image_b64 must be valid base64") from exc
        if len(data) > 16 << 20:
            raise HTTPException(413, "OCR input byte budget exceeded")
        if req.mime == "application/pdf":
            import pypdfium2

            try:
                with pypdfium2.PdfDocument(data) as pdf:
                    if not 1 <= len(pdf) <= 128:
                        raise HTTPException(413, "PDF page budget exceeded")
                    pixels = 0
                    for page in pdf:
                        with page:
                            width, height = page.get_size()
                            pixels += width * height * 4
                            if width * height * 4 > 40_000_000 or pixels > 100_000_000:
                                raise HTTPException(413, "PDF pixel budget exceeded")
            except pypdfium2.PdfiumError as exc:
                raise HTTPException(422, "invalid PDF") from exc
        elif req.mime in _SUFFIX:
            validate_image(data)
        else:
            raise HTTPException(415, "unsupported OCR MIME type")
        with tempfile.NamedTemporaryFile(suffix=_suffix(req.mime or "")) as fh:
            fh.write(data)
            fh.flush()
            pages = _recognize(fh.name)
    return {"text": "\n\n".join(p for p in pages if p), "pages": pages}
