"""Приёмка входа анализа и кандидатов в ограниченном объёме; тренды не ранжирует.

Запускается в окружении сервиса с его адресами S3, БД и графа. Полный диагностический артефакт
остаётся закрытым; сохранённый вход сверяется через db-service до и после чтения источников.
Живых вызовов модели нет.
"""
from __future__ import annotations

import argparse
import json
import os
from pathlib import Path
import time

import rpc_transport

from genproto.analysis.v1 import analysis_pb2, analysis_pb2_grpc
from signalgraph.candidates import cluster
from signalgraph.identity import Work, counting_units
from signalgraph.manifest import canonical, digest, load_input
from signalgraph.storage import Objects
from signalgraph.temporal import measure


def recheck(raw: bytes, manifest: dict) -> None:
    request = manifest["request"]
    with rpc_transport.channel(os.environ["DB_SERVICE_ADDR"],
                               options=[("grpc.max_receive_message_length", 32 << 20)]) as channel:
        response = analysis_pb2_grpc.AnalysisServiceStub(channel).Read(analysis_pb2.Envelope(json=json.dumps({
            "run_id": manifest["run_id"], "reader_id": request["owner_id"], "shared_corpus": request["shared_corpus"],
        })), timeout=15)
    frozen = json.loads(response.json)
    if frozen["sha256"] != digest(raw) or frozen["json"].encode() != raw:
        raise ValueError("database input differs from operator manifest")


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("--input", type=Path, required=True)
    parser.add_argument("--output", type=Path, required=True)
    parser.add_argument("--graph-address", default=os.getenv("GRAPH_COMPUTE_ADDR", "graph-compute:9093"))
    parser.add_argument("--verify-permutation", action="store_true")
    args = parser.parse_args()
    started = time.monotonic()
    raw = args.input.read_bytes()
    manifest = json.loads(raw)
    recheck(raw, manifest)
    corpus = load_input(raw, digest(raw), Objects().read)
    partition = cluster(corpus, args.graph_address)
    if args.verify_permutation:
        repeated = cluster({**corpus, "vectors": list(reversed(corpus["vectors"]))}, args.graph_address)
        if repeated["candidates"] != partition["candidates"] or repeated["unassigned"] != partition["unassigned"]:
            raise ValueError("canonical graph membership depends on input order")
    for candidate in partition["candidates"]:
        candidate["measurements"] = measure(corpus, candidate["work_ids"], corpus["coverage_windows"])
    recheck(raw, manifest)
    report = {"status": "candidates_only", "method_status": "not_computed", "corpus": corpus, "partition": partition}
    with args.output.open("xb") as stream:
        stream.write(canonical(report))
    summary = {"run_id": manifest["run_id"], "input_sha256": digest(raw), "status": report["status"],
               "records": len(corpus["records"]), "units": counting_units([Work(**w) for w in corpus["works"]]),
               "relations": len(corpus["relations"]), "candidates": len(partition["candidates"]),
               "releases": len(corpus["releases"]), "complete_coverage_windows": len(corpus["coverage_windows"]),
               "permutation_verified": args.verify_permutation,
               "unassigned_works": len(partition["unassigned"]), "read_bytes": corpus["read_bytes"],
               "elapsed_seconds": round(time.monotonic()-started, 3), "artifact_sha256": digest(args.output.read_bytes())}
    print(json.dumps(summary, ensure_ascii=False))


if __name__ == "__main__":
    main()
