# У PaddlePaddle 3.3.1 есть колёса CUDA 12.9 для amd64 и arm64; Python 3.12
# поддерживает и закреплённый стек PaddleX/OpenCV.
FROM python:3.12.14-slim-trixie AS deps

ENV PYTHONUNBUFFERED=1 \
    VIRTUAL_ENV=/opt/venv \
    PATH="/opt/venv/bin:$PATH" \
    PIP_NO_CACHE_DIR=1

WORKDIR /app

RUN apt-get update \
    && apt-get install -y --no-install-recommends \
        build-essential \
        libgl1 libglib2.0-0 libgomp1 \
    && rm -rf /var/lib/apt/lists/*

COPY ml-services/paddleocr-vl-service/requirements.txt .
RUN python -m venv "$VIRTUAL_ENV" \
    && pip install --upgrade pip \
    && pip install -r requirements.txt
# Индекс Paddle хранит GPU-колесо, но не все закреплённые зависимости Python/CUDA:
# их после скачивания колеса разрешает PyPI.
RUN pip download --no-deps --dest /tmp/paddle paddlepaddle-gpu==3.3.1 \
        -i https://www.paddlepaddle.org.cn/packages/stable/cu129/ \
    && pip install -c requirements.txt /tmp/paddle/*.whl \
    && pip check \
    && rm -rf /tmp/paddle

FROM python:3.12.14-slim-trixie AS runtime

ENV PYTHONUNBUFFERED=1 \
    VIRTUAL_ENV=/opt/venv \
    PATH="/opt/venv/bin:$PATH" \
    PADDLEOCR_VL_DEVICE=gpu

WORKDIR /app

RUN apt-get update \
    && apt-get install -y --no-install-recommends \
        libgl1 libglib2.0-0 libgomp1 \
    && rm -rf /var/lib/apt/lists/*

COPY --from=deps /opt/venv /opt/venv
COPY ml-services/paddleocr-vl-service/paddleocr_vl_server.py .
COPY ml-services/runtime /tmp/rag-runtime
RUN pip install --no-deps /tmp/rag-runtime && rm -rf /tmp/rag-runtime

EXPOSE 8088
CMD ["uvicorn", "paddleocr_vl_server:app", "--host", "0.0.0.0", "--port", "8088"]
