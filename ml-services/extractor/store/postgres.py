"""Кэш спанов на asyncpg; синхронный интерфейс вызывается только из потоков конвейера."""

import asyncio
import json
from pathlib import Path

from extractor.interfaces import SpanCache
from extractor.runtime.cache import CacheConflict, CacheCorruptionError, spans_json
from extractor.schemas import Span


async def migrate(pool):
    async with pool.acquire() as connection, connection.transaction():
        # Сериализует одновременный старт нескольких процессов API.
        await connection.execute("SELECT pg_advisory_xact_lock(721092026)")
        for path in sorted(Path(__file__).with_name("migrations").glob("*.sql")):
            await connection.execute(path.read_text(encoding="utf-8"))


class PostgresSpanCache(SpanCache):
    def __init__(self, pool, loop=None, *, timeout=10.0):
        self.pool = pool
        self.loop = loop or asyncio.get_running_loop()
        self.timeout = timeout

    def _sync(self, factory):
        try:
            current = asyncio.get_running_loop()
        except RuntimeError:
            current = None
        if current is self.loop:
            raise RuntimeError(
                "sync SpanCache must run in a pipeline thread; use aget/aput"
            )
        future = asyncio.run_coroutine_threadsafe(factory(), self.loop)
        try:
            return future.result(timeout=self.timeout)
        except TimeoutError:
            future.cancel()
            raise

    def get(self, text_sha256, model_version):
        return self._sync(lambda: self.aget(text_sha256, model_version))

    def put(self, text_sha256, model_version, spans):
        return self._sync(lambda: self.aput(text_sha256, model_version, spans))

    async def aget(self, text_sha256, model_version):
        raw = await self.pool.fetchval(
            "SELECT spans FROM span_cache WHERE text_sha256=$1 AND model_version=$2",
            text_sha256,
            model_version,
        )
        if raw is None:
            return None
        try:
            return [
                Span.model_validate(s)
                for s in (json.loads(raw) if isinstance(raw, str) else raw)
            ]
        except (ValueError, TypeError) as exc:
            raise CacheCorruptionError("повреждён кэш спанов") from exc

    async def aput(self, text_sha256, model_version, spans):
        value = spans_json(spans)
        await self.pool.execute(
            """INSERT INTO span_cache (text_sha256, model_version, spans)
               VALUES ($1,$2,$3::jsonb) ON CONFLICT DO NOTHING""",
            text_sha256,
            model_version,
            value,
        )
        previous = await self.aget(text_sha256, model_version)
        if spans_json(previous) != value:
            raise CacheConflict("одинаковый ключ кэша дал разные спаны")

    async def health(self):
        await self.pool.fetchval(
            "SELECT count(*) FROM (SELECT 1 FROM span_cache LIMIT 1) AS probe"
        )
        return True
