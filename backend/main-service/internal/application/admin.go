package application

import (
	"context"

	"github.com/example/main-service/internal/domain"

	commonv1 "github.com/example/main-service/internal/platform/genproto/common/v1"
)

type AdminService struct {
	users domain.UserCatalog
}

func NewAdminService(users domain.UserCatalog) *AdminService {
	return &AdminService{users: users}
}

// ListUsers отдаёт все учётные записи; хэши паролей в них не заполняются.
func (s *AdminService) ListUsers(ctx context.Context) ([]*commonv1.User, error) {
	return s.users.ListUsers(ctx)
}
