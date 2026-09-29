-- +goose Up
-- A pinned abstract and PDF are separate immutable representations of one work.
DROP INDEX documents_source_revision;
CREATE UNIQUE INDEX documents_source_revision ON documents (
    owner_id, (source_metadata->>'access_scope'), (source_metadata->>'provider'),
    (source_metadata->>'native_id'), (source_metadata->>'revision'), mime_type,
    (coalesce(source_metadata->>'test_run_id', ''))
) WHERE source_metadata IS NOT NULL;

CREATE TABLE source_collection_scopes (
    id TEXT PRIMARY KEY,
    owner_id TEXT NOT NULL,
    provider TEXT NOT NULL CHECK (provider IN ('crossref', 'arxiv', 'github')),
    scope JSONB NOT NULL,
    run_id TEXT NOT NULL,
    lease_token TEXT NOT NULL,
    cursor TEXT NOT NULL DEFAULT '',
    received INTEGER NOT NULL DEFAULT 0 CHECK (received BETWEEN 0 AND 200),
    page_number INTEGER NOT NULL DEFAULT 0,
    exhausted BOOLEAN NOT NULL DEFAULT false,
    incomplete BOOLEAN NOT NULL DEFAULT false,
    run_limit INTEGER NOT NULL CHECK (run_limit BETWEEN 1 AND 200),
    status TEXT NOT NULL CHECK (status IN ('running', 'partial', 'complete', 'failed')),
    test_run_id TEXT NOT NULL DEFAULT '',
    last_error TEXT NOT NULL DEFAULT '',
    last_success_at TIMESTAMPTZ,
    updated_at TIMESTAMPTZ NOT NULL DEFAULT now()
);

-- A single live lease per provider prevents another scope/worker from bypassing
-- its one-connection policy. The cooldown survives release and worker restart.
CREATE TABLE source_collection_limits (
    provider TEXT PRIMARY KEY,
    scope_id TEXT NOT NULL DEFAULT '',
    lease_token TEXT NOT NULL DEFAULT '',
    leased_until TIMESTAMPTZ NOT NULL DEFAULT '-infinity',
    next_request_at TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE TABLE source_collection_pages (
    scope_id TEXT NOT NULL REFERENCES source_collection_scopes(id),
    run_id TEXT NOT NULL,
    page_number INTEGER NOT NULL,
    cursor TEXT NOT NULL,
    next_cursor TEXT NOT NULL,
    exhausted BOOLEAN NOT NULL,
    received INTEGER NOT NULL,
    rejected INTEGER NOT NULL,
    raw_key TEXT NOT NULL,
    raw_hash TEXT NOT NULL,
    manifest_key TEXT NOT NULL,
    manifest_hash TEXT NOT NULL,
    observations JSONB NOT NULL,
    limitations JSONB NOT NULL,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    PRIMARY KEY (scope_id, run_id, page_number),
    UNIQUE (scope_id, run_id, raw_hash)
);

CREATE TABLE source_collection_documents (
    record_key TEXT PRIMARY KEY,
    document_id TEXT NOT NULL REFERENCES documents(id) ON DELETE CASCADE,
    source_hash TEXT NOT NULL,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE TABLE source_collection_dispatch (
    document_id TEXT PRIMARY KEY REFERENCES documents(id) ON DELETE CASCADE,
    scope_id TEXT NOT NULL REFERENCES source_collection_scopes(id),
    payload JSONB NOT NULL,
    dispatched_at TIMESTAMPTZ,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now()
);
CREATE INDEX source_collection_pending ON source_collection_dispatch(scope_id) WHERE dispatched_at IS NULL;

-- +goose Down
-- +goose StatementBegin
DO $$ BEGIN
    RAISE EXCEPTION 'Preserve source checkpoints and provenance; roll back the compatible reader instead';
END $$;
-- +goose StatementEnd
