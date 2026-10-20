from __future__ import annotations

import logging
import os
import threading

_LOGGER = logging.getLogger(__name__)
_LOCK = threading.Lock()
_INITIALIZED = False


def initialize_torch_determinism() -> None:
    """Глобальные флаги torch задаются до загрузки первого бэкенда."""
    global _INITIALIZED

    with _LOCK:
        if _INITIALIZED:
            return
        # Детерминированный cuBLAS на CUDA требует рабочее пространство до первого вызова; образ задаёт то же значение.
        os.environ.setdefault("CUBLAS_WORKSPACE_CONFIG", ":4096:8")
        import torch

        torch.use_deterministic_algorithms(True)
        torch.backends.cuda.matmul.allow_tf32 = False
        torch.backends.cudnn.allow_tf32 = False
        torch.backends.cudnn.benchmark = False
        torch.backends.cudnn.deterministic = True
        _INITIALIZED = True
        _LOGGER.info(
            "Runtime initialized process-wide torch determinism: "
            "deterministic_algorithms=True, cudnn.deterministic=True, "
            "cudnn.benchmark=False, cudnn.allow_tf32=False, matmul.allow_tf32=False"
        )
