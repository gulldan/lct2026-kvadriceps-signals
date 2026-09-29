-- name: CreateDocument :exec
INSERT INTO documents
  (id, owner_id, filename, mime_type, size, object_key, status, status_msg,
   chunk_count, created_at, updated_at, content_hash, source_metadata)
VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12, $13);

-- name: GetDocument :one
SELECT * FROM documents WHERE id = $1;

-- name: FindDocumentByHash :one
SELECT * FROM documents
WHERE content_hash = sqlc.arg('content_hash')
  AND (sqlc.arg('owner_id')::text = '' OR owner_id = sqlc.arg('owner_id'))
  AND status = 'indexed'
ORDER BY chunk_count DESC, created_at DESC
LIMIT 1;

-- name: ListDocumentsByOwner :many
SELECT * FROM documents
WHERE (sqlc.arg('owner_id')::text = '' OR owner_id = sqlc.arg('owner_id'))
 AND (sqlc.arg('before_id')::text='' OR (created_at,id)<
 (SELECT created_at,id FROM documents WHERE id=sqlc.arg('before_id')))
 AND (sqlc.arg('status')::text='' OR status=sqlc.arg('status'))
 AND (sqlc.arg('kind')::text='' OR kind=sqlc.arg('kind'))
ORDER BY created_at DESC,id DESC LIMIT sqlc.arg('lim');

-- name: UpdateDocumentStatus :execrows
UPDATE documents
SET status = sqlc.arg('status'),
    status_msg = sqlc.arg('status_msg'),
    chunk_count = COALESCE(sqlc.narg('chunk_count'), chunk_count),
    updated_at = now()
WHERE id = sqlc.arg('id')
  AND generation = sqlc.arg('generation')
  AND (status = sqlc.arg('status') OR (
      status NOT IN ('indexed','failed') AND (
        sqlc.arg('status') = 'failed' OR
        array_position(ARRAY['uploaded','queued','parsing','ocr','parsed','chunking','indexed'], sqlc.arg('status')::text)
        > array_position(ARRAY['uploaded','queued','parsing','ocr','parsed','chunking','indexed'], status)
      )
  ))
  AND NOT EXISTS (SELECT 1 FROM source_index_heads h WHERE h.document_id = documents.id
                  AND h.active_snapshot_id IS NOT NULL);

-- name: SetDocumentSource :execrows
-- Дата публикации берётся из паспорта, когда своей нет: паспорт типизирован и
-- проверен при записи, а docinfo файла здесь сознательно не в доверии. Уже
-- заполненное значение не трогается — паспорт дополняет разбор, а не спорит с ним.
UPDATE documents SET source_metadata = sqlc.arg('metadata')::jsonb,
    published_at = CASE WHEN documents.published_at <> '' THEN documents.published_at
                        ELSE coalesce(sqlc.arg('metadata')::jsonb -> 'publication' ->> 'value', '') END
WHERE id = sqlc.arg('id') AND owner_id = sqlc.arg('owner_id')
  AND (source_metadata IS NULL OR source_metadata = sqlc.arg('metadata')::jsonb);

-- name: UpdateDocumentTitle :exec
UPDATE documents
SET title = sqlc.arg('title'),
    updated_at = now()
WHERE id = sqlc.arg('id')
  AND source_metadata IS NULL;

-- name: UpdateDocumentKind :exec
UPDATE documents
SET kind = sqlc.arg('kind'),
    updated_at = now()
WHERE id = sqlc.arg('id');

-- name: UpdateDocumentMeta :exec
-- Дата из паспорта источника не уступает дате из файла: у arXiv docinfo PDF —
-- день сборки файла, а не публикации, и часто пуст. 25.09 у 168 свежих работ
-- из 232 колонка оставалась пустой, и радар считал их загруженными сегодня.
UPDATE documents
SET author = sqlc.arg('author'),
    published_at = CASE WHEN coalesce(documents.source_metadata -> 'publication' ->> 'value', '') <> ''
                        THEN documents.source_metadata -> 'publication' ->> 'value'
                        ELSE sqlc.arg('published_at') END,
    source_ref = sqlc.arg('source_ref'),
    updated_at = now()
WHERE id = sqlc.arg('id');
