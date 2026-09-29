package application

import (
	"context"
	"errors"
	"fmt"
	"slices"

	"github.com/example/db-service/internal/domain"
)

// SeedAutomation не переиспользует административную учётную запись для фоновых вычислений.
func (s *Service) SeedAutomation(ctx context.Context, username, passwordHash string) error {
	existing, err := s.users.GetByUsername(ctx, username)
	if err == nil {
		if !slices.Contains(existing.Roles, "automation") || slices.Contains(existing.Roles, "admin") {
			return errors.New("automation username belongs to a different role")
		}
		return nil
	}
	if !errors.Is(err, domain.ErrNotFound) {
		return err
	}
	_, err = s.CreateUser(ctx, username, passwordHash, []string{"operator", "automation"})
	if err != nil {
		return fmt.Errorf("seed automation identity: %w", err)
	}
	return nil
}
