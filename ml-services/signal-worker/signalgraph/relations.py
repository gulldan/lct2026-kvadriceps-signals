"""Explicit, versioned paper/repository references; never an official-code claim.

Records must already belong to one authorized, ready analysis manifest. The
additional cutoff check keeps a later README from supplying earlier evidence.
No network requests, title matching, organization inference or cross-kind merge.
"""
from __future__ import annotations

from dataclasses import dataclass
from datetime import datetime
import hashlib
import re
from urllib.parse import urlsplit, unquote

from .identity import Work, normalize_doi

URL = re.compile(r'https?://[^\s<>"\[\]]+')
ARXIV = re.compile(r'(?:https?://(?:export\.)?arxiv\.org/(?:abs|pdf)/|arxiv:\s*)'
                   r'(?P<id>\d{4}\.\d{4,5}|[a-z-]+(?:\.[A-Z]{2})?/\d{7})(?:v\d+)?(?:\.pdf)?', re.I)
DOI = re.compile(r'(?:https?://(?:dx\.)?doi\.org/|doi:\s*)(10\.\d{4,9}/[^\s<>"\[\]]+)', re.I)


@dataclass(frozen=True)
class LinkEvidence:
    document_id: str
    source_revision: str
    snapshot_id: str | None
    text_sha256: str
    char_start: int
    char_end: int
    quote: str
    direction: str
    target_identifier: str
    available_at: str


@dataclass(frozen=True)
class PaperRepositoryLink:
    id: str
    paper_id: str
    repository_id: str
    repository_root: str | None
    evidence: tuple[LinkEvidence, ...]
    relation: str = 'explicit_reference'
    official: bool = False
    company: str | None = None
    production_use: bool | None = None


def _time(value: str) -> datetime:
    result = datetime.fromisoformat(value.replace('Z', '+00:00'))
    if result.tzinfo is None:
        raise ValueError('evidence timestamps require a timezone')
    return result


def _available(record: dict, cutoff: datetime) -> str | None:
    source = record['source']
    value = source.get('available_at') or source.get('observed_at')
    if not value or _time(value) > cutoff:
        return None
    arxiv = source.get('arxiv') or {}
    if arxiv.get('version_updated') and _time(arxiv['version_updated']) > cutoff:
        return None
    return value


def _repository_slug(url: str) -> str | None:
    parts = urlsplit(url.rstrip(').,;:!'))
    if parts.scheme not in ('http', 'https') or parts.hostname not in ('github.com', 'www.github.com'):
        return None
    if parts.username or parts.password:
        return None
    path = unquote(parts.path).strip('/').split('/')
    if len(path) < 2 or not all(re.fullmatch(r'[\w.-]+', piece) for piece in path[:2]):
        return None
    return (path[0]+'/'+path[1].removesuffix('.git')).lower()


def _paper_references(text: str):
    for match in ARXIV.finditer(text):
        yield 'arxiv:'+match['id'].lower(), match.start(), match.end()
    for match in DOI.finditer(text):
        # Keep internal DOI punctuation; remove only surrounding prose syntax.
        value = match[1].rstrip('.,;:!')
        while value.endswith(')') and value.count(')') > value.count('('):
            value = value[:-1]
        if doi := normalize_doi(value):
            yield 'doi:'+doi, match.start(), match.end()-(len(match[1])-len(value))


def _evidence(record: dict, span: tuple[int, int], direction: str, target: str, available: str) -> LinkEvidence:
    text = record['text']
    text_hash = hashlib.sha256(text.encode()).hexdigest()
    if record.get('text_sha256', text_hash) != text_hash:
        raise ValueError('link evidence differs from the immutable text')
    start, end = span
    if not (0 <= start < end <= len(text)) or not record['source'].get('revision'):
        raise ValueError('link evidence requires a source revision and exact span')
    return LinkEvidence(record['id'], record['source']['revision'], record.get('snapshot_id'), text_hash,
                        start, end, text[start:end], direction, target, available)


def paper_repository_links(records: list[dict], works: list[Work], cutoff: str) -> list[PaperRepositoryLink]:
    deadline = _time(cutoff)
    by_document = {doc: work for work in works for doc in work.document_ids}
    eligible = {r['id']: (r, available) for r in records if (available := _available(r, deadline))}
    paper_aliases: dict[str, set[str]] = {}
    repo_slugs: dict[str, set[str]] = {}
    work_by_id = {work.id: work for work in works}
    for record, _ in eligible.values():
        work = by_document.get(record['id'])
        if work is None:
            continue
        if work.kind == 'paper':
            for alias in work.aliases:
                if alias.startswith(('arxiv:', 'doi:')):
                    paper_aliases.setdefault(alias.lower(), set()).add(work.id)
        elif work.kind == 'software_repository' and (slug := _repository_slug(record['source']['url'])):
            repo_slugs.setdefault(slug, set()).add(work.id)
    evidence: dict[tuple[str, str], list[LinkEvidence]] = {}
    for record, available in eligible.values():
        work = by_document.get(record['id'])
        if work is None or work.kind != 'software_repository':
            continue
        for identifier, start, end in _paper_references(record['text']):
            matches = paper_aliases.get(identifier, set())
            if len(matches) == 1:
                paper = next(iter(matches))
                evidence.setdefault((paper, work.id), []).append(
                    _evidence(record, (start, end), 'readme_to_paper', identifier, available))
    # A reused/ambiguous URL cannot override a repository's stable provider ID.
    # A unique reciprocal reference can resolve the fixture's renamed/fork URLs.
    for record, available in eligible.values():
        work = by_document.get(record['id'])
        if work is None or work.kind != 'paper':
            continue
        for match in URL.finditer(record['text']):
            slug = _repository_slug(match[0])
            matches = repo_slugs.get(slug, set())
            if len(matches) > 1:
                matches = {repo for repo in matches if (work.id, repo) in evidence}
            if len(matches) == 1:
                repository = next(iter(matches))
                end = match.end()-len(match[0])+len(match[0].rstrip(').,;:!'))
                evidence.setdefault((work.id, repository), []).append(
                    _evidence(record, (match.start(), end), 'paper_to_repository', slug, available))
    result = []
    for (paper, repository), anchors in sorted(evidence.items()):
        identity = hashlib.sha256((paper+'\0'+repository).encode()).hexdigest()
        ordered = tuple(sorted(set(anchors), key=lambda e: (e.document_id, e.char_start, e.direction)))
        result.append(PaperRepositoryLink(identity, paper, repository, work_by_id[repository].implementation_root, ordered))
    return result
