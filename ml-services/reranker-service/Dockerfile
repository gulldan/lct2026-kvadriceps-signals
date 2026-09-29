FROM python:3.14.7-slim-trixie AS deps

ENV VIRTUAL_ENV=/opt/venv \
    PATH="/opt/venv/bin:$PATH" \
    PIP_NO_CACHE_DIR=1

WORKDIR /app

COPY ml-services/reranker-service/requirements.txt .
RUN python -m venv "$VIRTUAL_ENV" \
    && pip install --upgrade pip \
    && pip install -r requirements.txt

FROM python:3.14.7-slim-trixie AS runtime

ENV PYTHONUNBUFFERED=1 \
    TOKENIZERS_PARALLELISM=false \
    VIRTUAL_ENV=/opt/venv \
    PATH="/opt/venv/bin:$PATH" \
    TORCH_DEVICE=cuda \
    RERANKER_MODEL=Qwen/Qwen3-Reranker-0.6B \
    RERANKER_MAX_TOKENS=4096 \
    RERANKER_BATCH=8

WORKDIR /app

# PyTorch/Triton компилирует прослойку драйвера CUDA на первом запросе оценки:
# без компилятора вывод упал бы уже после того, как /health объявил сервис здоровым.
RUN apt-get update \
    && apt-get install -y --no-install-recommends build-essential \
    && rm -rf /var/lib/apt/lists/*

COPY --from=deps /opt/venv /opt/venv
COPY ml-services/reranker-service/reranker_server.py .
COPY ml-services/runtime /tmp/rag-runtime
RUN pip install --no-deps /tmp/rag-runtime && rm -rf /tmp/rag-runtime

EXPOSE 8086
CMD ["uvicorn", "reranker_server:app", "--host", "0.0.0.0", "--port", "8086"]
