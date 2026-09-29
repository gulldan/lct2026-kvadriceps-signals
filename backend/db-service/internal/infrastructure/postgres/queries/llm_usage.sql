-- name: SetLLMUsageDaily :exec
-- Повторное или запоздавшее зеркало не уменьшает накопленный итог.
INSERT INTO llm_usage_daily
  (day, model, operation, requests, prompt_tokens, completion_tokens, cost_nano_usd, updated_at)
VALUES ($1, $2, $3, $4, $5, $6, $7, now())
ON CONFLICT (day, model, operation) DO UPDATE SET
  requests          = GREATEST(llm_usage_daily.requests, excluded.requests),
  prompt_tokens     = GREATEST(llm_usage_daily.prompt_tokens, excluded.prompt_tokens),
  completion_tokens = GREATEST(llm_usage_daily.completion_tokens, excluded.completion_tokens),
  cost_nano_usd     = GREATEST(llm_usage_daily.cost_nano_usd, excluded.cost_nano_usd),
  updated_at        = now();

-- name: ListLLMUsageDaily :many
-- The durable ledger for a date range, oldest first, for the Metrics dashboard.
SELECT day, model, operation, requests, prompt_tokens, completion_tokens, cost_nano_usd
FROM llm_usage_daily
WHERE day >= sqlc.arg('from_day') AND day <= sqlc.arg('to_day')
ORDER BY day, model, operation;
