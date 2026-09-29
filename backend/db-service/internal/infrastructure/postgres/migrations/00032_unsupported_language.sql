-- +goose Up
-- A material outside the supported languages is a distinct answer. It is not
-- `no_technology`, which claims the document was read and holds nothing, and it
-- is not `failed`, which claims the attempt broke. Without its own value such a
-- material would be counted as examined and would silently lower every rate
-- measured over the corpus.
ALTER TABLE technology_extractions DROP CONSTRAINT technology_extractions_status_check;
ALTER TABLE technology_extractions ADD CONSTRAINT technology_extractions_status_check
    CHECK (status IN ('complete', 'partial', 'no_technology', 'needs_review', 'failed', 'unsupported_language'));
-- An unread material has no sections to have processed, so the completeness
-- rule that binds `no_technology` must not be read as satisfied by zeros.
ALTER TABLE technology_extractions ADD CONSTRAINT technology_extractions_unsupported_unread
    CHECK (status <> 'unsupported_language' OR processed_sections = 0);

-- +goose Down
ALTER TABLE technology_extractions DROP CONSTRAINT technology_extractions_unsupported_unread;
ALTER TABLE technology_extractions DROP CONSTRAINT technology_extractions_status_check;
ALTER TABLE technology_extractions ADD CONSTRAINT technology_extractions_status_check
    CHECK (status IN ('complete', 'partial', 'no_technology', 'needs_review', 'failed'));
