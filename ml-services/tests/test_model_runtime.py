"""Развёрнутые API Transformers/FastAPI на крошечной локальной модели Qwen.

Без скачивания моделей и GPU; версии зависимостей — как в сервисах.
"""

import math
import os
import runpy
import tempfile
import unittest
from pathlib import Path
from unittest.mock import patch

from fastapi.testclient import TestClient
from tokenizers import Tokenizer, models, pre_tokenizers
from transformers import PreTrainedTokenizerFast, Qwen3Config, Qwen3ForCausalLM

ROOT = Path(__file__).resolve().parents[1]


class ModelRuntimeTest(unittest.TestCase):
    @classmethod
    def setUpClass(cls):
        cls.directory = tempfile.TemporaryDirectory()
        cls.addClassCleanup(cls.directory.cleanup)
        cls.checkpoint = cls.directory.name
        vocabulary = {
            word: i for i, word in enumerate(["[PAD]", "[EOS]", "[UNK]", "yes", "no", "material", "strength", "reuse"])
        }
        tokenizer = Tokenizer(models.WordLevel(vocabulary, unk_token="[UNK]"))
        tokenizer.pre_tokenizer = pre_tokenizers.WhitespaceSplit()
        PreTrainedTokenizerFast(
            tokenizer_object=tokenizer, pad_token="[PAD]", eos_token="[EOS]", unk_token="[UNK]"
        ).save_pretrained(cls.checkpoint)
        Qwen3ForCausalLM(
            Qwen3Config(
                vocab_size=len(vocabulary),
                hidden_size=16,
                intermediate_size=32,
                num_hidden_layers=1,
                num_attention_heads=2,
                num_key_value_heads=2,
                head_dim=8,
                pad_token_id=0,
                eos_token_id=1,
            )
        ).save_pretrained(cls.checkpoint)

    def test_embedding_startup_and_http_contract(self):
        with patch.dict(os.environ, {"TORCH_DEVICE": "cpu", "F2LLM_MODEL": self.checkpoint}):
            runtime = runpy.run_path(str(ROOT / "f2llm-service/f2llm_server.py"))
        with TestClient(runtime["app"]) as client:
            self.assertEqual(client.get("/health").status_code, 200)
            response = client.post("/v1/embeddings", json={"input": ["material strength", "reuse"]})
            self.assertEqual(response.status_code, 200, response.text)
            self.assertEqual(client.post("/v1/embeddings", json={"input": ["x"] * 129}).status_code, 422)
            self.assertEqual(client.post("/v1/embeddings", json={"input": "x" * 65537}).status_code, 422)
            vectors = response.json()["data"]
            self.assertEqual([row["index"] for row in vectors], [0, 1])
            for row in vectors:
                self.assertEqual(len(row["embedding"]), 16)
                self.assertTrue(all(math.isfinite(value) for value in row["embedding"]))
                self.assertAlmostEqual(sum(value**2 for value in row["embedding"]), 1.0, places=5)

    def test_reranker_startup_scores_and_top_n_contract(self):
        with patch.dict(os.environ, {"TORCH_DEVICE": "cpu", "RERANKER_MODEL": self.checkpoint}):
            runtime = runpy.run_path(str(ROOT / "reranker-service/reranker_server.py"))
        with TestClient(runtime["app"]) as client:
            self.assertEqual(client.get("/health").json()["mode"], "qwen3_yes_no")
            response = client.post(
                "/v1/rerank",
                json={
                    "query": "material",
                    "documents": ["strength", "reuse", "material strength"],
                    "top_n": 2,
                },
            )
            self.assertEqual(response.status_code, 200, response.text)
            self.assertEqual(client.post("/v1/rerank", json={"query": "q", "documents": ["x"] * 257}).status_code, 422)
            result = response.json()
            self.assertEqual(len(result["scores"]), 3)
            self.assertTrue(all(0 <= score <= 1 for score in result["scores"]))
            self.assertEqual(len(result["results"]), 2)
            for row in result["results"]:
                self.assertEqual(row["relevance_score"], result["scores"][row["index"]])
            self.assertGreaterEqual(result["results"][0]["relevance_score"], result["results"][1]["relevance_score"])
            self.assertEqual(
                client.post("/v1/rerank", json={"query": "material", "documents": []}).json()["scores"], []
            )


if __name__ == "__main__":
    unittest.main()
