"""Ограниченный пул с отдельным токенизатором на поток и сохранением порядка."""

from copy import deepcopy
from concurrent.futures import ThreadPoolExecutor
from threading import local


class TokenizerPool:
    def __init__(self, tokenizer, workers=2, chunk_size=64):
        self.tokenizer = tokenizer
        self.workers = workers
        self.chunk_size = chunk_size
        self._local = local()
        self._executor = None

    def _encode(self, job):
        texts, options = job
        if not hasattr(self._local, "tokenizer"):
            # Быстрый токенизатор меняет настройки усечения и дополнения при вызове.
            self._local.tokenizer = deepcopy(self.tokenizer)
        return self._local.tokenizer(texts, **options)

    def encode(self, texts, **options):
        from transformers import BatchEncoding

        if self.workers == 1 or len(texts) <= self.chunk_size:
            return self.tokenizer(list(texts), **options)
        if self._executor is None:
            self._executor = ThreadPoolExecutor(max_workers=self.workers, thread_name_prefix="extract-tokenize")
        jobs = ((list(texts[start:start + self.chunk_size]), options)
                for start in range(0, len(texts), self.chunk_size))
        data, encodings = {}, []
        for part in self._executor.map(self._encode, jobs):
            for name, values in part.items():
                data.setdefault(name, []).extend(values)
            encodings.extend(part.encodings)
        return BatchEncoding(data, encoding=encodings)

    def close(self):
        if self._executor is not None:
            self._executor.shutdown(wait=True)
            self._executor = None
