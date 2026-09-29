-- +goose Up
-- Additive: old readers keep using parser metadata; no date is backfilled.
ALTER TABLE documents ADD COLUMN source_metadata JSONB;
ALTER TABLE documents ADD CONSTRAINT documents_source_contract CHECK (
    source_metadata IS NULL OR (
        jsonb_typeof(source_metadata) = 'object'
        AND source_metadata->>'version' = '1'
        AND source_metadata->>'source_kind' IN ('paper', 'patent', 'company_material', 'analysis', 'software_repository', 'hypothesis')
        AND source_metadata->>'provider' IN ('arxiv', 'github', 'openalex', 'crossref', 'manual', 'generated')
        AND source_metadata->>'access_scope' IN ('public', 'shared', 'owner')
        AND length(source_metadata->>'native_id') BETWEEN 1 AND 512
        AND length(source_metadata->>'canonical_id') BETWEEN 1 AND 512
        AND length(source_metadata->>'revision') BETWEEN 1 AND 512
        AND source_metadata->>'content_hash' ~ '^[0-9a-f]{64}$'
        AND source_metadata->>'hash_algorithm' IN ('sha256', 'blake3')
        AND source_metadata->>'observed_at' IS NOT NULL
        AND (source_metadata->>'source_kind' <> 'software_repository' OR source_metadata->'publication' = 'null'::jsonb)
    ) IS TRUE
);
CREATE UNIQUE INDEX documents_source_revision ON documents (
    owner_id, (source_metadata->>'access_scope'), (source_metadata->>'provider'),
    (source_metadata->>'native_id'), (source_metadata->>'revision'),
    (coalesce(source_metadata->>'test_run_id', ''))
) WHERE source_metadata IS NOT NULL;

-- +goose Down
-- Rollback uses an old compatible reader; preserve provenance on downgrade.
-- +goose StatementBegin
DO $$ BEGIN
    RAISE EXCEPTION 'source provenance is retained: roll back the reader, not migration 17';
END $$;
-- +goose StatementEnd
