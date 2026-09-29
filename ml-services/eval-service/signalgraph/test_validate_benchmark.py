"""Регрессии на загрязнение бенчмарка и приёмку, пройденную без данных."""
import json
from pathlib import Path
import shutil
import tempfile
import unittest

from validate_benchmark import digest, validate


class BenchmarkIntegrityTests(unittest.TestCase):
    def setUp(self):
        self.temporary = tempfile.TemporaryDirectory()
        self.addCleanup(self.temporary.cleanup)
        self.root = Path(self.temporary.name) / 'benchmark'
        shutil.copytree(Path(__file__).parent, self.root, ignore=shutil.ignore_patterns('__pycache__'))

    def mutate_annotation(self, mutate):
        # Тест перезамораживает копию, чтобы дойти до смысловых проверок после
        # сверки хешей; настоящие артефакты репозитория тесты не переписывают.
        path = self.root / 'annotations.development.json'
        data = json.loads(path.read_text())
        mutate(data)
        path.write_text(json.dumps(data))
        freeze_path = self.root / 'freeze.development.json'
        freeze = json.loads(freeze_path.read_text())
        freeze['sha256'][path.name] = digest(path)
        freeze_path.write_text(json.dumps(freeze))

    def test_absent_holdout_is_not_a_pass(self):
        for prefix in ('annotations', 'inputs', 'freeze'):
            (self.root / f'{prefix}.holdout.json').unlink(missing_ok=True)
        self.assertIn('holdout', validate(self.root)['missing_splits'])
        with self.assertRaisesRegex(ValueError, 'missing splits'):
            validate(self.root, require_full_pool=True)

    def test_all_frozen_pools_have_fifteen_distinct_topics(self):
        result = validate(self.root, require_full_pool=True)
        self.assertEqual(result['full_pool_gate'], 'pass')
        self.assertEqual(result['missing_splits'], [])
        self.assertEqual(result['splits']['holdout']['eligible']['rare_control'], 0)

    def test_source_tamper_is_detected(self):
        source = self.root / 'sources' / '2306.04136v1.json'
        source.write_text(source.read_text().replace('KAPING', 'UNSUPPORTED'))
        with self.assertRaisesRegex(ValueError, 'frozen annotation artifact changed'):
            validate(self.root)

    def test_example_cannot_name_a_different_study(self):
        self.mutate_annotation(lambda data: data['topics'][0]['brief_support']['example'].update(label='Invented research'))
        with self.assertRaisesRegex(ValueError, 'unsupported research example'):
            validate(self.root)

    def test_real_quote_cannot_point_to_wrong_offset(self):
        self.mutate_annotation(lambda data: data['topics'][0]['brief_support']['advantage'].update(char_start=0))
        with self.assertRaisesRegex(ValueError, 'evidence quote/hash'):
            validate(self.root)

    def test_duplicate_topic_does_not_fill_another_position(self):
        self.mutate_annotation(lambda data: data['topics'].append(data['topics'][0].copy()))
        with self.assertRaisesRegex(ValueError, 'duplicate distinctness group'):
            validate(self.root)

    def test_unknown_advantage_prevents_eligible_label(self):
        self.mutate_annotation(lambda data: data['topics'][0]['brief_support'].update(advantage=None))
        with self.assertRaisesRegex(ValueError, 'eligible topic has unknown evidence'):
            validate(self.root)

    def test_automated_review_is_not_human_validation(self):
        self.mutate_annotation(lambda data: data['topics'][0].update(review_status='human_validated'))
        with self.assertRaisesRegex(ValueError, 'masquerade as human review'):
            validate(self.root)


if __name__ == '__main__':
    unittest.main()
