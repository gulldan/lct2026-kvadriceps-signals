-- +goose Up
CREATE TABLE source_collection_membership (
    scope_id TEXT NOT NULL REFERENCES source_collection_scopes(id) ON DELETE CASCADE,
    document_id TEXT NOT NULL REFERENCES documents(id) ON DELETE CASCADE,
    first_seen_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    PRIMARY KEY(scope_id, document_id)
);
CREATE INDEX source_collection_membership_document ON source_collection_membership(document_id);

-- Dispatch proves the first importing scope only. Older duplicate encounters
-- must be replayed or resolved from their hash-pinned page manifests; never infer
-- them by rerunning a title query over today's documents.
INSERT INTO source_collection_membership(scope_id,document_id,first_seen_at)
SELECT scope_id,document_id,created_at FROM source_collection_dispatch;

-- +goose Down
-- +goose StatementBegin
DO $$ BEGIN
    RAISE EXCEPTION 'Preserve collection membership; roll back the compatible reader instead';
END $$;
-- +goose StatementEnd
