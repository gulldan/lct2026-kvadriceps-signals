-- +goose Up
-- Canonical bytes retain the S3 hash; JSONB would change whitespace/key order.
CREATE TABLE signal_analysis_inputs (
    run_id TEXT PRIMARY KEY,
    owner_id TEXT NOT NULL,
    request_id TEXT NOT NULL,
    request_hash TEXT NOT NULL,
    manifest TEXT NOT NULL CHECK (octet_length(manifest) <= 16777216),
    manifest_hash TEXT NOT NULL,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    UNIQUE (owner_id, request_id)
);
CREATE TABLE signal_analysis_sources (
    run_id TEXT NOT NULL REFERENCES signal_analysis_inputs(run_id) ON DELETE CASCADE,
    document_id TEXT NOT NULL,
    snapshot_id TEXT NOT NULL,
    PRIMARY KEY (run_id, document_id)
);
CREATE INDEX signal_analysis_owner ON signal_analysis_inputs(owner_id, created_at DESC);

-- +goose Down
-- +goose StatementBegin
DO $$ BEGIN
    RAISE EXCEPTION 'Preserve immutable analysis provenance; roll back the compatible reader instead';
END $$;
-- +goose StatementEnd
