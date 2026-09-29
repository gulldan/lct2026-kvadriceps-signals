"""Local scheduler progress plus read-only dependency readiness; no inference."""
from __future__ import annotations

import json
import os
from pathlib import Path
import tempfile
import time
import urllib.request

import grpc
import rpc_transport

from genproto.graph.v1 import graph_pb2, graph_pb2_grpc
from .storage import Objects

PROGRESS = Path('/tmp/signal-worker-progress.json')


def update(seconds: int, path: Path = PROGRESS) -> None:
    if not 1 <= seconds <= 600:
        raise ValueError('invalid progress deadline')
    with tempfile.NamedTemporaryFile(mode='w', dir=path.parent, delete=False) as output:
        temporary = Path(output.name)
        json.dump({'pid': os.getpid(), 'deadline': time.time()+seconds}, output)
    try:
        temporary.replace(path)
    finally:
        temporary.unlink(missing_ok=True)


def progress(path: Path = PROGRESS) -> None:
    if path.stat().st_size > 1024:
        raise ValueError('oversized progress')
    state = json.loads(path.read_bytes())
    if (set(state) != {'pid','deadline'} or type(state['pid']) is not int or state['pid'] < 1 or
            not time.time() < state['deadline'] <= time.time()+600):
        raise ValueError('worker progress expired or invalid')
    os.kill(state['pid'], 0)


def graph_ready(address: str) -> None:
    # The empty request must be rejected by the real snapshot handler. This
    # probes RPC/version availability without loading vectors or changing data.
    with rpc_transport.channel(address) as channel:
        try:
            graph_pb2_grpc.GraphComputeStub(channel).ClusterSnapshot(graph_pb2.SnapshotClusterRequest(), timeout=2)
        except grpc.RpcError as error:
            if error.code() == grpc.StatusCode.INVALID_ARGUMENT:
                return
            raise
    raise ValueError('graph accepted an invalid empty snapshot')


def read(url: str) -> bytes:
    with urllib.request.urlopen(url, timeout=2) as response:
        raw = response.read(4097)
    if len(raw) > 4096:
        raise ValueError('oversized readiness response')
    return raw


def check() -> None:
    progress()
    if read(os.getenv('DB_READY_URL','http://db-service:9090/readyz')).strip() != b'ready':
        raise ValueError('DB is not ready')
    objects = Objects(readiness=True)
    objects.client.head_bucket(Bucket=objects.bucket)
    graph_ready(os.environ['GRAPH_COMPUTE_ADDR'])
    url = os.environ['EMBEDDINGS_URL'].removesuffix('/v1/embeddings')+'/health'
    model = json.loads(read(url))
    if model.get('status') != 'ok' or not model.get('model') or not model.get('revision'):
        raise ValueError('embedding model has not completed startup readiness')
