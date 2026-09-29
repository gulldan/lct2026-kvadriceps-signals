from pathlib import Path
import sys
import unittest
from unittest.mock import patch

sys.path.insert(0, str(Path(__file__).resolve().parents[1]))
from signalgraph.relevance import assess, prepare


class RelevanceTests(unittest.TestCase):
    def corpus(self):
        return {'input': {'request': {'query': 'RAG'}, 'model': {'name': 'pinned', 'revision': 'v1', 'dimension': 2}},
                'vectors': [{'canonical_id': 'retrieval', 'vector': [1, 0], 'title': 'Retrieval augmented generation',
                             'representative_text': 'A method for retrieval augmented generation.'},
                            {'canonical_id': 'physics', 'vector': [0, 1], 'title': 'Neutron matter',
                             'representative_text': 'The equation of state of neutron matter.'}]}

    def test_timeout_is_explicit_lexical_fallback_not_zero_semantic_score(self):
        with patch('signalgraph.relevance.query_vector', side_effect=TimeoutError):
            context, vector = prepare(self.corpus())
        self.assertEqual(context['mode'], 'lexical_fallback')
        self.assertEqual(context['query'], 'RAG')
        self.assertEqual(context['applied_query'], 'retrieval augmented generation')
        result = assess(self.corpus(), ['retrieval'], context, vector)
        self.assertTrue(result['relevant'])
        self.assertIsNone(result['semantic_similarity'])
        self.assertFalse(assess(self.corpus(), ['physics'], context, vector)['relevant'])

    def test_empty_corpus_does_not_call_model(self):
        corpus = self.corpus()
        corpus['vectors'] = []
        with patch('signalgraph.relevance.query_vector') as model:
            context, vector = prepare(corpus)
        self.assertEqual(context['mode'], 'empty_corpus')
        self.assertIsNone(vector)
        model.assert_not_called()


if __name__ == '__main__':
    unittest.main()
