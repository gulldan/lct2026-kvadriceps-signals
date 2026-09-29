-- +goose Up
-- The published names described something the code does not do: `as_observed`
-- is a filter on the observation date, and `reconstruction` reassembles
-- nothing — identity, embeddings and clusters are today's in both. The names
-- are corrected to the ones §CL-07 uses, and the old values are mapped rather
-- than dropped: a snapshot published under the old word must keep reading.
ALTER TABLE analysis_runs DROP CONSTRAINT analysis_runs_history_mode_check;
UPDATE analysis_runs SET history_mode = 'as_of' WHERE history_mode = 'as_observed';
UPDATE analysis_runs SET history_mode = 'retrospective_current_membership' WHERE history_mode = 'reconstruction';
ALTER TABLE analysis_runs ADD CONSTRAINT analysis_runs_history_mode_check
    CHECK (history_mode IN ('as_of', 'retrospective_current_membership'));

-- +goose Down
ALTER TABLE analysis_runs DROP CONSTRAINT analysis_runs_history_mode_check;
UPDATE analysis_runs SET history_mode = 'as_observed' WHERE history_mode = 'as_of';
UPDATE analysis_runs SET history_mode = 'reconstruction' WHERE history_mode = 'retrospective_current_membership';
ALTER TABLE analysis_runs ADD CONSTRAINT analysis_runs_history_mode_check
    CHECK (history_mode IN ('as_observed', 'reconstruction'));
