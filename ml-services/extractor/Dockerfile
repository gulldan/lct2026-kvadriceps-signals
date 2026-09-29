# syntax=docker/dockerfile:1.7
FROM python:3.12-slim AS deps
WORKDIR /app
ENV VIRTUAL_ENV=/opt/venv PATH="/opt/venv/bin:$PATH" PIP_NO_CACHE_DIR=1
# По умолчанию — численно проверенная GPU-сборка torch 2.8.0+cu128. CPU-образ:
# --build-arg TORCH_INDEX_URL=https://download.pytorch.org/whl/cpu --build-arg EXTRACTOR_DEVICE=cpu
ARG TORCH_INDEX_URL=https://download.pytorch.org/whl/cu128
COPY ml-services/extractor/requirements.txt .
# Отдельный индекс выбирает сборку CPU или CUDA одной закреплённой версии.
RUN python -m venv "$VIRTUAL_ENV" \
    && pip install --index-url "$TORCH_INDEX_URL" torch==2.8.0 \
    && pip install -r requirements.txt

FROM python:3.12-slim
ARG EXTRACTOR_DEVICE=cuda
WORKDIR /app
ENV PYTHONUNBUFFERED=1 PYTHONDONTWRITEBYTECODE=1 \
    VIRTUAL_ENV=/opt/venv PATH="/opt/venv/bin:$PATH" \
    PYTHONPATH=/app/ml-services CUBLAS_WORKSPACE_CONFIG=:4096:8 \
    HF_HOME=/var/lib/extractor/models/huggingface \
    EXTRACTOR_MODEL_CACHE_DIR=/var/lib/extractor/models \
    EXTRACTOR_SPAN_CACHE_DIR=/var/lib/extractor/spans \
    EXTRACTOR_DEVICE=$EXTRACTOR_DEVICE EXTRACTOR_CUDA_DTYPE=fp32
COPY --from=deps /opt/venv /opt/venv
COPY ml-services/extractor /app/ml-services/extractor
RUN mkdir -p /var/lib/extractor/models /var/lib/extractor/spans \
    && chown -R 65532:65532 /var/lib/extractor
USER 65532:65532
EXPOSE 8080
HEALTHCHECK --interval=30s --timeout=10s --start-period=15m --retries=3 \
    CMD python -c "import json, urllib.request; health = json.load(urllib.request.urlopen('http://127.0.0.1:8080/health', timeout=8)); raise SystemExit(0 if health.get('status') == 'ok' else 1)"
ENTRYPOINT ["python", "-m", "extractor.container_entrypoint"]
CMD ["python", "-m", "uvicorn", "extractor.api:app", "--host", "0.0.0.0", "--port", "8080", "--no-access-log"]
