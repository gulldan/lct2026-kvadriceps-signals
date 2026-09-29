"""Проверка замороженных входов бенчмарка, разметки и расширений, которые только дописываются.

Методы и модели здесь не импортируются. Проверка полного пула требует всех трёх
разбиений: отсутствующий holdout не проходит по умолчанию.
"""
from __future__ import annotations
import argparse
import hashlib
import json
from datetime import datetime
from pathlib import Path

SPLIT_CUTOFFS = {'development': '2023-12-31T23:59:59Z',
                 'validation': '2024-12-31T23:59:59Z', 'holdout': '2025-12-31T23:59:59Z'}
QUERIES = {'ai', 'rag', 'rare_control'}


def digest(path: Path) -> str:
    return hashlib.sha256(path.read_bytes()).hexdigest()


def _path(root: Path, name: str) -> Path:
    path = (root / name).resolve()
    if not path.is_relative_to(root.resolve()) or not path.is_file():
        raise ValueError('artifact outside benchmark or absent')
    return path


def _freezes(root: Path, splits: tuple[str, ...] | None = None) -> set[str]:
    pinned = set()
    for path in [*root.glob('freeze.*.json'), *root.glob('extensions/freeze.*.json')]:
        if splits is not None and path.name.split('.')[1] not in splits:
            continue
        freeze = json.loads(path.read_text())
        if freeze.get('frozen_before_method_implementation') is not True:
            raise ValueError('annotations were not frozen before method implementation')
        for name, expected in freeze['sha256'].items():
            if digest(_path(root, name)) != expected:
                raise ValueError(f'frozen annotation artifact changed: {name}')
            pinned.add(name)
    return pinned


def _sources(root: Path, inputs: dict, assigned: dict, pinned: set[str]) -> dict:
    sources = {}
    split, cutoff = inputs['split'], inputs['cutoff']
    if split not in SPLIT_CUTOFFS or cutoff != SPLIT_CUTOFFS[split]:
        raise ValueError('split/cutoff mismatch')
    if len(set(inputs['source_ids'])) != len(inputs['source_ids']):
        raise ValueError('duplicate source in input')
    for native in inputs['source_ids']:
        name = f'sources/{native}.json'
        source = json.loads(_path(root, name).read_text())
        raw = f"sources/{source['raw_file']}"
        if name not in pinned or raw not in pinned:
            raise ValueError('source is not frozen')
        if source['native_id'] != native or digest(_path(root, raw)) != source['raw_sha256']:
            raise ValueError('source identity/raw hash mismatch')
        if hashlib.sha256(source['text'].encode()).hexdigest() != source['text_sha256']:
            raise ValueError('source text changed')
        for field in ('published_at', 'available_at', 'version_updated_at'):
            if datetime.fromisoformat(source[field].replace('Z', '+00:00')) > datetime.fromisoformat(cutoff.replace('Z', '+00:00')):
                raise ValueError('future source revision in historical input')
        if source['canonical_id'] in assigned and assigned[source['canonical_id']] != split:
            raise ValueError('canonical work leaked between splits')
        assigned[source['canonical_id']] = split
        sources[native] = source
    return sources


def _topic(topic: dict, split: str, sources: dict) -> dict:
    if topic['split'] != split or topic['cutoff'] != SPLIT_CUTOFFS[split]:
        raise ValueError('mismatched annotation boundary')
    if topic['assessor'] != 'Codex' or topic['review_status'] != 'self_assessed_primary_sources':
        raise ValueError('self assessment cannot masquerade as human review')
    if not topic['decision_basis'] or not topic['counterevidence']:
        raise ValueError('annotation lacks its rationale and limitations')
    if not set(topic['source_ids']) <= set(sources):
        raise ValueError('annotation references source outside split')
    if set(topic['relevance']) != QUERIES or set(topic['eligible_gold']) != QUERIES:
        raise ValueError('missing query judgment')
    if topic['emergence'] not in ('emerging', 'mature', 'noise', 'disputed', 'insufficient_evidence'):
        raise ValueError('unknown emergence label')
    for field in ('problem', 'advantage'):
        anchor = topic['brief_support'][field]
        if anchor is None:
            if any(topic['eligible_gold'].values()) or not topic.get('missing_evidence_reason'):
                raise ValueError('eligible topic has unknown evidence')
            continue
        if anchor['source_id'] not in topic['source_ids']:
            raise ValueError('evidence outside topic membership')
        source = sources[anchor['source_id']]
        start, end = anchor['char_start'], anchor['char_end']
        if not 0 <= start < end <= len(source['text']):
            raise ValueError('invalid evidence offsets')
        if source['text'][start:end] != anchor['quote'] or source['text_sha256'] != anchor['text_sha256']:
            raise ValueError('evidence quote/hash does not match pinned primary text')
    example = topic['brief_support']['example']
    if example and (example['source_id'] not in topic['source_ids'] or example['kind'] != 'research'
                    or example['label'] != sources[example['source_id']]['title']):
        raise ValueError('unsupported research example')
    if any(topic['eligible_gold'].values()) and not example:
        raise ValueError('eligible topic lacks a supported example')
    if topic['emergence'] == 'emerging':
        works = {sources[key]['canonical_id'] for key in topic['source_ids']}
        periods = {(sources[key]['published_at'][:4], (int(sources[key]['published_at'][5:7])-1)//3)
                   for key in topic['source_ids']}
        if len(works) < 2 or len(periods) < 2:
            raise ValueError('emerging label lacks multiple works and periods')
    for query, value in topic['eligible_gold'].items():
        if type(value) is not bool or topic['relevance'][query] not in (0, 1, 2):
            raise ValueError('invalid relevance/eligibility judgment')
        if value and (topic['relevance'][query] != 2 or topic['emergence'] != 'emerging'):
            raise ValueError('inconsistent documentary label')
    return topic['eligible_gold']


def validate(root: Path, require_full_pool: bool = False, *,
             splits: tuple[str, ...] | None = None) -> dict:
    if splits is not None:
        if not splits or len(set(splits)) != len(splits) or not set(splits) <= SPLIT_CUTOFFS.keys():
            raise ValueError('invalid split selection')
        if require_full_pool:
            raise ValueError('a selected split cannot establish the full-pool gate')
    protocol = json.loads((root / 'protocol-freeze.json').read_text())
    if digest(_path(root, protocol['file'])) != protocol['sha256']:
        raise ValueError('frozen protocol changed')
    pinned = _freezes(root, splits)
    assigned, summary, groups, sources = {}, {}, {}, {}
    bundles = []
    for path in sorted(root.glob('annotations.*.json')):
        split = path.name.split('.')[1]
        if splits is not None and split not in splits:
            continue
        input_name = f'inputs.{split}.json'
        if path.name not in pinned or input_name not in pinned:
            raise ValueError('base annotation or input is not frozen')
        bundles.append((json.loads(path.read_text()), json.loads(_path(root, input_name).read_text())))
    for path in sorted(root.glob('extensions/annotations.*.json')):
        if splits is not None and path.name.split('.')[1] not in splits:
            continue
        if str(path.relative_to(root)) not in pinned:
            raise ValueError('annotation extension is not frozen')
        data = json.loads(path.read_text())
        bundles.append((data, data))
    for data, inputs in bundles:
        split = inputs['split']
        if data['cutoff'] != inputs['cutoff'] or set(data['queries']) != QUERIES:
            raise ValueError('split/query mismatch')
        sources.setdefault(split, {}).update(_sources(root, inputs, assigned, pinned))
        groups.setdefault(split, set())
        current = summary.setdefault(split, {'works': 0, 'topics': 0, 'eligible': dict.fromkeys(sorted(QUERIES), 0)})
        for topic in data['topics']:
            group = topic['distinctness_group']
            if group in groups[split]:
                raise ValueError('duplicate distinctness group in split')
            groups[split].add(group)
            for evidence in topic.get('decision_sources', []):
                if (evidence['source_id'] not in topic['source_ids'] or evidence['file'] not in pinned
                        or digest(_path(root, evidence['file'])) != evidence['sha256']
                        or not evidence['locator'] or not evidence['quote']):
                    raise ValueError('decision source is not pinned to the annotated work')
            for query, value in _topic(topic, split, sources[split]).items():
                current['eligible'][query] += int(value)
        current['works'], current['topics'] = len(sources[split]), len(groups[split])
        current['sufficient_pool'] = {q: n >= 15 for q, n in current['eligible'].items()}
    missing = sorted(set(SPLIT_CUTOFFS if splits is None else splits) - set(summary))
    insufficient = [s for s, row in summary.items() if not all(row['sufficient_pool'][q] for q in ('ai', 'rag'))]
    if require_full_pool and (missing or insufficient):
        raise ValueError(f'normal TOP-15 gate blocked: missing splits={missing}, inadequate pools={insufficient}')
    return {'assessor': 'Codex; self assessment, no human validation', 'splits': summary,
            'missing_splits': missing,
            'full_pool_gate': 'not_checked' if splits is not None else 'blocked' if missing or insufficient else 'pass'}


def validate_split(root: Path, split: str) -> dict:
    """Только это разбиение, не открывая закреплений и разметки других.

    Проверка полного пула и совпадения между разбиениями — отдельный шаг до настройки.
    """
    result = validate(root, splits=(split,))
    if result['missing_splits'] or not all(result['splits'][split]['sufficient_pool'][q]
                                         for q in ('ai', 'rag')):
        raise ValueError('selected split lacks a sufficient TOP-15 pool')
    return result


if __name__ == '__main__':
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument('--root', type=Path, default=Path(__file__).parent)
    parser.add_argument('--require-full-pool', action='store_true')
    args = parser.parse_args()
    print(json.dumps(validate(args.root, args.require_full_pool), ensure_ascii=False, indent=2))
