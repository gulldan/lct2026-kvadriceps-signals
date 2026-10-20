import unittest
from unittest.mock import patch

import evaluate


class JudgeResponseTest(unittest.TestCase):
    def test_empty_or_truncated_response_retries_and_never_becomes_a_score(self):
        valid = {"choices": [{"message": {"content": '{"score":0.75}'}, "finish_reason": "stop"}]}
        for content, finish in ((None, "length"), ('{"score":1}', "length"), (None, "stop")):
            bad = {"choices": [{"message": {"content": content}, "finish_reason": finish}]}
            with self.subTest(content=content, finish=finish), \
                 patch.object(evaluate, "JUDGE_URL", "https://model.test"), \
                 patch.object(evaluate, "JUDGE_MODEL", "test-model"), \
                 patch.object(evaluate, "JUDGE_MAX_TOKENS", 4096), \
                 patch.dict(evaluate.os.environ, {}, clear=True), \
                 patch.object(evaluate, "http", side_effect=[bad, valid]) as call:
                self.assertEqual(evaluate.judge("test"), 0.75)
                self.assertEqual([c.args[1]["max_tokens"] for c in call.call_args_list], [4096, 8192])

    def test_invalid_scores_remain_missing(self):
        for content in ('{"score":1.5}', '{"score":true}', '{"score":NaN}', 'step 1: no score', 'null'):
            response = {"choices": [{"message": {"content": content}, "finish_reason": "stop"}]}
            with self.subTest(content=content), \
                 patch.object(evaluate, "JUDGE_URL", "https://model.test"), \
                 patch.object(evaluate, "JUDGE_MODEL", "test-model"), \
                 patch.dict(evaluate.os.environ, {}, clear=True), \
                 patch.object(evaluate, "http", return_value=response) as call:
                self.assertIsNone(evaluate.judge("test"))
                self.assertEqual(call.call_count, 2)
