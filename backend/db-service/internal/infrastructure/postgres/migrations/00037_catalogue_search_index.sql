-- +goose Up
-- The catalogue asked its question in the wrong order: it read every mention
-- before it looked for the words. A leading-wildcard LIKE has no btree to stand
-- on, so the narrowing has to come from trigrams — and it has to come first,
-- before the access rule and the match reasons are worked out.
CREATE EXTENSION IF NOT EXISTS pg_trgm;

-- array_to_string is STABLE and cannot be indexed, so the joined form of
-- outputs gets an immutable name of its own. It is STABLE because it is generic
-- over anyarray and some element types print differently under different
-- settings; for text[] and a literal separator the result is fixed, which is
-- what makes the IMMUTABLE marking below true rather than convenient. The
-- separator is a newline: a
-- catalogue query is whitespace-normalised before it reaches the database and
-- can never contain one, so a match spanning two outputs is impossible. This
-- expression only narrows candidates; the per-element check in the query is
-- still what decides the `application` reason.
-- +goose StatementBegin
CREATE FUNCTION technology_outputs_text(outputs TEXT[]) RETURNS TEXT
    LANGUAGE sql IMMUTABLE PARALLEL SAFE STRICT AS
$$ SELECT lower(array_to_string(outputs, E'\n')) $$;
-- +goose StatementEnd

CREATE INDEX technologies_name_trgm ON technologies USING gin (lower(canonical_name) gin_trgm_ops);
CREATE INDEX technologies_description_trgm ON technologies USING gin (lower(description) gin_trgm_ops);
CREATE INDEX technology_aliases_value_trgm ON technology_aliases USING gin (lower(value) gin_trgm_ops);
CREATE INDEX technology_mentions_mechanism_trgm
    ON technology_mentions USING gin (lower(mechanism) gin_trgm_ops);
CREATE INDEX technology_mentions_problem_trgm
    ON technology_mentions USING gin (lower(problem) gin_trgm_ops);
CREATE INDEX technology_mentions_outputs_trgm
    ON technology_mentions USING gin (technology_outputs_text(outputs) gin_trgm_ops);

-- The access rule selects documents by an access_scope inside JSONB, and with
-- no statistics for that expression the planner reads the rule as matching a
-- handful of documents: it then enters the mentions from the document side and
-- reads the whole table. The expression has to match the one in the query
-- (const catalogueReadable) or the statistics silently stop applying.
CREATE STATISTICS documents_access_scope
    ON (coalesce(source_metadata->>'access_scope','')) FROM documents;
-- A statistics object holds nothing until an ANALYZE fills it, and the plan it
-- is here to prevent is the one Postgres picks meanwhile. ANALYZE is allowed
-- inside a transaction, so the migration does not leave that window open.
ANALYZE documents;

-- A catalogue page without a query is ordered by name. With this index it is
-- read in order and stops at the page, instead of sorting the whole catalogue
-- to throw all but twenty rows away.
CREATE INDEX technologies_name_order ON technologies(canonical_name, id);

-- +goose Down
DROP STATISTICS documents_access_scope;
DROP INDEX technologies_name_order;
DROP INDEX technology_mentions_outputs_trgm;
DROP INDEX technology_mentions_problem_trgm;
DROP INDEX technology_mentions_mechanism_trgm;
DROP INDEX technology_aliases_value_trgm;
DROP INDEX technologies_description_trgm;
DROP INDEX technologies_name_trgm;
DROP FUNCTION technology_outputs_text(TEXT[]);
