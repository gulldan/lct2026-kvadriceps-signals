"""Select a primary contribution span without rewriting the source text."""
from __future__ import annotations

import re

VERSION = 'explicit-contribution-span-v1'
CONTRIBUTION = re.compile(r'\b(?:we|this paper|this work)\s+(?:\w+\s+){0,3}'
                          r'(?:introduce|introduces|propose|proposes|present|presents)\b', re.I)
EVALUATION = re.compile(r'\b(?:experiments?|experimental results|empirical evaluations?|'
                        r'our results|we evaluate|we experimentally)\b', re.I)
SENTENCE_END = re.compile(r'[.!?]\s+(?=[A-Z])')


def contribution(text: str) -> dict:
    """Retain at most three sentences around an explicit contribution, before
    evaluation boilerplate. Full text remains the fallback when no such span is
    present. Offsets and the selected substring are part of the derived artifact.
    This selection makes no novelty, relevance or truth judgment.
    """
    match = CONTRIBUTION.search(text[:12000])
    if match is None:
        return {'version': VERSION, 'start': 0, 'end': len(text), 'text': text, 'fallback': True}
    start = match.start()
    ends = list(SENTENCE_END.finditer(text, match.end(), min(len(text), start + 1800)))
    end = ends[min(2, len(ends)-1)].start() + 1 if ends else min(len(text), start + 1800)
    evaluation = EVALUATION.search(text, match.end(), end)
    if evaluation:
        # Stop at its sentence boundary, so qualifiers are not silently joined.
        earlier = [mark.end() for mark in ends if mark.end() <= evaluation.start()]
        if earlier:
            end = earlier[-1]
    while end > start and text[end-1].isspace():
        end -= 1
    return {'version': VERSION, 'start': start, 'end': end, 'text': text[start:end], 'fallback': False}
