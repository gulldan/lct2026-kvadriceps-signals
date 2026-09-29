-- Кэш спанов теггера: ключ — хеш текста и версия теггера.

CREATE TABLE IF NOT EXISTS span_cache (
    text_sha256   CHAR(64)    NOT NULL,
    model_version TEXT        NOT NULL,
    spans         JSONB       NOT NULL,
    created_at    TIMESTAMPTZ NOT NULL DEFAULT now(),
    PRIMARY KEY (text_sha256, model_version)
);
