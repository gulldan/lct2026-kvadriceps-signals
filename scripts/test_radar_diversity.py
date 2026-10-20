"""Разметка каталога и положительный балл не заменяют проверенную отраслевую выдачу."""

import copy
import importlib.util
from pathlib import Path
import unittest

from test_radar_acceptance import scored_fixture

SPEC = importlib.util.spec_from_file_location("radar_diversity", Path(__file__).with_name("radar-diversity.py"))
DIVERSITY = importlib.util.module_from_spec(SPEC)
SPEC.loader.exec_module(DIVERSITY)


class DiversityTest(unittest.TestCase):
    def test_only_fresh_strict_grounded_complete_independent_signals_count(self):
        detail = scored_fixture()
        saved = {"query": "технологии", "final_response_received": True,
                 "response": {"status": "ready", "detail": detail},
                 "timing": {"fresh_collection_proven": True}}
        review = {"run": "run.json", "candidate_id": "tech-0", "industry_id": "industry",
                  "source_id": "0", "quote": detail["papers"][0]["title"],
                  "reviewed": True, "reason": "Проверена предметная принадлежность источника."}

        def report(source=saved, reviews=None):
            return DIVERSITY.analyze({"run.json": source}, reviews or [review],
                                     {"industry", "water"}, "fixture-v1")

        good = report()
        self.assertTrue(good["strict_batch_passed"])
        self.assertEqual(good["industry_count"], 1)
        self.assertFalse(good["content_coverage_passed"])
        for defect in ("cached", "interrupted", "old_model", "shortfall", "abstention", "one_group", "missing"):
            with self.subTest(defect=defect):
                broken = copy.deepcopy(saved)
                card = broken["response"]["detail"]["candidates"][0]
                if defect == "cached":
                    broken["timing"]["fresh_collection_proven"] = False
                elif defect == "interrupted":
                    broken["final_response_received"] = False
                elif defect == "old_model":
                    broken["response"]["detail"]["classifier_version"] = "old"
                elif defect == "shortfall":
                    card["shortfall"] = {"code": "below_top", "reason": "Ниже планки"}
                elif defect == "abstention":
                    card["model_score"].update(label="insufficient_data", data_gap="too_early")
                elif defect == "one_group":
                    card["groups"] = 1
                else:
                    card["card"].pop("example")
                self.assertEqual(report(broken)["industry_count"], 0)
        for mutation in ({"quote": "Несуществующая цитата"}, {"source_id": "foreign"}, {"reviewed": False}):
            self.assertEqual(report(reviews=[{**review, **mutation}])["industry_count"], 0)
        with self.assertRaises(ValueError):
            report(reviews=[review, {**review, "industry_id": "water"}])
        with self.assertRaises(ValueError):
            report(reviews=[{**review, "industry_id": "invented"}])
        repeated = [copy.deepcopy(saved), copy.deepcopy(saved)]
        for index, source in enumerate(repeated):
            answer = source["response"]["detail"]
            answer["candidates"][0]["qualification"]["canonical_name"] = answer["papers"][index]["title"]
        with self.assertRaises(ValueError):
            DIVERSITY.analyze({"run.json": repeated[0], "second.json": repeated[1]},
                              [review, {**review, "run": "second.json", "industry_id": "water"}],
                              {"industry", "water"}, "fixture-v1")


if __name__ == "__main__":
    unittest.main()
