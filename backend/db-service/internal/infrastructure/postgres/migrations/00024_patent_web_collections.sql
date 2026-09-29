-- +goose Up
-- The same bounded leases and immutable provenance now accept internal collectors.
ALTER TABLE source_collection_scopes
    DROP CONSTRAINT source_collection_scopes_provider_check;
ALTER TABLE source_collection_scopes
    ADD CONSTRAINT source_collection_scopes_provider_check
    CHECK (provider IN ('crossref', 'arxiv', 'github', 'patent', 'web'));

-- +goose Down
-- +goose StatementBegin
DO $$ BEGIN
    IF EXISTS (SELECT 1 FROM source_collection_scopes WHERE provider IN ('patent', 'web')) THEN
        RAISE EXCEPTION 'Preserve patent/web checkpoints; roll back compatible application images instead';
    END IF;
END $$;
-- +goose StatementEnd
ALTER TABLE source_collection_scopes
    DROP CONSTRAINT source_collection_scopes_provider_check;
ALTER TABLE source_collection_scopes
    ADD CONSTRAINT source_collection_scopes_provider_check
    CHECK (provider IN ('crossref', 'arxiv', 'github'));
