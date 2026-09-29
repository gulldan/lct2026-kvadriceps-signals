package postgres

import (
	"context"
	"encoding/json/v2"
	"errors"
	"fmt"

	"github.com/example/db-service/internal/platform/extractdoc"
	"github.com/jackc/pgx/v5"
)

// Review пишет правку новой версией с прежним значением, а не на месте.
func (r *TechnologyRepo) Review(ctx context.Context, in extractdoc.Review) (extractdoc.Review, error) {
	if err := in.Validate(); err != nil {
		return extractdoc.Review{}, err
	}
	tx, err := r.db.Pool.Begin(ctx)
	if err != nil {
		return extractdoc.Review{}, fmt.Errorf("begin review: %w", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()
	if err = checkTechnologyAccess(ctx, tx, in.AuthorID, in.TechnologyID, r.shared); err != nil {
		return extractdoc.Review{}, err
	}
	// Правки одной технологии, включая обратные, идут строго по очереди.
	if _, err = tx.Exec(ctx, `SELECT id FROM technologies WHERE id=$1 FOR UPDATE`, in.TechnologyID); err != nil {
		return extractdoc.Review{}, fmt.Errorf("lock reviewed technology: %w", err)
	}
	in, effect, err := reviewEffect(ctx, tx, in)
	if err != nil {
		return extractdoc.Review{}, err
	}
	if err = r.checkMergeAccess(ctx, tx, effect); err != nil {
		return extractdoc.Review{}, err
	}
	previous, err := json.Marshal(in.PreviousValue)
	if err != nil {
		return extractdoc.Review{}, fmt.Errorf("encode previous value: %w", err)
	}
	next, err := json.Marshal(in.NewValue)
	if err != nil {
		return extractdoc.Review{}, fmt.Errorf("encode new value: %w", err)
	}
	var revertsID *string
	if in.RevertsID != "" {
		revertsID = &in.RevertsID
	}
	// Повтор той же правки — та же правка: при конфликте вставки возвращается уже записанная версия.
	stored, err := scanReview(tx.QueryRow(ctx, `INSERT INTO technology_reviews
(id, technology_id, author_id, operation, reason, previous_value, new_value, fingerprint, reverts_id)
VALUES($1,$2,$3,$4,$5,$6,$7,$8,$9)
ON CONFLICT (technology_id, fingerprint) WHERE fingerprint <> ''
DO NOTHING
RETURNING id, technology_id, author_id, operation, reason, previous_value::text, new_value::text,
coalesce(reverts_id,''), created_at`,
		in.ID, in.TechnologyID, in.AuthorID, in.Operation, in.Reason, previous, next,
		in.Fingerprint(), revertsID))
	if errors.Is(err, pgx.ErrNoRows) {
		// Повтор старого запроса не должен накатываться поверх более новой правки.
		return scanReview(tx.QueryRow(ctx, `SELECT id, technology_id, author_id, operation, reason,
previous_value::text, new_value::text, coalesce(reverts_id,''), created_at
FROM technology_reviews WHERE technology_id=$1 AND fingerprint=$2`, in.TechnologyID, in.Fingerprint()))
	}
	if err != nil {
		return extractdoc.Review{}, err
	}
	if in.RevertsID != "" {
		var alreadyReverted bool
		if err = tx.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM technology_reviews WHERE reverts_id=$1 AND id<>$2)`,
			in.RevertsID, stored.ID).Scan(&alreadyReverted); err != nil {
			return extractdoc.Review{}, fmt.Errorf("check prior undo: %w", err)
		}
		if alreadyReverted {
			return extractdoc.Review{}, extractdoc.ErrConflict
		}
	}
	if err = applyReview(ctx, tx, effect); err != nil {
		return extractdoc.Review{}, err
	}
	if err = tx.Commit(ctx); err != nil {
		return extractdoc.Review{}, fmt.Errorf("commit review: %w", err)
	}
	return stored, nil
}

func (r *TechnologyRepo) checkMergeAccess(ctx context.Context, tx pgx.Tx, effect extractdoc.Review) error {
	if effect.Operation != extractdoc.OpMerge {
		return nil
	}
	into := stringValue(effect.NewValue, "same_as")
	if into == "" {
		into = stringValue(effect.PreviousValue, "same_as")
	}
	return checkTechnologyAccess(ctx, tx, effect.AuthorID, into, r.shared)
}

// reviewEffect восстанавливает отмену из сохранённой операции, а не из значений вызывающего.
func reviewEffect(ctx context.Context, tx pgx.Tx, in extractdoc.Review) (extractdoc.Review, extractdoc.Review, error) {
	if in.Operation != extractdoc.OpRevert {
		return in, in, nil
	}
	original, err := scanReview(tx.QueryRow(ctx, `SELECT id, technology_id, author_id, operation, reason,
previous_value::text, new_value::text, coalesce(reverts_id,''), created_at
FROM technology_reviews WHERE id=$1 AND technology_id=$2`, in.RevertsID, in.TechnologyID))
	if errors.Is(err, pgx.ErrNoRows) || original.Operation == extractdoc.OpRevert {
		return in, in, extractdoc.ErrInvalid
	}
	if err != nil {
		return in, in, err
	}
	in.PreviousValue, in.NewValue = original.NewValue, original.PreviousValue
	effect := in
	effect.Operation = original.Operation
	return in, effect, nil
}

func (r *TechnologyRepo) Reviews(ctx context.Context, in extractdoc.ReadReviews) ([]extractdoc.Review, error) {
	if in.ReaderID == "" || in.TechnologyID == "" {
		return nil, extractdoc.ErrInvalid
	}
	tx, err := r.db.Pool.BeginTx(ctx, pgx.TxOptions{IsoLevel: pgx.RepeatableRead, AccessMode: pgx.ReadOnly})
	if err != nil {
		return nil, fmt.Errorf("begin review history: %w", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()
	if err = checkTechnologyAccess(ctx, tx, in.ReaderID, in.TechnologyID, r.shared && in.SharedCorpus); err != nil {
		return nil, err
	}
	rows, err := tx.Query(ctx, `SELECT id, technology_id, author_id, operation, reason,
previous_value::text, new_value::text, coalesce(reverts_id,''), created_at
FROM technology_reviews WHERE technology_id=$1 ORDER BY created_at DESC, id`, in.TechnologyID)
	if err != nil {
		return nil, fmt.Errorf("read review history: %w", err)
	}
	defer rows.Close()
	out := []extractdoc.Review{}
	for rows.Next() {
		review, scanErr := scanReview(rows)
		if scanErr != nil {
			return nil, scanErr
		}
		out = append(out, review)
	}
	if err = rows.Err(); err != nil {
		return nil, fmt.Errorf("scan review history: %w", err)
	}
	if err = tx.Commit(ctx); err != nil {
		return nil, fmt.Errorf("commit review history: %w", err)
	}
	return out, nil
}

func scanReview(row pgx.Row) (extractdoc.Review, error) {
	var review extractdoc.Review
	var previous, next []byte
	if err := row.Scan(&review.ID, &review.TechnologyID, &review.AuthorID, &review.Operation,
		&review.Reason, &previous, &next, &review.RevertsID, &review.CreatedAt); err != nil {
		return review, fmt.Errorf("read review row: %w", err)
	}
	if err := json.Unmarshal(previous, &review.PreviousValue); err != nil {
		return review, fmt.Errorf("decode previous value: %w", err)
	}
	if err := json.Unmarshal(next, &review.NewValue); err != nil {
		return review, fmt.Errorf("decode new value: %w", err)
	}
	return review, nil
}

// checkTechnologyAccess пускает читателя хотя бы одной ревизии, где упомянута технология.
func checkTechnologyAccess(ctx context.Context, tx pgx.Tx, reader, technology string, shared bool) error {
	var readable bool
	err := tx.QueryRow(ctx, `SELECT EXISTS(
SELECT 1 FROM technology_mentions m JOIN documents d ON d.id = m.source_revision_id
LEFT JOIN source_index_heads h ON h.document_id = d.id
WHERE m.technology_id=$1 AND NOT coalesce(h.revoked,false)
  AND (d.owner_id=$2
       OR coalesce(d.source_metadata->>'access_scope','') = 'public'
       OR ($3 AND coalesce(d.source_metadata->>'access_scope','') = 'shared')))`,
		technology, reader, shared).Scan(&readable)
	if err != nil {
		return fmt.Errorf("check technology access: %w", err)
	}
	if !readable {
		return extractdoc.ErrNotFound
	}
	return nil
}

// applyReview при слиянии сохраняет оба идентификатора, чтобы привязанные доказательства не потерялись.
func applyReview(ctx context.Context, tx pgx.Tx, review extractdoc.Review) error {
	switch review.Operation {
	case extractdoc.OpRename:
		return updateTechnology(ctx, tx, `UPDATE technologies SET canonical_name=$2, updated_at=now()
WHERE id=$1 AND canonical_name=$3`,
			review.TechnologyID, stringValue(review.NewValue, "canonical_name"), stringValue(review.PreviousValue, "canonical_name"))
	case extractdoc.OpDescribe:
		return updateTechnology(ctx, tx, `UPDATE technologies SET description=$2, updated_at=now() WHERE id=$1 AND description=$3`,
			review.TechnologyID, stringValue(review.NewValue, "description"), stringValue(review.PreviousValue, "description"))
	case extractdoc.OpConfirm:
		return updateTechnology(ctx, tx, `UPDATE technologies SET validation_status=$2, updated_at=now()
WHERE id=$1 AND validation_status=$3`,
			review.TechnologyID, stringValue(review.NewValue, "validation_status"), stringValue(review.PreviousValue, "validation_status"))
	case extractdoc.OpMerge:
		return mergeTechnologies(ctx, tx, review)
	default:
		// Остальные операции — решения для следующего прогона; на месте ничего не меняется.
		return nil
	}
}

func updateTechnology(ctx context.Context, tx pgx.Tx, statement, id, value, previous string) error {
	if value == "" {
		return extractdoc.ErrInvalid
	}
	changed, err := tx.Exec(ctx, statement, id, value, previous)
	if err != nil {
		return fmt.Errorf("apply review: %w", err)
	}
	if changed.RowsAffected() != 1 {
		return extractdoc.ErrConflict
	}
	return nil
}

func mergeTechnologies(ctx context.Context, tx pgx.Tx, review extractdoc.Review) error {
	into := stringValue(review.NewValue, "same_as")
	undo := review.RevertsID != ""
	if undo {
		into = stringValue(review.PreviousValue, "same_as")
	}
	if into == "" || into == review.TechnologyID {
		return extractdoc.ErrInvalid
	}
	first, second := review.TechnologyID, into
	if first > second {
		first, second = second, first
	}
	if undo {
		changed, err := tx.Exec(ctx, `DELETE FROM technology_relations
WHERE technology_id=$1 AND related_technology_id=$2 AND relation='same_as'
AND decision_version IN ('expert-review-v1',$3)`, first, second, "expert-review-v1:"+review.RevertsID)
		if err != nil {
			return fmt.Errorf("revert merge: %w", err)
		}
		if changed.RowsAffected() != 1 {
			return extractdoc.ErrConflict
		}
		return nil
	}
	changed, err := tx.Exec(ctx, `INSERT INTO technology_relations
(technology_id, related_technology_id, relation, basis, decision_version)
VALUES($1,$2,'same_as',$3,$4)
ON CONFLICT (technology_id, related_technology_id, relation) DO NOTHING`,
		first, second, "expert review: "+review.Reason, "expert-review-v1:"+review.ID)
	if err != nil {
		return fmt.Errorf("merge technologies: %w", err)
	}
	if changed.RowsAffected() != 1 {
		return extractdoc.ErrConflict
	}
	return nil
}

func stringValue(value map[string]any, key string) string {
	if text, ok := value[key].(string); ok {
		return text
	}
	return ""
}
