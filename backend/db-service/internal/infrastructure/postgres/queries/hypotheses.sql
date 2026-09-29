-- name: InsertHypothesis :exec
INSERT INTO hypotheses
  (id, owner_id, run_id, title, statement, rationale, method, status, kpi_id,
   primary_cluster_id, trl, novelty_score, risk_score, value_score, confidence_score,
   composite_score, measurable, organization, function_area, source_type, location,
   tags, assessment, detail, generation, created_at, updated_at)
VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12, $13, $14, $15, $16, $17,
        $18, $19, $20, $21, $22, $23, $24, $25, $26, $27);

-- name: GetHypothesis :one
SELECT * FROM hypotheses WHERE id = $1;

-- name: UpdateHypothesis :execrows
UPDATE hypotheses
SET title = $2, statement = $3, rationale = $4, method = $5, status = $6,
    kpi_id = $7, primary_cluster_id = $8, trl = $9, novelty_score = $10,
    risk_score = $11, value_score = $12, confidence_score = $13, composite_score = $14,
    measurable = $15, organization = $16, function_area = $17, source_type = $18,
    location = $19, tags = $20, assessment = $21, detail = $22, generation = $23,
    updated_at = now(), version = version + 1
WHERE id = $1 AND version = sqlc.arg('expected_version') AND owner_id = sqlc.arg('owner_id');

-- name: ListHypotheses :many
SELECT id,owner_id,run_id,title,statement,rationale,method,status,kpi_id,primary_cluster_id,trl,novelty_score,risk_score,value_score,confidence_score,composite_score,measurable,organization,function_area,source_type,location,tags,CASE WHEN sqlc.arg('compact')::boolean THEN jsonb_strip_nulls(jsonb_build_object(
 'check',jsonb_build_object('verdict',assessment#>'{check,verdict}'),'ranking',jsonb_build_object('score',assessment#>'{ranking,score}'),
 'learning_priority',assessment->'learning_priority','itc',jsonb_build_object('score',assessment#>'{itc,score}',
 'techscore',assessment#>'{itc,techscore}','band',assessment#>'{itc,band}','axes',assessment#>'{itc,axes}'),
 'scores',assessment->'scores','schema',assessment->'schema','trl',jsonb_build_object('level',assessment#>'{trl,level}',
 'name',assessment#>'{trl,name}'))) ELSE assessment END::jsonb AS assessment,
 CASE WHEN sqlc.arg('compact')::boolean THEN '{}'::jsonb ELSE detail END::jsonb AS detail,
 CASE WHEN sqlc.arg('compact')::boolean THEN coalesce((SELECT jsonb_object_agg(key,value) FROM jsonb_each(CASE WHEN jsonb_typeof(generation)='object' THEN generation ELSE '{}'::jsonb END)
 WHERE jsonb_typeof(value) NOT IN ('array','object')),'{}'::jsonb) ELSE generation END::jsonb AS generation,created_at,updated_at,version FROM hypotheses
WHERE (sqlc.arg('before_id')::text='' OR (created_at,id)<
 (SELECT created_at,id FROM hypotheses WHERE id=sqlc.arg('before_id')))
 AND (sqlc.arg('owner_id')::text = '' OR owner_id = sqlc.arg('owner_id'))
  AND (sqlc.arg('status')::text = '' OR status = sqlc.arg('status'))
  AND (sqlc.arg('kpi_id')::text = '' OR kpi_id = sqlc.arg('kpi_id'))
  AND (sqlc.arg('cluster_id')::text = '' OR primary_cluster_id = sqlc.arg('cluster_id'))
  AND (sqlc.arg('function_area')::text = '' OR function_area = sqlc.arg('function_area'))
  AND (sqlc.arg('source_type')::text = '' OR source_type = sqlc.arg('source_type'))
  AND (sqlc.arg('organization')::text = '' OR organization = sqlc.arg('organization'))
  AND (sqlc.arg('min_trl')::int = 0 OR trl >= sqlc.arg('min_trl')::int)
  AND (sqlc.arg('max_trl')::int = 0 OR trl <= sqlc.arg('max_trl')::int)
  AND (cardinality(sqlc.arg('tags')::text[]) = 0 OR tags @> sqlc.arg('tags')::text[])
  AND (cardinality(sqlc.arg('document_ids')::text[]) = 0 OR EXISTS (
    SELECT 1 FROM hypothesis_evidence e
    WHERE e.hypothesis_id = hypotheses.id
      AND e.document_id = ANY(sqlc.arg('document_ids')::text[])))
  AND (sqlc.arg('search')::text='' OR position(lower(sqlc.arg('search')) in
 lower(concat_ws(' ',title,statement,rationale,location,organization,function_area,source_type,array_to_string(tags,' '))))>0)
  AND (sqlc.arg('queue')::text IN ('','all') OR hypothesis_queue(status,assessment)=sqlc.arg('queue')
 OR (sqlc.arg('queue')='needs_trl' AND trl IS NULL AND status NOT IN ('rejected','archived')))
ORDER BY
  CASE WHEN sqlc.arg('order_by')::text = 'created_at' THEN extract(epoch FROM created_at) END DESC NULLS LAST,
  CASE WHEN sqlc.arg('order_by')::text = 'trl' THEN trl::double precision END DESC NULLS LAST,
  CASE WHEN sqlc.arg('order_by')::text = 'novelty' THEN novelty_score END DESC NULLS LAST,
  CASE WHEN sqlc.arg('order_by')::text = 'value' THEN value_score END DESC NULLS LAST,
  CASE WHEN sqlc.arg('order_by')::text NOT IN ('created_at', 'trl', 'novelty', 'value')
       THEN composite_score END DESC NULLS LAST,
  created_at DESC, id DESC
LIMIT CASE WHEN sqlc.arg('lim')::int <= 0 THEN 100 ELSE least(sqlc.arg('lim')::int,500) END
OFFSET greatest(0,least(sqlc.arg('off')::int,100000));

-- name: HypothesisBoardStats :one
WITH base AS (SELECT * FROM hypotheses
WHERE (sqlc.arg('owner_id')::text = '' OR owner_id = sqlc.arg('owner_id'))
  AND (sqlc.arg('status')::text = '' OR status = sqlc.arg('status'))
  AND (sqlc.arg('kpi_id')::text = '' OR kpi_id = sqlc.arg('kpi_id'))
  AND (sqlc.arg('cluster_id')::text = '' OR primary_cluster_id = sqlc.arg('cluster_id'))
  AND (sqlc.arg('function_area')::text = '' OR function_area = sqlc.arg('function_area'))
  AND (sqlc.arg('source_type')::text = '' OR source_type = sqlc.arg('source_type'))
  AND (sqlc.arg('organization')::text = '' OR organization = sqlc.arg('organization'))
  AND (sqlc.arg('min_trl')::int = 0 OR trl >= sqlc.arg('min_trl')::int)
  AND (sqlc.arg('max_trl')::int = 0 OR trl <= sqlc.arg('max_trl')::int)
  AND (cardinality(sqlc.arg('tags')::text[]) = 0 OR tags @> sqlc.arg('tags')::text[])
  AND (cardinality(sqlc.arg('document_ids')::text[]) = 0 OR EXISTS (
    SELECT 1 FROM hypothesis_evidence e
    WHERE e.hypothesis_id = hypotheses.id
      AND e.document_id = ANY(sqlc.arg('document_ids')::text[])))
  AND (sqlc.arg('search')::text='' OR position(lower(sqlc.arg('search')) in
 lower(concat_ws(' ',title,statement,rationale,location,organization,function_area,source_type,array_to_string(tags,' '))))>0)
), filtered AS (SELECT * FROM base WHERE true
  AND (sqlc.arg('queue')::text IN ('','all') OR hypothesis_queue(status,assessment)=sqlc.arg('queue')
 OR (sqlc.arg('queue')='needs_trl' AND trl IS NULL AND status NOT IN ('rejected','archived')))
), facets AS (
 SELECT 'company' kind,organization value FROM filtered UNION ALL
 SELECT 'func',function_area FROM filtered UNION ALL
 SELECT 'source',source_type FROM filtered UNION ALL
 SELECT 'trl',trl::text FROM filtered WHERE trl IS NOT NULL UNION ALL
 SELECT 'research',lower(trim(tag)) FROM filtered,LATERAL unnest(tags) tag
 WHERE lower(trim(tag)) IN ('теоретическое исследование','практическое')
), values AS (
 SELECT DISTINCT kind,trim(value) value FROM facets
 WHERE trim(value) NOT IN ('','-','—') AND lower(trim(value))<>'n/a'
), facet_groups AS (SELECT kind,jsonb_agg(value ORDER BY value) vals FROM values GROUP BY kind)
SELECT jsonb_build_object('total',(SELECT count(*) FROM filtered),
 'queue_counts',jsonb_build_object('all',count(*),'needs_trl',count(*) FILTER(WHERE trl IS NULL AND status NOT IN ('rejected','archived')),
 'needs_verify',count(*) FILTER(WHERE hypothesis_queue(status,assessment)='needs_verify'),
 'ready',count(*) FILTER(WHERE hypothesis_queue(status,assessment)='ready'),
 'risk',count(*) FILTER(WHERE hypothesis_queue(status,assessment)='risk'),
 'insufficient',count(*) FILTER(WHERE hypothesis_queue(status,assessment)='insufficient')),
 'facets',jsonb_build_object('company','[]'::jsonb,'func','[]'::jsonb,'source','[]'::jsonb,'trl','[]'::jsonb,'research','[]'::jsonb)
 || coalesce((SELECT jsonb_object_agg(kind,vals) FROM facet_groups),'{}'::jsonb))::jsonb AS aggregates FROM base;

-- name: InsertEvidence :exec
INSERT INTO hypothesis_evidence
  (id, hypothesis_id, document_id, chunk_id, filename, snippet, stance, score, ord, created_at,
   relation, page_start, page_end, section_heading, origin)
VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12, $13, $14, $15);

-- name: ListEvidenceByHypothesis :many
SELECT * FROM hypothesis_evidence WHERE hypothesis_id = $1 ORDER BY ord ASC;

-- name: InsertRevision :one
INSERT INTO hypothesis_revisions
  (id, hypothesis_id, revision_no, editor_id, action, summary, patch, created_at)
VALUES ($1, $2,
        (SELECT COALESCE(MAX(revision_no), 0) + 1 FROM hypothesis_revisions WHERE hypothesis_id = $2),
        $3, $4, $5, $6, $7)
RETURNING revision_no;

-- name: ListRevisionsByHypothesis :many
SELECT * FROM hypothesis_revisions WHERE hypothesis_id = $1 ORDER BY revision_no ASC;
