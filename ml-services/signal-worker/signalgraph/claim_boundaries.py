"""Conservative boundaries for incomplete quantitative source comparisons.

These checks preserve a source clause; they do not establish semantic support.
The worker and the source-role evaluation use exactly the same policy.
"""
import re

COMPARISON = re.compile(r'\b(?:outperform\w*|surpass\w*|than|over|compared|baseline\w*|preferred)\b', re.I)
PERCENT = r'\d+(?:\.\d+)?(?:\s*[-–]{1,2}\s*\d+(?:\.\d+)?)?\s*\\?%'
VAGUE_PERCENT = re.compile(r'\bperformance\b[^.!?]{0,80}' + PERCENT +
                           '|' + PERCENT + r'[^.!?]{0,60}\bperformance\b', re.I)
UNRESOLVED_FACTOR = re.compile(r'\b\d+(?:\.\d+)?\s+times\b[^.!?]{0,100}\b'
                               r'(?:its|their|the)\s+(?:vanilla|standard|original)\s+counterpart\b', re.I)
METRIC = re.compile(r'\b(?:accuracy|precision|recall|ndcg|bleu|rouge|f1|perplexity|'
                    r'latency|throughput|cost|tokens?|parameters?|user data|storage|memory|'
                    r'success rate|preferred)\b', re.I)
CLAUSE_CUT = re.compile(r'\s+by\s+(?:up to\s+|an? average of\s+)?(?:\+\s*)?\d|'
                        r',\s+achieving\s+(?:an?\s+)?\d|'
                        r'\s+and\s+(?=[^.!?]{0,60}\(\d+(?:\.\d+)?\s+times\)'
                        r'[^.!?]{0,100}\b(?:vanilla|standard|original)\s+counterpart\b)|'
                        r'\s+\((?=[^)]{0,50}\d)', re.I)


def incomplete_quantification(quote: str) -> bool:
    return (bool(VAGUE_PERCENT.search(quote)) or bool(UNRESOLVED_FACTOR.search(quote)) or
            (bool(re.search(PERCENT + r'|\d+(?:\.\d+)?\s+points\b', quote)) and
             bool(COMPARISON.search(quote)) and not METRIC.search(quote)) or
            ('respectively' in quote.casefold() and len(re.findall(PERCENT, quote)) > 1))


def complete_clause_end(quote: str) -> int | None:
    """Return the unchanged extent, a safe clause boundary, or no usable span."""
    if incomplete_quantification(quote):
        cut = CLAUSE_CUT.search(quote)
        if cut is None:
            return None
        quote = quote[:cut.start()].rstrip()
    if len(quote) < 45 or incomplete_quantification(quote):
        return None
    return len(quote)
