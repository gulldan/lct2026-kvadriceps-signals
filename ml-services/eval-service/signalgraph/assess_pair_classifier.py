"""Оценка полного эксперимента по рёбрам только на источниках; на holdout не настраиваться."""
from __future__ import annotations

import argparse
import itertools
import json
from pathlib import Path

from validate_benchmark import validate_split


def assess(root: Path, experiment: Path, split: str) -> dict:
    if split not in ('development', 'validation'):
        raise ValueError('holdout cannot tune pair classification')
    validate_split(root, split)
    pin = json.loads((experiment / split / 'pin.json').read_text())
    vectors = json.loads((experiment / f'{split}-vectors.json').read_text())['vectors']
    ids = sorted(v['canonical_id'] for v in vectors)
    by_id = {v['canonical_id']: v['vector'] for v in vectors}
    calls = sorted((experiment / split).glob('call-*.json'))
    if len(calls) != pin['request_count']:
        raise ValueError('partial experiment cannot establish quality')
    pairs = {}
    for path in calls:
        call = json.loads(path.read_text())
        if call.get('rejected') or 'pairs' not in call:
            raise ValueError('experiment contains a rejected response')
        a = call['input']['query']['id']
        for b, relation in call['pairs'].items():
            key = tuple(sorted((a, b)))
            if a not in by_id or b not in by_id or a == b or key in pairs:
                raise ValueError('invalid or duplicated pair')
            pairs[key] = relation
    if len(pairs) != pin['pair_count']:
        raise ValueError('missing pair judgments')
    topics = json.loads((root / f'annotations.{split}.json').read_text())['topics']
    for path in sorted((root / 'extensions').glob(f'annotations.{split}.*.json')):
        topics.extend(json.loads(path.read_text())['topics'])
    groups = {t['id']: {'paper:' + json.loads((root / 'sources' / f'{n}.json').read_text())['canonical_id']
                       for n in t['source_ids']} for t in topics}
    expected = {tuple(sorted(p)) for g in groups.values() for p in itertools.combinations(g, 2)}
    positive = {p for p, value in pairs.items() if value == 'same'}

    def metrics(found):
        correct = len(found & expected)
        precision = correct / len(found) if found else 0
        recall = correct / len(expected) if expected else 0
        return {'correct': correct, 'predicted': len(found), 'expected': len(expected),
                'precision': precision, 'recall': recall,
                'f1': 2*precision*recall/(precision+recall) if precision+recall else 0}

    results = {}
    for algorithm in ('components', 'complete_link'):
        clusters = [{i} for i in ids]
        while True:
            options = []
            for a, b in itertools.combinations(range(len(clusters)), 2):
                cross = [tuple(sorted((x, y))) for x in clusters[a] for y in clusters[b]]
                accepted = [p in positive for p in cross]
                allowed = any(accepted) if algorithm == 'components' else all(accepted)
                if allowed:
                    similarity = min(sum(x*y for x,y in zip(by_id[p[0]], by_id[p[1]])) for p in cross)
                    options.append((-similarity, tuple(sorted(clusters[a] | clusters[b])), a, b))
            if not options:
                break
            _, _, a, b = min(options)
            clusters[a] |= clusters[b]
            clusters.pop(b)
        candidates = [sorted(c) for c in clusters if len(c) > 1]
        found = {tuple(p) for c in candidates for p in itertools.combinations(c, 2)}
        recovered = {t['id'] for t in topics if t['eligible_gold']['rag'] and
                     any(set(c) <= groups[t['id']] for c in candidates)}
        results[algorithm] = {'pairs': metrics(found), 'candidates': candidates,
                              'rag_pure_multiwork_groups': sorted(recovered),
                              'unassigned': sorted(next(iter(c)) for c in clusters if len(c) == 1)}
    return {'split': split, 'pin': pin, 'retrieved_gold_pairs': len(expected & pairs.keys()),
            'direct_pairs': metrics(positive), 'uncertain_pairs': sum(r == 'uncertain' for r in pairs.values()),
            'partitions': results}


if __name__ == '__main__':
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument('--experiment', type=Path, required=True)
    parser.add_argument('--split', choices=['development', 'validation'], required=True)
    parser.add_argument('--output', type=Path, required=True)
    args = parser.parse_args()
    root = Path(__file__).parent
    result = assess(root, args.experiment, args.split)
    with args.output.open('x') as stream:
        json.dump(result, stream, ensure_ascii=False, indent=2)
    print(json.dumps({'split': args.split, 'retrieved_gold_pairs': result['retrieved_gold_pairs'],
                      'direct_pairs': result['direct_pairs'], 'partitions': {
                          a: {'pairs': p['pairs'], 'rag_pure_groups': len(p['rag_pure_multiwork_groups'])}
                          for a, p in result['partitions'].items()}}))
