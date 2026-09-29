"""Проверка кода исполнителя на отдельно замороженных контрольных случаях из реальных источников.

Эти случаи — не разметка ранжирования и не могут заполнить ТОП-15. Состязательные
мутации записываются явно; исходные снимки остаются неизменными.
"""
from __future__ import annotations

import copy
from dataclasses import asdict
import json
from pathlib import Path
import sys

from validate_benchmark import _freezes, digest

sys.path.insert(0, str(Path(__file__).parents[2] / 'signal-worker'))
from signalgraph.identity import canonical_works, counting_units
from signalgraph.relations import paper_repository_links
from signalgraph.temporal import measure, work_date


def evaluate(root: Path) -> dict:
    pinned = _freezes(root)
    name = 'controls/source-contracts.json'
    if name not in pinned:
        raise ValueError('real source controls are not frozen')
    data = json.loads((root / name).read_text())
    if data['assessor'] != 'Codex' or data['review_status'] != 'self_assessed_primary_sources':
        raise ValueError('source controls are not an independent human evaluation')
    originals = {r['id']: r for r in data['records']}
    results = []
    for case in data['cases']:
        records = [copy.deepcopy(originals[key]) for key in case['record_ids']]
        mutation = case.get('mutation')
        if mutation:
            record = next(r for r in records if r['id'] == mutation['record_id'])
            if 'display_title' in mutation:
                record['title'] = mutation['display_title']
            if 'publication' in mutation:
                record['source']['publication'] = mutation['publication']
            if 'stars' in mutation:
                record['source']['github']['stars'] = mutation['stars']
        works = canonical_works(records)
        units = counting_units(works)
        operation = case['operation']
        if operation == 'links':
            links = paper_repository_links(records, works, case['cutoff'])
            passed = len(links) == case['expected_links'] and all(
                not link.official and link.company is None and link.production_use is None for link in links)
            evidence = {'links': [asdict(link) for link in links]}
        elif operation == 'units':
            passed = all(units[key] == value for key, value in case['expected'].items())
            evidence = units
        elif operation == 'date':
            value = work_date(asdict(works[0]), {r['id']: r for r in records})
            passed, evidence = value == case['expected_date'], {'date': value}
        elif operation in ('popularity', 'popularity_invariance'):
            corpus = {'input': {'request': {'start': '2023-01-01', 'cutoff': data['frozen_at']}},
                      'records': records, 'works': [asdict(work) for work in works]}
            measurements = measure(corpus, [work.id for work in works])
            passed = (measurements['stars_history'] == 'unknown_before_first_observation'
                      and all(p['normalized_activity'] is None for p in measurements['series'])
                      and units['canonical_scientific_works'] == 0)
            evidence = {'stars_history': measurements['stars_history'], 'units': units,
                        'scientific_activity': None}
        else:
            raise ValueError('unknown source control operation')
        results.append({'id': case['id'], 'pass': passed, 'evidence': evidence})
    return {'kind': 'real_source_contracts_not_ranking_quality', 'assessor': data['assessor'],
            'annotations_sha256': digest(root / name), 'passed': sum(r['pass'] for r in results),
            'total': len(results), 'results': results}


if __name__ == '__main__':
    result = evaluate(Path(__file__).parent)
    print(json.dumps(result, ensure_ascii=False, indent=2))
    raise SystemExit(0 if result['passed'] == result['total'] else 1)
