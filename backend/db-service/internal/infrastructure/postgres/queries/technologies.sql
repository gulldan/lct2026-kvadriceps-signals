-- name: RecordTechnologyExtraction :exec
INSERT INTO technology_extractions
  (source_revision_id, extractor_version, schema_version, status, reason_code,
   processed_sections, total_sections)
VALUES ($1, $2, $3, $4, $5, $6, $7)
ON CONFLICT (source_revision_id, extractor_version, schema_version) DO UPDATE
SET status = EXCLUDED.status,
    reason_code = EXCLUDED.reason_code,
    processed_sections = EXCLUDED.processed_sections,
    total_sections = EXCLUDED.total_sections,
    extracted_at = now();

-- name: GetTechnologyExtraction :one
SELECT * FROM technology_extractions
WHERE source_revision_id = $1 AND extractor_version = $2 AND schema_version = $3;

-- name: CreateTechnology :exec
INSERT INTO technologies
  (id, canonical_name, name_kind, description, validation_status, method_version)
VALUES ($1, $2, $3, $4, $5, $6);

-- name: GetTechnology :one
SELECT * FROM technologies WHERE id = $1;

-- name: AddTechnologyAlias :exec
INSERT INTO technology_aliases (technology_id, value, language, basis)
VALUES ($1, $2, $3, $4)
ON CONFLICT (technology_id, value, language) DO UPDATE SET basis = EXCLUDED.basis;

-- name: ListTechnologyAliases :many
SELECT * FROM technology_aliases WHERE technology_id = $1 ORDER BY language, value;

-- name: CreateTechnologyMention :exec
INSERT INTO technology_mentions
  (id, technology_id, source_revision_id, extractor_version, schema_version, name,
   description, role, problem, mechanism, inputs, outputs, extraction_status)
VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12, $13);

-- name: ListRevisionMentions :many
SELECT * FROM technology_mentions
WHERE source_revision_id = $1
  AND (sqlc.arg('extractor_version')::text = '' OR extractor_version = sqlc.arg('extractor_version'))
ORDER BY created_at, id;

-- name: ListTechnologyMentions :many
SELECT * FROM technology_mentions WHERE technology_id = $1 ORDER BY created_at, id;

-- name: AddTechnologyEvidence :exec
INSERT INTO technology_evidence
  (id, mention_id, source_revision_id, section, page, start_char, end_char, quote,
   element_path, snapshot_sha256)
VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10);

-- name: ListMentionEvidence :many
SELECT * FROM technology_evidence WHERE mention_id = ANY(sqlc.arg('mention_ids')::text[])
ORDER BY mention_id, start_char;

-- name: LinkTechnologyFieldEvidence :exec
INSERT INTO technology_field_evidence (mention_id, field, evidence_id)
VALUES ($1, $2, $3)
ON CONFLICT DO NOTHING;

-- name: ListMentionFieldEvidence :many
SELECT * FROM technology_field_evidence WHERE mention_id = ANY(sqlc.arg('mention_ids')::text[])
ORDER BY mention_id, field, evidence_id;

-- name: AddTechnologyRelation :exec
INSERT INTO technology_relations
  (technology_id, related_technology_id, relation, basis, decision_version)
VALUES ($1, $2, $3, $4, $5);

-- name: ListTechnologyRelations :many
SELECT * FROM technology_relations
WHERE technology_id = $1 OR related_technology_id = $1
ORDER BY relation, technology_id, related_technology_id;

-- name: AddEvidenceEvent :exec
INSERT INTO evidence_events
  (id, technology_id, source_revision_id, event_type, event_date, date_precision,
   organization, evidence_group, modality, depends_on_event_id)
VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10);

-- name: ListTechnologyEvidenceEvents :many
SELECT * FROM evidence_events WHERE technology_id = $1 ORDER BY event_date NULLS LAST, id;

-- name: AddTechnologyReview :exec
INSERT INTO technology_reviews
  (id, technology_id, author_id, operation, reason, previous_value, new_value)
VALUES ($1, $2, $3, $4, $5, $6, $7);

-- name: ListTechnologyReviews :many
SELECT * FROM technology_reviews WHERE technology_id = $1 ORDER BY created_at DESC, id;
