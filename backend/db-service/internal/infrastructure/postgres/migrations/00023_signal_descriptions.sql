-- +goose Up
CREATE TABLE signal_descriptions (
    job_id TEXT NOT NULL REFERENCES signal_analysis_jobs(id) ON DELETE CASCADE,
    candidate_id TEXT NOT NULL,
    version TEXT NOT NULL,
    bundle_json TEXT NOT NULL CHECK (octet_length(bundle_json) <= 32768),
    bundle_sha256 TEXT NOT NULL CHECK (bundle_sha256 ~ '^[a-f0-9]{64}$'),
    state TEXT NOT NULL DEFAULT 'prepared' CHECK (state IN ('prepared','validated','template')),
    attempt INTEGER NOT NULL DEFAULT 0 CHECK (attempt BETWEEN 0 AND 3),
    lease_token TEXT NOT NULL DEFAULT '',
    lease_until TIMESTAMPTZ,
    result_json TEXT NOT NULL DEFAULT '' CHECK (octet_length(result_json) <= 24576),
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    PRIMARY KEY(job_id,candidate_id,version),
    CHECK (state='prepared' OR result_json<>'')
);
CREATE INDEX signal_descriptions_pending ON signal_descriptions(version,created_at) WHERE state='prepared';

-- +goose Down
-- +goose StatementBegin
DO $$ BEGIN
    RAISE EXCEPTION 'Keep source-bound descriptions; roll back the compatible reader instead';
END $$;
-- +goose StatementEnd
