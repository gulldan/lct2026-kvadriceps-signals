package application

import (
	"context"
	"encoding/json/v2"
	"fmt"
	"strings"
	"uuid"

	"github.com/example/db-service/internal/domain"
	"github.com/example/db-service/internal/platform/indexdoc"
)

func (s *Service) ClusterHead(ctx context.Context, ownerID string) (string, error) {
	if strings.TrimSpace(ownerID) == "" {
		return "", domain.ErrInvalidArgument
	}
	return s.clusters.Head(ctx, ownerID)
}

// PublishClusters проверяет неизменяемое намерение перед атомарной публикацией набора.
func (s *Service) PublishClusters(ctx context.Context, ownerID, requestID, expected string,
	clusters []*domain.Cluster,
) ([]*domain.Cluster, error) {
	// Корпус из 100 тысяч документов уже даёт более 2000 тем; общий размер по-прежнему ограничен 32 МиБ.
	if _, err := uuid.Parse(requestID); err != nil || strings.TrimSpace(ownerID) == "" || len(clusters) > 5000 {
		return nil, domain.ErrInvalidArgument
	}
	if expected != "" {
		if _, err := uuid.Parse(expected); err != nil {
			return nil, domain.ErrInvalidArgument
		}
	}
	for _, c := range clusters {
		if c == nil || strings.TrimSpace(c.Label) == "" || len(c.Label) > 1024 || c.ChunkCount < 0 || c.DocumentCount < 0 {
			return nil, domain.ErrInvalidArgument
		}
		c.OwnerID = ownerID
	}
	// Хеш входа вычисляется до присвоения серверных идентификаторов и времени.
	raw, err := json.Marshal(clusters, json.Deterministic(true))
	if err != nil || len(raw) > 32<<20 {
		return nil, fmt.Errorf("invalid cluster publication: %w", domain.ErrInvalidArgument)
	}
	hash := indexdoc.Hash(append([]byte(expected+"\n"), raw...))
	for _, c := range clusters {
		c.ID, c.Status = uuid.NewV4().String(), domain.StatusActive
		c.CreatedAt, c.UpdatedAt = now(), now()
	}
	return s.clusters.Publish(ctx, ownerID, requestID, expected, hash, clusters)
}
