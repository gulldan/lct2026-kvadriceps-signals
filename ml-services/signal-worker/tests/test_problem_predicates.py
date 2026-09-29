import hashlib
import json
from pathlib import Path
import sys
import unittest

sys.path.insert(0, str(Path(__file__).resolve().parents[1]))
from signalgraph import brief


class ProblemPredicateTests(unittest.TestCase):
    def test_primary_development_sources_keep_failure_problem(self):
        packet = json.loads((Path(__file__).parent/'fixtures/problem_predicates.json').read_text())
        for source in packet['sources']:
            with self.subTest(source=source['native_id']):
                self.assertEqual(hashlib.sha256(source['text'].encode()).hexdigest(), source['text_sha256'])
                work = 'paper:'+source['canonical_id']
                corpus = {'works': [{'id': work, 'kind': 'paper', 'document_ids': ['doc']}],
                          'records': [{'id': 'doc', 'snapshot_id': 'snapshot', 'text': source['text'],
                                       'text_sha256': source['text_sha256'], 'representation': 'abstract',
                                       'source': {'provider': 'arxiv', 'revision': source['native_id']}}]}
                fields, evidence, missing = brief.build(corpus, {'work_ids': [work]})
                self.assertNotIn('problem', missing)
                self.assertIn(source['expected_problem_phrase'], fields['problem']['text'])
                for claim in evidence:
                    self.assertEqual(claim['quote'], source['text'][claim['start']:claim['end']])

    def test_negated_failure_and_evaluation_name_are_not_problems(self):
        for sentence in (
            'Our model does not fail to retrieve the relevant context for these questions.',
            'The approach never falls short in the tested retrieval conditions.',
            'It is not impossible to use these documents with this retriever.',
            'Our method achieves improved accuracy on ARC-Challenge and several related evaluations.',
        ):
            with self.subTest(sentence=sentence):
                self.assertFalse(brief.describes_problem(sentence))


if __name__ == '__main__':
    unittest.main()
