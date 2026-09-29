# Зависимости (grpcio/protobuf/redis) ставятся из готовых колёс, включая cp314
# manylinux для grpcio: компилятор не нужен, хватает slim. HTTP-вызовы идут через urllib.
FROM python:3.14.7-slim-trixie AS deps

ENV VIRTUAL_ENV=/opt/venv \
    PATH="/opt/venv/bin:$PATH" \
    PIP_NO_CACHE_DIR=1

WORKDIR /app

COPY ml-services/discovery-worker/requirements.txt .
RUN python -m venv "$VIRTUAL_ENV" \
    && pip install --upgrade pip \
    && pip install -r requirements.txt

FROM python:3.14.7-slim-trixie AS runtime

ENV PYTHONUNBUFFERED=1 \
    VIRTUAL_ENV=/opt/venv \
    PATH="/opt/venv/bin:$PATH"

WORKDIR /app

COPY --from=deps /opt/venv /opt/venv
COPY ml-services/runtime /tmp/rag-runtime
RUN pip install --no-deps /tmp/rag-runtime && rm -rf /tmp/rag-runtime
COPY ml-services/discovery-worker/worker.py .
COPY ml-services/discovery-worker/genproto ./genproto

CMD ["python", "-u", "worker.py"]
