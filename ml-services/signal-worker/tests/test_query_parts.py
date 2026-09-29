from pathlib import Path
import json
import sys
import unittest
from unittest.mock import patch

sys.path.insert(0, str(Path(__file__).resolve().parents[1]))
from signalgraph.relevance import assess, prepare, query_parts


class QueryPartTests(unittest.TestCase):
    def test_known_domain_modifier_and_quoted_or_unrecognized_method_names(self):
        self.assertEqual(query_parts('RAG для физики'),
                         ['RAG для физики', 'retrieval augmented generation', 'физики'])
        self.assertEqual(query_parts('RAG for image generation'),
                         ['RAG for image generation', 'retrieval augmented generation', 'image generation'])
        for title in ('Learning for Retrieval', 'A Framework for Image Generation',
                      '"Retrieval Augmented Generation for Knowledge-Intensive NLP Tasks"'):
            self.assertEqual(query_parts(title), [title])
        self.assertEqual(query_parts('RAG'), ['retrieval augmented generation'])

    def test_required_parts_cannot_be_satisfied_by_different_works(self):
        context = {'required_query_parts': ['retrieval', 'gravity'],
                   'applied_query': 'retrieval for gravity', 'mode': 'semantic_and_lexical'}
        corpus = {'vectors': [
            {'canonical_id': 'retrieval', 'title': 'Retrieval method', 'representative_text': '', 'vector': [1., 0.]},
            {'canonical_id': 'gravity', 'title': 'Gravity measurement', 'representative_text': '', 'vector': [0., 1.]},
        ]}
        self.assertFalse(assess(corpus, ['retrieval', 'gravity'], context, [[1., 0.], [0., 1.]])['relevant'])
        corpus['vectors'][0]['vector'] = [2**-.5, 2**-.5]
        self.assertTrue(assess(corpus, ['retrieval'], context, [[1., 0.], [0., 1.]])['relevant'])

    def test_constraint_embedding_failure_keeps_lexical_constraint(self):
        corpus = {'input': {'request': {'query': 'RAG for gravity'},
                            'model': {'name': 'pinned', 'revision': 'v1', 'dimension': 2}},
                  'vectors': [{'canonical_id': 'retrieval', 'title': 'Retrieval augmented generation',
                               'representative_text': '', 'vector': [1., 0.]}]}
        with patch('signalgraph.relevance.query_vector', side_effect=[[1., 0.], [1., 0.], TimeoutError]):
            context, vectors = prepare(corpus)
        self.assertEqual(context['mode'], 'lexical_fallback')
        self.assertIsNone(vectors)
        self.assertFalse(assess(corpus, ['retrieval'], context, vectors)['relevant'])

    def test_modifier_words_must_not_change_their_relationship(self):
        context = {'required_query_parts': query_parts('RAG for image generation'),
                   'applied_query': 'RAG for image generation', 'mode': 'lexical_fallback'}
        work = {'canonical_id': 'paper', 'title': 'RAG retrieval augmented generation',
                'representative_text': 'Image questions are answered using text generation.', 'vector': [1., 0.]}
        corpus = {'vectors': [work]}
        self.assertFalse(assess(corpus, ['paper'], context, None)['relevant'])
        work['representative_text'] = 'The system supports image generation from retrieved examples.'
        self.assertTrue(assess(corpus, ['paper'], context, None)['relevant'])

    def test_measured_development_queries_keep_image_generation_and_reject_wrong_task(self):
        packet = json.loads((Path(__file__).parent/'fixtures/query_parts_real.json').read_text())
        corpus = {'input': {'request': {'query': ''}, 'model': packet['model']}, 'vectors': packet['works']}
        for case in packet['cases']:
            with self.subTest(query=case['query'], work=case['work_id']):
                corpus['input']['request']['query'] = case['query']
                with patch('signalgraph.relevance.query_vector',
                           side_effect=lambda text, *_: packet['query_vectors'][text]):
                    context, vectors = prepare(corpus)
                self.assertEqual(assess(corpus, [case['work_id']], context, vectors)['relevant'], case['relevant'])


if __name__ == '__main__':
    unittest.main()
