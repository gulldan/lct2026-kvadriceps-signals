-- +goose Up
-- Объём поиска лежит в detail JSONB: приложению так удобнее, а тому, кто
-- оценивает нагрузку на модели, — нет. Представление даёт те же числа обычными
-- столбцами, чтобы прикинуть расход по десятку прогонов можно было одним
-- SELECT, не разбирая JSON руками и не спрашивая нас. Это чтение поверх той же
-- строки: ни копии данных, ни второго места, где правда может разойтись.
CREATE VIEW trend_live_search_volume AS
SELECT s.query,
       s.observed_at,
       s.papers,
       s.sources,
       (s.detail -> 'volume' ->> 'scanned')::bigint                     AS scanned,
       (s.detail -> 'volume' ->> 'fetched')::bigint                     AS fetched,
       (s.detail -> 'volume' ->> 'processed')::bigint                   AS processed,
       (s.detail -> 'volume' ->> 'stored')::bigint                      AS stored,
       (s.detail -> 'volume' ->> 'chars')::bigint                       AS chars,
       (s.detail -> 'volume' ->> 'pages')::bigint                       AS pages,
       (s.detail -> 'volume' ->> 'tokens')::bigint                      AS tokens,
       (s.detail -> 'volume' -> 'model' ->> 'calls')::bigint            AS model_calls,
       (s.detail -> 'volume' -> 'model' ->> 'failed')::bigint           AS model_failed,
       (s.detail -> 'volume' -> 'model' ->> 'prompt_tokens')::bigint    AS model_prompt_tokens,
       (s.detail -> 'volume' -> 'model' ->> 'completion_tokens')::bigint AS model_completion_tokens,
       (s.detail -> 'volume' -> 'model' ->> 'duration_ms')::bigint      AS model_duration_ms,
       (s.detail -> 'volume' -> 'analysis' ->> 'papers')::bigint        AS analysis_papers,
       (s.detail -> 'volume' -> 'analysis' ->> 'chars')::bigint         AS analysis_chars,
       (s.detail -> 'volume' -> 'analysis' ->> 'tokens')::bigint        AS analysis_tokens,
       (s.detail -> 'volume' -> 'classifier' ->> 'candidates')::bigint  AS classifier_candidates,
       (s.detail -> 'volume' -> 'classifier' ->> 'sources')::bigint     AS classifier_sources,
       (s.detail -> 'volume' -> 'classifier' ->> 'tokens')::bigint      AS classifier_tokens,
       (s.detail -> 'volume' -> 'classifier' ->> 'bytes')::bigint       AS classifier_bytes
FROM trend_live_searches s;

-- Ответы, записанные до появления поля, дают NULL, а не ноль: неизмеренное не
-- равно нулевому, и средние по расходу не должны их считать.
COMMENT ON VIEW trend_live_search_volume IS
    'Объём одного живого поиска и расход моделей; NULL — ответ записан до появления volume';

-- +goose Down
DROP VIEW trend_live_search_volume;
