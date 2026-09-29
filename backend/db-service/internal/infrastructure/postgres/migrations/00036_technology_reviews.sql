-- +goose Up
-- An expert correction is a versioned record, not an edit in place. The history
-- has to say who changed what, why, and what the value was before, or the
-- correction cannot be undone and the result cannot be explained.
ALTER TABLE technology_reviews
    -- fingerprint makes a repeated identical correction the same correction.
    -- Without it a double click writes a second version of an unchanged value
    -- and the history stops being a record of what actually changed.
    ADD COLUMN fingerprint TEXT NOT NULL DEFAULT '' CHECK (fingerprint = '' OR fingerprint ~ '^[0-9a-f]{64}$'),
    -- A revert names the correction it undoes, so the pair reads as one story
    -- rather than as two unrelated edits.
    ADD COLUMN reverts_id TEXT REFERENCES technology_reviews(id) ON DELETE SET NULL,
    ADD CONSTRAINT technology_reviews_revert_names_target
        CHECK ((operation = 'revert') = (reverts_id IS NOT NULL));
CREATE UNIQUE INDEX technology_reviews_fingerprint
    ON technology_reviews(technology_id, fingerprint) WHERE fingerprint <> '';

-- +goose Down
DROP INDEX technology_reviews_fingerprint;
ALTER TABLE technology_reviews
    DROP CONSTRAINT technology_reviews_revert_names_target,
    DROP COLUMN reverts_id,
    DROP COLUMN fingerprint;
