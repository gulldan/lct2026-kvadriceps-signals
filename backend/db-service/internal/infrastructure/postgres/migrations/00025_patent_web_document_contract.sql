-- +goose Up
-- Migration 24 admitted collector scopes; documents retain their independent
-- provenance constraint. Preserve every existing clause and admit both adapters.
ALTER TABLE documents DROP CONSTRAINT documents_source_contract;
ALTER TABLE documents ADD CONSTRAINT documents_source_contract CHECK (
    source_metadata IS NULL OR (
        jsonb_typeof(source_metadata) = 'object'
        AND source_metadata->>'version' = '1'
        AND source_metadata->>'source_kind' IN ('paper', 'patent', 'company_material', 'analysis', 'software_repository', 'hypothesis')
        AND source_metadata->>'provider' IN ('arxiv', 'github', 'openalex', 'crossref', 'manual', 'generated', 'patent', 'web')
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

-- +goose Down
-- Keep imported patent/web source identities and all immutable provenance.
-- +goose StatementBegin
DO $$ BEGIN
    RAISE EXCEPTION 'Preserve patent/web provenance; roll back compatible application images instead';
END $$;
-- +goose StatementEnd
