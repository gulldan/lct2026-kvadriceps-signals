# syntax=docker/dockerfile:1.7

# ast-grep поставляется glibc-бинарями, поэтому инструменты — в этой Debian-стадии.
FROM oven/bun:1.4.2-slim AS build

WORKDIR /app

COPY package.json bun.lock ./
# В чистой сети Docker резолвер Bun нестабилен с реестром и параллелизмом по умолчанию, а битый
# tarball («Integrity check failed») валит весь `docker compose up --build` и оставляет стек на
# старых образах. Поэтому реестр закреплён, параллелизм снижен, а сбой повторяется с чистым кэшем.
RUN for attempt in 1 2 3; do \
      bun install --frozen-lockfile \
        --registry https://registry.npmjs.org \
        --network-concurrency 4 && exit 0; \
      echo "bun install attempt $attempt failed; purging cache and retrying" >&2; \
      bun pm cache rm 2>/dev/null || true; \
      rm -rf "$HOME/.bun/install/cache" node_modules 2>/dev/null || true; \
      sleep 2; \
    done; \
    echo "bun install failed after 3 attempts" >&2; exit 1

COPY . .

# Пустой публичный шлюз оставляет API и WebSocket браузера на том же origin; /api проксирует
# nginx рантайма.
ARG RAG_API=http://nginx
ARG RAG_PUBLIC_API=
ENV RAG_API=$RAG_API \
    RAG_PUBLIC_API=$RAG_PUBLIC_API
RUN bun run build

FROM nginx:1.31.5-alpine AS runtime

ENV API_UPSTREAM=http://nginx \
    S3_PUBLIC_ENDPOINT=http://localhost:4566

COPY nginx.conf.template /etc/nginx/templates/default.conf.template
COPY --from=build /app/dist /usr/share/nginx/html

EXPOSE 80
