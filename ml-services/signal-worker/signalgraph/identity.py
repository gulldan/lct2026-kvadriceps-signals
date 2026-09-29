"""Canonical research units, without title matching or cross-kind promotion.

Callers must apply reader scope, test namespace, cutoff and ready checks before
passing an analysis manifest. This module never reads a live/searching corpus.
"""
from __future__ import annotations

import re
from dataclasses import dataclass
from urllib.parse import parse_qsl, urlencode, urlsplit, urlunsplit


def normalize_doi(value: str) -> str | None:
    value = value.strip()
    value = re.sub(r'^https?://(?:dx\.)?doi\.org/', '', value, flags=re.I)
    value = re.sub(r'^doi:\s*', '', value, flags=re.I).lower()
    if re.fullmatch(r'10\.\d{4,9}/[^\s<>]+', value):
        return value
    return None


def canonical_url(value: str) -> str:
    url = urlsplit(value.strip())
    if url.scheme not in ('http', 'https') or not url.hostname or url.username or url.password:
        raise ValueError('canonical URL must be a credential-free HTTP(S) source')
    params = [(key, val) for key, val in parse_qsl(url.query, keep_blank_values=True)
              if not key.lower().startswith('utm_') and key.lower() not in ('gclid', 'fbclid')]
    return urlunsplit((url.scheme.lower(), url.netloc.lower(), url.path or '/', urlencode(sorted(params)), ''))


@dataclass(frozen=True)
class Work:
    id: str
    kind: str
    document_ids: tuple[str, ...]
    aliases: tuple[str, ...]
    implementation_root: str | None = None
    patent_family: str | None = None
    limitations: tuple[str, ...] = ()


def _keys(record: dict) -> tuple[set[str], str | None, str | None, list[str]]:
    source = record['source']
    kind = source['source_kind']
    keys = {'canonical:' + source['canonical_id']}
    limits = []
    root = family = None
    if kind == 'software_repository':
        github = source['github']
        repository = github['repository_id']
        # Repository IDs override names/URLs; a rename is not a new project.
        keys = {'github:' + str(repository)}
        if github['fork']:
            if github['root_id'] is None:
                limits.append('fork_root_unknown; independent_implementation_not_established')
            else:
                root = 'github:' + str(github['root_id'])
        else:
            root = 'github:' + str(repository)
    elif kind == 'paper':
        for value in [source['canonical_id'], source['native_id'], (source.get('arxiv') or {}).get('doi', '')]:
            if doi := normalize_doi(value):
                keys.add('doi:' + doi)
        if arxiv := source.get('arxiv'):
            keys.add('arxiv:' + arxiv['base_id'])
    elif kind == 'patent':
        # Counting defaults to publications, preserving country/kind codes.
        relation = record.get('confirmed_patent_family')
        if relation:
            if not all(relation.get(key) for key in ('id', 'provider', 'basis')):
                raise ValueError('patent family needs an explicit provider identifier and evidence basis')
            family = relation['provider'] + ':' + relation['id']
        else:
            limits.append('patent_publication_unit; family_unconfirmed')
    if kind != 'software_repository':
        keys.add('native:' + source['provider'] + ':' + source['native_id'])
    # A metadata-only title/abstract hash is not identity evidence for a work.
    if record.get('representation') == 'full_text' and kind in ('paper', 'analysis', 'company_material'):
        keys.add('content:' + source['hash_algorithm'] + ':' + source['content_hash'])
    # Only an explicitly established canonical URL is used, never a homepage or
    # an arbitrary source URL shared by otherwise distinct stable IDs.
    if record.get('canonical_url'):
        if not record.get('canonical_url_basis'):
            raise ValueError('canonical URL needs an explicit provenance basis')
        if kind not in ('software_repository', 'patent'):
            keys.add('url:' + canonical_url(record['canonical_url']))
    return keys, root, family, limits


def canonical_works(records: list[dict]) -> list[Work]:
    records = sorted(records, key=lambda record: record['id'])
    if len({record['id'] for record in records}) != len(records):
        raise ValueError('manifest repeats a document ID')
    parents = list(range(len(records)))
    observed: dict[tuple[str, str], int] = {}
    metadata = []

    def find(node):
        while parents[node] != node:
            parents[node] = parents[parents[node]]
            node = parents[node]
        return node

    for i, record in enumerate(records):
        source = record['source']
        if source['source_kind'] == 'hypothesis':
            metadata.append(None)
            continue
        if source['source_kind'] not in ('paper', 'patent', 'analysis', 'company_material', 'software_repository'):
            raise ValueError('unsupported source counting unit')
        keys, root, family, limits = _keys(record)
        metadata.append((keys, root, family, limits))
        for key in sorted(keys):
            scoped = (source['source_kind'], key)
            if scoped in observed:
                a, b = find(i), find(observed[scoped])
                parents[max(a, b)] = min(a, b)
            observed[scoped] = i
    grouped: dict[int, list[int]] = {}
    for i, item in enumerate(metadata):
        if item is not None:
            grouped.setdefault(find(i), []).append(i)
    result = []
    for indices in grouped.values():
        kind = records[indices[0]]['source']['source_kind']
        aliases = sorted({key for i in indices for key in metadata[i][0]})
        # Prefer durable provider identifiers over hashes/URLs/display names.
        identity = min(aliases, key=lambda key: (not key.startswith(('doi:', 'arxiv:', 'github:')), key))
        roots = {metadata[i][1] for i in indices if metadata[i][1] is not None}
        families = {metadata[i][2] for i in indices if metadata[i][2] is not None}
        if len(roots) > 1 or len(families) > 1:
            raise ValueError('conflicting confirmed root/family identities in one work')
        limits = {limit for i in indices for limit in metadata[i][3]}
        if len({records[i]['source']['canonical_id'] for i in indices}) > 1:
            limits.add('provider_identifiers_preserved_as_aliases; inspect_copy_or_explicit_identity_basis')
        result.append(Work(kind + ':' + identity, kind, tuple(records[i]['id'] for i in indices), tuple(aliases),
                           next(iter(roots), None), next(iter(families), None), tuple(sorted(limits))))
    return sorted(result, key=lambda work: work.id)


def counting_units(works: list[Work]) -> dict[str, int]:
    return {
        'canonical_scientific_works': sum(work.kind == 'paper' for work in works),
        'repository_records': sum(work.kind == 'software_repository' for work in works),
        'independent_repositories': len({work.implementation_root for work in works if work.implementation_root}),
        'patent_publications': sum(work.kind == 'patent' for work in works),
        'confirmed_patent_families': len({work.patent_family for work in works if work.patent_family}),
    }
