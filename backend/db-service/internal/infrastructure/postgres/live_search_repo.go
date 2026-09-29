package postgres

import (
	"context"
	"encoding/json/v2"
	"errors"
	"fmt"
	"time"

	"github.com/example/db-service/internal/infrastructure/postgres/sqlcgen"
	"github.com/example/db-service/internal/platform/analysisdoc"
	"github.com/example/db-service/internal/platform/indexdoc"
	"github.com/jackc/pgx/v5"
)

// ReadLiveSearch: сначала точный запрос, иначе самый свежий по тем же терминам; Found без Fresh —
// ответ устарел, но годится как основа обновления.
func (r *AnalysisRepo) ReadLiveSearch(
	ctx context.Context, req analysisdoc.LiveSearchRead,
) (analysisdoc.LiveSearchStored, error) {
	if req.Query == "" && req.TermsKey == "" {
		return analysisdoc.LiveSearchStored{}, fmt.Errorf("%w: query or terms_key is required", analysisdoc.ErrInvalid)
	}
	q := sqlcgen.New(r.pool)
	if req.Query != "" {
		row, err := q.GetTrendLiveSearch(ctx, req.Query)
		switch {
		case err == nil:
			return liveSearchStored(ctx, q, row, req.WithVersion), nil
		case !errors.Is(err, pgx.ErrNoRows):
			return analysisdoc.LiveSearchStored{}, fmt.Errorf("read live search: %w", err)
		}
	}
	if req.TermsKey == "" {
		return analysisdoc.LiveSearchStored{}, nil
	}
	row, err := q.GetFreshTrendLiveSearchByTerms(ctx, req.TermsKey)
	switch {
	case errors.Is(err, pgx.ErrNoRows):
		return analysisdoc.LiveSearchStored{}, nil
	case err != nil:
		return analysisdoc.LiveSearchStored{}, fmt.Errorf("read live search by terms: %w", err)
	}
	return liveSearchStored(ctx, q, row, req.WithVersion), nil
}

func liveSearchStored(ctx context.Context, q *sqlcgen.Queries, row sqlcgen.TrendLiveSearch,
	withVersion bool,
) analysisdoc.LiveSearchStored {
	sources := int(row.Sources)
	if count, err := q.CountTrendLiveRecords(ctx, row.Query); err == nil {
		sources = int(count)
	}
	version := ""
	if withVersion {
		version = indexdoc.Hash(row.Detail)
	}
	return analysisdoc.LiveSearchStored{
		Version: version,
		Found:   true, Fresh: row.ExpiresAt.After(time.Now()),
		Query: row.Query, TermsKey: row.TermsKey, Terms: row.Terms,
		Papers: int(row.Papers), Sources: sources,
		ObservedAt: row.ObservedAt, ExpiresAt: row.ExpiresAt, Detail: row.Detail,
	}
}

func (r *AnalysisRepo) SaveLiveSearch(
	ctx context.Context, req analysisdoc.LiveSearchSave,
) (analysisdoc.LiveSearchStored, error) {
	if req.Query == "" || len(req.Detail) == 0 {
		return analysisdoc.LiveSearchStored{}, fmt.Errorf("%w: query and detail are required", analysisdoc.ErrInvalid)
	}
	records, err := json.Marshal(req.Records)
	if err != nil {
		return analysisdoc.LiveSearchStored{}, fmt.Errorf("encode live records: %w", err)
	}
	// Столбец NOT NULL: поиск без описания для карты хранит пустой объект.
	summary := req.Summary
	if len(summary) == 0 {
		summary = []byte("{}")
	}
	tx, err := r.pool.Begin(ctx)
	if err != nil {
		return analysisdoc.LiveSearchStored{}, fmt.Errorf("begin live search: %w", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()
	if err = lockLiveSearch(ctx, tx, req); err != nil {
		return analysisdoc.LiveSearchStored{}, err
	}
	q := sqlcgen.New(tx)
	if err = q.UpsertTrendLiveSearch(ctx, sqlcgen.UpsertTrendLiveSearchParams{
		Query: req.Query, TermsKey: req.TermsKey, Terms: liveTerms(req.Terms),
		Papers: int32(req.Papers), Sources: int32(len(req.Records)),
		ObservedAt: req.ObservedAt, ExpiresAt: req.ExpiresAt, Detail: req.Detail, Summary: summary,
	}); err != nil {
		return analysisdoc.LiveSearchStored{}, fmt.Errorf("upsert live search: %w", err)
	}
	if err = q.DeleteTrendLiveRecords(ctx, req.Query); err != nil {
		return analysisdoc.LiveSearchStored{}, fmt.Errorf("clear live records: %w", err)
	}
	if len(req.Records) > 0 {
		if err = q.InsertTrendLiveRecords(ctx, sqlcgen.InsertTrendLiveRecordsParams{
			Query: req.Query, ObservedAt: req.ObservedAt, Records: records,
		}); err != nil {
			return analysisdoc.LiveSearchStored{}, fmt.Errorf("insert live records: %w", err)
		}
	}
	// Ответы операторов не удаляются по сроку; убирается лишь прежний формат того
	// же запроса, иначе пересборка брала бы его снова.
	if err = q.DeleteSupersededTrendLiveSearches(ctx); err != nil {
		return analysisdoc.LiveSearchStored{}, fmt.Errorf("purge superseded live searches: %w", err)
	}
	// Хеш берётся из нормализованного JSONB под той же блокировкой, а не из следующего чтения после записи.
	row, err := q.GetTrendLiveSearch(ctx, req.Query)
	if err != nil {
		return analysisdoc.LiveSearchStored{}, fmt.Errorf("read saved live search version: %w", err)
	}
	if err := tx.Commit(ctx); err != nil {
		return analysisdoc.LiveSearchStored{}, fmt.Errorf("commit live search: %w", err)
	}
	version := ""
	if req.IfMatch != nil {
		version = indexdoc.Hash(row.Detail)
	}
	return analysisdoc.LiveSearchStored{
		Version: version,
		Found:   true, Fresh: req.ExpiresAt.After(time.Now()), Query: req.Query, TermsKey: req.TermsKey,
		Terms: req.Terms, Papers: req.Papers, Sources: len(req.Records),
		ObservedAt: req.ObservedAt, ExpiresAt: req.ExpiresAt,
	}, nil
}

// Блокировка ключа охватывает и первую вставку: блокировать отсутствующую строку через FOR UPDATE нельзя.
func lockLiveSearch(ctx context.Context, tx pgx.Tx, req analysisdoc.LiveSearchSave) error {
	if _, err := tx.Exec(ctx, "SELECT pg_advisory_xact_lock(hashtextextended($1, 0))", "live-search:"+req.Query); err != nil {
		return fmt.Errorf("lock live search: %w", err)
	}
	if req.IfMatch == nil {
		return nil
	}
	row, err := sqlcgen.New(tx).GetTrendLiveSearch(ctx, req.Query)
	if errors.Is(err, pgx.ErrNoRows) {
		if *req.IfMatch == "" {
			return nil
		}
		return fmt.Errorf("live search disappeared: %w", analysisdoc.ErrConflict)
	}
	if err != nil {
		return fmt.Errorf("read live search precondition: %w", err)
	}
	if *req.IfMatch != indexdoc.Hash(row.Detail) && !liveSearchReplacesRun(req, row.Detail) {
		return fmt.Errorf("live search changed: %w", analysisdoc.ErrConflict)
	}
	return nil
}

// Пересказ прежнего запуска не должен вытеснять свежий сбор; новый чужой запуск защищён прежним условием.
func liveSearchReplacesRun(req analysisdoc.LiveSearchSave, current []byte) bool {
	if req.BaselineRunID == "" || req.IfMatch == nil || *req.IfMatch == "" {
		return false
	}
	var before, after struct {
		RunID string `json:"run_id"`
	}
	return json.Unmarshal(current, &before) == nil && json.Unmarshal(req.Detail, &after) == nil &&
		before.RunID == req.BaselineRunID && after.RunID != "" && after.RunID != before.RunID
}

// liveTerms заменяет nil пустым списком: столбец terms объявлен NOT NULL.
func liveTerms(terms []string) []string {
	if terms == nil {
		return []string{}
	}
	return terms
}

// ListLiveSearches отдаёт историю без самих ответов, чтобы не тащить мегабайты.
func (r *AnalysisRepo) ListLiveSearches(ctx context.Context, req analysisdoc.LiveSearchList,
) (analysisdoc.LiveSearchHistory, error) {
	limit := req.Limit
	if limit <= 0 || limit > liveSearchHistoryMax {
		limit = liveSearchHistoryMax
	}
	rows, err := sqlcgen.New(r.pool).ListTrendLiveSearches(ctx, int32(limit))
	if err != nil {
		return analysisdoc.LiveSearchHistory{}, fmt.Errorf("list live searches: %w", err)
	}
	history := analysisdoc.LiveSearchHistory{Entries: make([]analysisdoc.LiveSearchEntry, 0, len(rows))}
	for _, row := range rows {
		history.Entries = append(history.Entries, analysisdoc.LiveSearchEntry{
			Query: row.Query, Fresh: row.ExpiresAt.After(time.Now()), Terms: row.Terms,
			Papers: int(row.Papers), Sources: int(row.Sources),
			ObservedAt: row.ObservedAt, ExpiresAt: row.ExpiresAt, Summary: row.Summary,
		})
	}
	return history, nil
}

// liveSearchHistoryMax велик, чтобы сохранённые запросы не выпадали с карты.
const liveSearchHistoryMax = 1000
