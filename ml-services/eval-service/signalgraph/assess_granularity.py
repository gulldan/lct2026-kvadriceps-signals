"""Оценка графовых экспериментов только на development/validation; разметка не меняется."""
from __future__ import annotations

import argparse
import itertools
import json
from pathlib import Path

from validate_benchmark import validate_split


def assess(root: Path, directory: Path, split: str, variant: str) -> list[dict]:
    if split not in ('development', 'validation'):
        raise ValueError('holdout cannot be used to choose graph granularity')
    validate_split(root, split)
    topics = json.loads((root / f'annotations.{split}.json').read_text())['topics']
    for path in sorted(root.glob(f'extensions/annotations.{split}.*.json')):
        topics += json.loads(path.read_text())['topics']
    sources = json.loads((directory / f'{split}.json').read_text())['sources']
    canonical = {s['native_id']: 'paper:' + s['canonical_id'] for s in sources}
    groups = {t['id']: {canonical[n] for n in t['source_ids']} for t in topics}
    expected = {tuple(sorted(pair)) for members in groups.values() for pair in itertools.combinations(members, 2)}
    partitions = json.loads((directory / f'{split}-{variant}partitions.json').read_text())['partitions']
    results = []
    for partition in partitions:
        found = {tuple(sorted(pair)) for c in partition['candidates'] for pair in itertools.combinations(c['work_ids'], 2)}
        correct = len(found & expected)
        precision = correct / len(found) if found else 0.
        recall = correct / len(expected) if expected else 0.
        recovered = {t['id'] for t in topics if t['eligible_gold']['rag'] and any(
            set(c['work_ids']) <= groups[t['id']] and len(c['work_ids']) >= 2 for c in partition['candidates'])}
        results.append({'config': partition['config'], 'representation': partition.get('representation'),
                        'centering_alpha': partition.get('alpha'), 'candidates': len(partition['candidates']),
                        'pair_correct': correct, 'pair_predicted': len(found), 'pair_expected': len(expected),
                        'pair_precision': precision, 'pair_recall': recall,
                        'pair_f1': 2*precision*recall/(precision+recall) if precision+recall else 0.,
                        'rag_pure_multiwork_groups': len(recovered), 'unassigned': len(partition['unassigned'])})
    return sorted(results, key=lambda r: (-r['pair_f1'], -r['rag_pure_multiwork_groups'],
                                         r['config']['knn_k'], r['config']['resolution']))


if __name__ == '__main__':
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument('--directory', type=Path, required=True)
    parser.add_argument('--variant', choices=('', 'strict-', 'fine-', 'focus-', 'center-'), default='strict-')
    args = parser.parse_args()
    report = {split: assess(Path(__file__).parent, args.directory, split, args.variant)
              for split in ('development', 'validation')}
    path = args.directory / f'granularity-{args.variant}assessment.json'
    with path.open('x') as stream:
        json.dump(report, stream, indent=2)
    print(json.dumps({split: rows[:3] for split, rows in report.items()}))
