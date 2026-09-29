-- name: ListKpiDocuments :many
SELECT sqlc.embed(d), kd.role, kd.created_at AS attached_at
FROM kpi_documents kd
JOIN documents d ON d.id = kd.document_id
JOIN kpis k ON k.id = kd.kpi_id
LEFT JOIN source_index_heads h ON h.document_id = d.id
WHERE kd.kpi_id = sqlc.arg('kpi_id') AND k.owner_id = sqlc.arg('reader_id')
  AND document_readable(sqlc.arg('reader_id'),d.owner_id,d.source_metadata,
                       sqlc.arg('shared')::boolean,coalesce(h.revoked,false))
ORDER BY kd.created_at ASC;

-- name: DetachKpiDocument :exec
DELETE FROM kpi_documents WHERE kpi_id = $1 AND document_id = $2;
