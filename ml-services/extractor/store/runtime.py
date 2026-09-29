"""Пул соединений HTTP-рантайма; при импорте соединений не открывается."""

import asyncio
import os
from contextlib import asynccontextmanager
from contextvars import ContextVar

import asyncpg

from .postgres import PostgresSpanCache, migrate

_span_cache = ContextVar("owned_span_cache", default=None)


def configured_span_cache():
    cache = _span_cache.get()
    if cache is not None:
        return cache
    if os.getenv("EXTRACTOR_POSTGRES_DSN"):
        raise RuntimeError("Postgres assembly must run inside open_storage()")
    from extractor.runtime import FileSpanCache

    return FileSpanCache(
        os.getenv("EXTRACTOR_SPAN_CACHE_DIR", ".cache/extractor/spans")
    )


@asynccontextmanager
async def open_storage(*, slots=1):
    dsn = os.getenv("EXTRACTOR_POSTGRES_DSN", "")
    if not dsn:
        yield configured_span_cache()
        return
    # Каждый слот вывода читает и пишет кэш через своё соединение; запасные отвечают
    # пробам здоровья, пока все слоты заняты.
    pool = await asyncpg.create_pool(
        dsn,
        min_size=1,
        max_size=slots + 2,
        timeout=5,
        command_timeout=10,
    )
    try:
        if os.getenv("EXTRACTOR_AUTO_MIGRATE", "0") == "1":
            await migrate(pool)
        cache = PostgresSpanCache(pool)
        token = _span_cache.set(cache)
        try:
            yield cache
        finally:
            _span_cache.reset(token)
    finally:
        try:
            async with asyncio.timeout(10):
                await pool.close()
        except TimeoutError:
            pool.terminate()
