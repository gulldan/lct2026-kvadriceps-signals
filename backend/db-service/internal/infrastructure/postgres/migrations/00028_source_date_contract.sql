-- +goose Up
-- Every stored source date carries its precision and the primary-response field
-- it came from. Checked against the live corpus first: 8134 sources, 8056 with a
-- publication, 355 priorities and 513 filings already satisfy it.
-- +goose StatementBegin
CREATE FUNCTION source_date_is_sourced(value JSONB) RETURNS BOOLEAN AS $$
    SELECT value IS NULL
        OR value = 'null'::jsonb
        OR (jsonb_typeof(value) = 'object'
            AND value->>'precision' IN ('day', 'month', 'year')
            AND length(coalesce(value->>'basis', '')) > 0
            AND length(coalesce(value->>'value', '')) > 0);
$$ LANGUAGE sql IMMUTABLE;
-- +goose StatementEnd

ALTER TABLE documents ADD CONSTRAINT documents_source_dates CHECK (
    source_metadata IS NULL OR (
        source_date_is_sourced(source_metadata->'publication')
        AND source_date_is_sourced(source_metadata->'revision_publication')
        AND source_date_is_sourced(source_metadata->'priority')
        AND source_date_is_sourced(source_metadata->'filing')
        AND source_date_is_sourced(source_metadata->'grant')
        -- Availability is our own observation, so it carries a basis instead of
        -- a precision; an undated page is known no later than observed_at.
        AND (source_metadata->'available_at' IS NULL
             OR source_metadata->'available_at' = 'null'::jsonb
             OR length(coalesce(source_metadata->>'available_basis', '')) > 0)
    ) IS TRUE
);

-- +goose Down
ALTER TABLE documents DROP CONSTRAINT documents_source_dates;
DROP FUNCTION source_date_is_sourced(JSONB);
