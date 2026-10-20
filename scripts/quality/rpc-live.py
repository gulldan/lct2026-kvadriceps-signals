"""Межъязыковой RPC: Python-клиент, Rust-сервер и реальные сертификаты CA."""
import argparse
import json
import os
from pathlib import Path
import socket
import subprocess
import tempfile
import urllib.error
import urllib.request
import uuid

import grpc
from graph.v1 import graph_pb2, graph_pb2_grpc
from rpc_transport import channel


def port():
    with socket.socket() as listener:
        listener.bind(("127.0.0.1", 0))
        return listener.getsockname()[1]


def http(url, method="GET", data=None):
    request = urllib.request.Request(
        url, data=json.dumps(data).encode() if data is not None else None,
        method=method, headers={"Content-Type": "application/json"},
    )
    with urllib.request.urlopen(request, timeout=5) as response:
        return response.read()


def check_identity(pki, address, collection, identity):
    os.environ["GRPC_TLS_KEY"] = str(pki / f"{identity}.key")
    os.environ["GRPC_TLS_CERT"] = str(pki / f"{identity}.pem")
    with channel(address, options=(("grpc.ssl_target_name_override", "graph-compute"),)) as connection:
        grpc.channel_ready_future(connection).result(timeout=15)
        stub = graph_pb2_grpc.GraphComputeStub(connection)
        try:
            stub.Cluster(graph_pb2.ClusterRequest(owner_id="quality-owner", collection=collection), timeout=5)
            assert identity == "main-service", "unauthorized service accepted"
        except grpc.RpcError as exc:
            assert identity == "auth" and exc.code() == grpc.StatusCode.PERMISSION_DENIED, (identity, exc.code())


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("--pki", type=Path, required=True)
    parser.add_argument("--binary", type=Path, required=True)
    args = parser.parse_args()
    rpc_address, http_address = f"127.0.0.1:{port()}", f"127.0.0.1:{port()}"
    collection = "quality_mtls_" + uuid.uuid4().hex
    base = os.environ["QDRANT_TEST_URL"].rstrip("/") + "/collections/" + collection
    env = {**os.environ, "APP_ENV": "production", "GRPC_TLS_MODE": "mtls", "GRPC_SERVICE_ID": "graph-compute",
           "GRPC_TLS_CA": str(args.pki / "ca.pem"), "GRPC_TLS_CERT": str(args.pki / "graph-compute.pem"),
           "GRPC_TLS_KEY": str(args.pki / "graph-compute.key"), "GRPC_ADDR": rpc_address,
           "HTTP_ADDR": http_address, "QDRANT_URL": os.environ["QDRANT_TEST_URL"]}
    os.environ.update(APP_ENV="production", GRPC_TLS_MODE="mtls", GRPC_TLS_CA=str(args.pki / "ca.pem"))
    http(base, "PUT", {"vectors": {"size": 3, "distance": "Cosine"}})
    with tempfile.TemporaryFile(mode="w+") as log:
        process = subprocess.Popen([str(args.binary)], env=env, stdout=log, stderr=subprocess.STDOUT)
        try:
            check_identity(args.pki, rpc_address, collection, "main-service")
            check_identity(args.pki, rpc_address, collection, "auth")
            try:
                http(f"http://{http_address}/v1/cluster", "POST", {})
                raise AssertionError("production HTTP compute remained enabled")
            except urllib.error.HTTPError as exc:
                assert exc.code == 404
            assert http(f"http://{http_address}/healthz") == b"ok"
            print("Python -> Rust mTLS: allowed identity, denied identity, disabled HTTP compute: PASS")
        except BaseException:
            log.seek(0)
            print(log.read())
            raise
        finally:
            process.terminate()
            try:
                process.wait(timeout=5)
            except subprocess.TimeoutExpired:
                process.kill()
                process.wait(timeout=5)
            http(base, "DELETE")


if __name__ == "__main__":
    main()
