"""Produce a complete immutable projection; incomplete topics stay outside TOP-15."""
from __future__ import annotations

from . import brief, candidates, emergence, ranking, relevance
from .manifest import canonical
from .temporal import measure


def compute(corpus: dict, job: dict, graph_address: str) -> dict:
    partition = candidates.cluster(corpus, graph_address)
    search, query_vector = relevance.prepare(corpus)
    by_vector = {work['canonical_id']: work for work in corpus['vectors']}
    result_candidates = []
    for group in partition['candidates']:
        measurements = measure(corpus, group['work_ids'], corpus['coverage_windows'])
        assessment = emergence.assess(measurements)
        fields, evidence, missing = brief.build(corpus, group)
        if not assessment['eligible']:
            missing.append('temporal_basis')
        representatives = list(dict.fromkeys(group['representative_work_ids'] + group['work_ids']))
        title = next((by_vector[work]['title'] for work in representatives if by_vector[work]['title'].strip()),
                     'Тема без названия')
        result_candidates.append({
            'id': group['candidate_id'], 'title': title,
            'work_ids': group['work_ids'], 'measurements': measurements, 'assessment': assessment,
            'brief': fields, 'evidence': evidence, 'complete_for_brief': not missing, 'missing_fields': missing,
            'relevance': relevance.assess(corpus, group['work_ids'], search, query_vector),
        })
    ranked, diversity = ranking.select(result_candidates, corpus['vectors'], corpus['input_sha256'])
    complete = len(ranked) == 15
    request = corpus['input']['request']
    result = {
        'version': 'analysis-result-v1', 'job_id': job['id'], 'input_run_id': corpus['input']['run_id'],
        'input_sha256': corpus['input_sha256'], 'method_sha256': job['method_sha256'],
        'query': request['query'], 'cutoff': request['cutoff'], 'candidates': result_candidates,
        'unassigned': partition['unassigned'], 'works': corpus['works'], 'relations': corpus['relations'],
        'coverage': {'windows': corpus['coverage_windows'], 'enrollment': corpus['coverage_enrollment'],
                     'read_status': corpus['coverage_read_status']},
        'search': search, 'limitations': corpus['input']['limitations'] + corpus['coverage_limitations'] + [
            'method_quality_not_frozen', 'representative_source_titles_pending_topic_label_evaluation',
            'extractive_claim_selection_pending_grounding_evaluation'],
        'ranking': {'ordered_ids': [c['id'] for c in ranked], 'diversity': diversity,
                    'completeness': 'complete' if complete else 'insufficient_coverage',
                    'reasons': [] if complete else ['fewer_than_15_relevant_complete_temporal_candidates']},
    }
    # The encoding itself rejects NaN/Infinity; the DB also verifies the complete
    # schema, source membership and exact quotes before accepting these bytes.
    if len(canonical(result)) > 16 << 20:
        raise ValueError('complete analysis result exceeds its publication budget')
    return result
