"""Слепой пакет для внешней проверки; замороженная эталонная разметка не меняется.

Пакет — запрос документальных решений, а не подпись и не самостоятельная
независимая проверка. Источники и разметка holdout сюда не выгружаются.
"""
from __future__ import annotations

import argparse
import hashlib
import json
from pathlib import Path

from validate_benchmark import validate_split


def create(root: Path, method_path: Path) -> dict:
    cases = []
    for split in ('development', 'validation'):
        validate_split(root, split)
        bundles = [root/f'annotations.{split}.json', *sorted(root.glob(f'extensions/annotations.{split}.*.json'))]
        for bundle in bundles:
            for topic in json.loads(bundle.read_text())['topics']:
                sources = [json.loads((root/'sources'/f'{native}.json').read_text()) for native in topic['source_ids']]
                cases.append({'id': topic['id'], 'split': split, 'cutoff': topic['cutoff'],
                              'proposed_subject': topic['label'],
                              'sources': [{key: source[key] for key in ('native_id','canonical_id','title','url',
                                                                       'published_at','available_at','text_sha256')}
                                          for source in sources],
                              'independent_decision': {'relevance': {'ai':None,'rag':None,'rare_control':None},
                                                       'emergence':None, 'distinctness_group':None,
                                                       'problem':None,'advantage':None,'example':None,
                                                       'independence_of_works':None,'counterevidence':None,
                                                       'rationale':None}})
    return {'version':'signalgraph-independent-review-request-v1', 'status':'awaiting_external_review',
            'method_sha256':hashlib.sha256(method_path.read_bytes()).hexdigest(),
            'protocol_sha256':hashlib.sha256((root/'PROTOCOL.md').read_bytes()).hexdigest(),
            'reviewer':{'name':None,'affiliation':None,'independent_of_implementation':None,
                        'reviewed_at':None,'attestation':None},
            'instructions':[
                'Оцените первичные ревизии, доступные к cutoff, без просмотра численных score метода и прежних labels.',
                'Для каждого обязательного поля укажите native_id, text_sha256, точный locator и основание; неизвестное оставьте null.',
                'Отдельно решите, независимы ли работы и относятся ли они к одной технической теме; общие названия недостаточны.',
                'В сравнении сохраните baseline, dataset, единицы, условия и характер claimed/measured/planned.',
                'Не меняйте frozen annotations: разногласия оформляются отдельной версией benchmark и новым протоколом.',
                'До заморозки добавьте положительные не-IT области и затухшие всплески; текущий AI/RAG набор этого не доказывает.',
                'Holdout не входит в этот пакет: его открывают после фиксации кода, конфигурации и параметров по итогам development/validation.',
                'Вторая независимая экспертиза улучшает достоверность, но протокол v1 не требует подписи человека до запуска holdout.',
            ], 'cases':cases,
            'release_gate':{'quality_status':'not_frozen','independence_limitation':'no independent expert adjudication',
                           'required':[
                            'non_it_and_decayed_burst_coverage','fixed_temporal_population',
                            'passed_preregistered_holdout','15_distinct_supported_topics_per_sufficient_query']}}


if __name__ == '__main__':
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument('--method',type=Path,required=True)
    parser.add_argument('--output',type=Path,required=True)
    args = parser.parse_args()
    packet = create(Path(__file__).parent,args.method)
    with args.output.open('x') as stream:
        json.dump(packet,stream,ensure_ascii=False,indent=2)
    print(json.dumps({'status':packet['status'],'cases':len(packet['cases']),
                      'holdout_exported':False,'method_sha256':packet['method_sha256']}))
