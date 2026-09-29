"""Query relevance is separate from temporal emergence and repository popularity."""
from __future__ import annotations

import json
import math
import os
import re
import urllib.error
import urllib.request

VERSION = 'query-relevance-v2-development-scoped'
SIMILARITY_THRESHOLD = .4
ALIASES = {
    'ии': 'artificial intelligence machine learning large language models',
    'технологии в ии': 'artificial intelligence machine learning large language models',
    'искусственный интеллект': 'artificial intelligence machine learning large language models',
    'ai': 'artificial intelligence machine learning large language models',
    'artificial intelligence': 'artificial intelligence machine learning large language models',
    'rag': 'retrieval augmented generation',
    'retrieval augmented generation': 'retrieval augmented generation',
    'поиск с дополненной генерацией': 'retrieval augmented generation',
}
STOP = {'the', 'and', 'for', 'with', 'technology', 'technologies', 'технологии', 'в', 'и', 'по', 'на', 'a', 'of'}


def query_vector(query: str, model: dict, url: str) -> list[float]:
    request = urllib.request.Request(url, data=json.dumps({'model': model['name'], 'input': [query]}).encode(),
                                     headers={'Content-Type': 'application/json'}, method='POST')
    with urllib.request.urlopen(request, timeout=15) as response:
        raw = response.read((1 << 20) + 1)
    if len(raw) > 1 << 20:
        raise ValueError('oversized query embedding')
    payload = json.loads(raw)
    if payload.get('model') != model['name'] or payload.get('revision') != model['revision']:
        raise ValueError('query embedding revision mismatch')
    rows = payload.get('data', [])
    if len(rows) != 1 or rows[0].get('index') != 0:
        raise ValueError('invalid query embedding row')
    vector = rows[0]['embedding']
    if len(vector) != model['dimension'] or not all(type(x) in (int, float) and math.isfinite(x) for x in vector):
        raise ValueError('invalid query vector')
    norm = math.sqrt(math.fsum(x*x for x in vector))
    if not norm:
        raise ValueError('zero query vector')
    return [x/norm for x in vector]


def query_parts(query: str) -> list[str]:
    expanded = ALIASES.get(query.casefold(), query)
    parts = re.split(r'\s+(?:для|for)\s+', query, maxsplit=1, flags=re.I)
    # Only an explicit known domain prefix introduces an application constraint.
    # Arbitrary paper names and quoted titles retain their whole-query meaning.
    domain = ALIASES.get(parts[0].casefold().replace('-', ' '))
    if len(parts) == 2 and domain and set(re.findall(r'\w+', parts[1].casefold())) - STOP:
        return list(dict.fromkeys((expanded, domain, parts[1])))
    return [expanded]


def prepare(corpus: dict) -> tuple[dict, list[list[float]] | None]:
    query = corpus['input']['request']['query']
    parts = query_parts(query)
    context = {'version': VERSION, 'query': query, 'applied_query': parts[0], 'mode': 'semantic_and_lexical',
               'required_query_parts': parts,
               'match_meaning': 'all_required_parts_on_one_work; modifier_literal_phrase_or_semantics',
               'semantic_threshold': SIMILARITY_THRESHOLD, 'model': corpus['input']['model'], 'reason': None}
    if not corpus['vectors']:
        return {**context, 'mode': 'empty_corpus'}, None
    try:
        vectors = [query_vector(part, corpus['input']['model'],
                               os.getenv('EMBEDDINGS_URL', 'http://f2llm-service:8085/v1/embeddings'))
                   for part in parts]
    except (urllib.error.URLError, TimeoutError, OSError, ValueError, KeyError, TypeError):
        return {**context, 'mode': 'lexical_fallback', 'reason': 'query_embedding_unavailable_or_incompatible'}, None
    return context, vectors


def assess(corpus: dict, work_ids: list[str], context: dict, vectors: list[list[float]] | None) -> dict:
    members = set(work_ids)
    selected = [work for work in corpus['vectors'] if work['canonical_id'] in members]
    parts = context.get('required_query_parts', [context['applied_query']])
    tokens = [set(re.findall(r'\w+', part.casefold())) - STOP for part in parts]
    if vectors is not None and len(vectors) != len(parts):
        raise ValueError('query part/vector membership differs')
    lexical = 0.0
    semantic = None
    relevant = False
    for work in selected:
        text_words = re.findall(r'\w+', (work['title']+' '+work['representative_text']).casefold())
        words = set(text_words)
        overlaps = [len(part & words)/max(1, len(part)) for part in tokens]
        if len(parts) > 1:
            # "image" in the input and "text generation" elsewhere do not
            # establish the modifier "image generation" on lexical evidence.
            phrase = ' '.join(re.findall(r'\w+', parts[-1].casefold()))
            overlaps[-1] = float(' '+phrase+' ' in ' '+' '.join(text_words)+' ')
        similarities = ([math.fsum(a*b for a, b in zip(vector, work['vector'], strict=True))
                         for vector in vectors] if vectors is not None else [None]*len(parts))
        lexical = max(lexical, min(overlaps))
        if vectors is not None:
            similarity = min(similarities)
            semantic = similarity if semantic is None else max(semantic, similarity)
        relevant |= all(overlap >= .6 or (similarity is not None and similarity >= SIMILARITY_THRESHOLD)
                        for overlap, similarity in zip(overlaps, similarities, strict=True))
    return {'version': VERSION, 'relevant': relevant,
            'semantic_similarity': semantic, 'lexical_overlap': lexical, 'mode': context['mode']}
