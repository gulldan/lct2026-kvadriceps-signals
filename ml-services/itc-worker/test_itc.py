import contextlib
import io
import json
import unittest
from unittest.mock import patch

import itc


class CalculationContractTest(unittest.TestCase):
    def setUp(self):
        self.themes = [
            {"id": f"c{i}", "label": "Same theme", "document_count": 2,
             "params": {"members": ["d1", "d2"]}, "representatives": []}
            for i in range(2)
        ]
        self.writes = []

    def documents(self):
        return [{"id": "d1", "published_at": "2024"}, {"id": "d2", "published_at": "2025"}]

    def gateway(self, path, payload=None, method=None):
        if payload is not None:
            self.writes.append((path, payload, method))
            return {}
        if path == "/clusters?view=full":
            return self.themes
        if path == "/hypotheses":
            return []
        if path == "/hypotheses/graph":
            return {"edges": []}
        if path.startswith("/documents?"):
            return [] if "offset=500" in path else self.documents()
        if path.startswith("/documents/"):
            return {"chunks": [{"text": "Materials research 2024"}]}
        raise AssertionError(path)

    def test_equal_ranks_do_not_compare_dicts_or_replace_membership(self):
        components = [({k: {"value": n} for k in ("SM", "NV", "IP", "HR")}, 0.5, 5) for n in (40, 60)]
        with patch.object(itc, "login"), patch.object(itc, "gw", self.gateway), \
             patch.object(itc, "embed_all", return_value=[[1.0, 0.0]]), \
             patch.object(itc, "compute_components", side_effect=components), \
             patch.object(itc, "DRY", False), contextlib.redirect_stdout(io.StringIO()):
            itc.main()
        self.assertEqual([path for path, _, _ in self.writes], ["/clusters/c0/itc", "/clusters/c1/itc"])
        for _, payload, _ in self.writes:
            self.assertEqual(set(payload), {"itc"})
        self.assertEqual(self.themes[0]["params"], {"members": ["d1", "d2"]})

    def test_missing_source_data_stops_before_any_write(self):
        self.themes[0]["params"] = {}
        with patch.object(itc, "login"), patch.object(itc, "gw", self.gateway):
            with self.assertRaisesRegex(ValueError, "document membership"):
                itc.main()
        self.assertEqual(self.writes, [])

    def test_weights_explain_aggregate_and_missing_dates_are_explicit(self):
        with open(itc.LOCAL_RUBRIC, encoding="utf-8") as source:
            rubric = json.load(source)
        components, score, _ = itc.compute_components(rubric, {"SM": 0.6, "NV": 0.8, "IP": 0.7, "HR": 0.4}, True)
        self.assertAlmostEqual(sum(c["weight"] for c in components.values()), 1)
        self.assertAlmostEqual(sum(c["contribution"] for c in components.values()), score)

        def undated(path, payload=None, method=None):
            if payload is None and path.startswith("/documents?"):
                return [] if "offset=500" in path else [{"id": "d1"}, {"id": "d2"}]
            if payload is None and path.startswith("/documents/"):
                return {"chunks": [{"text": "Materials research"}]}
            return self.gateway(path, payload, method)

        with patch.object(itc, "login"), patch.object(itc, "gw", undated), \
             patch.object(itc, "embed_all", return_value=[[1.0, 0.0]]), \
             patch.object(itc, "DRY", False), contextlib.redirect_stdout(io.StringIO()):
            itc.main()
        for _, payload, _ in self.writes:
            signals = payload["itc"]["signals"]
            self.assertFalse(signals["freshness_available"])
            self.assertTrue(signals["novelty_available"])
            self.assertEqual(signals["pub_count"], 0)
            self.assertEqual(signals["embedded_count"], 2)

    def test_organization_line_wrapping_does_not_inflate_coverage(self):
        self.assertEqual(itc.extract_orgs("Shanghai University; Shanghai\nUniversity"), {"Shanghai University"})

    def test_equal_known_dates_remain_measured_coordinates(self):
        def same_year(path, payload=None, method=None):
            if payload is None and path.startswith("/documents?"):
                return [] if "offset=500" in path else [{"id": "d1", "published_at": "2025"}, {"id": "d2", "published_at": "2025"}]
            return self.gateway(path, payload, method)

        with patch.object(itc, "login"), patch.object(itc, "gw", same_year), \
             patch.object(itc, "embed_all", return_value=[[1.0, 0.0]]), \
             patch.object(itc, "DRY", False), contextlib.redirect_stdout(io.StringIO()):
            itc.main()
        for _, payload, _ in self.writes:
            self.assertTrue(payload["itc"]["signals"]["freshness_available"])
            self.assertEqual(payload["itc"]["axes"]["momentum"], 0.5)

    def test_same_year_different_months_are_distinct_coordinates(self):
        # С точностью до года темы одного года слились бы в одну линию.
        months = ["2026-01-10", "2026-04-10", "2026-07-10", "2026-09-10"]
        self.themes = [
            {"id": f"c{i}", "label": "Same theme", "document_count": 1,
             "params": {"members": [f"d{i}"]}, "representatives": []}
            for i in range(4)
        ]

        def by_month(path, payload=None, method=None):
            if payload is None and path.startswith("/documents?"):
                return [] if "offset=500" in path else [
                    {"id": f"d{i}", "published_at": m} for i, m in enumerate(months)]
            if payload is None and path.startswith("/documents/"):
                return {"chunks": [{"text": "Materials research"}]}
            return self.gateway(path, payload, method)

        with patch.object(itc, "login"), patch.object(itc, "gw", by_month), \
             patch.object(itc, "embed_all", return_value=[[1.0, 0.0]] * 4), \
             patch.object(itc, "DRY", False), contextlib.redirect_stdout(io.StringIO()):
            itc.main()
        momentum = [payload["itc"]["axes"]["momentum"] for _, payload, _ in self.writes]
        self.assertEqual(momentum, sorted(momentum))
        self.assertEqual(len(set(momentum)), 4)
        self.assertTrue(all(p["itc"]["signals"]["freshness_available"] for _, p, _ in self.writes))

    def test_sparse_dates_do_not_place_a_theme(self):
        # Две даты на 40 документов — ниже порога датированности: координата была бы догадкой.
        self.themes[0]["document_count"] = 40

        with patch.object(itc, "login"), patch.object(itc, "gw", self.gateway), \
             patch.object(itc, "embed_all", return_value=[[1.0, 0.0]]), \
             patch.object(itc, "DRY", False), contextlib.redirect_stdout(io.StringIO()):
            itc.main()
        available = {path: payload["itc"]["signals"]["freshness_available"] for path, payload, _ in self.writes}
        self.assertEqual(available, {"/clusters/c0/itc": False, "/clusters/c1/itc": True})


if __name__ == "__main__":
    unittest.main()
