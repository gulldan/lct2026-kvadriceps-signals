package postgres

import (
	"context"
	"fmt"

	"github.com/example/db-service/internal/domain"
	"github.com/example/db-service/internal/infrastructure/postgres/sqlcgen"
	"github.com/jackc/pgx/v5"
)

// Проверка передаёт полный актуальный набор: старые позиции и выбывшие фрагменты не должны
// пережить новый вердикт. Изменения свидетельств и самой гипотезы фиксируются одной транзакцией.
func replaceVerifiedEvidence(
	ctx context.Context, tx pgx.Tx, qtx *sqlcgen.Queries, h *domain.Hypothesis,
) ([]*domain.Evidence, error) {
	kept := make([]string, 0, len(h.Evidence))
	seen := make(map[string]bool, len(h.Evidence))
	for _, e := range h.Evidence {
		if e == nil || e.HypothesisID != "" && e.HypothesisID != h.ID {
			return nil, domain.ErrInvalidArgument
		}
		if e.ID == "" {
			continue
		}
		if seen[e.ID] {
			return nil, domain.ErrInvalidArgument
		}
		seen[e.ID] = true
		tag, err := tx.Exec(ctx, `UPDATE hypothesis_evidence
 SET document_id=$3,chunk_id=$4,filename=$5,snippet=$6,stance=$7,score=$8,ord=$9,relation=$10,
 page_start=$11,page_end=$12,section_heading=$13,origin=$14 WHERE id=$1 AND hypothesis_id=$2`,
			e.ID, h.ID, e.DocumentID, e.ChunkID, e.Filename, e.Snippet, e.Stance, e.Score, e.Ord, e.Relation,
			e.PageStart, e.PageEnd, e.SectionHeading, e.Origin)
		if err != nil {
			return nil, fmt.Errorf("update verified evidence: %w", err)
		}
		if tag.RowsAffected() != 1 {
			return nil, domain.ErrInvalidArgument
		}
		kept = append(kept, e.ID)
	}
	if _, err := tx.Exec(ctx, `DELETE FROM hypothesis_evidence WHERE hypothesis_id=$1 AND NOT (id=ANY($2::text[]))`,
		h.ID, kept); err != nil {
		return nil, fmt.Errorf("remove obsolete verified evidence: %w", err)
	}
	return insertNewEvidence(ctx, qtx, h)
}
