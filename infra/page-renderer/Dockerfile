FROM ghcr.io/browserless/chromium:v2.24.3
USER root
RUN apt-get update && apt-get install -y --no-install-recommends iptables util-linux \
    && rm -rf /var/lib/apt/lists/*
COPY --chmod=0755 start.sh /usr/local/bin/start-isolated-renderer
ENTRYPOINT ["/usr/local/bin/start-isolated-renderer"]
