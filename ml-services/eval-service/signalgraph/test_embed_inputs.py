"""Загрязнённый вход и сменившийся сервис эмбеддингов отклоняются до публикации."""
import io
import hashlib
import json
from pathlib import Path
import tempfile
import unittest
from unittest.mock import patch

from embed_inputs import embed, MODEL, REVISION
from prepare_inputs import export


class EmbeddingBoundaryTests(unittest.TestCase):
    def setUp(self):
        temporary = tempfile.TemporaryDirectory()
        self.addCleanup(temporary.cleanup)
        self.root = Path(temporary.name)
        export(Path(__file__).parent, self.root, 'development')
        self.path = self.root / 'development.json'
        self.output = self.root / 'vectors.json'

    def test_export_does_not_contain_judgments_or_gold_membership(self):
        raw = self.path.read_text()
        for key in ('eligible_gold', 'decision_basis', 'distinctness_group', 'brief_support', 'topics'):
            self.assertNotIn('"' + key + '"', raw)

    def test_changed_model_is_not_silently_accepted(self):
        body = io.BytesIO(json.dumps({'model': MODEL, 'revision': REVISION + '-changed', 'data': []}).encode())
        with patch('urllib.request.urlopen', return_value=body):
            with self.assertRaisesRegex(ValueError, 'model/revision'):
                embed(self.path, self.output, 'http://test.invalid')
        self.assertFalse(self.output.exists())

    def test_future_revision_is_rejected_before_inference(self):
        data = json.loads(self.path.read_text())
        data['sources'][0]['available_at'] = '2027-01-01T00:00:00Z'
        self.path.write_text(json.dumps(data))
        with patch('urllib.request.urlopen') as request:
            with self.assertRaisesRegex(ValueError, 'cutoff'):
                embed(self.path, self.output, 'http://test.invalid')
            request.assert_not_called()
        self.assertFalse(self.output.exists())

    def test_changed_abstract_is_rejected_before_inference(self):
        data = json.loads(self.path.read_text())
        data['sources'][0]['text'] += ' unsupported changed content'
        self.path.write_text(json.dumps(data))
        with patch('urllib.request.urlopen') as request:
            with self.assertRaisesRegex(ValueError, 'frozen hash'):
                embed(self.path, self.output, 'http://test.invalid')
            request.assert_not_called()
        self.assertFalse(self.output.exists())

    def test_instruction_is_bound_to_actual_encoder_input(self):
        inputs = []
        def response(request, **_):
            texts = json.loads(request.data)['input']
            inputs.extend(texts)
            return io.BytesIO(json.dumps({'model': MODEL, 'revision': REVISION,
                'embeddings': [[1.] + [0.] * 1023 for _ in texts]}).encode())
        instruction = 'Identify the main technical approach.'
        with patch('urllib.request.urlopen', side_effect=response):
            embed(self.path, self.output, 'http://test.invalid', instruction=instruction)
        artifact = json.loads(self.output.read_text())
        self.assertEqual(artifact['instruction'], instruction)
        for text, vector in zip(inputs, artifact['vectors'], strict=True):
            self.assertTrue(text.startswith('Instruct: '+instruction+'\nQuery: '))
            self.assertEqual(vector['embedding_input_sha256'], hashlib.sha256(text.encode()).hexdigest())
