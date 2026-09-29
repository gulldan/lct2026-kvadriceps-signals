-- +goose Up
CREATE TABLE hypothesis_jobs (
 id text PRIMARY KEY,
 owner_id text NOT NULL,
 kind text NOT NULL CHECK (kind IN ('generate','verify','enrich','assess_trl','competitors','refine','tag')),
 input jsonb NOT NULL CHECK (octet_length(input::text)<=65536),
 state text NOT NULL CHECK (state IN ('queued','running','succeeded','failed')),
 result_ids text[] NOT NULL DEFAULT '{}',
 error text NOT NULL DEFAULT '',
 created_at timestamptz NOT NULL DEFAULT now(),
 started_at timestamptz,
 heartbeat_at timestamptz,
 finished_at timestamptz,
 attempt integer NOT NULL DEFAULT 0 CHECK (attempt BETWEEN 0 AND 3),
 token text NOT NULL DEFAULT '',
 lease_until timestamptz,
 not_before timestamptz NOT NULL DEFAULT now()
);
CREATE INDEX hypothesis_jobs_queue ON hypothesis_jobs(not_before,created_at,id) WHERE state IN ('queued','running');
CREATE INDEX hypothesis_jobs_owner ON hypothesis_jobs(owner_id,created_at DESC,id DESC);
CREATE TABLE hypothesis_job_effects (
 job_id text NOT NULL REFERENCES hypothesis_jobs(id),
 operation text NOT NULL,
 result_id text NOT NULL,
 PRIMARY KEY(job_id,operation)
);
-- +goose Down
DROP TABLE hypothesis_job_effects;
DROP TABLE hypothesis_jobs;
