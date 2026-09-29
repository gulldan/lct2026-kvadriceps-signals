"""Эмбеддинг неизменяемой выгрузки источников закреплённым живым сервисом F2LLM.

Результат — отдельный артефакт с адресацией по содержимому. Здесь нет разметки,
загрузки источников, запасных векторов, подмены модели и настройки графа.
"""
from __future__ import annotations

import argparse
from datetime import datetime
import hashlib
import json
import math
from pathlib import Path
import sys
import time
import urllib.request
from holdout_gate import verify

local_worker = Path(__file__).resolve().parent.parent.parent / 'signal-worker'
if local_worker.is_dir():
    sys.path.insert(0, str(local_worker))

MODEL = 'codefuse-ai/F2LLM-v2-0.6B'
REVISION = ('8786315a8711c242ee03ec67c74dd9ad0a61e2cf/last-token-l2-v1/'
            'max512/torch.bfloat16/torch2.14.0/transformers5.16.1')
DIMENSION = 1024


def embed(path: Path, output: Path, url: str, focus: bool = False, instruction: str = '',
          freeze: Path | None = None, freeze_sha256: str | None = None) -> dict:
    from signalgraph.representation import contribution

    if output.exists():
        raise ValueError('embedding output is immutable; choose a new artifact')
    if len(instruction) > 512 or '\x00' in instruction:
        raise ValueError('invalid embedding instruction')
    prefix = f'Instruct: {instruction}\nQuery: ' if instruction else ''
    raw = path.read_bytes()
    source_input = json.loads(raw)
    if source_input['split'] == 'holdout':
        pin = verify(freeze, freeze_sha256)
        if source_input.get('candidate_freeze_sha256') != freeze_sha256 or pin['embedding'] != {
                'model': {'name': MODEL, 'revision': REVISION, 'dimension': DIMENSION},
                'instruction': instruction, 'focus': focus}:
            raise ValueError('holdout embedding input or configuration differs from candidate freeze')
    if source_input['version'] != 'benchmark-sources-v1' or len(source_input['sources']) > 2000:
        raise ValueError('invalid or oversized benchmark input')
    sources = source_input['sources']
    if len({source['canonical_id'] for source in sources}) != len(sources):
        raise ValueError('benchmark repeats a canonical work')
    cutoff = datetime.fromisoformat(source_input['cutoff'].replace('Z', '+00:00'))
    for source in sources:
        if hashlib.sha256(source['text'].encode()).hexdigest() != source['text_sha256']:
            raise ValueError('source text differs from frozen hash')
        for field in ('published_at', 'available_at', 'version_updated_at'):
            if datetime.fromisoformat(source[field].replace('Z', '+00:00')) > cutoff:
                raise ValueError('source is unavailable at the benchmark cutoff')
    vectors = []
    started = time.monotonic()
    for offset in range(0, len(sources), 8):
        batch = sources[offset:offset + 8]
        spans = [contribution(s['text']) if focus else None for s in batch]
        texts = [prefix + s['title'] + '\n' + (span['text'] if span else s['text']) for s, span in zip(batch, spans)]
        request = urllib.request.Request(url, data=json.dumps({'model': MODEL, 'input': texts}).encode(),
                                         headers={'Content-Type': 'application/json'}, method='POST')
        with urllib.request.urlopen(request, timeout=90) as response:
            body = response.read((4 << 20) + 1)
        if len(body) > 4 << 20:
            raise ValueError('embedding response exceeds bounded size')
        result = json.loads(body)
        if result.get('model') != MODEL or result.get('revision') != REVISION:
            raise ValueError('live embedding model/revision differs from the pinned pipeline')
        embeddings = result.get('embeddings')
        if embeddings is None:
            rows = sorted(result.get('data', []), key=lambda r: r['index'])
            if [r['index'] for r in rows] != list(range(len(batch))):
                raise ValueError('embedding response indices are missing or repeated')
            embeddings = [r['embedding'] for r in rows]
        if len(embeddings) != len(batch):
            raise ValueError('embedding response count differs from input')
        for source, text, vector, span in zip(batch, texts, embeddings, spans):
            if len(vector) != DIMENSION or not all(math.isfinite(x) for x in vector):
                raise ValueError('non-finite or incompatible embedding')
            norm = math.sqrt(sum(x*x for x in vector))
            if norm <= 0:
                raise ValueError('zero embedding')
            vectors.append({'canonical_id': 'paper:' + source['canonical_id'],
                            'vector': [x/norm for x in vector], 'title': source['title'],
                            'representative_text': source['text'],
                            'contribution_span': span,
                            'embedding_input_sha256': hashlib.sha256(text.encode()).hexdigest()})
    artifact = {'version': 'benchmark-vectors-v1', 'source_input_sha256': hashlib.sha256(raw).hexdigest(),
                'split': source_input['split'], 'model': {'name': MODEL, 'revision': REVISION, 'dimension': DIMENSION},
                'instruction': instruction, 'instruction_sha256': hashlib.sha256(instruction.encode()).hexdigest(),
                'representation': ('title-newline-explicit-contribution-span-v1' if focus else 'title-newline-primary-abstract')
                                  + '; server max512 token truncation; L2; one vector per work',
                'vectors': vectors}
    data = json.dumps(artifact, ensure_ascii=False, sort_keys=True, separators=(',', ':'), allow_nan=False).encode()
    with output.open('xb') as stream:
        stream.write(data)
    return {'split': source_input['split'], 'works': len(vectors), 'model_revision': REVISION,
            'sha256': hashlib.sha256(data).hexdigest(), 'seconds': round(time.monotonic()-started, 3)}


if __name__ == '__main__':
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument('--input', type=Path, required=True)
    parser.add_argument('--output', type=Path, required=True)
    parser.add_argument('--url', default='http://f2llm-service:8085/v1/embeddings')
    parser.add_argument('--focus', action='store_true', help='Use an exact contribution span as the derived representation')
    parser.add_argument('--instruction', default='', help='F2 task instruction; included in input hashes and artifact')
    parser.add_argument('--freeze', type=Path)
    parser.add_argument('--freeze-sha256')
    args = parser.parse_args()
    print(json.dumps(embed(args.input, args.output, args.url, args.focus, args.instruction, args.freeze, args.freeze_sha256)))
