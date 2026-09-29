-- +goose Up
-- Claim, capacity and description queries filtered analyses by test_run_id
-- through manifest::jsonb on every call: every manifest (megabytes each) was
-- parsed twice a second, one claim took half a second and Postgres ran at a
-- full core. The value never changes after insert, so it is computed once.
ALTER TABLE signal_analysis_inputs
    ADD COLUMN test_run_id TEXT NOT NULL
    GENERATED ALWAYS AS (COALESCE(manifest::jsonb->'request'->>'test_run_id', '')) STORED;
CREATE INDEX signal_analysis_inputs_test_run ON signal_analysis_inputs(test_run_id);

-- +goose Down
DROP INDEX signal_analysis_inputs_test_run;
ALTER TABLE signal_analysis_inputs DROP COLUMN test_run_id;
