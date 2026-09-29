-- +goose Up
-- Mutable publication pointers are separate from immutable source provenance.
CREATE TABLE source_index_heads (
    document_id TEXT PRIMARY KEY REFERENCES documents(id) ON DELETE CASCADE,
    lease_token TEXT NOT NULL DEFAULT '',
    leased_until TIMESTAMPTZ NOT NULL DEFAULT '-infinity',
    active_snapshot_id TEXT,
    last_error TEXT NOT NULL DEFAULT '',
    legacy_pruned BOOLEAN NOT NULL DEFAULT false,
    revoked BOOLEAN NOT NULL DEFAULT false,
    updated_at TIMESTAMPTZ NOT NULL DEFAULT now()
);
CREATE TABLE source_index_snapshots (
    snapshot_id TEXT PRIMARY KEY,
    document_id TEXT NOT NULL REFERENCES documents(id) ON DELETE CASCADE,
    manifest JSONB NOT NULL,
    ready BOOLEAN NOT NULL DEFAULT false,
    pruned BOOLEAN NOT NULL DEFAULT false,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    ready_at TIMESTAMPTZ,
    UNIQUE (document_id, snapshot_id)
);
CREATE INDEX source_index_snapshots_document ON source_index_snapshots(document_id, created_at DESC);
CREATE TABLE source_corpus_epoch (id BOOLEAN PRIMARY KEY DEFAULT true CHECK(id), value BIGINT NOT NULL);
INSERT INTO source_corpus_epoch(id, value) VALUES (true, 1);

-- +goose Down
DROP TABLE source_index_snapshots;
DROP TABLE source_index_heads;
DROP TABLE source_corpus_epoch;
