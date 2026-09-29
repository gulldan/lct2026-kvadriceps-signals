FROM python:3.14.7-slim-trixie AS deps

ENV VIRTUAL_ENV=/opt/venv \
    PATH="/opt/venv/bin:$PATH" \
    PIP_NO_CACHE_DIR=1

WORKDIR /app

COPY ml-services/f2llm-service/requirements.txt .
RUN python -m venv "$VIRTUAL_ENV" \
    && pip install --upgrade pip \
    && pip install -r requirements.txt

FROM python:3.14.7-slim-trixie AS runtime

ENV PYTHONUNBUFFERED=1 \
    TOKENIZERS_PARALLELISM=false \
    VIRTUAL_ENV=/opt/venv \
    PATH="/opt/venv/bin:$PATH" \
    TORCH_DEVICE=auto \
    F2LLM_MODEL=codefuse-ai/F2LLM-v2-0.6B \
    F2LLM_MAX_TOKENS=512

WORKDIR /app

# PyTorch/Triton компилирует прослойку драйвера CUDA на первом настоящем запросе:
# без компилятора в образе контейнер пройдёт /health и упадёт на первом выводе
# с «Failed to find C compiler».
RUN apt-get update \
    && apt-get install -y --no-install-recommends build-essential \
    && rm -rf /var/lib/apt/lists/*

COPY --from=deps /opt/venv /opt/venv
COPY ml-services/f2llm-service/f2llm_server.py .
COPY ml-services/runtime /tmp/rag-runtime
RUN pip install --no-deps /tmp/rag-runtime && rm -rf /tmp/rag-runtime

EXPOSE 8085
CMD ["uvicorn", "f2llm_server:app", "--host", "0.0.0.0", "--port", "8085"]
