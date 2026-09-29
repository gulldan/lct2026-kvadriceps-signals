import asyncio

import pytest

from extractor.store import CacheConflict, PostgresSpanCache
from extractor.schemas import Span

def span(**updates):
    return Span(start=0, end=5, text="метод", canonical_ru="метод", subtype="method", role="contribution", confidence=0.5).model_copy(update=updates)


async def test_sync_cache_uses_owner_loop_and_never_overwrites():
    owner_loop = asyncio.get_running_loop()

    class Pool:
        data = None

        async def fetchval(self, sql, *args):
            assert asyncio.get_running_loop() is owner_loop
            await asyncio.sleep(0)
            return self.data

        async def execute(self, sql, digest, version, value):
            assert asyncio.get_running_loop() is owner_loop
            if self.data is None:
                self.data = value

    cache = PostgresSpanCache(Pool())
    assert await asyncio.to_thread(cache.get, "a" * 64, "v1") is None
    await asyncio.to_thread(cache.put, "a" * 64, "v1", [span()])
    assert (await asyncio.to_thread(cache.get, "a" * 64, "v1"))[0].confidence == 0.5
    with pytest.raises(CacheConflict):
        await asyncio.to_thread(cache.put, "a" * 64, "v1", [span(confidence=0.9)])
    assert (await cache.aget("a" * 64, "v1"))[0].confidence == 0.5
    with pytest.raises(RuntimeError, match="pipeline thread"):
        cache.get("a" * 64, "v1")
