-- +goose Up
-- One imported publication is one record. A second file of the same patent is
-- another copy of the same invention, not a second invention, so the identity
-- is the publication number within its jurisdiction, per owner and scope.
CREATE UNIQUE INDEX documents_manual_patent_publication ON documents (
    owner_id,
    (source_metadata->>'access_scope'),
    (source_metadata->'patent'->>'jurisdiction'),
    (source_metadata->'patent'->>'publication_number'),
    (coalesce(source_metadata->>'test_run_id', ''))
) WHERE source_metadata->>'provider' = 'manual'
    AND source_metadata->>'source_kind' = 'patent'
    AND source_metadata->'patent'->>'publication_number' IS NOT NULL;

-- A patent identity, when present, carries the country and the publication it
-- belongs to; a translation names the publication it restates.
ALTER TABLE documents ADD CONSTRAINT documents_patent_identity CHECK (
    source_metadata IS NULL
    OR source_metadata->'patent' IS NULL
    OR source_metadata->'patent' = 'null'::jsonb
    OR (
        source_metadata->>'source_kind' = 'patent'
        AND source_metadata->'patent'->>'jurisdiction' ~ '^[A-Z]{2}$'
        AND length(coalesce(source_metadata->'patent'->>'publication_number', '')) BETWEEN 1 AND 32
        AND (source_metadata->'patent'->>'translation_of' IS NULL
             OR source_metadata->'patent'->>'translation_of'
                <> source_metadata->'patent'->>'publication_number')
    ) IS TRUE
);

-- +goose Down
DROP INDEX documents_manual_patent_publication;
ALTER TABLE documents DROP CONSTRAINT documents_patent_identity;
