-- +goose Up
-- Until now a collection could only be started by the collector's own
-- configuration file, so a user could neither choose a scope nor see its
-- coverage before reading a ranking built from it. A requested collection is
-- the same scope row, entered one stage earlier.
ALTER TABLE source_collection_scopes DROP CONSTRAINT source_collection_scopes_status_check;
ALTER TABLE source_collection_scopes ADD CONSTRAINT source_collection_scopes_status_check
    CHECK (status IN ('requested', 'running', 'partial', 'complete', 'failed'));

-- The provider check never learned the two collectors added later, so a patent
-- or web scope passed Go validation and then failed at the database.
ALTER TABLE source_collection_scopes DROP CONSTRAINT source_collection_scopes_provider_check;
ALTER TABLE source_collection_scopes ADD CONSTRAINT source_collection_scopes_provider_check
    CHECK (provider IN ('crossref', 'arxiv', 'github', 'patent', 'web'));

-- request_id is idempotency, not scope: the same id with the same input is the
-- same collection, and with a different input it is a conflict rather than an
-- overwrite. request_fingerprint is what makes that difference visible.
ALTER TABLE source_collection_scopes
    ADD COLUMN request_id TEXT NOT NULL DEFAULT '',
    ADD COLUMN request_fingerprint TEXT NOT NULL DEFAULT ''
        CHECK (request_fingerprint = '' OR request_fingerprint ~ '^[0-9a-f]{64}$'),
    ADD COLUMN request_urls JSONB NOT NULL DEFAULT '[]'::jsonb,
    ADD COLUMN requested_at TIMESTAMPTZ;
CREATE UNIQUE INDEX source_collection_scopes_request
    ON source_collection_scopes(owner_id, request_id) WHERE request_id <> '';

-- +goose Down
DROP INDEX source_collection_scopes_request;
ALTER TABLE source_collection_scopes
    DROP COLUMN request_id, DROP COLUMN request_fingerprint,
    DROP COLUMN request_urls, DROP COLUMN requested_at;
ALTER TABLE source_collection_scopes DROP CONSTRAINT source_collection_scopes_provider_check;
ALTER TABLE source_collection_scopes ADD CONSTRAINT source_collection_scopes_provider_check
    CHECK (provider IN ('crossref', 'arxiv', 'github'));
ALTER TABLE source_collection_scopes DROP CONSTRAINT source_collection_scopes_status_check;
ALTER TABLE source_collection_scopes ADD CONSTRAINT source_collection_scopes_status_check
    CHECK (status IN ('running', 'partial', 'complete', 'failed'));
