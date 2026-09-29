"""Ранжирование по одним источникам и фиксированные абляции; holdout требует заморозки кандидата.

Процесс никогда не читает разметку. Получившийся неизменяемый файл оценивается
потом в assess_ranking.py. Ни публикации, ни загрузки источников, ни активации.
"""
from __future__ import annotations

import argparse
from datetime import datetime, timedelta
import hashlib
import json
from pathlib import Path
import time
import uuid

from signalgraph import brief, candidates, emergence, method, ranking, relevance
from signalgraph.manifest import canonical, digest
import keyword_baseline
from holdout_gate import verify, claim_run
from signalgraph.temporal import measure

QUERIES = {'ai': 'технологии в ИИ', 'rag': 'методы retrieval-augmented generation',
           'rare_control': 'retrieval-augmented generation для измерения квантовой гравитации'}


def materialize(source_path: Path, vector_path: Path, freeze: Path | None = None,
                freeze_sha256: str | None = None) -> dict:
    raw = source_path.read_bytes()
    data, embeddings = json.loads(raw), json.loads(vector_path.read_bytes())
    if data['split'] not in ('development', 'validation', 'holdout') or embeddings['split'] != data['split']:
        raise ValueError('source/vector split differs or is unknown')
    if data['split'] == 'holdout':
        pin = verify(freeze, freeze_sha256)
        if data.get('candidate_freeze_sha256') != freeze_sha256 or pin['method'] != method.definition() or \
                embeddings['model'] != pin['embedding']['model'] or QUERIES != pin['queries'] or \
                embeddings.get('instruction', '') != pin['embedding']['instruction'] or \
                any(row.get('contribution_span') is not None for row in embeddings['vectors']) or \
                pin['baseline'] != {'version': keyword_baseline.VERSION, 'config': keyword_baseline.CONFIG}:
            raise ValueError('holdout inputs, method, model or baseline differ from candidate freeze')
    if embeddings['source_input_sha256'] != digest(raw):
        raise ValueError('vectors refer to different source inputs')
    sources = data['sources']
    if {v['canonical_id'] for v in embeddings['vectors']} != {'paper:'+s['canonical_id'] for s in sources}:
        raise ValueError('source/vector membership differs')
    records, works = [], []
    cutoff = datetime.fromisoformat(data['cutoff'].replace('Z', '+00:00'))
    for source in sources:
        if hashlib.sha256(source['text'].encode()).hexdigest() != source['text_sha256']:
            raise ValueError('source text changed')
        if any(datetime.fromisoformat(source[f].replace('Z', '+00:00')) > cutoff
               for f in ('published_at','available_at','version_updated_at')):
            raise ValueError('source crosses temporal cutoff')
        doc = str(uuid.uuid5(uuid.NAMESPACE_URL, source['url']))
        records.append({'id': doc, 'snapshot_id': str(uuid.uuid5(uuid.NAMESPACE_URL, source['text_sha256'])),
                        'text': source['text'], 'text_sha256': source['text_sha256'], 'representation': 'abstract',
                        'source': {'revision': source['native_id'], 'provider': source['provider'],
                                   'publication': {'value': source['published_at'][:10], 'precision': 'day',
                                                   'basis': 'pinned primary version publication'}}})
        works.append({'id': 'paper:'+source['canonical_id'], 'kind': 'paper', 'document_ids': [doc]})
    start = min(s['published_at'][:4] for s in sources)+'-01-01'
    coverage = [{'start': start, 'end_exclusive': (cutoff+timedelta(seconds=1)).date().isoformat(),
                 'complete': True, 'work_ids': [w['id'] for w in works],
                 'basis': {'kind': data['coverage'], 'source_input_sha256': digest(raw)}}]
    return {'input_sha256': digest(raw), 'input': {'run_id': 'benchmark-'+data['split'],
            'model': embeddings['model'], 'request': {'start': start, 'cutoff': data['cutoff'], 'query': ''}},
            'records': records, 'works': works, 'relations': [], 'vectors': embeddings['vectors'],
            'coverage_windows': coverage, 'coverage_enrollment': None}


def run(source_path: Path, vector_path: Path, graph: str, freeze: Path | None = None,
        freeze_sha256: str | None = None) -> dict:
    corpus = materialize(source_path, vector_path, freeze, freeze_sha256)
    holdout = json.loads(source_path.read_bytes())['split'] == 'holdout'
    receipt = claim_run(freeze, freeze_sha256, corpus['input_sha256'], digest(vector_path.read_bytes())) if holdout else None
    started = time.monotonic()
    partition = candidates.cluster(corpus, graph)
    keyword_partition = keyword_baseline.cluster(json.loads(source_path.read_bytes())['sources'])
    base = []
    for group in partition['candidates']:
        measured = measure(corpus, group['work_ids'], corpus['coverage_windows'])
        assessed = emergence.assess(measured)
        fields, evidence, missing = brief.build(corpus, group)
        base.append({'id': group['candidate_id'], 'work_ids': group['work_ids'], 'assessment': assessed,
                     'brief': fields, 'evidence': evidence, 'missing_fields': missing,
                     'complete_for_brief': not missing and assessed['eligible']})
    keyword_rows = []
    for group in keyword_partition['candidates']:
        fields, evidence, missing = brief.build(corpus, group)
        keyword_rows.append({'id':group['candidate_id'], 'work_ids':group['work_ids'],
                             'brief':fields, 'evidence':evidence, 'missing_fields':missing})
    queries = {}
    for name, query in QUERIES.items():
        corpus['input']['request']['query'] = query
        context, vector = relevance.prepare(corpus)
        rows = [{**c, 'relevance': relevance.assess(corpus, c['work_ids'], context, vector)} for c in base]
        mmr, diversity = ranking.select(rows, corpus['vectors'], corpus['input_sha256'])
        relevant = [c for c in rows if c['relevance']['relevant']]
        lexicographic = sorted((c for c in relevant if c['complete_for_brief']),
                               key=lambda c: (tuple(-n for n in c['assessment']['order']), c['id']))[:15]
        # Общие кластеры и извлечение отделяют ранжирование от формирования тем.
        # Абляция снимает только временной отбор, но не пропуск обязательных полей.
        without_temporal = sorted((c for c in relevant if not c['missing_fields']),
                                  key=lambda c: (-len(c['work_ids']), c['id']))[:15]
        keyword = sorted((c for c in rows if c['relevance']['lexical_overlap'] >= .6 and not c['missing_fields']),
                         key=lambda c: (-c['relevance']['lexical_overlap'], -len(c['work_ids']), c['id']))[:15]
        lexical_context = {**context, 'mode':'lexical_only'}
        independent_keyword_rows = [{**c, 'relevance':relevance.assess(corpus,c['work_ids'],lexical_context,None)}
                                    for c in keyword_rows]
        independent_keyword = sorted((c for c in independent_keyword_rows if not c['missing_fields']
                                      and c['relevance']['lexical_overlap'] >= keyword_baseline.CONFIG['query_overlap']),
                                     key=lambda c:(-len(c['work_ids']),c['id']))[:15]
        systems = {'temporal_mmr': mmr, 'without_mmr': lexicographic,
                   'without_temporal': without_temporal, 'keyword_count_shared_clusters': keyword,
                   'keyword_count': independent_keyword}
        queries[name] = {'query': query, 'search': context, 'candidates': rows+independent_keyword_rows, 'diversity': diversity,
                         'systems': {key: {'ordered_ids': [c['id'] for c in values],
                                          'completeness': 'complete' if len(values) == 15 else 'insufficient_coverage'}
                                     for key, values in systems.items()}}
    return {'version': 'ranking-holdout-run-v1' if holdout else 'ranking-development-run-v1',
            'split': json.loads(source_path.read_bytes())['split'],
            'source_input_sha256': corpus['input_sha256'], 'vectors_sha256': digest(vector_path.read_bytes()),
            'method': method.definition(), 'partition': partition, 'queries': queries,
            'keyword_partition':keyword_partition,
            'evaluator_sha256':{p.name:digest(p.read_bytes()) for p in (Path(__file__),Path(keyword_baseline.__file__))},
            'seconds': round(time.monotonic()-started, 3),
            'coverage': 'complete curated benchmark pool, not provider census',
            'baseline_scope': 'independent TF-IDF lexical clusters and count order; common source-bound brief gate',
            'holdout_opened': holdout, 'candidate_freeze_sha256': freeze_sha256 if holdout else None,
            'holdout_receipt': receipt}


if __name__ == '__main__':
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument('--sources', type=Path, required=True)
    parser.add_argument('--vectors', type=Path, required=True)
    parser.add_argument('--output', type=Path, required=True)
    parser.add_argument('--graph', default='graph-compute:9093')
    parser.add_argument('--freeze', type=Path)
    parser.add_argument('--freeze-sha256')
    parser.add_argument('--development-config', type=Path,
                        help='Development-only proposed graph parameters; included in the method artifact')
    args = parser.parse_args()
    if args.output.exists():
        raise ValueError('ranking output is immutable; do not recompute an existing artifact')
    if args.development_config:
        if json.loads(args.sources.read_bytes())['split'] != 'development':
            raise ValueError('configuration exploration is restricted to development')
        proposal = json.loads(args.development_config.read_bytes())
        if set(proposal) != set(candidates.CONFIG):
            raise ValueError('incomplete graph configuration proposal')
        candidates.CONFIG.update(proposal)
    result = run(args.sources, args.vectors, args.graph, args.freeze, args.freeze_sha256)
    with args.output.open('xb') as stream:
        stream.write(canonical(result))
    print(json.dumps({'split': result['split'], 'seconds': result['seconds'], 'clusters': len(result['partition']['candidates']),
                      'output_sha256': digest(args.output.read_bytes()),
                      'queries': {k: {s: len(r['ordered_ids']) for s,r in q['systems'].items()}
                                  for k,q in result['queries'].items()}}))
