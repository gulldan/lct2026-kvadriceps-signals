"""Use the existing Rust kNN/Leiden service with verified canonical vectors."""
from __future__ import annotations

import rpc_transport

from genproto.graph.v1 import graph_pb2, graph_pb2_grpc

CONFIG = {"algorithm": "leiden", "seed": 42, "knn_k": 6, "min_size": 2,
          "resolution": 1.0, "sim_threshold": 0.18, "mutual_knn": True,
          "max_iterations": 30, "granularity": "document", "knn_block_size": 256}
VERSION = "canonical-knn-leiden-v2-strict-edges"


def cluster(corpus: dict, address: str, config: dict | None = None) -> dict:
    config = CONFIG if config is None else config
    vectors = corpus["vectors"]
    if not vectors:
        return {"version": VERSION, "config": config, "candidates": [], "unassigned": [], "work_count": 0}
    model = corpus["input"]["model"]
    request = graph_pb2.SnapshotClusterRequest(
        manifest_sha256=corpus["input_sha256"], model_revision=model["revision"], dimension=model["dimension"],
        config=graph_pb2.ClusterConfig(**config), works=[graph_pb2.SnapshotWork(
            canonical_id=work["canonical_id"], vector=work["vector"], title=work["title"],
            representative_text=work["representative_text"],
        ) for work in vectors],
    )
    with rpc_transport.channel(address, options=[("grpc.max_send_message_length", 32 << 20)]) as channel:
        response = graph_pb2_grpc.GraphComputeStub(channel).ClusterSnapshot(request, timeout=90)
    known = {work["canonical_id"] for work in vectors}
    assigned = set()
    candidates = []
    for candidate in response.clusters:
        members = sorted(candidate.members)
        if len(set(members)) != len(members) or not set(members) <= known or assigned.intersection(members):
            raise ValueError("cluster output violates immutable canonical membership")
        assigned.update(members)
        if any(r.document_id not in members for r in candidate.representatives):
            raise ValueError("representative work is outside candidate membership")
        candidates.append({"candidate_id": corpus["input_sha256"][:16] + "-" + candidate.fingerprint,
                           "work_ids": members, "fingerprint": candidate.fingerprint,
                           "average_similarity": candidate.metrics.avg_similarity,
                           "representative_work_ids": [r.document_id for r in candidate.representatives]})
    candidates.sort(key=lambda candidate: candidate["candidate_id"])
    return {"version": VERSION, "config": config, "candidates": candidates, "unassigned": sorted(known-assigned),
            "work_count": len(known), "edge_count": response.stats.edge_count,
            "modularity": response.stats.modularity, "compute_ms": response.stats.compute_ms}
