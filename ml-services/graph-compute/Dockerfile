# syntax=docker/dockerfile:1.7
#
# Контекст сборки — корень репозитория: build.rs компилирует центральный контракт
# backend/proto. Корневой .dockerignore сужает контекст до backend/proto/ и
# ml-services/graph-compute/. Форк rustworkx-core закреплён git-зависимостью
# в Cargo.toml и скачивается cargo при сборке, в контексте он не нужен.

FROM rust:1.98.0-slim-trixie AS builder

ENV PATH=/usr/local/cargo/bin:${PATH}

WORKDIR /src/ml-services/graph-compute

RUN apt-get update \
    && apt-get install -y --no-install-recommends \
        build-essential \
        ca-certificates \
        cmake \
        protobuf-compiler \
        pkg-config \
        git \
    && rm -rf /var/lib/apt/lists/*

COPY backend/proto /src/backend/proto
COPY ml-services/graph-compute /src/ml-services/graph-compute

RUN --mount=type=cache,id=graph-compute-cargo-registry,target=/usr/local/cargo/registry \
    --mount=type=cache,id=graph-compute-cargo-git,target=/usr/local/cargo/git \
    --mount=type=cache,id=graph-compute-target,target=/src/ml-services/graph-compute/target \
    cargo build --locked --release --features service --bins \
    && set -eux; \
    mkdir -p /runtime-rootfs/usr/local/bin; \
    cp target/release/graph_compute_server /runtime-rootfs/usr/local/bin/; \
    for bin in /runtime-rootfs/usr/local/bin/graph_compute_server; do \
        ldd "$bin" | awk '/=> \// { print $3 } /^\// { print $1 }'; \
    done | sort -u | while read -r lib; do \
        test -n "$lib"; \
        dest_dir="$(readlink -f "$(dirname "$lib")")"; \
        dest_path="$dest_dir/$(basename "$lib")"; \
        mkdir -p "/runtime-rootfs$dest_dir"; \
        cp -Lv "$lib" "/runtime-rootfs$dest_path"; \
    done

FROM gcr.io/distroless/cc-debian13:latest

WORKDIR /work

COPY --from=builder /runtime-rootfs/ /

# gRPC — основной канал (его вызывает cluster-worker), HTTP — отладка и health.
EXPOSE 9093 8080

ENTRYPOINT ["/usr/local/bin/graph_compute_server"]
