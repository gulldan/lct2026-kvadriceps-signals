import contextlib
import io
import json
import unittest
from types import SimpleNamespace
from urllib.error import HTTPError
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

    def test_unchanged_inputs_skip_only_theme_recalculation(self):
        saved = {}
        store = SimpleNamespace(get=saved.get, set=lambda key, value: saved.update({key: value.encode()}))

        def run():
            self.writes.clear()
            with patch.object(itc, "login"), patch.object(itc, "gw", self.gateway), \
                 patch.object(itc, "signature_store", return_value=store), \
                 patch.object(itc, "embed_all", side_effect=lambda texts: [[1.0, 0.0]] * len(texts)), \
                 patch.object(itc, "DRY", False), contextlib.redirect_stdout(io.StringIO()):
                itc.main()
            return [path for path, _, _ in self.writes]

        themes = ["/clusters/c0/itc", "/clusters/c1/itc"]
        self.assertEqual(run(), themes)
        self.assertEqual(run(), [])
        with patch.object(itc, "FORCE", True):
            self.assertEqual(run(), themes)
        self.themes[1]["params"] = {"members": ["d2"]}
        self.assertEqual(run(), themes)

    def test_hypothesis_link_requires_unambiguous_cited_document_membership(self):
        self.themes[1]["params"] = {"members": ["d3", "d4"]}
        evidence = {
            "backed": [{"document_id": "d1", "stance": "supports", "snippet": "Evidence"}] * 2,
            "unrelated": [{"document_id": "outside", "stance": "supports", "snippet": "Evidence"}],
            "split": [{"document_id": d, "stance": "supports", "snippet": "Evidence"} for d in ("d1", "d3")],
            "context": [{"document_id": "d1", "stance": "context", "snippet": "Background"}],
            "input": [{"document_id": "d1", "stance": "supports", "snippet": "Request", "origin": "input"}],
            "partial": [{"document_id": "d1", "stance": "supports", "snippet": "Evidence"},
                        {"document_id": "d3", "stance": "context", "snippet": "Other source"}],
        }

        def grounded(path, payload=None, method=None):
            if payload is None and path == "/hypotheses":
                return [{"id": key, "statement": "Identical semantic vector"} for key in evidence]
            if payload is None and path.endswith("/evidence"):
                return evidence[path.split("/")[2]]
            return self.gateway(path, payload, method)

        with patch.object(itc, "login"), patch.object(itc, "gw", grounded), \
             patch.object(itc, "embed_all", side_effect=lambda texts: [[1.0, 0.0]] * len(texts)), \
             patch.object(itc, "DRY", False), contextlib.redirect_stdout(io.StringIO()):
            itc.main()
        self.assertEqual(
            [(path, payload) for path, payload, _ in self.writes if path.startswith("/hypotheses/")],
            [("/hypotheses/backed/itc", {"cluster_id": "c0"})],
        )

    def test_concurrent_hypothesis_change_skips_only_expected_rejections(self):
        self.themes[1]["params"] = {"members": ["d3", "d4"]}
        for endpoint, code, reason, skipped in (
            ("evidence", 404, "not found", True),
            ("itc", 404, "not found", True),
            ("itc", 400, "invalid argument: topic association requires cited documents", True),
            ("itc", 400, "invalid argument: cited documents belong to multiple topics", True),
            ("itc", 400, "invalid argument: topic membership is not supported by cited documents", True),
            ("itc", 409, "resource changed; reload before retrying", True),
            ("itc", 409, "request precondition failed", False),
            ("evidence", 409, "resource changed; reload before retrying", False),
            ("evidence", 400, "invalid argument: topic membership is not supported by cited documents", False),
            ("itc", 400, "invalid argument: associated topic has no valid ITC", False),
            ("itc", 400, "invalid request", False),
            ("itc", 400, None, False),
            ("evidence", 404, None, False),
            ("evidence", 403, "forbidden", False),
            ("itc", 503, "service temporarily unavailable", False),
        ):
            with self.subTest(endpoint=endpoint, code=code, reason=reason):
                self.writes.clear()
                body = json.dumps({"error": reason}).encode() if reason else b"<html>Error</html>"
                error = HTTPError("http://gateway", code, "Error", {}, io.BytesIO(body))

                def changed(path, payload=None, method=None):
                    if path == f"/hypotheses/changed/{endpoint}":
                        raise error
                    if path == "/hypotheses":
                        return [{"id": key} for key in ("changed", "backed")]
                    if path.endswith("/evidence"):
                        return [{"document_id": "d1", "stance": "supports", "snippet": "Evidence"}]
                    return self.gateway(path, payload, method)

                with patch.object(itc, "login"), patch.object(itc, "gw", changed), \
                     patch.object(itc, "embed_all", side_effect=lambda texts: [[1.0, 0.0]] * len(texts)), \
                     patch.object(itc, "DRY", False), contextlib.redirect_stdout(io.StringIO()):
                    if skipped:
                        itc.main()
                    else:
                        with self.assertRaises(HTTPError) as caught:
                            itc.main()
                        self.assertIs(caught.exception, error)
                expected = [("/hypotheses/backed/itc", {"cluster_id": "c0"})] if skipped else []
                self.assertEqual(
                    [(path, payload) for path, payload, _ in self.writes if path.startswith("/hypotheses/")],
                    expected,
                )

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
