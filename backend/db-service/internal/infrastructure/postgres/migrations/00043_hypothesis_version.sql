-- +goose Up
ALTER TABLE hypotheses ADD COLUMN version bigint NOT NULL DEFAULT 1 CHECK (version > 0);

-- +goose Down
ALTER TABLE hypotheses DROP COLUMN version;
