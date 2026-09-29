package postgres

import (
	"context"
	"errors"
	"fmt"

	"github.com/example/db-service/internal/domain"
	"github.com/example/db-service/internal/infrastructure/postgres/sqlcgen"
	"github.com/jackc/pgx/v5"
)

func (r *ClusterRepo) Head(ctx context.Context, ownerID string) (string, error) {
	var head string
	err := r.db.Pool.QueryRow(ctx, `SELECT coalesce(publication_id,'') FROM cluster_heads WHERE owner_id=$1`, ownerID).Scan(&head)
	if errors.Is(err, pgx.ErrNoRows) {
		return "", nil
	}
	if err != nil {
		return "", fmt.Errorf("read cluster head: %w", err)
	}
	return head, nil
}

// Publish сохраняет набор и указатель в одной транзакции. Старые строки нужны
// для ссылок гипотез; активный список выбирает только опубликованное поколение.
func (r *ClusterRepo) Publish(ctx context.Context, ownerID, requestID, expected, hash string,
	clusters []*domain.Cluster,
) ([]*domain.Cluster, error) {
	tx, err := r.db.Pool.Begin(ctx)
	if err != nil {
		return nil, fmt.Errorf("begin cluster publication: %w", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()
	if _, err = tx.Exec(ctx, `INSERT INTO cluster_heads(owner_id) VALUES($1) ON CONFLICT DO NOTHING`, ownerID); err != nil {
		return nil, fmt.Errorf("create cluster head: %w", err)
	}
	var head, savedHash string
	if err = tx.QueryRow(
		ctx,
		`SELECT coalesce(publication_id,'') FROM cluster_heads WHERE owner_id=$1 FOR UPDATE`,
		ownerID,
	).Scan(
		&head,
	); err != nil {
		return nil, fmt.Errorf("lock cluster head: %w", err)
	}
	err = tx.QueryRow(
		ctx,
		`SELECT request_hash FROM cluster_publications WHERE owner_id=$1 AND id=$2`,
		ownerID,
		requestID,
	).Scan(
		&savedHash,
	)
	if err == nil {
		if savedHash != hash {
			return nil, domain.ErrConflict
		}
		rows, readErr := r.q.WithTx(
			tx,
		).ListClusterPublication(
			ctx,
			sqlcgen.ListClusterPublicationParams{
				OwnerID:       ownerID,
				PublicationID: &requestID,
			},
		)
		if readErr != nil {
			return nil, fmt.Errorf("read previous cluster publication: %w", readErr)
		}
		out := make([]*domain.Cluster, 0, len(rows))
		for _, row := range rows {
			out = append(out, clusterFromRow(row))
		}
		return out, nil
	}
	if !errors.Is(err, pgx.ErrNoRows) {
		return nil, fmt.Errorf("read cluster request: %w", err)
	}
	if head != expected {
		return nil, domain.ErrConflict
	}
	if _, err = tx.Exec(
		ctx,
		`INSERT INTO cluster_publications(owner_id,id,request_hash) VALUES($1,$2,$3)`,
		ownerID,
		requestID,
		hash,
	); err != nil {
		return nil, fmt.Errorf("save cluster publication: %w", err)
	}
	rows := make([][]any, 0, len(clusters))
	for _, c := range clusters {
		rows = append(rows, []any{c.ID, ownerID, c.Label, c.Summary, orEmpty(c.Keywords), c.Method,
			c.ChunkCount, c.DocumentCount, jsonbOr(c.Representatives, "[]"), jsonbOr(c.Params, "{}"),
			c.Status, c.CreatedAt, c.UpdatedAt, requestID})
	}
	if _, err = tx.CopyFrom(ctx, pgx.Identifier{"clusters"}, []string{"id", "owner_id", "label", "summary", "keywords",
		"method", "chunk_count", "document_count", "representatives", "params", "status", "created_at",
		"updated_at", "publication_id"}, pgx.CopyFromRows(
		rows,
	)); err != nil {
		return nil, fmt.Errorf("copy cluster publication: %w", err)
	}
	if _, err = tx.Exec(ctx, `UPDATE cluster_heads SET publication_id=$2 WHERE owner_id=$1`, ownerID, requestID); err != nil {
		return nil, fmt.Errorf("advance cluster head: %w", err)
	}
	if err = tx.Commit(ctx); err != nil {
		return nil, fmt.Errorf("commit cluster publication: %w", err)
	}
	return clusters, nil
}
