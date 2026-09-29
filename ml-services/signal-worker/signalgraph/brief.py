"""Conservative extractive briefs. No numeric rewriting or inferred deployments.

Sentence classification remains an evaluated part of the method. Matching a
rule preserves the author's claim and its context; it is not independent proof
that the reported advantage generalizes beyond that study.
"""
from __future__ import annotations

import re

from .manifest import canonical, digest
from .claim_boundaries import complete_clause_end, incomplete_quantification

VERSION = 'source-claim-extracts-v13-development-topic-wide-fields'
MAX_BRIEF_WORKS = 15
RULES = {
    'problem': re.compile(r'\b(?:challeng\w*|suffer\w*|struggl\w*|bottleneck|difficult\w*|'
                          r'insufficient|lack\w*|limited|unable|cannot|expensive|costly|neglect\w*|arduous|'
                          r'non-deterministic|unreproducible)\b|\bnot reproducible\b|'
                          r'\bpoorly (?:designed|understood|specified|supported|performing)\b|'
                          r'\b(?:fails?|failed)\s+(?:to|on|in|at)\b|\bfalls? short\b|\bimpossible to\b|'
                          r'\b(?:face|has|have|pose)\b[^.!?]{0,80}\blimitations?\b|'
                          r'\bexceeds? (?:the )?(?:context|memory|budget|limit)\b', re.I),
    'advantage': re.compile(r'\b(?:outperforms?|surpasses?|boosts?|reduces?|improves?|improvements?|achieves?|superior)\b|'
                            r'\b(?:higher (?:performance|accuracy|recall|precision|throughput)|'
                            r'(?:lower|less) (?:latency|cost|memory|error)|fewer tokens)\b', re.I),
    'example': re.compile(r'\b(?:(?:we|this (?:paper|work|study))\s+'
                          r'(?:(?:also|first|further|then|systematically|empirically)\s+){0,2}'
                          r'(?:proposes?|presents?|introduces?|develops?|releases?|evaluates?|appl(?:y|ies)|'
                          r'implements?|tests?|conducts?|performs?|harness(?:es)?|fine-tunes?)|experiments? on|evaluated on)\b', re.I),
}
AUTHOR = re.compile(r'\b(?:we|our|proposed)\b|\b(?:tested|evaluated)\s+on\b|'
                    r'\b(?:results?|experiments?|evaluations?)\s+'
                    r'(?:show|demonstrate|indicate|reveal|confirm|suggest|find)\w*\b', re.I)
INSTRUCTION = re.compile(r'ignore (?:all |the )?(?:previous|above)|system prompt|developer message|'
                         r'api key|do not follow|assistant:', re.I)
NEGATED = re.compile(r"\b(?:not|never|no|without|cannot|fails?|unable)\s+(?:\w+\s+){0,2}$|n't\s+(?:\w+\s+){0,2}$", re.I)
NON_RESULT = re.compile(r'\b(?:room|potential|need|scope|opportunity|opportunities)\s+for\s+(?:\w+\s+){0,2}$|'
                        r'\b(?:whether|if)\b[^.!?;]*$', re.I)
INTENDED_VERB = re.compile(r'\b(?:to|can|could|may|might|will|would)\s+(?:\w+\s+){0,2}$', re.I)
ABBREVIATION = re.compile(r'\b(?:e\.g|i\.e|et al|figs?|eqs?|secs?|vs|dr|prof)\.$', re.I)
ABSTRACT_HEADING = re.compile(r'^[ \t]*abstract(?:[ \t]*[:.—–-][ \t]*|[ \t]*\r?\n)', re.I | re.M)
PROBLEM_REFERENT = re.compile(r'^(?:However,\s*)?(?:(?:To\s+(?:overcome|address|tackle|mitigate|alleviate)|Despite|Because of|Due to)'
                              r'\s+(?:the|this|these|above)\b|(?:this|these|such|other|its|their)\b|'
                              r'it\b(?!\s+is\s+(?:challenging|difficult|hard|important|necessary|possible)\b))', re.I)
EMAIL = re.compile(r'\b[\w.%+-]+@[\w.-]+\.[a-z]{2,}\b', re.I)
URL = re.compile(r'https?://', re.I)
# A hyphenated proper name such as ARC-Challenge names an evaluation, not a
# problem. Ignore that token only; other limitation statements still count.
NAMED_CHALLENGE = re.compile(r'\b[A-Z][A-Za-z0-9]*(?:[-‐‑–—][A-Za-z0-9]+)*[-‐‑–—]Challenge\b')


def describes_problem(quote: str) -> bool:
    names = [(match.start(), match.end()) for match in NAMED_CHALLENGE.finditer(quote)]
    for match in RULES['problem'].finditer(quote):
        if any(start <= match.start() and match.end() <= end for start, end in names):
            continue
        if match.group().casefold().startswith(('fail', 'fall', 'impossible')) and \
                NEGATED.search(quote[max(0, match.start()-120):match.start()]):
            continue
        return True
    return False


REPORTED_COMPARISON = re.compile(r'\b(?:results?|experiments?|evaluations?)\b[^.!?;]{0,100}'
                                 r'\b(?:show|demonstrate|indicate|reveal|confirm|find)\w*\b|'
                                 r'\bwe\s+(?:show|demonstrate|find|observe)\b', re.I)


def positive_advantage(quote: str) -> bool:
    for match in RULES['advantage'].finditer(quote):
        prefix = quote[max(0, match.start()-120):match.start()]
        if NEGATED.search(prefix) or NON_RESULT.search(prefix):
            continue
        comparative = match.group().casefold().startswith(('higher ', 'lower ', 'less ', 'fewer '))
        if comparative and not REPORTED_COMPARISON.search(prefix):
            continue
        if not comparative and match.group().lower() not in ('improvement', 'improvements') and INTENDED_VERB.search(prefix):
            continue
        return True
    return False


def sentence_spans(text: str):
    # The bounded front matter includes an abstract and its surrounding context.
    # Offsets always address the original unnormalised Unicode text.
    # Only a terminal punctuation mark ends a span. Excluding every dot from
    # the body drops or truncates claims containing decimal values or URLs.
    heading = ABSTRACT_HEADING.search(text[:4000])
    offset = heading.end() if heading else 0
    start = offset
    for match in re.finditer(r'[.!?](?=\s|$)|$', text[offset:16000]):
        end = offset + match.end()
        if ABBREVIATION.search(text[max(start, end-12):end]):
            continue
        next_start = end
        while start < end and text[start].isspace():
            start += 1
        while end > start and text[end-1].isspace():
            end -= 1
        if 45 <= end-start <= 1400 and '\x00' not in text[start:end]:
            yield start, end
        start = next_start


def build(corpus: dict, candidate: dict) -> tuple[dict, list[dict], list[str]]:
    by_doc = {record['id']: record for record in corpus['records']}
    by_work = {work['id']: work for work in corpus['works']}
    representatives = list(dict.fromkeys(candidate.get('representative_work_ids', []) + candidate['work_ids']))
    selected = {}
    for work_id in representatives[:MAX_BRIEF_WORKS]:
        work = by_work[work_id]
        if work['kind'] not in ('paper', 'company_material'):
            continue
        records = sorted((by_doc[doc] for doc in work['document_ids']), key=lambda r: (
            r['representation'] != 'abstract', r['source']['provider'] != 'arxiv', r['id']))
        for record in records[:1]:
            record_selected = {}
            spans = list(sentence_spans(record['text']))
            for number, (start, end) in enumerate(spans):
                quote = record['text'][start:end]
                # PDF page headers can be interleaved with a sentence. An
                # author/contact block is not a usable claim boundary.
                if INSTRUCTION.search(quote) or EMAIL.search(quote):
                    continue
                extent = complete_clause_end(quote)
                if extent is None:
                    continue
                quote, end = quote[:extent], start + extent
                for field, pattern in RULES.items():
                    if field in record_selected or not pattern.search(quote):
                        continue
                    if field == 'problem' and not describes_problem(quote):
                        continue
                    if field == 'advantage' and (not AUTHOR.search(quote) or not positive_advantage(quote)):
                        continue
                    field_start, field_quote = start, quote
                    if field == 'problem' and number and PROBLEM_REFERENT.search(quote):
                        antecedent = number
                        # Follow at most two explicit references without rewriting
                        # or detaching a limitation from its primary antecedent.
                        while antecedent > max(0, number - 2) and PROBLEM_REFERENT.search(field_quote):
                            antecedent -= 1
                            field_start = spans[antecedent][0]
                            field_quote = record['text'][field_start:end]
                        if (len(field_quote) > 2000 or INSTRUCTION.search(field_quote) or EMAIL.search(field_quote) or
                                URL.search(field_quote) or
                                incomplete_quantification(field_quote)):
                            continue
                    evidence = {'field': field, 'work_id': work_id, 'document_id': record['id'],
                                'snapshot_id': record['snapshot_id'], 'source_revision': record['source']['revision'],
                                'text_sha256': record['text_sha256'], 'start': field_start, 'end': end, 'quote': field_quote,
                                'claim_type': 'research_claim' if work['kind'] == 'paper' else 'company_claim'}
                    evidence['id'] = digest(canonical(evidence))
                    record_selected[field] = evidence
            # A brief describes the topic: each field keeps its own source and
            # the first representative that states it wins, so a problem from
            # one work and an advantage from another are never merged into one
            # sentence — the reader sees two attributed quotes.
            for field, found in record_selected.items():
                selected.setdefault(field, found)
        if len(selected) == len(RULES):
            break
    evidence = [selected[field] for field in RULES if field in selected]
    fields = {field: ({'text': selected[field]['quote'], 'evidence_ids': [selected[field]['id']]}
                      if field in selected else None) for field in RULES}
    brief = {**fields, 'description_status': 'template', 'evidence_sha256': digest(canonical(evidence))}
    return brief, evidence, [field for field in RULES if field not in selected]
