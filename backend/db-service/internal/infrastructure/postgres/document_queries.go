package postgres

import "fmt"

import (
	"context"
	"encoding/json"
	"errors"
	"slices"
	"strings"
	"sync"
	"time"

	"github.com/example/db-service/internal/domain"
	"github.com/example/db-service/internal/platform/contracts"
	"github.com/jackc/pgx/v5"
	"github.com/rs/zerolog"
	"golang.org/x/sync/singleflight"
)

// DocumentQueries применяет права до выбора представления статьи, фильтров и LIMIT.
type DocumentQueries struct {
	db     *DB
	shared bool
	mu     sync.Mutex
	orders map[contracts.DocumentQuery]documentOrder
	stats  map[contracts.DocumentQuery]documentStatsEntry
	counts singleflight.Group
}

type documentStatsEntry struct {
	stats contracts.DocumentStats
	at    time.Time
}

// Подсчёт идёт секунды, а цифры корпуса за сутки почти не меняются, поэтому дашборд сутки получает
// прошлый подсчёт и только просит обновить его в фоне. Прогрев раз в documentStatsWarmEvery держит
// подсчёт администраторов моложе documentStatsStale, даже если дашборд сутки никто не открывал.
const (
	documentStatsFresh     = 30 * time.Second
	documentStatsStale     = 24 * time.Hour
	documentStatsWarmEvery = 12 * time.Hour
)

// documentStatsPlan решает, отдать ли прошлый подсчёт сразу и нужен ли пересчёт в фоне.
// Без годного подсчёта запрос ждёт свежий: отдавать нечего.
func documentStatsPlan(cached bool, age time.Duration) (serve, refresh bool) {
	serve = cached && age < documentStatsStale
	return serve, serve && age >= documentStatsFresh
}

// documentOrder — порядок доступных id одного обхода: offset- и курсорные страницы фоновых
// воркеров режутся из него, а не пересчитывают дедупликацию всего корпуса на каждой странице.
type documentOrder struct {
	ids []string
	at  time.Time
}

const (
	documentOrderTTL = 2 * time.Minute
	documentOrderCap = 8
)

func NewDocumentQueries(db *DB, shared bool) *DocumentQueries {
	return &DocumentQueries{db: db, shared: shared, orders: map[contracts.DocumentQuery]documentOrder{},
		stats: map[contracts.DocumentQuery]documentStatsEntry{}}
}

// Доступ и дедупликация описаны один раз, а проекция у каждого пути своя: узкая
// выборка остаётся в памяти, а тяжёлые тексты читаются по готовым id.
const readableHead = `WITH readable AS (
 SELECT `

const readableAccess = `($2::boolean OR d.owner_id=$1)
 AND document_readable($1,d.owner_id,d.source_metadata,
 CASE WHEN d.source_metadata IS NULL THEN $2 ELSE $3 END,coalesce(h.revoked,false))`

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
 WHERE ` + readableAccess + `
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

const documentColumns = `d.id,d.owner_id,d.filename,d.mime_type,d.size,d.status,d.status_msg,
 d.chunk_count,d.created_at,d.updated_at,d.title,d.author,d.published_at,d.source_ref,d.kind,d.source_metadata,
 d.generation`

func documentFields(d *domain.Document) []any {
	return []any{&d.ID, &d.OwnerID, &d.Filename, &d.MIMEType, &d.Size, &d.Status, &d.StatusMsg,
		&d.ChunkCount, &d.CreatedAt, &d.UpdatedAt, &d.Title, &d.Author, &d.PublishedAt, &d.SourceRef, &d.Kind,
		&d.SourceMetadata, &d.Generation}
}

const readableStats = readableHead +
	`d.id,d.size,d.status,d.chunk_count,d.created_at,d.filename,d.title,` +
	readableRest + `id,size,status,chunk_count,created_at,file_type,` +
	`lower(coalesce(nullif(title,''),filename)) AS topic_title` + readableTail

func (r *DocumentQueries) arguments(ctx context.Context, q contracts.DocumentQuery) ([]any, error) {
	if q.ReaderID == "" || len(q.Query) > 512 || len(q.BeforeID) > 128 || q.Offset < 0 {
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
	args, err := r.arguments(ctx, q)
	if err != nil {
		return domain.DocumentPage{Documents: []*domain.Document{}}, err
	}
	limit := q.Limit
	if limit <= 0 {
		limit = 100
	}
	limit = min(limit, 500)
	key := q
	key.BeforeID, key.Limit, key.Offset = "", 0, 0
	if q.Offset > 0 || q.BeforeID != "" {
		out, ok, cerr := r.continuation(ctx, key, args, q, limit)
		if cerr != nil || ok {
			return out, cerr
		}
	}
	// Первая страница и разошедшийся снимок идут полным запросом, а следующая страница снимет свежий порядок.
	r.mu.Lock()
	delete(r.orders, key)
	r.mu.Unlock()
	return r.queryPage(ctx, args, q, limit)
}

func (r *DocumentQueries) continuation(ctx context.Context, key contracts.DocumentQuery, args []any,
	q contracts.DocumentQuery, limit int,
) (domain.DocumentPage, bool, error) {
	out := domain.DocumentPage{Documents: []*domain.Document{}}
	ids, err := r.order(ctx, key, args)
	if err != nil {
		return out, false, err
	}
	start := q.Offset
	if q.BeforeID != "" {
		i := slices.Index(ids, q.BeforeID)
		if i < 0 {
			return out, false, nil
		}
		start += i + 1
	}
	page := ids[min(start, len(ids)):min(start+limit, len(ids))]
	if len(page) > 0 {
		rows, qerr := r.db.Pool.Query(ctx, `SELECT `+documentColumns+` FROM unnest($4::text[]) WITH ORDINALITY p(id,n)
 JOIN documents d ON d.id=p.id LEFT JOIN source_index_heads h ON h.document_id=d.id
 WHERE `+readableAccess+` ORDER BY p.n`, args[0], args[1], args[2], page)
		if qerr != nil {
			return out, false, fmt.Errorf("page: %w", qerr)
		}
		out.Documents, err = pgx.CollectRows(rows, func(row pgx.CollectableRow) (*domain.Document, error) {
			d := new(domain.Document)
			return d, row.Scan(documentFields(d)...)
		})
		if err != nil {
			return out, false, fmt.Errorf("page: %w", err)
		}
	}
	// Документ, удалённый или закрытый после снимка, сдвинул бы страницу.
	if len(out.Documents) < len(page) {
		return out, false, nil
	}
	out.Total = int64(len(ids))
	if start+limit < len(ids) {
		out.NextCursor = page[len(page)-1]
	}
	return out, true, nil
}

func (r *DocumentQueries) order(ctx context.Context, key contracts.DocumentQuery, args []any) ([]string, error) {
	r.mu.Lock()
	o, ok := r.orders[key]
	r.mu.Unlock()
	if ok && time.Since(o.at) < documentOrderTTL {
		return o.ids, nil
	}
	rows, err := r.db.Pool.Query(ctx, readablePage+`SELECT id FROM filtered ORDER BY created_at DESC,id DESC`, args...)
	if err != nil {
		return nil, fmt.Errorf("page order: %w", err)
	}
	ids, err := pgx.CollectRows(rows, pgx.RowTo[string])
	if err != nil {
		return nil, fmt.Errorf("page order: %w", err)
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	for k, o := range r.orders {
		if time.Since(o.at) >= documentOrderTTL || len(r.orders) >= documentOrderCap {
			delete(r.orders, k)
		}
	}
	r.orders[key] = documentOrder{ids: ids, at: time.Now()}
	return ids, nil
}

func (r *DocumentQueries) queryPage(ctx context.Context, args []any, q contracts.DocumentQuery,
	limit int,
) (domain.DocumentPage, error) {
	out := domain.DocumentPage{Documents: []*domain.Document{}}
	tx, err := r.db.Pool.BeginTx(ctx, pgx.TxOptions{IsoLevel: pgx.RepeatableRead, AccessMode: pgx.ReadOnly})
	if err != nil {
		return out, fmt.Errorf("page: %w", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()
	rows, err := tx.Query(ctx, readablePage+`, page AS (SELECT id,created_at FROM filtered
 WHERE ($7='' OR (created_at,id)<(SELECT created_at,id FROM documents WHERE id=$7))
 ORDER BY created_at DESC,id DESC LIMIT $8 OFFSET $9)
 SELECT (SELECT count(*) FROM filtered),`+documentColumns+`
 FROM page p JOIN documents d ON d.id=p.id ORDER BY p.created_at DESC,p.id DESC`,
		append(slices.Clip(args), q.BeforeID, limit+1, q.Offset)...)
	if err != nil {
		return out, fmt.Errorf("page: %w", err)
	}
	defer rows.Close()
	for rows.Next() {
		d := new(domain.Document)
		if err = rows.Scan(append([]any{&out.Total}, documentFields(d)...)...); err != nil {
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

// Stats агрегирует только доступную часть корпуса внутри БД. Тема каждого документа считается
// research_topic заново, и на 100 тыс. документов подсчёт идёт 4–7 с, поэтому дашборд получает прошлый
// подсчёт сразу, а свежий считается в фоне. Права проверяются до кэша, ключ включает читателя: частные
// источники одного администратора не видны другому.
func (r *DocumentQueries) Stats(ctx context.Context, q contracts.DocumentQuery) (contracts.DocumentStats, error) {
	args, err := r.arguments(ctx, q)
	if err != nil {
		return contracts.DocumentStats{}, err
	}
	key := q
	key.BeforeID, key.Limit, key.Offset = "", 0, 0
	r.mu.Lock()
	cached, ok := r.stats[key]
	r.mu.Unlock()
	serve, refresh := documentStatsPlan(ok, time.Since(cached.at))
	if refresh {
		go func() { _, _ = r.countStats(context.WithoutCancel(ctx), key, args) }()
	}
	if serve {
		return cached.stats, nil
	}
	return r.countStats(ctx, key, args)
}

// countStats ведёт не больше одного подсчёта на ключ: запрос, пришедший во время прогрева или фонового
// пересчёта, ждёт его итога, а не запускает второй. Ушедший клиент подсчёт не обрывает — итог нужен
// следующему открытию.
func (r *DocumentQueries) countStats(
	ctx context.Context,
	key contracts.DocumentQuery,
	args []any,
) (contracts.DocumentStats, error) {
	done := r.counts.DoChan(fmt.Sprintf("%#v", key), func() (any, error) {
		countCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), 2*time.Minute)
		defer cancel()
		out, err := r.computeStats(countCtx, args)
		if err == nil {
			r.storeStats(key, out)
		}
		return out, err
	})
	select {
	case res := <-done:
		out, _ := res.Val.(contracts.DocumentStats)
		return out, res.Err
	case <-ctx.Done():
		return contracts.DocumentStats{}, fmt.Errorf("stats: %w", ctx.Err())
	}
}

// WarmStats считает дашборд администраторов при старте и затем по расписанию, чтобы первое открытие
// после выкладки и после тихих суток не ждало полного подсчёта.
func (r *DocumentQueries) WarmStats(ctx context.Context, log zerolog.Logger) {
	for {
		rows, err := r.db.Pool.Query(ctx, `SELECT id FROM users WHERE 'admin'=ANY(roles) ORDER BY id LIMIT $1`,
			documentOrderCap)
		var readers []string
		if err == nil {
			readers, err = pgx.CollectRows(rows, pgx.RowTo[string])
		}
		if err != nil && ctx.Err() == nil {
			log.Warn().Err(err).Msg("admin stats warm-up: list admins")
		}
		// По одному читателю за раз: параллельные подсчёты по 100 тыс. документов нагрузили бы базу на старте.
		for _, reader := range readers {
			q := contracts.DocumentQuery{ReaderID: reader, AllOwners: true}
			args, qerr := r.arguments(ctx, q)
			if qerr == nil {
				_, qerr = r.countStats(ctx, q, args)
			}
			if qerr != nil && ctx.Err() == nil {
				log.Warn().Err(qerr).Str("reader", reader).Msg("admin stats warm-up")
			}
		}
		select {
		case <-ctx.Done():
			return
		case <-time.After(documentStatsWarmEvery):
		}
	}
}

func (r *DocumentQueries) storeStats(key contracts.DocumentQuery, out contracts.DocumentStats) {
	r.mu.Lock()
	defer r.mu.Unlock()
	for k, cached := range r.stats {
		if time.Since(cached.at) >= documentStatsStale || len(r.stats) >= documentOrderCap {
			delete(r.stats, k)
		}
	}
	r.stats[key] = documentStatsEntry{stats: out, at: time.Now()}
}

func (r *DocumentQueries) computeStats(ctx context.Context, args []any) (contracts.DocumentStats, error) {
	var out contracts.DocumentStats
	var raw []byte
	err := r.db.Pool.QueryRow(ctx, readableStats+documentTopicCTE+`SELECT jsonb_build_object(`+documentTopicAggregate+`
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
	for i, topic := range out.TopicDistribution.Topics {
		out.TopicDistribution.Topics[i].Label = documentTopicLabel(topic.ID)
	}
	return out, nil
}
