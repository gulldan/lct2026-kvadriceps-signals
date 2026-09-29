-- +goose Up
-- Одно правило применяется до выдачи строк и до создания связей с документом.
-- +goose StatementBegin
CREATE FUNCTION document_readable(reader_id text, owner_id text, source jsonb, shared boolean, revoked boolean)
RETURNS boolean LANGUAGE sql IMMUTABLE PARALLEL SAFE
RETURN coalesce(reader_id <> '' AND NOT revoked AND (
    reader_id = owner_id OR
    CASE WHEN source IS NULL THEN shared
         ELSE source->>'access_scope' = 'public'
              OR (source->>'access_scope' = 'shared' AND shared)
    END), false);
-- +goose StatementEnd

-- +goose Down
DROP FUNCTION document_readable(text, text, jsonb, boolean, boolean);
