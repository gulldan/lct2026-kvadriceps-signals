"""Проверки метрик и происхождения; эти фикстуры не измеряют качество метода."""
import copy
import json
from pathlib import Path
import tempfile
import unittest

from assess_ranking import assess, gold_gain
from prepare_inputs import export


class RankingAssessmentTests(unittest.TestCase):
    def setUp(self):
        self.root = Path(__file__).parent.resolve()
        self.temporary = tempfile.TemporaryDirectory()
        self.addCleanup(self.temporary.cleanup)
        self.directory = Path(self.temporary.name)
        pin = export(self.root, self.directory, 'development')
        topics = json.loads((self.root/'annotations.development.json').read_text())['topics']
        topic = next(t for t in topics if t['eligible_gold']['ai'])
        works = ['paper:'+json.loads((self.root/'sources'/f'{native}.json').read_text())['canonical_id']
                 for native in topic['source_ids']]
        rows = [{'id':name, 'work_ids':members, 'evidence':[]} for name, members in
                [('correct', works), ('duplicate', works), ('unknown', ['not-a-gold-work'])]]
        ranking = {'ordered_ids':[r['id'] for r in rows], 'completeness':'insufficient_coverage'}
        self.run = {'split':'development', 'source_input_sha256':pin['sha256'],
                    'method':{'code_sha256':'a'*64}, 'seconds':0,
                    'queries':{'ai':{'candidates':rows, 'systems':{
                        'temporal_mmr':ranking, 'keyword_count_shared_clusters':ranking}}}}
        self.path = self.directory/'controlled-run.json'

    def write_run(self):
        self.path.write_text(json.dumps(self.run))

    def test_missing_slots_duplicates_and_unknown_members_cannot_inflate_precision(self):
        self.write_run()
        result = assess(self.root, self.path)['queries']['ai']['systems']['temporal_mmr']
        self.assertEqual(result['denominator'], 15)
        self.assertEqual(result['true_positive'], 1)
        self.assertEqual(result['p_at_15'], 1/15)
        self.assertEqual(result['distinct_gold_groups'], 1)
        self.assertEqual(result['false_or_unknown'], 2)
        self.assertEqual([r['gain'] for r in result['rows']], [3,0,0])

    def test_a_different_source_manifest_cannot_use_frozen_labels(self):
        self.run['source_input_sha256'] = '0'*64
        self.write_run()
        with self.assertRaisesRegex(ValueError, 'frozen source-only manifest'):
            assess(self.root, self.path)

    def test_holdout_is_rejected_before_any_annotation_is_opened(self):
        self.run['split'] = 'holdout'
        self.write_run()
        with self.assertRaisesRegex(ValueError, 'holdout cannot be opened'):
            assess(self.directory/'no-labels-exist', self.path)

    def test_preregistered_partial_gain_includes_supported_adjacent_topics(self):
        adjacent = {'eligible_gold':{'ai':False}, 'relevance':{'ai':1}, 'emergence':'mature',
                    'brief_support':dict.fromkeys(('problem','advantage','example'), True)}
        self.assertEqual(gold_gain(adjacent, 'ai'), 1)
        disputed = copy.deepcopy(adjacent)
        disputed.update(relevance={'ai':2}, emergence='disputed')
        self.assertEqual(gold_gain(disputed, 'ai'), 1)
        disputed['brief_support']['advantage'] = None
        self.assertEqual(gold_gain(disputed, 'ai'), 0)


if __name__ == '__main__':
    unittest.main()
