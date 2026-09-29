"""Настройка не должна открывать разметку, источники и файлы заморозки другого разбиения."""
import json
from pathlib import Path
import tempfile
import unittest
from unittest.mock import patch

from validate_benchmark import validate, validate_split


class SplitIsolationTests(unittest.TestCase):
    def setUp(self):
        self.root = Path(__file__).parent.resolve()
        self.allowed = {'protocol-freeze.json', 'PROTOCOL.md'}
        for path in (self.root.glob('freeze.development.json'),
                     self.root.glob('extensions/freeze.development.*.json')):
            for pin in path:
                self.allowed.add(str(pin.relative_to(self.root)))
                self.allowed.update(json.loads(pin.read_text())['sha256'])

    def test_selected_split_cannot_read_other_splits(self):
        original = Path.open

        def guarded(path, *args, **kwargs):
            if path.is_relative_to(self.root):
                self.assertIn(str(path.relative_to(self.root)), self.allowed)
            return original(path, *args, **kwargs)

        with patch.object(Path, 'open', guarded):
            result = validate_split(self.root, 'development')
        self.assertEqual(set(result['splits']), {'development'})
        self.assertEqual(result['splits']['development']['works'], 64)
        self.assertEqual(result['full_pool_gate'], 'not_checked')

    def test_subset_cannot_claim_full_pool_acceptance(self):
        with self.assertRaisesRegex(ValueError, 'full-pool gate'):
            validate(self.root, require_full_pool=True, splits=('development',))

    def test_selected_source_tamper_is_still_detected(self):
        with tempfile.TemporaryDirectory() as directory:
            root = Path(directory)
            for name in self.allowed:
                target = root / name
                target.parent.mkdir(parents=True, exist_ok=True)
                target.write_bytes((self.root / name).read_bytes())
            source = root / 'sources/2306.04136v1.json'
            source.write_text(source.read_text().replace('KAPING', 'UNSUPPORTED'))
            with self.assertRaisesRegex(ValueError, 'frozen annotation artifact changed'):
                validate_split(root, 'development')


if __name__ == '__main__':
    unittest.main()
