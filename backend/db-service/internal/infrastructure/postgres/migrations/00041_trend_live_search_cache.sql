-- +goose Up
-- Разовый поиск по открытым источникам держался в памяти одного процесса:
-- перезапуск терял и собранное, и результат, вторая реплика собирала заново, а
-- повторный запрос жюри заново опрашивал arXiv, OpenAlex, патенты и веб. Здесь
-- лежат и сырые собранные записи, и готовый результат анализа, как того требует
-- постановка задачи: повторный или близкий запрос переиспользует собранное,
-- а обновляется только то, что устарело.
CREATE TABLE trend_live_searches (
    query       TEXT PRIMARY KEY,
    -- Ключ нормализованного набора терминов: «слабые сигналы в ИИ» и
    -- «ИИ, слабые сигналы» после перевода дают один набор и одну строку.
    terms_key   TEXT        NOT NULL,
    terms       TEXT[]      NOT NULL DEFAULT '{}',
    papers      INTEGER     NOT NULL DEFAULT 0,
    sources     INTEGER     NOT NULL DEFAULT 0,
    observed_at TIMESTAMPTZ NOT NULL,
    expires_at  TIMESTAMPTZ NOT NULL,
    detail      JSONB       NOT NULL
);
CREATE INDEX trend_live_searches_terms ON trend_live_searches (terms_key, observed_at DESC);
CREATE INDEX trend_live_searches_expires ON trend_live_searches (expires_at);

-- Сырые записи источников с паспортом: наименование, ссылка, дата, тип
-- источника, язык оригинала и уровень доверенности хранятся рядом с адресом,
-- по которому их получили.
CREATE TABLE trend_live_records (
    query          TEXT        NOT NULL REFERENCES trend_live_searches (query) ON DELETE CASCADE,
    url            TEXT        NOT NULL,
    channel        TEXT        NOT NULL,
    source         TEXT        NOT NULL DEFAULT '',
    title          TEXT        NOT NULL DEFAULT '',
    published_on   TEXT        NOT NULL DEFAULT '',
    language       TEXT        NOT NULL DEFAULT '',
    publisher_type TEXT        NOT NULL DEFAULT '',
    trust          TEXT        NOT NULL DEFAULT '',
    trust_basis    TEXT        NOT NULL DEFAULT '',
    content_hash   TEXT        NOT NULL DEFAULT '',
    observed_at    TIMESTAMPTZ NOT NULL,
    PRIMARY KEY (query, url)
);
CREATE INDEX trend_live_records_url ON trend_live_records (url);
CREATE INDEX trend_live_records_trust ON trend_live_records (trust);

-- +goose Down
DROP TABLE trend_live_records;
DROP TABLE trend_live_searches;
