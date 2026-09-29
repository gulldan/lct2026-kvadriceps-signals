-- +goose Up
-- A collection run becomes a record of what was asked for, what came back and
-- why it stopped. Counts accumulate per committed page; completeness is claimed
-- only by a scope that exhausted its provider without rejections or limitations.
CREATE TABLE collection_manifests (
    scope_id TEXT NOT NULL REFERENCES source_collection_scopes(id) ON DELETE CASCADE,
    run_id TEXT NOT NULL,
    owner_id TEXT NOT NULL,
    provider TEXT NOT NULL,
    scope JSONB NOT NULL,
    run_limit INTEGER NOT NULL CHECK (run_limit BETWEEN 1 AND 200),
    cursor TEXT NOT NULL DEFAULT '',
    pages INTEGER NOT NULL DEFAULT 0 CHECK (pages >= 0),
    listed INTEGER NOT NULL DEFAULT 0 CHECK (listed >= 0),
    received INTEGER NOT NULL DEFAULT 0 CHECK (received >= 0),
    rejected INTEGER NOT NULL DEFAULT 0 CHECK (rejected >= 0),
    processed INTEGER NOT NULL DEFAULT 0 CHECK (processed >= 0),
    limitations JSONB NOT NULL DEFAULT '[]'::jsonb,
    exhausted BOOLEAN NOT NULL DEFAULT false,
    incomplete BOOLEAN NOT NULL DEFAULT false,
    completeness TEXT NOT NULL DEFAULT 'partial' CHECK (completeness IN ('complete', 'partial', 'failed')),
    started_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    PRIMARY KEY (scope_id, run_id),
    CHECK (listed >= received AND received >= processed),
    CHECK (completeness <> 'complete' OR (exhausted AND NOT incomplete))
);
CREATE INDEX collection_manifests_owner ON collection_manifests(owner_id, started_at DESC);

-- Collection runs that already happened are summarized from their own page
-- receipts, not from today's scope status: a run is exhausted when its last
-- page said so, never because a later run of the same scope finished.
INSERT INTO collection_manifests(scope_id, run_id, owner_id, provider, scope, run_limit, cursor, pages,
    listed, received, rejected, processed, limitations, exhausted, incomplete, completeness, started_at, updated_at)
SELECT p.scope_id, p.run_id, s.owner_id, s.provider, s.scope, s.run_limit,
    (array_agg(p.next_cursor ORDER BY p.page_number DESC))[1],
    count(*)::int,
    sum(p.received + p.rejected)::int,
    sum(p.received)::int,
    sum(p.rejected)::int,
    sum(jsonb_array_length(p.observations))::int,
    '[]'::jsonb,
    bool_or(p.exhausted),
    bool_or(p.rejected > 0 OR jsonb_array_length(p.limitations) > 0),
    CASE WHEN bool_or(p.exhausted) AND NOT bool_or(p.rejected > 0 OR jsonb_array_length(p.limitations) > 0)
         THEN 'complete' ELSE 'partial' END,
    min(p.created_at), max(p.created_at)
FROM source_collection_pages p
JOIN source_collection_scopes s ON s.id = p.scope_id
GROUP BY p.scope_id, p.run_id, s.owner_id, s.provider, s.scope, s.run_limit;

-- One published analysis, with every component version it was produced by.
-- metrics_version carries both the measurement series and the emergence rule;
-- parser_version carries the distinct parser revisions of the corpus it read.
CREATE TABLE analysis_runs (
    id TEXT PRIMARY KEY REFERENCES signal_analysis_jobs(id) ON DELETE CASCADE,
    owner_id TEXT NOT NULL,
    request_id TEXT NOT NULL,
    input_run_id TEXT NOT NULL REFERENCES signal_analysis_inputs(run_id) ON DELETE CASCADE,
    cutoff TIMESTAMPTZ NOT NULL,
    history_mode TEXT NOT NULL CHECK (history_mode IN ('as_observed', 'reconstruction')),
    extraction_version TEXT NOT NULL CHECK (length(extraction_version) BETWEEN 1 AND 512),
    embedding_version TEXT NOT NULL CHECK (length(embedding_version) BETWEEN 1 AND 512),
    identity_version TEXT NOT NULL CHECK (length(identity_version) BETWEEN 1 AND 512),
    clustering_version TEXT NOT NULL CHECK (length(clustering_version) BETWEEN 1 AND 512),
    metrics_version TEXT NOT NULL CHECK (length(metrics_version) BETWEEN 1 AND 512),
    parser_version TEXT NOT NULL CHECK (length(parser_version) BETWEEN 1 AND 512),
    method_version TEXT NOT NULL CHECK (length(method_version) BETWEEN 1 AND 128),
    method_sha256 TEXT NOT NULL CHECK (method_sha256 ~ '^[a-f0-9]{64}$'),
    result_sha256 TEXT NOT NULL CHECK (result_sha256 ~ '^[a-f0-9]{64}$'),
    comparable_with TEXT REFERENCES analysis_runs(id) ON DELETE SET NULL,
    limitations JSONB NOT NULL DEFAULT '[]'::jsonb,
    published_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    CHECK (comparable_with IS NULL OR comparable_with <> id)
);
CREATE INDEX analysis_runs_owner ON analysis_runs(owner_id, published_at DESC);

CREATE TABLE analysis_run_manifests (
    run_id TEXT NOT NULL REFERENCES analysis_runs(id) ON DELETE CASCADE,
    scope_id TEXT NOT NULL,
    collection_run_id TEXT NOT NULL,
    PRIMARY KEY (run_id, scope_id, collection_run_id),
    FOREIGN KEY (scope_id, collection_run_id)
        REFERENCES collection_manifests(scope_id, run_id) ON DELETE RESTRICT
);

-- A result whose inputs are not recorded cannot be reproduced, so it is not a
-- published run. Deferred: the manifest links are inserted in the same
-- transaction, right after the run itself.
-- +goose StatementBegin
CREATE FUNCTION analysis_run_requires_manifest() RETURNS trigger AS $$
BEGIN
    IF NOT EXISTS (SELECT 1 FROM analysis_run_manifests WHERE run_id = NEW.id) THEN
        RAISE EXCEPTION 'analysis run % has no input manifest', NEW.id USING ERRCODE = 'check_violation';
    END IF;
    RETURN NULL;
END;
$$ LANGUAGE plpgsql;
-- +goose StatementEnd
CREATE CONSTRAINT TRIGGER analysis_runs_require_manifest AFTER INSERT ON analysis_runs
    DEFERRABLE INITIALLY DEFERRED FOR EACH ROW EXECUTE FUNCTION analysis_run_requires_manifest();

-- Two periods are comparable only when every component that could move a number
-- is identical. A changed parser or extractor breaks the series; it does not
-- quietly continue it.
-- +goose StatementBegin
CREATE FUNCTION analysis_run_comparability() RETURNS trigger AS $$
DECLARE earlier analysis_runs%ROWTYPE;
BEGIN
    IF NEW.comparable_with IS NULL THEN
        RETURN NEW;
    END IF;
    SELECT * INTO earlier FROM analysis_runs WHERE id = NEW.comparable_with;
    IF NOT FOUND THEN
        RAISE EXCEPTION 'comparable run % does not exist', NEW.comparable_with USING ERRCODE = 'check_violation';
    END IF;
    IF (earlier.extraction_version, earlier.embedding_version, earlier.identity_version,
        earlier.clustering_version, earlier.metrics_version, earlier.parser_version)
       IS DISTINCT FROM
       (NEW.extraction_version, NEW.embedding_version, NEW.identity_version,
        NEW.clustering_version, NEW.metrics_version, NEW.parser_version) THEN
        RAISE EXCEPTION 'run % is not comparable with %: component versions differ', NEW.id, NEW.comparable_with
            USING ERRCODE = 'check_violation';
    END IF;
    RETURN NEW;
END;
$$ LANGUAGE plpgsql;
-- +goose StatementEnd
CREATE TRIGGER analysis_runs_comparability BEFORE INSERT OR UPDATE ON analysis_runs
    FOR EACH ROW EXECUTE FUNCTION analysis_run_comparability();

-- +goose Down
-- Published runs record how a result was obtained; a rollback keeps them.
-- +goose StatementBegin
DO $$ BEGIN
    RAISE EXCEPTION 'Preserve collection manifests and analysis runs; roll back the compatible reader instead';
END $$;
-- +goose StatementEnd
