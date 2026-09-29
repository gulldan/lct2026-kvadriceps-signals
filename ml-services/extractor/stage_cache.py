"""Кэш вердиктов стадии в процессе для повторного закреплённого входа из предложения."""

#: Версия формы вердикта в ключе кэша: записи без распределения по кодам 0..5
#: дали бы тёплому ответу другую уверенность.
STAGE_CACHE_VERSION = "stage-verdict-probs@2"


class EvidenceStageCache:
    """Ограниченный кэш процесса; входы и результаты отделены от объектов запроса."""

    def __init__(self, max_entries=100000, max_bytes=128 << 20):
        from collections import OrderedDict
        from threading import Lock

        self._entries = OrderedDict()
        self._lock = Lock()
        self.max_entries, self.max_bytes = max_entries, max_bytes
        self._bytes = 0

    @staticmethod
    def _copy(block, doc_id=None):
        sentences, evidence, verdicts = block
        def rebound(item):
            return item.model_copy(update={"doc_id": doc_id or item.doc_id})
        return (
            [rebound(item) for item in sentences],
            [rebound(item) for item in evidence],
            [item.model_copy(update={"doc_id": doc_id or item.doc_id, "quotes": list(item.quotes),
                                     "noise_flags": list(item.noise_flags),
                                     "probs": None if item.probs is None else list(item.probs)})
             for item in verdicts],
        )

    def get(self, key, doc_id):
        with self._lock:
            entry = self._entries.get(key)
            if entry is None:
                return None
            self._entries.move_to_end(key)
            return self._copy(entry[0], doc_id)

    def put(self, key, sentences, evidence, verdicts):
        block = self._copy((sentences, evidence, verdicts))
        # Верхняя оценка строк в памяти без создания дополнительных UTF-8 копий.
        size = 512 + len(repr(key)) * 4 + sum(
            1024 + 4 * (len(item.sentence) + len(item.context_before) +
                        len(item.context_after) + len(item.candidate_text))
            for item in sentences
        ) + sum(768 + 4 * (len(item.sentence) + sum(map(len, item.quotes))) for item in verdicts)
        if size > self.max_bytes or self.max_entries < 1:
            return
        with self._lock:
            old = self._entries.pop(key, None)
            if old:
                self._bytes -= old[1]
            self._entries[key] = (block, size)
            self._bytes += size
            while len(self._entries) > self.max_entries or self._bytes > self.max_bytes:
                _, (_, removed) = self._entries.popitem(last=False)
                self._bytes -= removed

    def clear(self):
        with self._lock:
            self._entries.clear()
            self._bytes = 0
