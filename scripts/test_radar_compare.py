import importlib.util
import unittest
from pathlib import Path

SPEC = importlib.util.spec_from_file_location("radar_compare", Path(__file__).with_name("radar-compare.py"))
COMPARE = importlib.util.module_from_spec(SPEC)
SPEC.loader.exec_module(COMPARE)


class ComparisonTest(unittest.TestCase):
    def test_only_completed_matching_queries_can_have_a_delta(self):
        saved = {"query": "одинаковый запрос", "final_response_received": True,
                 "response": {"status": "ready", "detail": {"candidates": []}}}
        self.assertTrue(COMPARE.compare(saved, saved)["comparable"])
        self.assertIsNone(COMPARE.compare(saved, {**saved, "final_response_received": False})["delta"])
        with self.assertRaises(ValueError):
            COMPARE.compare(saved, {**saved, "query": "другой запрос"})
        card = {"model": "reader", "source_contexts": [
            {"doi": "10.1234/example", "coverage": "excerpted_fulltext", "provider": "europepmc"}],
            "prior_art": {"status": "unavailable"}}
        repeated = {**saved, "response": {"status": "ready", "detail": {"candidates": [{"card": card}, {"card": card}]}}}
        measured = COMPARE.metrics(repeated)
        self.assertEqual(measured["fulltext_sources"], 1)
        self.assertEqual(measured["prior_art_checked"], 0)
        self.assertEqual(measured["prior_art_unavailable"], 2)
        self.assertFalse(COMPARE.compare(saved, repeated)["comparable"])
        card["study_limitations"] = "Испытание проведено только в лаборатории."
        card["evidence"] = [{"field": "study_limitations", "verified": True, "source_id": "paper",
                             "quote": "Laboratory trial only"}]
        card["source_contexts"][0].update(source_id="paper", text="Laboratory trial only")
        measured = COMPARE.metrics(repeated)
        self.assertEqual(measured["study_limitations_cards"], 2)
        self.assertEqual(measured["verified_core_claims"], 0)
        self.assertEqual(measured["cited_fulltext_sources"], 1)
        card["evidence"][0]["verified"] = False
        self.assertEqual(COMPARE.metrics(repeated)["study_limitations_cards"], 0)
