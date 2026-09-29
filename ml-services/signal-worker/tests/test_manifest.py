import copy
import json
from pathlib import Path
import sys
import unittest
from unittest.mock import patch

sys.path.insert(0, str(Path(__file__).resolve().parents[1]))
from signalgraph.manifest import canonical, digest, load_input
from signalgraph import manifest as loader


class ManifestTests(unittest.TestCase):
    def fixture(self):
        text = "Проверяемый исходный текст."
        source = {"provider": "manual", "source_kind": "paper", "revision": "v1", "native_id": "paper-1",
                  "canonical_id": "paper-1", "access_scope": "owner", "observed_at": "2024-02-01T00:00:00Z",
                  "publication": {"value": "2024-01", "precision": "month", "basis": "source"},
                  "content_hash": digest(text.encode()), "hash_algorithm": "sha256", "url": "https://example.org/paper"}
        model = {"name": "fixed", "revision": "fixed-v1", "dimension": 2}
        chunk = {"id": "chunk-1", "index": 0, "text": text, "char_start": 0, "char_end": len(text),
                 "vector": [1.0, 0.5], "metadata": {}, "lexical_text": text}
        snapshot = {"document_id": "doc-1", "owner_id": "reader", "snapshot_id": "snapshot-1",
                    "model": model, "parser_revision": "parser-v1", "pipeline_revision": "pipeline-v1",
                    "source": source, "text_hash": digest(text.encode()), "chunks": [chunk]}
        objects = {"snapshot": canonical(snapshot), "text": text.encode()}
        index = {key: snapshot[key] for key in ["document_id", "owner_id", "snapshot_id", "model",
                                               "parser_revision", "pipeline_revision"]}
        index.update({"data": {"key": "snapshot", "sha256": digest(objects["snapshot"])},
                      "text": {"key": "text", "sha256": digest(objects["text"])}, "chunk_count": 1,
                      "source_revision": "v1", "raw_hash": source["content_hash"]})
        record = {"id": "doc-1", "title": "Paper", "source": source, "source_hash": digest(canonical(source)),
                  "index": index, "limitations": [], "representation": "full_text"}
        manifest = {"version": "analysis-input-v2", "records": [record], "model": model, "pages": [],
                    "request": {"owner_id": "reader", "cutoff": "2024-12-31T23:59:59Z",
                                "mode": "as_observed", "shared_corpus": True}}
        return manifest, objects

    def load(self, manifest, objects):
        raw = canonical(manifest)
        return load_input(raw, digest(raw), lambda key, maximum: objects[key])

    def test_exact_text_vector_and_canonical_work(self):
        manifest, objects = self.fixture()
        corpus = self.load(manifest, objects)
        self.assertEqual(len(corpus["works"]), 1)
        self.assertEqual(corpus["records"][0]["text"], objects["text"].decode())
        self.assertAlmostEqual(sum(v*v for v in corpus["vectors"][0]["vector"]), 1)

    def test_input_budget_matches_go_without_relaxing_source_text_budget(self):
        manifest, objects = self.fixture()
        raw = canonical(manifest)
        raw += b' ' * (loader.MAX_INPUT - len(raw))
        self.assertEqual(len(load_input(raw, digest(raw), lambda key, maximum: objects[key])['works']), 1)
        with self.assertRaisesRegex(ValueError, 'size limit'):
            load_input(raw + b' ', digest(raw + b' '), lambda *_: self.fail('oversized input reached storage'))
        self.assertEqual(loader.MAX_TEXT, 16 << 20)

    def test_verified_article_body_precedes_title_only_alias(self):
        title = {'id': 'a', 'representation': 'metadata_or_text',
                 'source': {'provider': 'web', 'text_status': 'metadata_only'}}
        article = {'id': 'z', 'representation': 'metadata_or_text',
                   'source': {'provider': 'web', 'text_status': 'article_text'}}
        abstract = {'id': 'b', 'representation': 'abstract', 'source': {'provider': 'arxiv'}}
        pdf = {'id': 'c', 'representation': 'full_text', 'source': {'provider': 'arxiv'}}
        self.assertEqual(sorted([title, article, abstract, pdf], key=loader.representation_preference),
                         [pdf, article, abstract, title])

    def test_cumulative_budget_passes_exact_total_and_bounds_next_read(self):
        manifest, objects = self.fixture()
        raw = canonical(manifest)
        total = sum(map(len, objects.values()))
        calls = []

        def read(key, maximum):
            calls.append((key, maximum))
            return objects[key]

        with patch.object(loader, 'MAX_TOTAL', total):
            self.assertEqual(load_input(raw, digest(raw), read)['read_bytes'], total)
        self.assertEqual(calls[-1], ('text', len(objects['text'])))
        calls.clear()
        with patch.object(loader, 'MAX_TOTAL', total - 1):
            with self.assertRaisesRegex(ValueError, 'artifact budget'):
                load_input(raw, digest(raw), read)
        self.assertEqual(calls[-1], ('text', len(objects['text']) - 1))
        calls.clear()
        with patch.object(loader, 'MAX_TOTAL', len(objects['snapshot'])):
            with self.assertRaisesRegex(ValueError, 'artifact budget'):
                load_input(raw, digest(raw), read)
        self.assertEqual([key for key, _ in calls], ['snapshot'])

    def test_tamper_or_foreign_scope_cannot_be_repaired_from_live_index(self):
        manifest, objects = self.fixture()
        broken = copy.deepcopy(objects)
        broken["text"] += b" changed"
        with self.assertRaisesRegex(ValueError, "text hash"):
            self.load(manifest, broken)
        manifest["request"]["owner_id"] = "foreign"
        with self.assertRaisesRegex(ValueError, "foreign source"):
            load_input(canonical(manifest), digest(canonical(manifest)), lambda *args: self.fail("read before ACL"))

    def test_span_model_and_future_coverage(self):
        manifest, objects = self.fixture()
        snapshot = json.loads(objects["snapshot"])
        snapshot["chunks"][0]["char_start"] = 1
        objects["snapshot"] = canonical(snapshot)
        manifest["records"][0]["index"]["data"]["sha256"] = digest(objects["snapshot"])
        with self.assertRaisesRegex(ValueError, "fragment differs"):
            self.load(manifest, objects)
        manifest, objects = self.fixture()
        manifest["model"] = {**manifest["model"], "dimension": 3}
        with self.assertRaisesRegex(ValueError, "mixed embedding"):
            self.load(manifest, objects)
        manifest, objects = self.fixture()
        manifest["pages"] = [{"captured_at": "2025-01-01T00:00:00Z"}]
        with self.assertRaisesRegex(ValueError, "future collection"):
            self.load(manifest, objects)

    def test_test_source_requires_matching_namespace_and_owner_before_storage_read(self):
        manifest, objects = self.fixture()
        record = manifest['records'][0]
        record['source']['test_run_id'] = 'tr-test-228aab26-6fc0-416f-8fa3-fb0101f168f0'
        record['source']['access_scope'] = 'public'
        record['source_hash'] = digest(canonical(record['source']))
        snapshot = json.loads(objects['snapshot'])
        snapshot['source'] = record['source']
        objects['snapshot'] = canonical(snapshot)
        record['index']['data']['sha256'] = digest(objects['snapshot'])
        manifest['request']['test_run_id'] = record['source']['test_run_id']
        self.assertEqual(len(self.load(manifest, objects)['works']), 1)
        for namespace, owner in [('', 'reader'), ('another-test', 'reader'),
                                 (record['source']['test_run_id'], 'foreign')]:
            manifest['request'].update(test_run_id=namespace, owner_id=owner)
            raw = canonical(manifest)
            with self.assertRaisesRegex(ValueError, 'non-product source'):
                load_input(raw, digest(raw), lambda *args: self.fail('read before namespace/owner check'))


if __name__ == "__main__":
    unittest.main()
