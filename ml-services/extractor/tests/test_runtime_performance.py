"""Паритет пакетирования, пула токенизации и доступных численных режимов."""

import numpy as np
import pytest
import torch

from extractor.runtime import ModelRuntime, RuntimeConfig
from extractor.stage_e5_lr import E5LRStageScorer, INSTRUCTION, linear_probabilities
from extractor.tests.runtime_fixtures import checkpoint, offline
from extractor.tests.test_stage_e5_lr import lr_registry

__all__ = ["checkpoint", "offline", "lr_registry"]


def test_token_pool_preserves_logits_and_offsets(checkpoint):
    registry, _, _, _ = checkpoint()
    texts = ["We propose " + "quantum computing " * (i % 9 + 1) for i in range(140)]
    serial = ModelRuntime("toy", task="token-classification", registry=registry,
                          config=RuntimeConfig(tokenization_workers=1, batch_size=1))
    pooled = ModelRuntime("toy", task="token-classification", registry=registry,
                          config=RuntimeConfig(tokenization_workers=2, tokenization_chunk_size=16, batch_size=16))
    expected, actual = serial.predict(texts), pooled.predict(texts)
    try:
        repeated = pooled.predict(list(reversed(texts)))
        for a, b, c in zip(expected, actual, reversed(repeated), strict=True):
            assert a.offsets == b.offsets == c.offsets
            np.testing.assert_allclose(a.logits, b.logits, atol=1e-6, rtol=1e-6)
            np.testing.assert_array_equal(b.logits, c.logits)
    finally:
        serial.tokenization.close()
        pooled.tokenization.close()


def test_e5_length_buckets_match_previous_longest_logits(lr_registry):
    registry, _, _ = lr_registry
    runtime = E5LRStageScorer(registry, config=RuntimeConfig(batch_size=8, tokenization_chunk_size=8)).runtime
    sentences = ["We propose " + "quantum computing " * (i % 12 + 1) for i in range(70)]
    runtime.load()
    expected = []
    with torch.inference_mode():
        for start in range(0, len(sentences), 8):
            inputs = runtime.tokenizer([f"Instruct: {INSTRUCTION}\nQuery: {text}" for text in sentences[start:start+8]],
                                       padding=True, truncation=True, max_length=192, return_tensors="pt")
            hidden = runtime.backend(**inputs).last_hidden_state
            mask = inputs["attention_mask"].unsqueeze(-1).to(hidden.dtype)
            pooled = (hidden * mask).sum(dim=1) / mask.sum(dim=1).clamp(min=1e-9)
            expected.extend(torch.nn.functional.normalize(pooled.float(), p=2, dim=1).numpy())
    try:
        actual = runtime.encode(sentences)
        old_logits = np.array(expected) @ runtime.coefficients.T + runtime.intercept
        new_logits = actual @ runtime.coefficients.T + runtime.intercept
        np.testing.assert_allclose(old_logits, new_logits, atol=1e-6, rtol=1e-6)
        np.testing.assert_allclose(linear_probabilities(np.array(expected), runtime.coefficients, runtime.intercept, runtime.classes),
                                   runtime.probabilities(sentences), atol=1e-6, rtol=1e-6)
        np.testing.assert_array_equal(actual, runtime.encode(sentences))
    finally:
        runtime.tokenization.close()


def test_cpu_stays_eager_fp32_and_cuda_settings_come_from_environment(checkpoint, lr_registry):
    # На CPU настройки cuda_* не действуют: эталонные eager/fp32 и неизменные числа.
    config = RuntimeConfig(cuda_dtype="bf16", cuda_attention="sdpa")
    registry, _, _, _ = checkpoint()
    tagger = ModelRuntime("toy", task="token-classification", registry=registry, config=config).load()
    assert tagger.backend.model.config._attn_implementation == "eager"
    assert tagger.backend.dtype == torch.float32 and tagger.version.endswith("|cpu:fp32:eager")
    stage = E5LRStageScorer(lr_registry[0], config=config).runtime.load()
    assert stage.backend.config._attn_implementation == "eager"
    assert "|cpu:fp32:eager|" in stage.version
    stage.tokenization.close()
    tagger.tokenization.close()

    from extractor.assembly import runtime_configs

    def numerics(environ, device="cuda", e5_stage=True):
        return [c.torch_numerics for c in runtime_configs(device, e5_stage=e5_stage, environ=environ)]

    assert numerics({}) == ["cuda:fp32:sdpa", "cuda:fp32:sdpa"]
    assert numerics({"EXTRACTOR_CUDA_DTYPE": ""}, e5_stage=False) == ["cuda:fp32:sdpa", "cuda:fp32:sdpa"]
    assert numerics({"EXTRACTOR_STAGE_CUDA_DTYPE": "bf16"}) == ["cuda:fp32:sdpa", "cuda:bf16:sdpa"]
    assert numerics({"EXTRACTOR_CUDA_ATTENTION": "eager"}) == ["cuda:fp32:eager", "cuda:fp32:eager"]
    assert numerics({"EXTRACTOR_CUDA_DTYPE": "fp32", "EXTRACTOR_TAGGER_CUDA_DTYPE": "bf16",
                     "EXTRACTOR_STAGE_CUDA_ATTENTION": "sdpa", "EXTRACTOR_CUDA_ATTENTION": "eager"}
                    ) == ["cuda:bf16:eager", "cuda:fp32:sdpa"]
    assert numerics({"EXTRACTOR_CUDA_ATTENTION": "sdpa", "EXTRACTOR_CUDA_DTYPE": "bf16"},
                    device="cpu") == ["cpu:fp32:eager", "cpu:fp32:eager"]
    with pytest.raises(ValueError, match="cuda_attention"):
        runtime_configs("cuda", environ={"EXTRACTOR_CUDA_ATTENTION": "flash"})

    def batches(environ, device="cuda", e5_stage=True):
        return [c.batch_size for c in runtime_configs(device, e5_stage=e5_stage, environ=environ)]

    assert batches({}) == [16, 8] and batches({}, e5_stage=False) == [16, 16]
    assert batches({"EXTRACTOR_TAGGER_BATCH_SIZE": "64", "EXTRACTOR_STAGE_BATCH_SIZE": "32"},
                   device="cpu") == [64, 32]
    with pytest.raises(ValueError, match="EXTRACTOR_STAGE_BATCH_SIZE"):
        runtime_configs("cuda", environ={"EXTRACTOR_STAGE_BATCH_SIZE": "0"})


@pytest.mark.skipif(not torch.cuda.is_available(), reason="для bf16 и SDPA нужен CUDA-хост")
def test_cuda_sdpa_and_bf16_against_eager_fp32(checkpoint, lr_registry):
    # Паритет с допуском: SDPA и bf16 меняют последние знаки, но не метки.
    # Повтор в одной конфигурации обязан совпадать побитно.
    registry, _, _, _ = checkpoint()
    texts = ["We propose quantum computing.", "A method using a prototype."]
    modes = [("fp32", "eager"), ("fp32", "sdpa"), ("bf16", "sdpa")]
    runtimes = [ModelRuntime("toy", task="token-classification", registry=registry,
                             config=RuntimeConfig(device="cuda", cuda_dtype=dtype, cuda_attention=attention))
                for dtype, attention in modes]
    reference, sdpa, bf16 = [runtime.predict(texts) for runtime in runtimes]
    for base, fused, half in zip(reference, sdpa, bf16, strict=True):
        np.testing.assert_allclose(base.logits, fused.logits, atol=1e-4, rtol=1e-4)
        np.testing.assert_allclose(base.logits, half.logits, atol=2e-3, rtol=2e-2)
        for other in (fused, half):
            np.testing.assert_array_equal(base.logits.argmax(-1), other.logits.argmax(-1))
    for first, second in zip(bf16, runtimes[2].predict(texts), strict=True):
        np.testing.assert_array_equal(first.logits, second.logits)
    registry, _, _ = lr_registry
    stages = [E5LRStageScorer(registry, config=RuntimeConfig(device="cuda", cuda_dtype=dtype,
                                                             cuda_attention=attention)).runtime
              for dtype, attention in modes]
    base, fused, half = [runtime.probabilities(texts) for runtime in stages]
    np.testing.assert_allclose(base, fused, atol=1e-4, rtol=1e-4)
    np.testing.assert_allclose(base, half, atol=5e-3, rtol=2e-2)
    np.testing.assert_array_equal(base.argmax(-1), half.argmax(-1))
    np.testing.assert_array_equal(half, stages[2].probabilities(texts))
