FROM python:3.14.7-slim-trixie AS deps
WORKDIR /app
ENV VIRTUAL_ENV=/opt/venv PATH="/opt/venv/bin:$PATH" PIP_NO_CACHE_DIR=1
COPY ml-services/signal-worker/requirements.txt .
RUN python -m venv "$VIRTUAL_ENV" && pip install -r requirements.txt

FROM python:3.14.7-slim-trixie
WORKDIR /app
ENV PYTHONUNBUFFERED=1 VIRTUAL_ENV=/opt/venv PATH="/opt/venv/bin:$PATH" PYTHONDONTWRITEBYTECODE=1
COPY --from=deps /opt/venv /opt/venv
COPY ml-services/runtime /tmp/rag-runtime
RUN pip install --no-deps /tmp/rag-runtime && rm -rf /tmp/rag-runtime
COPY ml-services/signal-worker/worker.py ml-services/signal-worker/method.json ml-services/signal-worker/requirements.txt ./
COPY ml-services/signal-worker/signalgraph ./signalgraph
COPY ml-services/signal-worker/genproto ./genproto
USER 65532:65532
CMD ["python", "worker.py"]
