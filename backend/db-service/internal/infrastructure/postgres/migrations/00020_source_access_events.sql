-- +goose Up
ALTER TABLE source_index_heads ADD COLUMN last_finish JSONB;
CREATE TABLE source_access_events (
    request_id TEXT PRIMARY KEY,
    document_id TEXT NOT NULL REFERENCES documents(id) ON DELETE CASCADE,
    owner_id TEXT NOT NULL,
    revoked BOOLEAN NOT NULL,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now()
);
CREATE INDEX source_access_events_document ON source_access_events(document_id, created_at);

-- +goose Down
DROP TABLE source_access_events;
ALTER TABLE source_index_heads DROP COLUMN last_finish;
