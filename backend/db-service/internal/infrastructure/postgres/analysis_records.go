package postgres

import (
	"cmp"
	"context"
	"encoding/json/v2"
	"fmt"
	"slices"
	"strings"

	"github.com/example/db-service/internal/platform/analysisdoc"
	"github.com/example/db-service/internal/platform/indexdoc"
	"github.com/example/db-service/internal/platform/sourcedoc"
	"github.com/jackc/pgx/v5"
)

func analysisCollectionScopes(ctx context.Context, tx pgx.Tx, request analysisdoc.Request) error {
	if len(request.CollectionScopeIDs) == 0 {
		return nil
	}
	var count int
	err := tx.QueryRow(ctx, `SELECT count(*) FROM source_collection_scopes
WHERE id=ANY($1::text[]) AND owner_id=$2 AND test_run_id=$3`,
		request.CollectionScopeIDs, request.OwnerID, request.TestRunID).Scan(&count)
	if err != nil {
		return fmt.Errorf("read selected collection scopes: %w", err)
	}
	if count != len(request.CollectionScopeIDs) {
		return analysisdoc.ErrNotFound
	}
	return nil
}

// analysisSourcesWhere фильтрует доступ до любого потолка; администраторам обхода нет.
const analysisSourcesWhere = `
FROM documents d LEFT JOIN source_index_heads h ON h.document_id=d.id
LEFT JOIN source_index_snapshots s ON s.document_id=d.id AND s.snapshot_id=h.active_snapshot_id AND s.ready
WHERE d.source_metadata IS NOT NULL AND (coalesce(d.source_metadata->>'test_run_id','')=''
 OR ($3<>'' AND d.source_metadata->>'test_run_id'=$3 AND d.owner_id=$1))
AND d.source_metadata->>'source_kind'<>'hypothesis' AND NOT coalesce(h.revoked,false)
AND (d.owner_id=$1 OR d.source_metadata->>'access_scope'='public'
 OR ($2 AND d.source_metadata->>'access_scope'='shared'))
AND (coalesce(cardinality($4::text[]),0)=0 OR EXISTS (
 SELECT 1 FROM source_collection_membership m
 WHERE m.document_id=d.id AND m.scope_id=ANY($4::text[]) AND m.first_seen_at<=$5))`

func analysisRecords(ctx context.Context, tx pgx.Tx, input *analysisdoc.Input) error {
	args := []any{input.Request.OwnerID, input.Request.SharedCorpus, input.Request.TestRunID,
		input.Request.CollectionScopeIDs, input.Request.Cutoff}
	var total int
	if err := tx.QueryRow(ctx, `SELECT count(*)`+analysisSourcesWhere, args...).Scan(&total); err != nil {
		return fmt.Errorf("count analysis sources: %w", err)
	}
	// Корпус больше потолка читается сначала по совпадению заголовка с терминами, затем по свежести,
	// а срез записывается в ограничения результата — вместо отказа работать. Совпадение по целому
	// слову, чтобы «ai» не находило «chain»; термины только [a-z0-9 -], экранировать нечего.
	// ponytail: лексическое совпадение только по заголовку, аннотаций в этой таблице нет;
	// ранжировать эмбеддингами, когда промахи по заголовку заметно теряют релевантное.
	terms := input.Request.SearchTerms
	rows, err := tx.Query(ctx, `SELECT d.id,d.title,d.mime_type,d.source_metadata,s.manifest,
 (SELECT count(*) FROM unnest($7::text[]) term WHERE d.title ~* ('\m'||term||'\M'))`+
		analysisSourcesWhere+`
ORDER BY 6 DESC, coalesce(d.source_metadata->'publication'->>'value','') DESC, d.id LIMIT $6`,
		append(args, analysisdoc.MaxDocuments, terms)...)
	if err != nil {
		return fmt.Errorf("read analysis sources: %w", err)
	}
	defer rows.Close()
	matched := 0
	for rows.Next() {
		record, hits, reason, readErr := analysisRecord(rows, input.Request)
		if readErr != nil {
			return readErr
		}
		if hits > 0 {
			matched++
			record.QueryMatched = true
		}
		if reason != "" {
			input.Excluded = append(input.Excluded, analysisdoc.Exclusion{DocumentID: record.ID, Reason: reason})
			continue
		}
		if input.Model != nil && *input.Model != record.Index.Model {
			return fmt.Errorf("incompatible source embedding models: %w", analysisdoc.ErrInvalid)
		}
		input.Model = new(record.Index.Model)
		input.Records = append(input.Records, record)
	}
	if err = rows.Err(); err != nil {
		return fmt.Errorf("scan analysis sources: %w", err)
	}
	switch {
	case total <= analysisdoc.MaxDocuments:
	case len(terms) == 0:
		input.Limitations = append(input.Limitations, fmt.Sprintf(
			"corpus_truncated: %d newest documents of %d analysed; narrow the scope for full coverage",
			analysisdoc.MaxDocuments, total))
	default:
		input.Limitations = append(input.Limitations, fmt.Sprintf(
			"corpus_truncated: %d of %d documents analysed: %d with a title matching %s, the rest the newest;"+
				" narrow the scope for full coverage", analysisdoc.MaxDocuments, total, matched,
			strings.Join(terms, " / ")))
	}
	return nil
}

func analysisRecord(row pgx.Row, request analysisdoc.Request) (analysisdoc.Record, int, string, error) {
	var record analysisdoc.Record
	var sourceRaw, manifestRaw []byte
	var hits int
	if err := row.Scan(&record.ID, &record.Title, &record.MIMEType, &sourceRaw, &manifestRaw, &hits); err != nil {
		return record, 0, "", fmt.Errorf("scan frozen source: %w", err)
	}
	source, err := sourcedoc.Decode(sourceRaw)
	if err != nil {
		return record, hits, "invalid_provenance", nil //nolint:nilerr // Quarantine is explicit in the frozen exclusion list.
	}
	canonical, err := json.Marshal(source, json.Deterministic(true))
	if err != nil {
		return record, hits, "", fmt.Errorf("encode source identity hash: %w", err)
	}
	record.SourceHash = indexdoc.Hash(canonical)
	var reason string
	record.Source, record.Limitations, reason = analysisdoc.EligibleSource(*source, request)
	if reason != "" {
		return record, hits, reason, nil
	}
	if len(manifestRaw) == 0 {
		return record, hits, "index_not_ready", nil
	}
	record.Index, err = indexdoc.Decode[indexdoc.Manifest](string(manifestRaw))
	if err != nil || record.Index.Validate() != nil || record.Index.DocumentID != record.ID ||
		record.Index.SourceRevision != source.Revision || record.Index.RawHash != source.ContentHash {
		return record, hits, "invalid_index_manifest", nil //nolint:nilerr // Malformed indexes cannot enter usable membership.
	}
	record.Representation = "metadata_or_text"
	if record.MIMEType == "application/pdf" {
		record.Representation = "full_text"
	}
	return record, hits, "", nil
}

func analysisPages(ctx context.Context, tx pgx.Tx, input *analysisdoc.Input) error {
	rows, err := tx.Query(ctx, `SELECT p.scope_id,p.run_id,p.page_number,s.provider,s.scope,
p.raw_key,p.raw_hash,p.manifest_key,p.manifest_hash,p.received,p.rejected,p.exhausted,
p.limitations,p.created_at FROM source_collection_pages p
JOIN source_collection_scopes s ON s.id=p.scope_id
WHERE s.test_run_id=$4 AND s.owner_id=$1 AND p.created_at<=$3
AND (coalesce(cardinality($5::text[]),0)=0 OR s.id=ANY($5::text[]))
ORDER BY p.created_at DESC,p.scope_id,p.run_id,p.page_number LIMIT $2`,
		input.Request.OwnerID, analysisdoc.MaxPages+1, input.Request.Cutoff, input.Request.TestRunID,
		input.Request.CollectionScopeIDs)
	if err != nil {
		return fmt.Errorf("read collection coverage: %w", err)
	}
	defer rows.Close()
	for rows.Next() {
		var page analysisdoc.Page
		err = rows.Scan(&page.ScopeID, &page.RunID, &page.Number, &page.Provider, &page.Scope,
			&page.Raw.Key, &page.Raw.SHA256, &page.Manifest.Key, &page.Manifest.SHA256, &page.Received,
			&page.Rejected, &page.Exhausted, &page.Limitations, &page.CapturedAt)
		if err != nil {
			return fmt.Errorf("scan collection coverage: %w", err)
		}
		input.Pages = append(input.Pages, page)
	}
	if err = rows.Err(); err != nil {
		return fmt.Errorf("read collection coverage rows: %w", err)
	}
	// Страниц больше потолка: берутся новейшие, срез пишется в ограничения, а
	// порядок восстанавливается ради стабильного хеша входа.
	if len(input.Pages) > analysisdoc.MaxPages {
		input.Pages = input.Pages[:analysisdoc.MaxPages]
		input.Limitations = append(input.Limitations, fmt.Sprintf(
			"collection_coverage_truncated: %d newest pages read; narrow the scope for full coverage",
			analysisdoc.MaxPages))
	}
	slices.SortFunc(input.Pages, func(a, b analysisdoc.Page) int {
		return cmp.Or(cmp.Compare(a.ScopeID, b.ScopeID), cmp.Compare(a.RunID, b.RunID), cmp.Compare(a.Number, b.Number))
	})
	input.Limitations = append(input.Limitations, "collection_coverage_limited_to_readers_own_scopes",
		"collection_coverage_after_cutoff_excluded")
	if len(input.Request.CollectionScopeIDs) > 0 {
		input.Limitations = append(input.Limitations,
			"explicit_collection_scopes; membership_does_not_imply_complete_pagination",
			"legacy_duplicate_scope_membership_requires_replay_or_verified_page_backfill")
	}
	return nil
}
