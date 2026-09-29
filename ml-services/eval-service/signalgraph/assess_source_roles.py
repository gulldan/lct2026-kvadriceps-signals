"""Оценка закреплённого отбора источников на эталонных группах, отдельно от кластеризации."""
from __future__ import annotations

import argparse
from collections import Counter
import hashlib
import json
from pathlib import Path

from validate_benchmark import validate_split


def assess(root: Path, experiment: Path, split: str) -> dict:
    if split not in ('development', 'validation'):
        raise ValueError('holdout cannot tune source selection')
    validate_split(root, split)
    raw = (experiment / f'{split}.json').read_bytes()
    source_input = json.loads(raw)
    sources = {s['native_id']: s for s in source_input['sources']}
    pin = json.loads((experiment / split / 'pin.json').read_text())
    if pin['source_input_sha256'] != hashlib.sha256(raw).hexdigest():
        raise ValueError('experiment input changed')
    selections, failures = {}, []
    for path in sorted((experiment / split).glob('batch-*.json')):
        batch = json.loads(path.read_text())
        if not batch.get('extractions'):
            failures.append(path.name)
        for native, fields in batch.get('extractions', {}).items():
            if native not in sources or native in selections:
                raise ValueError('foreign or duplicate source selection')
            source = sources[native]
            for field in fields.values():
                if field is None:
                    continue
                if (field['text_sha256'] != source['text_sha256'] or
                    not 0 <= field['start'] < field['end'] <= len(source['text']) or
                    source['text'][field['start']:field['end']] != field['quote']):
                    raise ValueError('selected span differs from frozen source')
            selections[native] = fields
    if len(selections) != len(sources) or failures:
        raise ValueError('incomplete experiment; do not score a convenient subset')
    topics = json.loads((root / f'annotations.{split}.json').read_text())['topics']
    for path in sorted((root / 'extensions').glob(f'annotations.{split}.*.json')):
        topics.extend(json.loads(path.read_text())['topics'])
    rows, counts = [], Counter()
    for topic in topics:
        if not any(topic['eligible_gold'].values()):
            continue
        fields = {}
        for field, role in (('problem', 'problem'), ('advantage', 'advantage'), ('example', 'mechanism')):
            actual = next(({'source_id': native, **selections[native][role]}
                           for native in sorted(topic['source_ids']) if selections[native][role]), None)
            gold = topic['brief_support'][field]
            status = 'unknown'
            if actual:
                status = ('contains_frozen_anchor' if field != 'example' and
                          actual['source_id'] == gold['source_id'] and gold['quote'] in actual['quote']
                          else 'semantic_review_required')
            counts[field + ':' + status] += 1
            fields[field] = {'actual': actual, 'gold': gold, 'status': status}
        rows.append({'topic_id': topic['id'], 'label': topic['label'], 'fields': fields})
    return {'split': split, 'source_count': len(sources), 'membership': 'frozen gold oracle; component only',
            'selection': 'first non-null source role in native-ID order; example uses paper mechanism',
            'experiment_pin': pin, 'counts': dict(counts), 'topics': rows,
            'semantic_precision': 'not inferred from span validity or anchor agreement'}


if __name__ == '__main__':
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument('--experiment', type=Path, required=True)
    parser.add_argument('--output', type=Path, required=True)
    args = parser.parse_args()
    root = Path(__file__).parent
    report = {split: assess(root, args.experiment, split) for split in ('development', 'validation')}
    with args.output.open('x') as stream:
        json.dump(report, stream, ensure_ascii=False, indent=2)
    print(json.dumps({s: {'sources': r['source_count'], 'counts': r['counts']} for s, r in report.items()}))
