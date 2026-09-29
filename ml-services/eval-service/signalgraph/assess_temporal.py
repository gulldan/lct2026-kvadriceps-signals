"""Временной отбор отдельно, на замороженном эталонном членстве и никогда на holdout.

Знаменатель — перечисленный отобранный пул. Диагностика ничего не утверждает о полноте
провайдера, качестве кластеризации, опоре справки и приёмке ТОП-15.
"""
from __future__ import annotations

import argparse
from collections import Counter
from datetime import datetime, timedelta
import hashlib
import json
from pathlib import Path
import uuid

from signalgraph.emergence import Config, assess
from signalgraph.temporal import measure
from validate_benchmark import validate_split


def evaluate(root: Path, inputs: Path, split: str, config: Config = Config()) -> dict:
    if split not in ('development', 'validation'):
        raise ValueError('holdout cannot tune temporal eligibility')
    validate_split(root, split)
    raw = (inputs / f'{split}.json').read_bytes()
    data = json.loads(raw)
    if data['split'] != split or data['coverage'] != 'complete_enumeration_of_curated_pool_not_provider_census':
        raise ValueError('unverified input population')
    topics = json.loads((root / f'annotations.{split}.json').read_text())['topics']
    for path in sorted((root / 'extensions').glob(f'annotations.{split}.*.json')):
        topics.extend(json.loads(path.read_text())['topics'])
    sources = {s['native_id']: s for s in data['sources']}
    if len(sources) != len(data['sources']) or set(sources) != {n for t in topics for n in t['source_ids']}:
        raise ValueError('incomplete or duplicated input pool')
    cutoff = data['cutoff']
    deadline = datetime.fromisoformat(cutoff.replace('Z', '+00:00'))
    records, works = [], []
    for native, source in sorted(sources.items()):
        saved = json.loads((root / 'sources' / f'{native}.json').read_text())
        if any(source[k] != saved[k] for k in ('canonical_id', 'published_at', 'available_at', 'text_sha256')):
            raise ValueError('frozen source identity, date or text changed')
        if hashlib.sha256(source['text'].encode()).hexdigest() != source['text_sha256']:
            raise ValueError('source hash mismatch')
        if datetime.fromisoformat(source['available_at'].replace('Z', '+00:00')) > deadline:
            raise ValueError('future source in historical pool')
        doc = str(uuid.uuid5(uuid.NAMESPACE_URL, source['url']))
        records.append({'id': doc, 'snapshot_id': str(uuid.uuid5(uuid.NAMESPACE_URL, source['text_sha256'])),
                        'source': {'revision': native, 'publication': {
                            'value': source['published_at'][:10], 'precision': 'day',
                            'basis': 'frozen archived arXiv v1 publication timestamp'}}})
        works.append({'id': 'paper:' + source['canonical_id'], 'kind': 'paper', 'document_ids': [doc]})
    start = min(s['published_at'][:4] for s in sources.values()) + '-01-01'
    corpus = {'records': records, 'works': works, 'input': {'request': {'start': start, 'cutoff': cutoff}}}
    coverage = [{'start': start, 'end_exclusive': (deadline + timedelta(seconds=1)).date().isoformat(),
                 'complete': True, 'work_ids': [w['id'] for w in works],
                 'basis': {'kind': data['coverage'], 'source_input_sha256': hashlib.sha256(raw).hexdigest()}}]
    rows, failures = [], Counter()
    for topic in topics:
        members = sorted('paper:' + sources[n]['canonical_id'] for n in topic['source_ids'])
        measured = measure(corpus, members, coverage)
        result = assess(measured, config)
        if any(topic['eligible_gold'].values()) and not result['eligible']:
            failures.update(result['reasons'])
        rows.append({'topic_id': topic['id'], 'eligible_gold': topic['eligible_gold'],
                     'emergence_gold': topic['emergence'], 'work_ids': members, 'assessment': result})
    return {'split': split, 'membership': 'frozen gold oracle; temporal component only',
            'coverage': data['coverage'], 'source_input_sha256': hashlib.sha256(raw).hexdigest(),
            'method_sha256': hashlib.sha256(Path(assess.__code__.co_filename).read_bytes()).hexdigest(),
            'eligible_by_query': {query: {
                'gold': sum(r['eligible_gold'][query] for r in rows),
                'recovered': sum(r['eligible_gold'][query] and r['assessment']['eligible'] for r in rows)}
                for query in ('ai', 'rag')},
            'eligible_total': sum(r['assessment']['eligible'] for r in rows),
            'gold_rejection_reasons': dict(failures), 'topics': rows}


if __name__ == '__main__':
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument('--inputs', type=Path, required=True)
    parser.add_argument('--output', type=Path, required=True)
    args = parser.parse_args()
    root = Path(__file__).parent
    report = {split: evaluate(root, args.inputs, split) for split in ('development', 'validation')}
    with args.output.open('x') as stream:
        json.dump(report, stream, ensure_ascii=False, indent=2)
    print(json.dumps({split: {k: value[k] for k in ('eligible_by_query', 'eligible_total', 'gold_rejection_reasons')}
                      for split, value in report.items()}))
