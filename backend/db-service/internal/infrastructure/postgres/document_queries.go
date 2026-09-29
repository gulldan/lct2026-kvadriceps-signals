package postgres

import "fmt"

import (
	"context"
	"encoding/json"
	"errors"
	"slices"
	"strings"

	"github.com/example/db-service/internal/domain"
	"github.com/example/db-service/internal/platform/contracts"
	"github.com/jackc/pgx/v5"
)

// DocumentQueries применяет права до выбора представления статьи, фильтров и LIMIT.
type DocumentQueries struct {
	db     *DB
	shared bool
}

func NewDocumentQueries(db *DB, shared bool) *DocumentQueries {
	return &DocumentQueries{db: db, shared: shared}
}

// Доступ и дедупликация описаны один раз, а проекция у каждого пути своя: узкая
// выборка остаётся в памяти, а тяжёлые тексты читаются по готовым id.
const readableHead = `WITH readable AS (
 SELECT `

const readableRest = `
 document_file_type(d.filename,d.mime_type) AS file_type,
 row_number() OVER (PARTITION BY
 CASE WHEN d.source_metadata->>'kind'='paper' THEN
 jsonb_build_array(d.owner_id,d.source_metadata->>'access_scope',d.source_metadata->>'canonical_id',
 coalesce(d.source_metadata->>'test_run_id',''))::text ELSE d.id END
 ORDER BY CASE coalesce(d.source_metadata->>'text_status','')
 WHEN 'full_text' THEN 4 WHEN 'pdf' THEN 4 WHEN '' THEN 3
 WHEN 'abstract_only' THEN 2 WHEN 'title_and_abstract' THEN 2
 WHEN 'title_only' THEN 1 WHEN 'metadata_only' THEN 1 ELSE 0 END DESC,
 d.source_metadata->>'observed_at' DESC NULLS LAST,d.created_at DESC,d.id DESC) AS representation
 FROM documents d LEFT JOIN source_index_heads h ON h.document_id=d.id
 WHERE ($2::boolean OR d.owner_id=$1)
 AND document_readable($1,d.owner_id,d.source_metadata,
 CASE WHEN d.source_metadata IS NULL THEN $2 ELSE $3 END,coalesce(h.revoked,false))
), filtered AS (
 SELECT `

const readableTail = `
 FROM readable WHERE representation=1
 AND ($4='' OR position(lower($4) in lower(filename))>0 OR position(lower($4) in lower(title))>0)
 AND ($5='' OR status=$5 OR ($5='processing' AND status NOT IN ('indexed','failed')))
 AND ($6='' OR file_type=$6)
) `

const readablePage = readableHead + `d.id,d.status,d.created_at,d.filename,d.title,` +
	readableRest + `id,created_at` + readableTail

const readableStats = readableHead +
	`d.id,d.size,d.status,d.chunk_count,d.created_at,d.filename,d.title,` +
	readableRest + `id,size,status,chunk_count,created_at,file_type` + readableTail

func (r *DocumentQueries) arguments(ctx context.Context, q contracts.DocumentQuery) ([]any, error) {
	if q.ReaderID == "" || len(q.Query) > 512 || len(q.BeforeID) > 128 || q.Offset < 0 || q.Offset > 100000 {
		return nil, domain.ErrInvalidArgument
	}
	if q.AllOwners {
		var allowed bool
		err := r.db.Pool.QueryRow(
			ctx,
			`SELECT roles && ARRAY['admin','operator']::text[] FROM users WHERE id=$1`,
			q.ReaderID,
		).Scan(
			&allowed,
		)
		if errors.Is(err, pgx.ErrNoRows) || err == nil && !allowed {
			return nil, domain.ErrNotFound
		}
		if err != nil {
			return nil, fmt.Errorf("read document query permissions: %w", err)
		}
	}
	return []any{q.ReaderID, q.AllOwners, r.shared, strings.TrimSpace(q.Query), q.Status, q.FileType}, nil
}

func (r *DocumentQueries) Page(ctx context.Context, q contracts.DocumentQuery) (domain.DocumentPage, error) {
	out := domain.DocumentPage{Documents: []*domain.Document{}}
	args, err := r.arguments(ctx, q)
	if err != nil {
		return out, err
	}
	limit := q.Limit
	if limit <= 0 {
		limit = 100
	}
	limit = min(limit, 500)
	tx, err := r.db.Pool.BeginTx(ctx, pgx.TxOptions{IsoLevel: pgx.RepeatableRead, AccessMode: pgx.ReadOnly})
	if err != nil {
		return out, fmt.Errorf("page: %w", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()
	rows, err := tx.Query(ctx, readablePage+`, page AS (SELECT id,created_at FROM filtered
 WHERE ($7='' OR (created_at,id)<(SELECT created_at,id FROM documents WHERE id=$7))
 ORDER BY created_at DESC,id DESC LIMIT $8 OFFSET $9)
 SELECT (SELECT count(*) FROM filtered),d.id,d.owner_id,d.filename,d.mime_type,d.size,d.status,d.status_msg,
 d.chunk_count,d.created_at,d.updated_at,d.title,d.author,d.published_at,d.source_ref,d.kind,d.source_metadata,
 d.generation FROM page p JOIN documents d ON d.id=p.id ORDER BY p.created_at DESC,p.id DESC`,
		append(slices.Clip(args), q.BeforeID, limit+1, q.Offset)...)
	if err != nil {
		return out, fmt.Errorf("page: %w", err)
	}
	defer rows.Close()
	for rows.Next() {
		d := new(domain.Document)
		if err = rows.Scan(&out.Total, &d.ID, &d.OwnerID, &d.Filename, &d.MIMEType, &d.Size, &d.Status, &d.StatusMsg,
			&d.ChunkCount, &d.CreatedAt, &d.UpdatedAt, &d.Title, &d.Author, &d.PublishedAt, &d.SourceRef, &d.Kind,
			&d.SourceMetadata, &d.Generation); err != nil {
			return out, fmt.Errorf("page: %w", err)
		}
		out.Documents = append(out.Documents, d)
	}
	if err = rows.Err(); err != nil {
		return out, fmt.Errorf("page: %w", err)
	}
	rows.Close()
	// Счёт едет колонкой строки; второй проход нужен лишь за концом выдачи.
	if len(out.Documents) == 0 {
		if err = tx.QueryRow(ctx, readablePage+`SELECT count(*) FROM filtered`, args...).Scan(&out.Total); err != nil {
			return out, fmt.Errorf("page: %w", err)
		}
	}
	if len(out.Documents) > limit {
		out.Documents = out.Documents[:limit]
		out.NextCursor = out.Documents[limit-1].ID
	}
	if operationErr := tx.Commit(ctx); operationErr != nil {
		return out, fmt.Errorf("page: %w", operationErr)
	}
	return out, nil
}

// Stats агрегирует только доступную часть корпуса внутри БД.
func (r *DocumentQueries) Stats(ctx context.Context, q contracts.DocumentQuery) (contracts.DocumentStats, error) {
	var out contracts.DocumentStats
	args, err := r.arguments(ctx, q)
	if err != nil {
		return out, err
	}
	var raw []byte
	err = r.db.Pool.QueryRow(ctx, readableStats+`SELECT jsonb_build_object(
 'total',count(*),'total_size_bytes',coalesce(sum(size),0),'total_chunks',coalesce(sum(chunk_count),0),
 'indexed_pct',CASE WHEN count(*)=0 THEN 0 ELSE (count(*) FILTER(WHERE status='indexed')*100+count(*)/2)/count(*) END,
 'by_status',coalesce((SELECT jsonb_object_agg(status,n) FROM (SELECT status,count(*) n FROM filtered GROUP BY status) s),'{}'),
 'by_file_type',coalesce((SELECT jsonb_object_agg(file_type,n) FROM
 (SELECT file_type,count(*) n FROM filtered GROUP BY file_type) s),'{}'),
 'uploads_by_day',(SELECT jsonb_agg(jsonb_build_object('day',day::date,'count',n) ORDER BY day) FROM (
 SELECT day,count(f.id) n FROM generate_series((now() AT TIME ZONE 'UTC')::date-13,
 (now() AT TIME ZONE 'UTC')::date,interval '1 day') day
 LEFT JOIN filtered f ON (f.created_at AT TIME ZONE 'UTC')::date=day::date GROUP BY day) s),
 'top_by_chunks',coalesce((SELECT jsonb_agg(jsonb_build_object('id',t.id,'filename',d.filename,'title',d.title,
 'chunk_count',t.chunk_count)
 ORDER BY t.chunk_count DESC,t.id) FROM
 (SELECT id,chunk_count FROM filtered ORDER BY chunk_count DESC,id LIMIT 10) t
 JOIN documents d ON d.id=t.id),'[]')) FROM filtered`,
		args...).Scan(
		&raw,
	)
	if err != nil {
		return out, fmt.Errorf("stats: %w", err)
	}
	err = json.Unmarshal(raw, &out)
	if err != nil {
		return out, fmt.Errorf("stats: %w", err)
	}
	return out, nil
}
