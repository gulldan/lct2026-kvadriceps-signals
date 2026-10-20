"""Запуск: python3 -m unittest discover -s scripts/tools/data -p 'test_signalgraph*.py'."""

import copy
import hashlib
import importlib.util
import json
from pathlib import Path
import unittest
from datetime import date

from audit_signalgraph_corpus import audit, publication_day

ROOT = Path(__file__).resolve().parents[3]
spec = importlib.util.spec_from_file_location("test_manifest", ROOT / "scripts/tools/demo/signalgraph_test_manifest.py")
manifest_tools = importlib.util.module_from_spec(spec)
spec.loader.exec_module(manifest_tools)


def verify_fixture(fixture):
    if fixture["version"] != 1 or fixture["synthetic"] is not True:
        raise ValueError("fixtures must be explicit versioned synthetic data")
    ids = set()
    for r in fixture["records"]:
        if r["id"] in ids:
            raise ValueError("duplicate fixture ID")
        ids.add(r["id"])
        if hashlib.sha256(r["text"].encode()).hexdigest() != r["source"]["content_hash"]:
            raise ValueError("fixture text hash mismatch")
        for a in r["anchors"]:
            if not 0 <= a["start"] < a["end"] <= len(r["text"]) or r["text"][a["start"]:a["end"]] != a["quote"]:
                raise ValueError("fixture anchor mismatch")
        if not r["source"]["test_run_id"].startswith("tr-test-"):
            raise ValueError("fixture could enter production corpus")


class FoundationTests(unittest.TestCase):
    def test_fixture_integrity_and_independent_oracle(self):
        fixture_bytes = (ROOT / "backend/platform/pkg/sourcedoc/testdata/v1/corpus.json").read_bytes()
        self.assertEqual(fixture_bytes, (ROOT / "frontend/tests/fixtures/signalgraph-corpus-v1.json").read_bytes())
        fixture = json.loads(fixture_bytes)
        verify_fixture(fixture)
        works = {r["source"]["canonical_id"] for r in fixture["records"] if r["source"]["source_kind"] == "paper"}
        self.assertEqual(len(works), fixture["oracle"]["canonical_scientific_works"])
        self.assertEqual(fixture["oracle"]["early_quarter_counts"]["2025-Q4"], 0)
        self.assertIsNone(fixture["oracle"]["early_quarter_counts"]["2026-Q1"])
        excluded = [r["id"] for r in fixture["records"] if r["source"]["observed_at"] > fixture["cutoff"]]
        self.assertEqual(excluded, fixture["oracle"]["excluded_future_revisions"])
        for field, value in [("text", "tampered text"), ("anchors", [{"start": 0, "end": 7, "quote": "made up"}])]:
            changed = copy.deepcopy(fixture)
            changed["records"][0][field] = value
            with self.assertRaises(ValueError):
                verify_fixture(changed)

    def test_manifest_cleanup_is_bounded(self):
        run = "tr-test-1d3f9a88-7fd2-4983-959c-a8298749d6fe"
        m = dict(version=1, run_id=run, owner_id=run, object_prefix="tests/"+run+"/", document_ids=["3b8d0bf4-851d-46a1-9bbf-7f5bb6f982ba"])
        sql = manifest_tools.cleanup_sql(m)
        self.assertIn("FOR UPDATE", sql)
        self.assertIn("DELETE FROM documents WHERE id IN", sql)
        self.assertIn("status <> 'uploaded'", sql)
        for field, value in [("document_ids", []), ("document_ids", m["document_ids"]*51), ("owner_id", "other"), ("run_id", "live"), ("document_ids", ["'; DELETE FROM documents;--"]), ("object_prefix", "documents/")]:
            with self.subTest(field=field, value=value), self.assertRaises(ValueError):
                manifest_tools.validate({**m, field: value})

    def test_read_only_audit_does_not_promote_parser_or_future_dates(self):
        cutoff = date(2026, 8, 31)
        for value in ("1601-01-01", "2026-09-01", "2023-02-29", "2024", ""):
            self.assertIsNone(publication_day(value, cutoff))
        self.assertEqual(publication_day("2024-02-29", cutoff), "2024-02-29")
        doc = dict(id="doc", title="Retrieval-augmented generation", filename="paper.pdf", kind="", status="indexed", chunk_count=3, content_hash="a"*64, published_at="2024-02-29", created_at="2024-02-29", source_ref="pdf")
        snapshot = dict(documents=[doc], manifests=[])
        result = audit(snapshot, cutoff)
        self.assertEqual(result["directions"]["rag"]["classifications"], {"ineligible": 1})
        self.assertEqual(result, audit(snapshot, cutoff))
        unrelated = {**doc, "title": "Регенерация сорбента и извлечение золота"}
        self.assertEqual(audit(dict(documents=[unrelated], manifests=[]), cutoff)["directions"]["rag"]["matched"], 0)
        snapshot["manifests"] = [dict(path="fixture", metadata=[dict(arxiv_id="2401.00001v1", published="2024-01-01", updated="2024-01-01")], uploads=[dict(ok=True, document_id="doc", arxiv_id="2401.00001v1")])]
        self.assertEqual(audit(snapshot, cutoff)["directions"]["rag"]["classifications"], {"recoverable": 1})
        snapshot["manifests"][0]["metadata"][0]["updated"] = "2026-09-01"
        self.assertEqual(audit(snapshot, cutoff)["directions"]["rag"]["classifications"], {"ineligible": 1})


if __name__ == "__main__":
    unittest.main()
