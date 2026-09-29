import sys
from pathlib import Path
import unittest

sys.path.insert(0, str(Path(__file__).parents[1]))
from signalgraph.representation import contribution


class ContributionSpanTests(unittest.TestCase):
    def test_preserves_exact_primary_wording_and_offsets(self):
        text = ('General retrieval background. We propose a graph path retriever. '
                'It follows explicit edges. Experiments show a gain of up to 10%. Other material.')
        span = contribution(text)
        self.assertEqual(span['text'], 'We propose a graph path retriever. It follows explicit edges.')
        self.assertEqual(text[span['start']:span['end']], span['text'])
        self.assertFalse(span['fallback'])

    def test_absent_contribution_does_not_invent_one(self):
        text = 'No proposed method is described. The claimed gain could not be reproduced.'
        span = contribution(text)
        self.assertEqual(span['text'], text)
        self.assertTrue(span['fallback'])
