-- +goose Up
-- Extraction is the most expensive step of the pipeline. Without an identity a
-- restart pays for every document again and writes the same mentions twice, so
-- the job is addressed by what would change its answer and by nothing else:
-- the material it reads, the parser that produced that text, the extractor and
-- the output schema. Re-observing an unchanged document changes none of those,
-- so it neither creates a revision nor starts extraction again.
CREATE TABLE technology_extraction_jobs (
    id TEXT PRIMARY KEY CHECK (id ~ '^[0-9a-f]{64}$'),
    source_revision_id TEXT NOT NULL REFERENCES documents(id) ON DELETE CASCADE,
    owner_id TEXT NOT NULL CHECK (length(owner_id) BETWEEN 1 AND 256),
    source_revision_hash TEXT NOT NULL CHECK (source_revision_hash ~ '^[0-9a-f]{64}$'),
    parser_version TEXT NOT NULL CHECK (length(parser_version) BETWEEN 1 AND 512),
    extractor_version TEXT NOT NULL CHECK (length(extractor_version) BETWEEN 1 AND 128),
    schema_version TEXT NOT NULL CHECK (length(schema_version) BETWEEN 1 AND 128),
    -- The stages are separate on purpose: a queued job is not a running one, a
    -- partial answer is not a complete one, and a cancelled job is not a
    -- failure of the material.
    state TEXT NOT NULL DEFAULT 'queued'
        CHECK (state IN ('queued', 'running', 'completed', 'partial', 'failed', 'cancelled')),
    attempt INTEGER NOT NULL DEFAULT 0 CHECK (attempt BETWEEN 0 AND 3),
    lease_token TEXT NOT NULL DEFAULT '',
    lease_until TIMESTAMPTZ,
    reason_code TEXT NOT NULL DEFAULT '' CHECK (length(reason_code) <= 128),
    input_key TEXT NOT NULL DEFAULT '' CHECK (length(input_key) <= 2048),
    input_sha256 TEXT NOT NULL DEFAULT '' CHECK (input_sha256 = '' OR input_sha256 ~ '^[0-9a-f]{64}$'),
    intermediate_key TEXT NOT NULL DEFAULT '' CHECK (length(intermediate_key) <= 2048),
    intermediate_sha256 TEXT NOT NULL DEFAULT ''
        CHECK (intermediate_sha256 = '' OR intermediate_sha256 ~ '^[0-9a-f]{64}$'),
    output_key TEXT NOT NULL DEFAULT '' CHECK (length(output_key) <= 2048),
    output_sha256 TEXT NOT NULL DEFAULT '' CHECK (output_sha256 = '' OR output_sha256 ~ '^[0-9a-f]{64}$'),
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    -- The four-part key is the identity; the id is its content address, and
    -- both are declared so a wrongly computed id cannot create a second job.
    UNIQUE (source_revision_hash, parser_version, extractor_version, schema_version),
    -- An answer names the artifact it produced. Without this a worker could
    -- publish "completed" and leave nothing to read back.
    CHECK (state NOT IN ('completed', 'partial') OR (output_key <> '' AND output_sha256 <> '')),
    -- Only a running attempt holds a lease. A terminal job holding one would
    -- let a lost worker come back and overwrite a published answer.
    CHECK (state = 'running' OR (lease_token = '' AND lease_until IS NULL)),
    CHECK (state <> 'running' OR (lease_token <> '' AND lease_until IS NOT NULL))
);
CREATE INDEX technology_extraction_jobs_revision ON technology_extraction_jobs(source_revision_id);
-- Claiming scans for work of one extractor and schema; a terminal job is never
-- in that scan, which is what makes a repeated run cost nothing.
CREATE INDEX technology_extraction_jobs_claimable
    ON technology_extraction_jobs(extractor_version, schema_version, created_at)
    WHERE state IN ('queued', 'running');

-- +goose Down
DROP TABLE technology_extraction_jobs;
