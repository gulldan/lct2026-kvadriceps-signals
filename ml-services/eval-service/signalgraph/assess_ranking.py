"""Оценка готовых прогонов ранжирования по замороженной документальной разметке.

Ранжировщик разметку не получает. Неизвестная или смешанная принадлежность в плюс
не идёт. Точные якоря и отдельная проверка документальной опоры считаются раздельно.
"""
from __future__ import annotations

import argparse
from collections import Counter
import hashlib
import json
import math
from pathlib import Path
import tempfile

from prepare_inputs import export
from validate_benchmark import validate_split
from holdout_gate import verify


def gold_gain(topic: dict, query: str) -> int:
    if topic['eligible_gold'][query]:
        return 3
    if topic['relevance'][query] >= 1 and all(topic['brief_support'].values()) and \
            (topic['emergence'] == 'disputed' or topic['relevance'][query] == 1):
        return 1
    return 0


def assess(root: Path, run_path: Path, freeze: Path | None = None,
           freeze_sha256: str | None = None) -> dict:
    raw = run_path.read_bytes()
    run = json.loads(raw)
    split = run['split']
    if split not in ('development', 'validation', 'holdout'):
        raise ValueError('unknown split')
    if split == 'holdout':
        pin = verify(freeze, freeze_sha256, benchmark=True)
        if run.get('candidate_freeze_sha256') != freeze_sha256 or run['method'] != pin['method']:
            raise ValueError('holdout run differs from candidate freeze')
    validate_split(root, split)
    with tempfile.TemporaryDirectory() as directory:
        expected = export(root, Path(directory), split, freeze, freeze_sha256)
    if run['source_input_sha256'] != expected['sha256']:
        raise ValueError('run does not refer to the frozen source-only manifest')
    labels = json.loads((root/f'annotations.{split}.json').read_text())['topics']
    for path in sorted(root.glob(f'extensions/annotations.{split}.*.json')):
        labels.extend(json.loads(path.read_text())['topics'])
    work_group, gold = {}, {}
    for topic in labels:
        gold[topic['distinctness_group']] = topic
        for native in topic['source_ids']:
            source = json.loads((root/'sources'/f'{native}.json').read_text())
            work = 'paper:'+source['canonical_id']
            if work in work_group and work_group[work] != topic['distinctness_group']:
                raise ValueError('ambiguous gold membership')
            work_group[work] = topic['distinctness_group']
    report = {}
    for query, result in run['queries'].items():
        by_id = {c['id']:c for c in result['candidates']}
        ranked = {}
        ideal = sorted((gold_gain(t, query) for t in labels), reverse=True)[:15]
        idcg = sum(g/math.log2(i+2) for i,g in enumerate(ideal))
        for system, ranking in result['systems'].items():
            seen, rows = set(), []
            for identifier in ranking['ordered_ids']:
                c = by_id[identifier]
                groups = {work_group.get(w) for w in c['work_ids']}
                group = next(iter(groups)) if len(groups) == 1 and None not in groups else None
                label = gold.get(group)
                eligible = bool(label and label['eligible_gold'][query] and group not in seen)
                gain = gold_gain(label, query) if label and group not in seen else 0
                duplicate = group is not None and group in seen
                if group is not None:
                    seen.add(group)
                anchors = Counter()
                for field in ('problem','advantage','example'):
                    evidence = next((e for e in c['evidence'] if e['field'] == field), None)
                    support = label['brief_support'][field] if label else None
                    if evidence is None:
                        anchors['missing'] += 1
                    elif field != 'example' and support and evidence['source_revision'] == support['source_id'] \
                            and support['quote'] in evidence['quote']:
                        anchors['contains_frozen_anchor'] += 1
                    else:
                        anchors['semantic_review_required'] += 1
                rows.append({'candidate_id': identifier, 'gold_group': group, 'mixed_or_unknown': group is None,
                             'duplicate': duplicate, 'eligible_gold': eligible, 'gain': gain,
                             'mandatory_claims': dict(anchors)})
            positives = sum(r['eligible_gold'] for r in rows)
            dcg = sum(r['gain']/math.log2(i+2) for i,r in enumerate(rows))
            ranked[system] = {'p_at_15': positives/15, 'true_positive': positives, 'denominator': 15,
                              'ndcg_at_15': dcg/idcg if idcg else 0., 'filled': len(rows),
                              'false_or_unknown': len(rows)-positives, 'distinct_gold_groups': len(seen),
                              'completeness': ranking['completeness'], 'rows': rows}
        baseline_name = 'keyword_count' if 'keyword_count' in ranked else 'keyword_count_shared_clusters'
        current, baseline = ranked['temporal_mmr'], ranked[baseline_name]
        eligible_pool = sum(t['eligible_gold'][query] for t in labels)
        gates = {'precision': current['p_at_15'] >= .8,
                 'ndcg': current['ndcg_at_15'] >= .8,
                 'fill': current['filled'] == 15,
                 'distinctness': current['distinct_gold_groups'] == 15,
                 'not_below_baseline': current['p_at_15'] >= baseline['p_at_15']}
        if query == 'rare_control':
            gates = {'honest_insufficient_coverage': current['completeness'] == 'insufficient_coverage',
                     'no_false_topics': current['filled'] == 0}
        report[query] = {'eligible_pool': eligible_pool, 'sufficient_pool': eligible_pool >= 15,
                         'baseline':baseline_name,
                         'systems': ranked, 'development_gates': gates, 'protocol_gates': gates,
                         'ndcg_difference_from_baseline': current['ndcg_at_15']-baseline['ndcg_at_15']}
    return {'version': 'ranking-documentary-assessment-v1', 'split': split,
            'run_sha256': hashlib.sha256(raw).hexdigest(), 'method_code_sha256': run['method']['code_sha256'],
            'seconds': run['seconds'], 'queries': report,
            'quality_status': 'not_frozen', 'holdout_opened': split == 'holdout',
            'candidate_freeze_sha256': freeze_sha256 if split == 'holdout' else None,
            'independence': 'Codex primary-source documentary labels; independent expert adjudication absent',
            'support_precision': 'exact anchors reported separately; documentary claim support has not yet been adjudicated',
            'acceptance': 'not accepted; report failed protocol gates and unresolved claim support; independence is a limitation, not an invented human-approval gate'}


if __name__ == '__main__':
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument('--run', type=Path, required=True)
    parser.add_argument('--output', type=Path, required=True)
    parser.add_argument('--freeze', type=Path)
    parser.add_argument('--freeze-sha256')
    args = parser.parse_args()
    report = assess(Path(__file__).parent, args.run, args.freeze, args.freeze_sha256)
    with args.output.open('x') as stream:
        json.dump(report, stream, ensure_ascii=False, indent=2)
    print(json.dumps({q:{'p_at_15':r['systems']['temporal_mmr']['p_at_15'],
                         'filled':r['systems']['temporal_mmr']['filled'], 'gates':r['development_gates']}
                      for q,r in report['queries'].items()}))
