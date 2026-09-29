"""Ограниченный графовый эксперимент на development/validation по векторам одних источников."""
from __future__ import annotations

import argparse
import hashlib
import itertools
import json
from pathlib import Path

from signalgraph.candidates import cluster, CONFIG


def experiment(vector_path: Path, output: Path, address: str, fine: bool = False) -> dict:
    raw = vector_path.read_bytes()
    vectors = json.loads(raw)
    if vectors['split'] not in ('development', 'validation'):
        raise ValueError('holdout cannot be used for graph configuration experiments')
    corpus = {'input_sha256': vectors['source_input_sha256'],
              'input': {'model': vectors['model']}, 'vectors': vectors['vectors']}
    partitions = []
    settings = (itertools.product((2, 4, 6), (6., 8., 10.), (.5, .7, .8), (False, True)) if fine else
                itertools.product((2, 4, 6), (1., 2., 3., 6.), (.18, .5, .65), (True,)))
    for k, resolution, threshold, mutual in settings:
        config = {**CONFIG, 'knn_k': k, 'resolution': resolution, 'sim_threshold': threshold, 'mutual_knn': mutual}
        partition = cluster(corpus, address, config)
        partitions.append(partition)
    report = {'split': vectors['split'], 'input_sha256': vectors['source_input_sha256'],
              'vectors_sha256': hashlib.sha256(raw).hexdigest(), 'partitions': partitions}
    with output.open('x') as stream:
        json.dump(report, stream, ensure_ascii=False, sort_keys=True, separators=(',', ':'), allow_nan=False)
    return {'split': vectors['split'], 'configurations': len(partitions),
            'candidate_range': [min(len(p['candidates']) for p in partitions), max(len(p['candidates']) for p in partitions)],
            'sha256': hashlib.sha256(output.read_bytes()).hexdigest()}


if __name__ == '__main__':
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument('--vectors', type=Path, required=True)
    parser.add_argument('--output', type=Path, required=True)
    parser.add_argument('--address', default='graph-compute:9093')
    parser.add_argument('--fine', action='store_true', help='Check finer resolution and strict union-kNN edges')
    args = parser.parse_args()
    print(json.dumps(experiment(args.vectors, args.output, args.address, args.fine)))
