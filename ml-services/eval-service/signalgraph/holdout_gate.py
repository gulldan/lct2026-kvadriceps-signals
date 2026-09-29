"""Явная заморозка кандидата для одного прогона holdout, отдельно от приёмки качества.

Заморозка хеширует байты, не заглядывая в оценки holdout. Ранжировщик проверяет
только код; разметка остаётся в процессе выгрузки и оценки. Проваленный holdout
попадает в отчёт, а дальнейшая настройка требует нового протокола и нетронутого holdout.
"""
from __future__ import annotations

import argparse
from datetime import datetime, timezone
import hashlib
import json
from pathlib import Path
import re
import sys

VERSION = 'signalgraph-holdout-candidate-freeze-v1'
ROOT = Path(__file__).resolve().parents[3]
BENCHMARK = Path('ml-services/eval-service/signalgraph')


def digest(raw: bytes) -> str:
    return hashlib.sha256(raw).hexdigest()


def canonical(value: dict) -> bytes:
    return json.dumps(value, ensure_ascii=False, sort_keys=True, separators=(',', ':'), allow_nan=False).encode()


def code_files(root: Path) -> list[Path]:
    worker = root/'ml-services/signal-worker'
    return sorted([*worker.glob('signalgraph/*.py'), *worker.glob('genproto/**/*.py'),
                   worker/'worker.py', worker/'requirements.txt', worker/'method.json',
                   *(root/BENCHMARK).glob('*.py')])


def create(root: Path, runtimes: dict) -> dict:
    for role in ('runner', 'graph', 'embedding'):
        if not re.fullmatch(r'sha256:[a-f0-9]{64}', runtimes.get(role, '')):
            raise ValueError('pin the actual runner, graph and embedding image IDs')
    sys.path.insert(0, str(root/'ml-services/signal-worker'))
    from signalgraph import method
    from embed_inputs import MODEL, REVISION, DIMENSION
    from keyword_baseline import VERSION as baseline_version, CONFIG as baseline_config
    from run_ranking import QUERIES
    raw_method = (root/'ml-services/signal-worker/method.json').read_bytes()
    definition = method.verify(raw_method)
    benchmark = root/BENCHMARK
    names = {'PROTOCOL.md', 'protocol-freeze.json'}
    # Читаются только готовые манифесты хешей; разметка holdout здесь не разбирается.
    for path in [*benchmark.glob('freeze.*.json'), *benchmark.glob('extensions/freeze.*.json')]:
        names.add(str(path.relative_to(benchmark)))
        names.update(json.loads(path.read_bytes())['sha256'])
    files = {str(BENCHMARK/name): digest((benchmark/name).read_bytes()) for name in sorted(names)}
    return {'version': VERSION, 'created_at': datetime.now(timezone.utc).isoformat(),
            'split': 'holdout', 'purpose': 'one preregistered evaluation, not acceptance of quality',
            'method': definition, 'method_sha256': digest(raw_method),
            'code': {str(path.relative_to(root)): digest(path.read_bytes()) for path in code_files(root)},
            'benchmark': files, 'runtimes': runtimes, 'queries': QUERIES,
            'embedding': {'model': {'name': MODEL, 'revision': REVISION, 'dimension': DIMENSION},
                          'instruction': '', 'focus': False},
            'baseline': {'version': baseline_version, 'config': baseline_config},
            'independence': 'Codex documentary labels; no second independent expert review',
            'after_holdout': 'no tuning or threshold/label changes; new protocol and untouched holdout required'}


def verify(path: Path | None, expected: str | None, *, root: Path = ROOT,
           benchmark: bool = False) -> dict:
    if path is None or expected is None:
        raise ValueError('holdout cannot be opened without an explicit candidate freeze and SHA-256')
    raw = path.read_bytes()
    if digest(raw) != expected:
        raise ValueError('candidate freeze SHA-256 mismatch')
    freeze = json.loads(raw)
    if freeze.get('version') != VERSION or freeze.get('split') != 'holdout':
        raise ValueError('invalid candidate freeze')
    actual_files = {str(p.relative_to(root)) for p in code_files(root)}
    if set(freeze['code']) != actual_files:
        raise ValueError('candidate code membership changed after freeze')
    for section in ('code', 'benchmark') if benchmark else ('code',):
        for name, sha256 in freeze[section].items():
            artifact = (root/name).resolve()
            if not artifact.is_relative_to(root.resolve()) or digest(artifact.read_bytes()) != sha256:
                raise ValueError(f'candidate {section} artifact changed: {name}')
    return freeze


def claim_run(path: Path, sha256: str, source_sha256: str, vectors_sha256: str) -> dict:
    receipt = {'candidate_freeze_sha256': sha256, 'source_input_sha256': source_sha256,
               'vectors_sha256': vectors_sha256, 'started_at': datetime.now(timezone.utc).isoformat()}
    try:
        with path.with_suffix('.opened.json').open('xb') as stream:
            stream.write(canonical(receipt))
    except FileExistsError as exc:
        raise ValueError('holdout run already started for this freeze; do not rerun or retune') from exc
    return receipt


if __name__ == '__main__':
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument('--runtimes', type=Path, required=True)
    parser.add_argument('--output', type=Path, required=True)
    args = parser.parse_args()
    raw = canonical(create(ROOT, json.loads(args.runtimes.read_bytes())))
    with args.output.open('xb') as stream:
        stream.write(raw)
    args.output.chmod(0o444)
    print(json.dumps({'candidate_freeze_sha256': digest(raw), 'holdout_opened': False,
                      'purpose': 'code/configuration freeze; quality remains unaccepted'}))
