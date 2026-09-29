import os
import unittest
from unittest.mock import patch

import worker


class LabelSignalsTest(unittest.TestCase):
    def test_markup_and_bibliography_do_not_become_a_label(self):
        """Карточка Crossref вместо текста статьи не должна называть тему."""
        record = (
            "Title: Analysis of coal mining Language: zh Source: Crossref "
            "Source type: article DOI: 10.36012/se Publisher: Encyclopaedic "
            "Matched query: flotation Abstract: "
            "<jats:p>The paper describes flotation recovery of copper ore</jats:p>"
        )
        cleaned = worker.content_signals([record])
        self.assertEqual(cleaned, [
            "Analysis of coal mining",
            "The paper describes flotation recovery of copper ore",
        ])
        label, _, keywords = worker.keyword_fallback(cleaned)
        lowered = label.lower() + " " + " ".join(keywords).lower()
        for token in ("jats", "crossref", "record", "doi", "publisher"):
            self.assertNotIn(token, lowered, label)

    def test_metadata_does_not_become_a_research_topic(self):
        record = "Title: 试论重矿物在物源分析中的研究应用 Language: zh Source: Crossref DOI: 10.123/example URL: https://example.test Abstract: Heavy minerals identify sediment provenance."
        self.assertEqual(worker.content_signals([record]), [
            "试论重矿物在物源分析中的研究应用", "Heavy minerals identify sediment provenance.",
        ])

    def test_chinese_title_survives_when_generation_is_unavailable(self):
        title = "试论重矿物在物源分析中的研究应用"
        self.assertEqual(worker.keyword_fallback([title])[0], title)

    def test_ordinary_document_text_is_preserved(self):
        self.assertEqual(worker.content_signals(["Polymer coatings improve corrosion resistance."]),
                         ["Polymer coatings improve corrosion resistance."])

    def test_label_request_has_a_budget_and_uses_document_content(self):
        response = {"choices": [{"message": {"content": '{"label":"Коррозионная стойкость полимерных покрытий","summary":"Защита стали","keywords":["полимеры"]}'}}]}
        with patch.object(worker, "VLLM_URL", "https://model.example"), \
             patch.object(worker, "VLLM_API_KEY", "test-only"), \
             patch.object(worker, "throttle_vllm"), \
             patch.object(worker, "http", return_value=response) as call, \
             patch.dict(os.environ, {"VLLM_REASONING_EFFORT": "none"}):
            label, *_ = worker.label_community(["Title: Polymer coatings Language: en Source: Crossref URL: https://example.test"])
        self.assertEqual(label, "Коррозионная стойкость полимерных покрытий")
        payload = call.call_args.args[1]
        self.assertEqual(payload["max_tokens"], 1024)
        self.assertEqual(payload["reasoning_effort"], "none")
        self.assertIn("Polymer coatings", payload["messages"][0]["content"])
        self.assertNotIn("Crossref", payload["messages"][0]["content"])

    def test_foreign_language_answer_is_retried_as_a_translation(self):
        # Такое название проверку общности проходит: в нём нет знакомых ей слов.
        chinese = {"choices": [{"message": {"content": '{"label":"碳捕集与氢能系统中余热回收与溶剂再生方法","summary":"","keywords":["氢能"]}'}}]}
        russian = {"choices": [{"message": {"content": '{"label":"Улавливание CO₂ в водородных системах","summary":"Рекуперация тепла","keywords":["водород"]}'}}]}
        with patch.object(worker, "VLLM_URL", "https://model.example"), \
             patch.object(worker, "VLLM_API_KEY", "test-only"), \
             patch.object(worker, "throttle_vllm"), \
             patch.object(worker, "time"), \
             patch.object(worker, "http", side_effect=[chinese, russian]) as call:
            label, *_ = worker.label_community(["Title: 碳捕集与氢能系统中余热回收 Language: zh Source: Crossref"])
        self.assertEqual(label, "Улавливание CO₂ в водородных системах")
        second = call.call_args_list[1].args[1]["messages"][0]["content"]
        self.assertIn("Переведи на русский язык", second)
        self.assertIn("碳捕集与氢能系统中余热回收与溶剂再生方法", second)
        # Во втором запросе исходных документов уже нет: только перевод ответа.
        self.assertNotIn("Ниже названия, ключевые слова и аннотации", second)


if __name__ == "__main__":
    unittest.main()
