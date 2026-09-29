"""Проверяемый транспорт внутренних RPC, общий для Python-исполнителей."""

import os
from pathlib import Path

import grpc


def channel(address: str, *, options=()):
    mode = os.getenv("GRPC_TLS_MODE", "")
    if mode in ("", "disabled"):
        if os.getenv("APP_ENV") == "production":
            raise RuntimeError("production requires GRPC_TLS_MODE=mtls")
        return grpc.insecure_channel(address, options=options)
    if mode != "mtls":
        raise RuntimeError("unsupported GRPC_TLS_MODE")
    credentials = grpc.ssl_channel_credentials(
        root_certificates=Path(os.environ["GRPC_TLS_CA"]).read_bytes(),
        private_key=Path(os.environ["GRPC_TLS_KEY"]).read_bytes(),
        certificate_chain=Path(os.environ["GRPC_TLS_CERT"]).read_bytes(),
    )
    return grpc.secure_channel(address, credentials, options=options)
