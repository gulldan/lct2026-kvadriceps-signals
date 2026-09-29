-- +goose Up
ALTER TABLE documents ADD COLUMN generation bigint NOT NULL DEFAULT 1 CHECK (generation > 0);
CREATE TABLE ingestion_outbox (
    id text PRIMARY KEY,
    document_id text NOT NULL REFERENCES documents(id) ON DELETE CASCADE,
    generation bigint NOT NULL CHECK (generation > 0),
    route text NOT NULL,
    payload bytea NOT NULL CHECK (octet_length(payload) <= 5242880),
    event_type text NOT NULL,
    created_at timestamptz NOT NULL DEFAULT now(),
    published_at timestamptz
);
CREATE INDEX ingestion_outbox_pending ON ingestion_outbox(created_at, id) WHERE published_at IS NULL;

-- +goose Down
DROP TABLE ingestion_outbox;
ALTER TABLE documents DROP COLUMN generation;
