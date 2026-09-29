package postgres

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json/v2"
	"fmt"
	"strconv"
	"strings"

	"github.com/example/db-service/internal/platform/extractdoc"
	"github.com/jackc/pgx/v5"
)

func (r *TechnologyRepo) SearchTechnologies(ctx context.Context, in extractdoc.SearchTechnologies,
) (extractdoc.TechnologyPage, error) {
	page := extractdoc.TechnologyPage{Technologies: []extractdoc.TechnologyHit{},
		SchemaVersion: extractdoc.CatalogueSchemaVersion}
	if err := in.Validate(); err != nil {
		return page, err
	}
	in.SharedCorpus = r.shared && in.SharedCorpus
	query := strings.Join(strings.Fields(strings.ToLower(in.Query)), " ")
	tx, err := r.db.Pool.BeginTx(ctx, pgx.TxOptions{IsoLevel: pgx.RepeatableRead, AccessMode: pgx.ReadOnly})
	if err != nil {
		return page, fmt.Errorf("begin catalogue search: %w", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()
	if page.CatalogueVersion, err = catalogueVersion(ctx, tx, in, query); err != nil {
		return page, err
	}
	offset, err := cursorOffset(in.Cursor, page.CatalogueVersion)
	if err != nil {
		return page, err
	}
	if query == "" && !in.IncludeAll {
		page.Reason = extractdoc.ReasonQueryRequired
		return page, nil
	}
	hits, err := catalogueHits(ctx, tx, in, query, offset)
	if err != nil {
		return page, err
	}
	if len(hits) > in.Limit {
		page.NextCursor = page.CatalogueVersion + ":" + strconv.Itoa(offset+in.Limit)
		hits = hits[:in.Limit]
	}
	page.Technologies = hits
	switch {
	case len(hits) > 0 || offset > 0:
	case query == "":
		page.Reason = extractdoc.ReasonEmptyCatalogue
	default:
		// Подсказка ближайшего промаха превратила бы «ничего» в ложное попадание.
		page.Reason = extractdoc.ReasonNoMatch
	}
	if err = tx.Commit(ctx); err != nil {
		return page, fmt.Errorf("commit catalogue search: %w", err)
	}
	return page, nil
}

// catalogueVersion не принимает курсор, выданный до пересчёта.
func catalogueVersion(ctx context.Context, tx pgx.Tx, in extractdoc.SearchTechnologies, query string) (string, error) {
	var epoch int64
	if err := tx.QueryRow(ctx, `SELECT value FROM technology_catalogue_epoch WHERE id=true`).Scan(&epoch); err != nil {
		return "", fmt.Errorf("read catalogue version: %w", err)
	}
	// Курсор привязан к читателю, запросу и фильтрам, а не только к данным.
	in.Query, in.Cursor, in.Limit = query, "", 0
	raw, err := json.Marshal(in)
	if err != nil {
		return "", fmt.Errorf("encode catalogue scope: %w", err)
	}
	sum := sha256.Sum256(append(raw, []byte(strconv.FormatInt(epoch, 10))...))
	return hex.EncodeToString(sum[:])[:32], nil
}

func cursorOffset(cursor, version string) (int, error) {
	if cursor == "" {
		return 0, nil
	}
	pinned, rest, found := strings.Cut(cursor, ":")
	if !found || pinned != version {
		return 0, extractdoc.ErrConflict
	}
	offset, err := strconv.Atoi(rest)
	if err != nil || offset < 0 || offset > 100000 {
		return 0, extractdoc.ErrInvalid
	}
	return offset, nil
}

func catalogueHits(ctx context.Context, tx pgx.Tx, in extractdoc.SearchTechnologies,
	query string, offset int,
) ([]extractdoc.TechnologyHit, error) {
	statement := catalogueAllSQL
	args := []any{in.ReaderID, in.SharedCorpus, emptyToNil(in.Languages),
		emptyToNil(in.Roles), in.Limit + 1, offset}
	if query != "" {
		query = strings.NewReplacer(`\`, `\\`, `%`, `\%`, `_`, `\_`).Replace(query)
		statement, args = catalogueSearchSQL, append(args, query)
	}
	rows, err := tx.Query(ctx, statement, args...)
	if err != nil {
		return nil, fmt.Errorf("search catalogue: %w", err)
	}
	defer rows.Close()
	out := []extractdoc.TechnologyHit{}
	for rows.Next() {
		var hit extractdoc.TechnologyHit
		var reasons []string
		if err = rows.Scan(&hit.TechnologyID, &hit.CanonicalName, &hit.Description,
			&hit.ValidationStatus, &reasons, &hit.MatchedText); err != nil {
			return nil, fmt.Errorf("scan catalogue hit: %w", err)
		}
		hit.MatchReasons = reasons
		out = append(out, hit)
	}
	if err = rows.Err(); err != nil {
		return nil, fmt.Errorf("scan catalogue: %w", err)
	}
	return out, nil
}

func emptyToNil(values []string) []string {
	if len(values) == 0 {
		return nil
	}
	return values
}

// catalogueReadable — правило доступа. Без статистики по access_scope (CREATE STATISTICS в миграции 00037)
// Postgres строит план от всей таблицы упоминаний.
const catalogueReadable = `
WITH readable AS (
    SELECT d.id, d.source_metadata->>'language' AS language
    FROM documents d
    LEFT JOIN source_index_heads h ON h.document_id = d.id
    WHERE NOT coalesce(h.revoked, false)
      AND (d.owner_id = $1
           OR coalesce(d.source_metadata->>'access_scope','') = 'public'
           OR ($2 AND coalesce(d.source_metadata->>'access_scope','') = 'shared'))
)`

const catalogueReadableMention = `EXISTS (SELECT 1 FROM technology_mentions m
       JOIN readable r ON r.id = m.source_revision_id WHERE m.technology_id = t.id)`

// catalogueMentionFilters: каждый фильтр может выполнить своё читаемое упоминание, не обязательно одно и то же.
const catalogueMentionFilters = `
  AND ($3::text[] IS NULL OR EXISTS (SELECT 1 FROM technology_mentions m
       JOIN readable r ON r.id = m.source_revision_id
       WHERE m.technology_id = t.id AND r.language = ANY($3)))
  AND ($4::text[] IS NULL OR EXISTS (SELECT 1 FROM technology_mentions m
       JOIN readable r ON r.id = m.source_revision_id
       WHERE m.technology_id = t.id AND m.role = ANY($4)))`

// catalogueSearchSQL читает через триграммные индексы только строки с запросом; совпадение в читаемом
// упоминании уже доказывает доступ, остальные совпадения проверяются отдельно.
const catalogueSearchSQL = catalogueReadable + `, matched AS (
    SELECT t.id AS technology_id, 1 AS rank, 'name' AS reason, false AS proven, NULL::text AS snippet
    FROM technologies t
    WHERE lower(t.canonical_name) LIKE '%' || $7 || '%'
    UNION ALL
    SELECT a.technology_id, 2, 'alias', false, a.value FROM technology_aliases a
    WHERE lower(a.value) LIKE '%' || $7 || '%'
    UNION ALL
    SELECT t.id, 3, 'description', false, NULL FROM technologies t
    WHERE lower(t.description) LIKE '%' || $7 || '%'
    UNION ALL
    SELECT m.technology_id, 4, 'mechanism', true, m.mechanism FROM technology_mentions m
    JOIN readable r ON r.id = m.source_revision_id
    WHERE lower(m.mechanism) LIKE '%' || $7 || '%'
    UNION ALL
    SELECT m.technology_id, 5, 'problem', true, m.problem FROM technology_mentions m
    JOIN readable r ON r.id = m.source_revision_id
    WHERE lower(m.problem) LIKE '%' || $7 || '%'
    UNION ALL
    SELECT m.technology_id, 6, 'application', true,
      (SELECT o FROM unnest(m.outputs) o WHERE lower(o) LIKE '%' || $7 || '%' LIMIT 1)
    FROM technology_mentions m
    JOIN readable r ON r.id = m.source_revision_id
    WHERE technology_outputs_text(m.outputs) LIKE '%' || $7 || '%'
      AND EXISTS (SELECT 1 FROM unnest(m.outputs) o WHERE lower(o) LIKE '%' || $7 || '%')
), reasons AS (
    SELECT technology_id, array_agg(reason ORDER BY rank) AS reasons, bool_or(proven) AS proven
    FROM (SELECT DISTINCT technology_id, rank, reason, proven FROM matched) m
    GROUP BY technology_id
), snippets AS (
    -- The matched text of the best-ranked field that is not already on the
    -- card: a name or a description match shows itself, an alias, a mechanism,
    -- a problem or an output does not.
    SELECT DISTINCT ON (technology_id) technology_id, snippet FROM matched
    WHERE snippet IS NOT NULL
    ORDER BY technology_id, rank
)
SELECT t.id, t.canonical_name, t.description, t.validation_status, g.reasons, coalesce(s.snippet, '')
FROM reasons g
JOIN technologies t ON t.id = g.technology_id
LEFT JOIN snippets s ON s.technology_id = g.technology_id
WHERE (g.proven OR ` + catalogueReadableMention + `)` + catalogueMentionFilters + `
ORDER BY cardinality(g.reasons) DESC, t.canonical_name, t.id
LIMIT $5 OFFSET $6`

const catalogueAllSQL = catalogueReadable + `
SELECT t.id, t.canonical_name, t.description, t.validation_status, '{}'::text[], ''
FROM technologies t
WHERE ` + catalogueReadableMention + catalogueMentionFilters + `
ORDER BY t.canonical_name, t.id
LIMIT $5 OFFSET $6`
