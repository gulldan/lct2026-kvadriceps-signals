FROM ghcr.io/astral-sh/uv:0.12.10-python3.14-trixie-slim

ENV PYTHONUNBUFFERED=1 \
    VIRTUAL_ENV=/opt/venv \
    PATH="/opt/venv/bin:$PATH"

WORKDIR /app

RUN apt-get update \
    && apt-get install -y --no-install-recommends \
        libgl1 libglib2.0-0 libgomp1 \
    && rm -rf /var/lib/apt/lists/*

COPY ml-services/diagram-parser-service/pyproject.toml ml-services/diagram-parser-service/uv.lock /app/diagram-parser-service/
COPY ml-services/diagram-parser-service/diagram_parser_service /app/diagram-parser-service/diagram_parser_service

RUN python -m venv "$VIRTUAL_ENV" \
    && cd /app/diagram-parser-service \
    && uv sync --locked --no-dev --active

COPY ml-services/runtime /tmp/rag-runtime
RUN uv pip install --python "$VIRTUAL_ENV/bin/python" --no-deps /tmp/rag-runtime && rm -rf /tmp/rag-runtime

EXPOSE 8089
CMD ["uvicorn", "diagram_parser_service.server:app", "--host", "0.0.0.0", "--port", "8089"]
