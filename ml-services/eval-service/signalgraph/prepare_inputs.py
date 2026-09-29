"""Выгрузка только замороженных входов-источников; оценки в рабочую область метода не попадают."""
from __future__ import annotations

import argparse
import hashlib
import json
from pathlib import Path

from validate_benchmark import SPLIT_CUTOFFS, validate_split
from holdout_gate import verify


def export(root: Path, destination: Path, split: str, freeze: Path | None = None,
           freeze_sha256: str | None = None) -> dict:
    if split == 'holdout':
        verify(freeze, freeze_sha256, root=root.resolve().parents[2], benchmark=True)
    validate_split(root, split)
    base = json.loads((root / f'inputs.{split}.json').read_text())
    source_ids = set(base['source_ids'])
    # Расширения хранят id источников вместе с разметкой: читаются только поля
    # манифеста, темы, членство и оценки не копируются.
    for path in sorted(root.glob(f'extensions/annotations.{split}.*.json')):
        source_ids.update(json.loads(path.read_text())['source_ids'])
    sources = []
    for native in sorted(source_ids):
        source = json.loads((root / 'sources' / f'{native}.json').read_text())
        sources.append({key: source[key] for key in (
            'native_id', 'canonical_id', 'title', 'text', 'authors', 'text_sha256',
            'published_at', 'available_at', 'available_basis', 'version_updated_at',
            'observed_at', 'source_kind', 'provider', 'representation', 'url',
        )})
        if 'publication_date' in source:
            sources[-1]['publication_date'] = source['publication_date']
    data = {'version': 'benchmark-sources-v1', 'split': split,
            'cutoff': SPLIT_CUTOFFS[split], 'sources': sources,
            'coverage': 'complete_enumeration_of_curated_pool_not_provider_census',
            'historical_reconstruction': True}
    if split == 'holdout':
        data['candidate_freeze_sha256'] = freeze_sha256
    raw = json.dumps(data, ensure_ascii=False, sort_keys=True, separators=(',', ':')).encode()
    destination.mkdir(parents=True, exist_ok=True)
    with (destination / f'{split}.json').open('xb') as stream:
        stream.write(raw)
    return {'split': split, 'sources': len(sources), 'sha256': hashlib.sha256(raw).hexdigest()}


if __name__ == '__main__':
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument('--split', choices=sorted(SPLIT_CUTOFFS), required=True)
    parser.add_argument('--destination', type=Path, required=True)
    parser.add_argument('--freeze', type=Path)
    parser.add_argument('--freeze-sha256')
    args = parser.parse_args()
    print(json.dumps(export(Path(__file__).parent, args.destination, args.split, args.freeze, args.freeze_sha256)))
