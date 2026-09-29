from pathlib import Path
import sys
import unittest

sys.path.insert(0, str(Path(__file__).resolve().parents[1]))
from signalgraph.temporal import measure, quarters
from signalgraph.emergence import assess
from signalgraph.manifest import canonical


class TemporalTests(unittest.TestCase):
    def corpus(self):
        dates = [("first", "2023-02", "month"), ("second", "2023-05-04", "day"),
                 ("duplicate", "2023-06-09", "day"), ("year", "2023", "year"), ("unknown", None, None)]
        records = [{"id": name, "snapshot_id": name+"-snapshot", "source": {"revision": "v1",
                    "publication": {"value": value, "precision": precision, "basis": "primary metadata"} if value else None}}
                   for name, value, precision in dates]
        works = [{"id": "w1", "kind": "paper", "document_ids": ["first", "duplicate"]},
                 {"id": "w2", "kind": "paper", "document_ids": ["second"]},
                 {"id": "w3", "kind": "paper", "document_ids": ["year"]},
                 {"id": "w4", "kind": "paper", "document_ids": ["unknown"]}]
        return {"records": records, "works": works,
                "input": {"request": {"start": "2023-01-01", "cutoff": "2023-09-15T23:59:59Z"}}}

    def test_dedup_precision_and_missing_exposure(self):
        result = measure(self.corpus(), ["w1", "w2", "w3", "w4"])
        self.assertEqual([p["found"]["scientific_works"] for p in result["series"]], [1, 1, 0])
        self.assertTrue(all(p["normalized_activity"] is None for p in result["series"]))
        self.assertTrue(all(p["comparable_exposure"] is None for p in result["series"]))
        self.assertFalse(result["series"][-1]["complete_period"])
        self.assertEqual(result["earliest"]["date"]["precision"], "year")
        self.assertEqual(len(result["unallocated"]), 2)

    def test_examined_zero_is_distinct_from_gap_and_partial_quarter(self):
        coverage = [{"start": "2023-01-01", "end_exclusive": "2024-01-01", "complete": True,
                     "basis": "frozen controlled dated population manifest", "work_ids": ["w1", "w2"]}]
        result = measure(self.corpus(), ["w1"], coverage)
        self.assertEqual(result["series"][0]["normalized_activity"], 1)
        self.assertEqual(result["series"][1]["normalized_activity"], 0)
        self.assertEqual(result["series"][1]["comparable_scientific_works"], 0)
        self.assertIsNone(result["series"][2]["comparable_scientific_works"])
        self.assertIsNone(result["series"][2]["normalized_activity"])

    def test_quarter_end_and_invalid_membership(self):
        self.assertTrue(quarters("2024-01-01", "2024-03-31T23:59:59Z")[0]["complete_period"])
        self.assertFalse(quarters("2024-02-01", "2024-03-31T23:59:59Z")[0]["complete_period"])
        with self.assertRaises(ValueError):
            measure(self.corpus(), ["foreign"])
        with self.assertRaises(ValueError):
            measure(self.corpus(), ["w1", "w1"])

    def test_one_complete_keyword_scope_cannot_cover_the_whole_direction(self):
        corpus = self.corpus()
        corpus["coverage_enrollment"] = {"scientific_scope_ids": ["broad-ai", "exact-paper"],
                                          "unscoped_scientific_work_ids": []}
        coverage = [{"start": "2023-01-01", "end_exclusive": "2023-07-01", "complete": True,
                     "basis": {"scope_id": "exact-paper"}, "work_ids": ["w1"]},
                    {"start": "2023-04-01", "end_exclusive": "2023-10-01", "complete": True,
                     "basis": {"scope_id": "broad-ai"}, "work_ids": ["w1", "w2"]}]
        result = measure(corpus, ["w1"], coverage)
        self.assertIsNone(result["series"][0]["normalized_activity"])
        self.assertEqual(result["scientific_scope_ids"], ["broad-ai", "exact-paper"])
        self.assertEqual([period["covered_scientific_scope_ids"] for period in result["series"]],
                         [["exact-paper"], ["broad-ai", "exact-paper"], ["broad-ai"]])
        self.assertEqual([sorted(set(result["scientific_scope_ids"]) - set(period["covered_scientific_scope_ids"]))
                          for period in result["series"]], [["broad-ai"], [], ["exact-paper"]])
        self.assertEqual(result["series"][1]["normalized_activity"], 0)
        self.assertIsNone(result["series"][2]["normalized_activity"])
        corpus["coverage_enrollment"] = {"scientific_scope_ids": ["exact-paper"],
                                          "unscoped_scientific_work_ids": ["w2"]}
        result = measure(corpus, ["w1"], coverage)
        self.assertIsNone(result["series"][0]["normalized_activity"])
        self.assertEqual(result["unscoped_scientific_work_ids"], ["w2"])
        self.assertTrue(all("unscoped_scientific_work_ids" not in period for period in result["series"]))

    def test_shared_coverage_provenance_bounds_size_without_changing_measurements(self):
        corpus = self.corpus()
        corpus["input"]["request"].update(start="2022-01-01", cutoff="2026-09-18T00:00:00Z")
        for number in range(1193 - len(corpus["works"])):
            identifier = f"paper:arxiv:2301.{number:05d}"
            corpus["records"].append({"id": identifier, "snapshot_id": identifier,
                                      "source": {"revision": "v1", "publication": None}})
            corpus["works"].append({"id": identifier, "kind": "paper", "document_ids": [identifier]})
        for record in corpus["records"][:2]:
            record["query_matched"] = True
        baseline = measure(corpus, ["w1", "w2"], [])
        scopes = [f"{number:064x}" for number in range(72)]
        unscoped = sorted(work["id"] for work in corpus["works"])
        corpus["coverage_enrollment"] = {
            "scientific_scope_ids": scopes, "unscoped_scientific_work_ids": unscoped,
        }
        result = measure(corpus, ["w1", "w2"], [])
        self.assertEqual(len(result["series"]), 19)
        self.assertEqual(result["scientific_scope_ids"], scopes)
        self.assertEqual(result["unscoped_scientific_work_ids"], unscoped)
        for actual, expected in zip(result["series"], baseline["series"], strict=True):
            self.assertNotIn("unscoped_scientific_work_ids", actual)
            self.assertNotIn("missing_scientific_scope_ids", actual)
            self.assertEqual(sorted(set(result["scientific_scope_ids"]) - set(actual["covered_scientific_scope_ids"])),
                             scopes)
            self.assertEqual(actual, expected)
        self.assertEqual(assess(result), assess(baseline))
        legacy = {key: value for key, value in result.items()
                  if key not in ("unscoped_scientific_work_ids", "scientific_scope_ids")}
        legacy["series"] = [{**{key: value for key, value in period.items() if key != "covered_scientific_scope_ids"},
                             "unscoped_scientific_work_ids": unscoped, "missing_scientific_scope_ids": scopes}
                            for period in result["series"]]
        compact_bytes, legacy_bytes = len(canonical(result)), len(canonical(legacy))
        self.assertGreater(115 * legacy_bytes, 16 << 20)
        self.assertLess(115 * compact_bytes, 16 << 20)


if __name__ == "__main__":
    unittest.main()
