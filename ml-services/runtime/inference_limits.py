"""Бюджеты входа и приёма для HTTP-сервисов вычисления моделей."""

import asyncio
import io
import threading
import time
from contextlib import contextmanager

from fastapi import HTTPException
from starlette.responses import PlainTextResponse


class InferenceGate:
    def __init__(self, app, *, max_bytes: int, capacity: int = 3):
        self.app = app
        self.max_bytes = max_bytes
        self.slots = threading.BoundedSemaphore(capacity)

    async def __call__(self, scope, receive, send):
        if scope["type"] != "http" or scope["method"] != "POST":
            return await self.app(scope, receive, send)
        if not self.slots.acquire(blocking=False):
            return await PlainTextResponse("inference queue full", 503, headers={"Retry-After": "2"})(scope, receive, send)
        try:
            body = bytearray()
            deadline = time.monotonic() + 10
            while True:
                try:
                    message = await asyncio.wait_for(receive(), max(0, deadline - time.monotonic()))
                except TimeoutError:
                    return await PlainTextResponse("request body deadline exceeded", 408)(scope, receive, send)
                if message["type"] == "http.disconnect":
                    return
                chunk = message.get("body", b"")
                if len(body) + len(chunk) > self.max_bytes:
                    return await PlainTextResponse("request body exceeds byte budget", 413)(scope, receive, send)
                body.extend(chunk)
                if not message.get("more_body", False):
                    break

            async def bounded_receive():
                nonlocal body
                if body is None:
                    return await receive()
                result = {"type": "http.request", "body": bytes(body), "more_body": False}
                body = None
                return result

            return await self.app(scope, bounded_receive, send)
        finally:
            self.slots.release()


@contextmanager
def inference_slot(lock):
    if not lock.acquire(timeout=2):
        raise HTTPException(503, "inference queue deadline exceeded", headers={"Retry-After": "2"})
    try:
        yield
    finally:
        lock.release()


def validate_image(data: bytes):
    from PIL import Image, UnidentifiedImageError

    try:
        with Image.open(io.BytesIO(data)) as image:
            frames = getattr(image, "n_frames", 1)
            if frames > 128:
                raise HTTPException(413, "image frame budget exceeded")
            pixels = 0
            for frame in range(frames):
                image.seek(frame)
                count = image.width * image.height
                pixels += count
                if count > 40_000_000 or pixels > 100_000_000:
                    raise HTTPException(413, "image pixel budget exceeded")
    except (UnidentifiedImageError, OSError, Image.DecompressionBombError) as exc:
        raise HTTPException(422, "invalid image or image pixel budget exceeded") from exc
