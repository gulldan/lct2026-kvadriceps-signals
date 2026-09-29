"""Deterministic source-vector MMR; a position needs a temporal basis, relevance and at least two brief fields.

The DB independently reconstructs these centroids from hash-pinned snapshots.
No vectors leave the immutable input boundary. Parameters are development
choices, not a claim of expert-validated semantic distinctness.
"""
from __future__ import annotations

import math

VERSION = 'source-centroid-mmr-v2-verified-body'
LAMBDA = .75
DUPLICATE_THRESHOLD = .92
SCALE = 1_000_000


def centroid(vectors: list[list[float]]) -> list[float]:
    if not vectors or not vectors[0]:
        raise ValueError('missing ranking representation')
    dimension = len(vectors[0])
    if any(len(v) != dimension or not all(math.isfinite(n) for n in v) for v in vectors):
        raise ValueError('incompatible ranking representation')
    mean = [math.fsum(v[i] for v in vectors) / len(vectors) for i in range(dimension)]
    norm = math.sqrt(math.fsum(n*n for n in mean))
    if not norm or not math.isfinite(norm):
        raise ValueError('zero ranking centroid')
    return [n/norm for n in mean]


MIN_BRIEF_FIELDS = 2


def rankable(candidate: dict) -> bool:
    fields = sum(candidate['brief'][name] is not None for name in ('problem', 'advantage', 'example'))
    return bool(candidate['assessment']['eligible'] and candidate['relevance']['relevant'] and fields >= MIN_BRIEF_FIELDS)


def select(candidates: list[dict], vectors: list[dict], input_sha256: str) -> tuple[list[dict], dict]:
    pool = sorted((c for c in candidates if rankable(c)),
                  key=lambda c: (tuple(-n for n in c['assessment']['order']), c['id']))
    by_work = {v['canonical_id']: v['vector'] for v in vectors}
    if len(by_work) != len(vectors):
        raise ValueError('repeated canonical ranking representation')
    centers = {c['id']: centroid([by_work[w] for w in sorted(c['work_ids'])]) for c in pool}
    remaining = {c['id']: c for c in pool}
    quality = {c['id']: (len(pool)-i)*SCALE//len(pool) for i, c in enumerate(pool)}
    redundant = dict.fromkeys(remaining, 0)
    selected, suppressed = [], []
    while remaining and len(selected) < 15:
        # Integer scores and quantized cosine make Python/Go tie decisions identical.
        best = min(remaining, key=lambda key: (-(3*quality[key]-redundant[key]), key))
        selected.append(remaining.pop(best))
        for key in sorted(remaining):
            cosine = math.fsum(a*b for a, b in zip(centers[best], centers[key], strict=True))
            similarity = math.floor(max(0., min(1., cosine))*SCALE+.5)
            redundant[key] = max(redundant[key], similarity)
            if redundant[key] >= int(DUPLICATE_THRESHOLD*SCALE):
                suppressed.append(key)
                del remaining[key]
    return selected, {'version': VERSION, 'lambda': LAMBDA, 'duplicate_threshold': DUPLICATE_THRESHOLD,
                      'input_sha256': input_sha256, 'suppressed_ids': sorted(suppressed)}
