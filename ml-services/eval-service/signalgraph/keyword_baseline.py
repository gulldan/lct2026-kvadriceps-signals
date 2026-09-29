"""Фиксированная лексическая база по одним источникам, независимая от смысловой кластеризации.

Правило затравки не даёт строить транзитивные мосты по ключевым словам. Параметры
зафиксированы до замеров этой базы; ни разметка, ни эмбеддинги на вход не идут.
"""
from collections import Counter
import hashlib
import json
import math
import re

VERSION = 'tfidf-keyword-count-v1'
CONFIG = {'minimum_document_frequency':2, 'maximum_document_fraction':.5,
          'seed_cosine_threshold':.25, 'minimum_works':2, 'query_overlap':.6,
          'ngrams':[1,2], 'order':'work_count_descending_then_stable_id'}
STOP = set('a an and are as at be been by can for from has have in into is it its of on or '
           'our that the their these this to was we were which with'.split())


def cluster(sources: list[dict]) -> dict:
    ordered = sorted(sources, key=lambda source: source['canonical_id'])
    counts = []
    frequency = Counter()
    for source in ordered:
        words = [word for word in re.findall(r'[a-z][a-z0-9]+',
                 (source['title']+' '+source['text']).casefold()) if word not in STOP]
        terms = Counter(words+[' '.join(pair) for pair in zip(words, words[1:])])
        counts.append(terms)
        frequency.update(terms.keys())
    profiles = []
    for terms in counts:
        weights = {word:(1+math.log(count))*(1+math.log((1+len(ordered))/(1+frequency[word])))
                   for word,count in terms.items()
                   if CONFIG['minimum_document_frequency'] <= frequency[word] <=
                   CONFIG['maximum_document_fraction']*len(ordered)}
        norm = math.sqrt(math.fsum(value*value for value in weights.values()))
        profiles.append({word:value/norm for word,value in weights.items()} if norm else {})
    groups = []
    for index, profile in enumerate(profiles):
        matches = [(math.fsum(value*profiles[group[0]].get(word,0) for word,value in profile.items()), i)
                   for i,group in enumerate(groups)]
        similarity, matched = max(matches, default=(0,-1), key=lambda row:(row[0],-row[1]))
        if similarity >= CONFIG['seed_cosine_threshold']:
            groups[matched].append(index)
        else:
            groups.append([index])
    candidates, unassigned = [], []
    for group in groups:
        members = ['paper:'+ordered[index]['canonical_id'] for index in group]
        if len(members) < CONFIG['minimum_works']:
            unassigned.extend(members)
            continue
        fingerprint = hashlib.sha256(json.dumps(members,separators=(',',':')).encode()).hexdigest()
        candidates.append({'candidate_id':'keyword-'+fingerprint, 'work_ids':members,
                           'representative_work_ids':members[:1]})
    return {'version':VERSION, 'config':CONFIG, 'candidates':candidates, 'unassigned':sorted(unassigned),
            'representation':'TF-IDF unigrams+bigrams of source title and abstract; no embeddings'}
