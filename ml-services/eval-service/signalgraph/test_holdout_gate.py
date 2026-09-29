"""Контроль целостности holdout, а не свидетельство качества ранжирования."""
import json
from pathlib import Path
import tempfile
import unittest

from holdout_gate import VERSION, canonical, digest, code_files, verify, claim_run
from prepare_inputs import export


class HoldoutGateTests(unittest.TestCase):
    def setUp(self):
        temporary = tempfile.TemporaryDirectory()
        self.addCleanup(temporary.cleanup)
        self.root = Path(temporary.name)
        for path in code_files(self.root):
            path.parent.mkdir(parents=True, exist_ok=True)
            path.write_bytes(b'frozen candidate code')
        self.path = self.root/'candidate.json'
        self.pin = {'version': VERSION, 'split': 'holdout',
                    'method': {'quality_status': 'not_frozen'},
                    'code': {str(p.relative_to(self.root)): digest(p.read_bytes()) for p in code_files(self.root)},
                    'benchmark': {'not-opened-labels.json': 'a'*64}}
        self.seal()

    def seal(self):
        self.path.write_bytes(canonical(self.pin))
        self.sha256 = digest(self.path.read_bytes())

    def test_unaccepted_quality_does_not_prevent_frozen_evaluation(self):
        result = verify(self.path, self.sha256, root=self.root)
        self.assertEqual(result['method']['quality_status'], 'not_frozen')
        self.assertFalse((self.root/'not-opened-labels.json').exists())

    def test_changed_code_is_rejected(self):
        code_files(self.root)[0].write_bytes(b'changed after freeze')
        with self.assertRaisesRegex(ValueError, 'code artifact changed'):
            verify(self.path, self.sha256, root=self.root)

    def test_added_module_is_rejected(self):
        added = self.root/'ml-services/signal-worker/signalgraph/new_ranker.py'
        added.parent.mkdir(parents=True)
        added.write_text('changed implementation')
        with self.assertRaisesRegex(ValueError, 'membership changed'):
            verify(self.path, self.sha256, root=self.root)

    def test_changed_freeze_is_rejected(self):
        self.pin['method']['quality_status'] = 'accepted'
        self.path.write_bytes(canonical(self.pin))
        with self.assertRaisesRegex(ValueError, 'SHA-256 mismatch'):
            verify(self.path, self.sha256, root=self.root)

    def test_export_requires_freeze_before_reading_labels(self):
        with self.assertRaisesRegex(ValueError, 'explicit candidate freeze'):
            export(self.root/'no-benchmark', self.root/'out', 'holdout')

    def test_second_run_is_rejected(self):
        claim_run(self.path, self.sha256, 'b'*64, 'c'*64)
        with self.assertRaisesRegex(ValueError, 'already started'):
            claim_run(self.path, self.sha256, 'b'*64, 'c'*64)
        receipt = json.loads(self.path.with_suffix('.opened.json').read_bytes())
        self.assertEqual(receipt['source_input_sha256'], 'b'*64)


if __name__ == '__main__':
    unittest.main()
