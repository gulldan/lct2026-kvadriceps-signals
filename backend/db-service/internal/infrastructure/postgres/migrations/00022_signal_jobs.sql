-- +goose Up
CREATE TABLE signal_analysis_jobs (
    id TEXT PRIMARY KEY,
    generation BIGSERIAL UNIQUE NOT NULL,
    input_run_id TEXT NOT NULL REFERENCES signal_analysis_inputs(run_id) ON DELETE CASCADE,
    method_json TEXT NOT NULL CHECK (octet_length(method_json) <= 65536),
    method_sha256 TEXT NOT NULL CHECK (method_sha256 ~ '^[a-f0-9]{64}$'),
    scope_key TEXT NOT NULL,
    state TEXT NOT NULL DEFAULT 'prepared' CHECK (state IN ('prepared','validated','published','failed')),
    attempt INTEGER NOT NULL DEFAULT 0 CHECK (attempt BETWEEN 0 AND 3),
    lease_token TEXT NOT NULL DEFAULT '',
    lease_until TIMESTAMPTZ,
    not_before TIMESTAMPTZ NOT NULL DEFAULT now(),
    result_json TEXT NOT NULL DEFAULT '' CHECK (octet_length(result_json) <= 16777216),
    result_sha256 TEXT NOT NULL DEFAULT '',
    failure_code TEXT NOT NULL DEFAULT '',
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    UNIQUE (input_run_id, method_sha256),
    CHECK (state NOT IN ('validated','published') OR
        (result_json <> '' AND result_sha256 ~ '^[a-f0-9]{64}$'))
);
CREATE INDEX signal_jobs_pending ON signal_analysis_jobs(method_sha256, not_before, generation)
    WHERE state IN ('prepared','validated');
CREATE TABLE signal_analysis_heads (
    owner_id TEXT NOT NULL,
    scope_key TEXT NOT NULL,
    job_id TEXT NOT NULL REFERENCES signal_analysis_jobs(id) ON DELETE CASCADE,
    input_created_at TIMESTAMPTZ NOT NULL,
    generation BIGINT NOT NULL,
    PRIMARY KEY(owner_id,scope_key)
);

-- +goose Down
-- +goose StatementBegin
DO $$ BEGIN
    RAISE EXCEPTION 'Preserve published analysis provenance; roll back the compatible reader instead';
END $$;
-- +goose StatementEnd
