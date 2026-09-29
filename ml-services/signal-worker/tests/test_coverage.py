from pathlib import Path
import sys
import unittest
from unittest.mock import patch

sys.path.insert(0, str(Path(__file__).resolve().parents[1]))
from signalgraph.coverage import MAX_COVERAGE_OBJECT, MAX_COVERAGE_PAGES, enrich
from signalgraph import manifest as loader
from signalgraph.manifest import canonical, digest
import test_manifest


class CoverageTests(unittest.TestCase):
    def fixture(self):
        source = {"provider": "arxiv", "native_id": "2301.00001", "revision": "v1", "content_hash": "a"*64,
                  "source_kind": "paper", "publication": {"value": "2023-01-20", "precision": "day", "basis": "provider"}}
        record = {"id": "doc", "source": source, "representation": "full_text", "limitations": [], "snapshot_id": "snap"}
        scope = {"from": "2023-01-01", "through": "2023-12-31", "query": "bounded control"}
        raw = b"recorded primary response"
        page = {"provider": "arxiv", "scope": scope, "raw_sha256": digest(raw), "received": 1, "exhausted": True,
                "observed_at": "2023-12-31T00:00:00Z", "cursor": "0", "next_cursor": "", "provider_total": 1,
                "records": [{"source": source}]}
        stored = {"page": page, "prepared": [{"source": source, "text_status": "pdf", "limitations": []}]}
        page_raw = canonical(stored)
        ref = {"provider": "arxiv", "scope": scope, "raw": {"key": "raw", "sha256": digest(raw)},
               "manifest": {"key": "manifest", "sha256": digest(page_raw)}, "received": 1, "exhausted": True,
               "scope_id": "scope", "run_id": "run", "number": 0, "rejected": 0}
        corpus = {"records": [record], "works": [{"id": "work", "kind": "paper", "document_ids": ["doc"]}],
                  "input": {"request": {"cutoff": "2023-12-31T23:59:59Z"}, "pages": [ref]}}
        return corpus, {"raw": raw, "manifest": page_raw}, stored

    def test_complete_population_and_material_status(self):
        corpus, objects, _ = self.fixture()
        enrich(corpus, lambda key, maximum: objects[key], page_limit=1)
        self.assertEqual(corpus["records"][0]["text_status"], "pdf")
        self.assertEqual(corpus["coverage_windows"][0]["work_ids"], ["work"])
        self.assertEqual(corpus["coverage_read_status"]["read_pages"], 1)
        self.assertEqual(corpus["coverage_limitations"], [])

    def test_optional_coverage_budget_keeps_verified_sources_and_all_enrollment(self):
        for case, page_count, remaining, page_limit in (
            ("page_count", MAX_COVERAGE_PAGES + 1, None, MAX_COVERAGE_PAGES),
            ("remaining_bytes", 2, 3 * MAX_COVERAGE_OBJECT, 1),
        ):
            with self.subTest(case=case):
                manifest, objects = test_manifest.ManifestTests().fixture()
                template = self.fixture()[0]["input"]["pages"][0]
                manifest["pages"] = [
                    {**template, "provider": "arxiv" if i % 2 else "crossref", "scope_id": f"scope-{i}",
                     "captured_at": "2023-12-31T00:00:00Z"}
                    for i in range(page_count)
                ]
                raw = canonical(manifest)
                source_bytes = sum(map(len, objects.values()))
                total_limit = loader.MAX_TOTAL if remaining is None else source_bytes + remaining
                calls = []

                def read(key, maximum):
                    self.assertIn(key, objects, "optional collection objects must not be read")
                    calls.append(key)
                    return objects[key]

                with patch.object(loader, "MAX_TOTAL", total_limit):
                    corpus = loader.load_input(raw, digest(raw), read)
                self.assertEqual(calls, ["snapshot", "text"])
                self.assertEqual(corpus["read_bytes"], source_bytes)
                self.assertEqual(corpus["input"], manifest)
                self.assertEqual(corpus["input_sha256"], digest(raw))
                self.assertEqual(len(corpus["records"]), len(manifest["records"]))
                for field, value in manifest["records"][0].items():
                    self.assertEqual(corpus["records"][0][field], value)
                self.assertEqual(corpus["records"][0]["text"], objects["text"].decode())
                self.assertEqual(corpus["coverage_windows"], [])
                self.assertEqual(corpus["coverage_enrollment"], {
                    "scientific_scope_ids": sorted(ref["scope_id"] for ref in manifest["pages"]),
                    "unscoped_scientific_work_ids": [corpus["works"][0]["id"]],
                })
                self.assertEqual(corpus["coverage_read_status"], {
                    "status": "not_read_page_budget", "total_pages": page_count, "read_pages": 0,
                    "page_limit": page_limit, "object_byte_limit": MAX_COVERAGE_OBJECT,
                })
                self.assertEqual(corpus["coverage_limitations"], ["collection_coverage_not_read_page_budget"])
                objects["text"] += b"tampered"
                with patch.object(loader, "MAX_TOTAL", total_limit):
                    with self.assertRaisesRegex(ValueError, "source text hash mismatch"):
                        loader.load_input(raw, digest(raw), read)

    def test_missing_ready_input_capped_search_and_tamper_are_not_complete(self):
        for case in ["missing", "capped", "late", "tamper", "manifest_tamper"]:
            with self.subTest(case=case):
                corpus, objects, stored = self.fixture()
                if case == "missing":
                    corpus["records"], corpus["works"] = [], []
                elif case == "capped":
                    stored["page"]["limitations"] = ["provider_cap"]
                    objects["manifest"] = canonical(stored)
                    corpus["input"]["pages"][0]["manifest"]["sha256"] = digest(objects["manifest"])
                elif case == "late":
                    stored["page"]["observed_at"] = "2024-01-01T00:00:00Z"
                    objects["manifest"] = canonical(stored)
                    corpus["input"]["pages"][0]["manifest"]["sha256"] = digest(objects["manifest"])
                elif case == "manifest_tamper":
                    objects["manifest"] += b" "
                else:
                    objects["raw"] = b"changed response"
                if case in ["late", "tamper", "manifest_tamper"]:
                    with self.assertRaises(ValueError):
                        enrich(corpus, lambda key, maximum: objects[key])
                else:
                    enrich(corpus, lambda key, maximum: objects[key])
                    self.assertEqual(corpus["coverage_windows"], [])

    def test_observed_release_metadata_is_lower_bound_without_page_access(self):
        corpus, _, _ = self.fixture()
        record = corpus["records"][0]
        record["source"].update({"provider": "github", "source_kind": "software_repository", "publication": None,
                                  "github": {"repository_id": 123, "release_id": 7, "released_at": "2023-10-01T00:00:00Z"}})
        corpus["works"][0]["kind"] = "software_repository"
        corpus["input"]["pages"] = []
        enrich(corpus, lambda *args: self.fail("no page read"))
        self.assertEqual(len(corpus["releases"]), 1)
        self.assertEqual(corpus["releases"][0]["basis"]["kind"], "source_metadata")
        self.assertEqual(corpus["coverage_windows"], [])

    def test_exhausted_github_search_does_not_certify_scientific_zero(self):
        corpus, objects, stored = self.fixture()
        corpus["records"], corpus["works"] = [], []
        stored["page"].update(provider="github", received=0, provider_total=0, records=[])
        stored["prepared"] = []
        objects["manifest"] = canonical(stored)
        corpus["input"]["pages"][0].update(provider="github", received=0)
        corpus["input"]["pages"][0]["manifest"]["sha256"] = digest(objects["manifest"])
        enrich(corpus, lambda key, maximum: objects[key])
        self.assertEqual(corpus["coverage_windows"], [])
        self.assertEqual(corpus["coverage_enrollment"]["scientific_scope_ids"], [])


if __name__ == "__main__":
    unittest.main()
