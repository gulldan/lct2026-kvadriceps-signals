package postgres

import (
	"context"
	"errors"
	"fmt"

	"github.com/example/db-service/internal/platform/extractdoc"
	"github.com/example/db-service/internal/platform/indexdoc"
	"github.com/example/db-service/internal/platform/sourcedoc"
	"github.com/jackc/pgx/v5"
)

// TechnologyRepo только читает: запуск извлечения — отдельное право.
type TechnologyRepo struct {
	db     *DB
	shared bool
}

func NewTechnologyRepo(db *DB) *TechnologyRepo { return &TechnologyRepo{db: db} }

func (r *TechnologyRepo) WithSharedCorpus(shared bool) *TechnologyRepo {
	r.shared = shared
	return r
}

// ReadRevision отвечает на нечитаемую ревизию ровно как на отсутствующую.
func (r *TechnologyRepo) ReadRevision(ctx context.Context, in extractdoc.ReadRevision,
) (extractdoc.RevisionTechnologies, error) {
	var out extractdoc.RevisionTechnologies
	if err := in.Validate(); err != nil {
		return out, err
	}
	tx, err := r.db.Pool.BeginTx(ctx, pgx.TxOptions{IsoLevel: pgx.RepeatableRead, AccessMode: pgx.ReadOnly})
	if err != nil {
		return out, fmt.Errorf("begin technology read: %w", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()
	if err = checkRevisionAccess(ctx, tx, in, r.shared); err != nil {
		return out, err
	}
	out.SourceRevisionID = in.SourceRevisionID
	out.ExtractorVersion, out.SchemaVersion = in.ExtractorVersion, in.SchemaVersion
	if err = readCoverage(ctx, tx, &out); err != nil {
		return out, err
	}
	if out.Technologies, err = readRevisionMentions(ctx, tx, out); err != nil {
		return out, err
	}
	if err = tx.Commit(ctx); err != nil {
		return out, fmt.Errorf("commit technology read: %w", err)
	}
	return out, nil
}

// checkRevisionAccess выполняется первым, чтобы извлечения не раскрыли закрытый документ.
func checkRevisionAccess(ctx context.Context, tx pgx.Tx, in extractdoc.ReadRevision, shared bool) error {
	var owner string
	var sourceRaw []byte
	var revoked bool
	err := tx.QueryRow(ctx, `SELECT d.owner_id, d.source_metadata, coalesce(h.revoked,false)
FROM documents d LEFT JOIN source_index_heads h ON h.document_id = d.id WHERE d.id=$1`,
		in.SourceRevisionID).Scan(&owner, &sourceRaw, &revoked)
	if errors.Is(err, pgx.ErrNoRows) {
		return extractdoc.ErrNotFound
	}
	if err != nil {
		return fmt.Errorf("read revision access: %w", err)
	}
	var source *sourcedoc.Source
	if len(sourceRaw) > 0 {
		if source, err = sourcedoc.Decode(sourceRaw); err != nil {
			return fmt.Errorf("decode revision source: %w", err)
		}
	}
	if !indexdoc.CanRead(in.ReaderID, owner, source, shared && in.SharedCorpus, revoked) {
		return extractdoc.ErrNotFound
	}
	return nil
}

func readCoverage(ctx context.Context, tx pgx.Tx, out *extractdoc.RevisionTechnologies) error {
	err := tx.QueryRow(ctx, `SELECT extractor_version, schema_version, status, reason_code,
processed_sections, total_sections, extracted_at::text
FROM technology_extractions
WHERE source_revision_id=$1
  AND ($2::text = '' OR extractor_version=$2) AND ($3::text = '' OR schema_version=$3)
ORDER BY extracted_at DESC, extractor_version DESC LIMIT 1`,
		out.SourceRevisionID, out.ExtractorVersion, out.SchemaVersion).Scan(&out.ExtractorVersion,
		&out.SchemaVersion, &out.Coverage.Status, &out.Coverage.ReasonCode,
		&out.Coverage.ProcessedSections, &out.Coverage.TotalSections, &out.Coverage.ExtractedAt)
	if errors.Is(err, pgx.ErrNoRows) {
		// «Не проверено» — не то же самое, что «технологий нет».
		out.Coverage = extractdoc.Coverage{Status: "not_extracted"}
		return nil
	}
	if err != nil {
		return fmt.Errorf("read extraction coverage: %w", err)
	}
	return nil
}

func readRevisionMentions(ctx context.Context, tx pgx.Tx, in extractdoc.RevisionTechnologies,
) ([]extractdoc.Mention, error) {
	out := []extractdoc.Mention{}
	if in.Coverage.Status == "not_extracted" {
		return out, nil
	}
	rows, err := tx.Query(ctx, `SELECT id, technology_id, name, description, role, problem, mechanism,
inputs, outputs, extraction_status FROM technology_mentions
WHERE source_revision_id=$1 AND extractor_version=$2 AND schema_version=$3 ORDER BY created_at, id`,
		in.SourceRevisionID, in.ExtractorVersion, in.SchemaVersion)
	if err != nil {
		return nil, fmt.Errorf("read revision mentions: %w", err)
	}
	defer rows.Close()
	byID := map[string]int{}
	for rows.Next() {
		var m extractdoc.Mention
		if err = rows.Scan(&m.MentionID, &m.TechnologyID, &m.Name, &m.Description, &m.Role, &m.Problem,
			&m.Mechanism, &m.Inputs, &m.Outputs, &m.ExtractionStatus); err != nil {
			return nil, fmt.Errorf("scan revision mention: %w", err)
		}
		m.SourceRevisionID = in.SourceRevisionID
		m.Evidence = []extractdoc.Quote{}
		byID[m.MentionID] = len(out)
		out = append(out, m)
	}
	if err = rows.Err(); err != nil {
		return nil, fmt.Errorf("scan revision mentions: %w", err)
	}
	if err = attachQuotes(ctx, tx, out, byID); err != nil {
		return nil, err
	}
	return out, attachFieldEvidence(ctx, tx, out, byID)
}

func attachQuotes(ctx context.Context, tx pgx.Tx, mentions []extractdoc.Mention, byID map[string]int) error {
	if len(mentions) == 0 {
		return nil
	}
	rows, err := tx.Query(ctx, `SELECT mention_id, id, section, page, start_char, end_char, quote,
element_path, snapshot_sha256 FROM technology_evidence
WHERE mention_id = ANY($1::text[]) ORDER BY mention_id, start_char`, mentionIDs(mentions))
	if err != nil {
		return fmt.Errorf("read revision quotes: %w", err)
	}
	defer rows.Close()
	for rows.Next() {
		var mention string
		var quote extractdoc.Quote
		var page *int
		if err = rows.Scan(&mention, &quote.EvidenceID, &quote.Locator.Section, &page,
			&quote.Locator.StartChar, &quote.Locator.EndChar, &quote.Quote,
			&quote.Locator.ElementPath, &quote.Locator.SnapshotSHA256); err != nil {
			return fmt.Errorf("scan revision quote: %w", err)
		}
		if page != nil {
			quote.Locator.Page = *page
		}
		if i, ok := byID[mention]; ok {
			mentions[i].Evidence = append(mentions[i].Evidence, quote)
		}
	}
	if err = rows.Err(); err != nil {
		return fmt.Errorf("scan revision quotes: %w", err)
	}
	return nil
}

func attachFieldEvidence(ctx context.Context, tx pgx.Tx, mentions []extractdoc.Mention, byID map[string]int) error {
	if len(mentions) == 0 {
		return nil
	}
	rows, err := tx.Query(ctx, `SELECT mention_id, field, evidence_id FROM technology_field_evidence
WHERE mention_id = ANY($1::text[]) ORDER BY mention_id, field, evidence_id`, mentionIDs(mentions))
	if err != nil {
		return fmt.Errorf("read field evidence: %w", err)
	}
	defer rows.Close()
	for rows.Next() {
		var mention, field, evidence string
		if err = rows.Scan(&mention, &field, &evidence); err != nil {
			return fmt.Errorf("scan field evidence: %w", err)
		}
		i, ok := byID[mention]
		if !ok {
			continue
		}
		if mentions[i].FieldEvidence == nil {
			mentions[i].FieldEvidence = map[string][]string{}
		}
		mentions[i].FieldEvidence[field] = append(mentions[i].FieldEvidence[field], evidence)
	}
	if err = rows.Err(); err != nil {
		return fmt.Errorf("scan field evidence: %w", err)
	}
	return nil
}

func mentionIDs(mentions []extractdoc.Mention) []string {
	ids := make([]string, 0, len(mentions))
	for _, m := range mentions {
		ids = append(ids, m.MentionID)
	}
	return ids
}

// ReadTechnology считает только читаемые упоминания, чтобы закрытые не сдвигали ни одну цифру.
func (r *TechnologyRepo) ReadTechnology(ctx context.Context, in extractdoc.ReadTechnology,
) (extractdoc.Technology, error) {
	var out extractdoc.Technology
	if err := in.Validate(); err != nil {
		return out, err
	}
	shared := r.shared && in.SharedCorpus
	tx, err := r.db.Pool.BeginTx(ctx, pgx.TxOptions{IsoLevel: pgx.RepeatableRead, AccessMode: pgx.ReadOnly})
	if err != nil {
		return out, fmt.Errorf("begin technology projection: %w", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()
	if err = checkTechnologyAccess(ctx, tx, in.ReaderID, in.TechnologyID, shared); err != nil {
		return out, err
	}
	err = tx.QueryRow(ctx, `SELECT id, canonical_name, name_kind, description, validation_status, method_version
FROM technologies WHERE id=$1`, in.TechnologyID).Scan(&out.TechnologyID, &out.CanonicalName,
		&out.NameKind, &out.Description, &out.ValidationStatus, &out.MethodVersion)
	if errors.Is(err, pgx.ErrNoRows) {
		return out, extractdoc.ErrNotFound
	}
	if err != nil {
		return out, fmt.Errorf("read technology: %w", err)
	}
	if out.Aliases, err = readAliases(ctx, tx, in.TechnologyID); err != nil {
		return out, err
	}
	if err = projectMentions(ctx, tx, in, shared, &out); err != nil {
		return out, err
	}
	if err = tx.Commit(ctx); err != nil {
		return out, fmt.Errorf("commit technology projection: %w", err)
	}
	return out, nil
}

func readAliases(ctx context.Context, tx pgx.Tx, id string) ([]extractdoc.Alias, error) {
	rows, err := tx.Query(ctx, `SELECT value, language, basis FROM technology_aliases
WHERE technology_id=$1 ORDER BY language, value`, id)
	if err != nil {
		return nil, fmt.Errorf("read technology aliases: %w", err)
	}
	defer rows.Close()
	out := []extractdoc.Alias{}
	for rows.Next() {
		var alias extractdoc.Alias
		if err = rows.Scan(&alias.Value, &alias.Language, &alias.Basis); err != nil {
			return nil, fmt.Errorf("scan technology alias: %w", err)
		}
		out = append(out, alias)
	}
	if err = rows.Err(); err != nil {
		return nil, fmt.Errorf("scan technology aliases: %w", err)
	}
	return out, nil
}

// projectMentions отдаёт читаемые упоминания, а остальные только подсчитывает.
func projectMentions(ctx context.Context, tx pgx.Tx, in extractdoc.ReadTechnology,
	shared bool, out *extractdoc.Technology,
) error {
	rows, err := tx.Query(ctx, `SELECT m.source_revision_id, m.id, m.technology_id, m.name, m.description, m.role, m.problem,
m.mechanism, m.inputs, m.outputs, m.extraction_status, d.title,
coalesce(d.source_metadata->'publication'->>'value',''),
(d.owner_id=$2 OR coalesce(d.source_metadata->>'access_scope','') = 'public'
 OR ($3 AND coalesce(d.source_metadata->>'access_scope','') = 'shared')) AND NOT coalesce(h.revoked,false) AS readable
FROM technology_mentions m
JOIN documents d ON d.id = m.source_revision_id
LEFT JOIN source_index_heads h ON h.document_id = d.id
WHERE m.technology_id=$1 ORDER BY m.created_at, m.id`, in.TechnologyID, in.ReaderID, shared)
	if err != nil {
		return fmt.Errorf("read technology mentions: %w", err)
	}
	defer rows.Close()
	out.Mentions = []extractdoc.Mention{}
	byID := map[string]int{}
	readableRevisions, restrictedRevisions := map[string]bool{}, map[string]bool{}
	for rows.Next() {
		var m extractdoc.Mention
		var revision string
		var readable bool
		if err = rows.Scan(&revision, &m.MentionID, &m.TechnologyID, &m.Name, &m.Description, &m.Role, &m.Problem,
			&m.Mechanism, &m.Inputs, &m.Outputs, &m.ExtractionStatus, &m.SourceTitle, &m.SourcePublishedAt,
			&readable); err != nil {
			return fmt.Errorf("scan technology mention: %w", err)
		}
		if !readable {
			restrictedRevisions[revision] = true
			continue
		}
		readableRevisions[revision] = true
		m.SourceRevisionID = revision
		m.Evidence = []extractdoc.Quote{}
		byID[m.MentionID] = len(out.Mentions)
		out.Mentions = append(out.Mentions, m)
	}
	if err = rows.Err(); err != nil {
		return fmt.Errorf("scan technology mentions: %w", err)
	}
	out.ReadableRevisions, out.RestrictedRevisions = len(readableRevisions), len(restrictedRevisions)
	if err = attachQuotes(ctx, tx, out.Mentions, byID); err != nil {
		return err
	}
	return attachFieldEvidence(ctx, tx, out.Mentions, byID)
}
