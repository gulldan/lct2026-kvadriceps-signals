package application

import (
	"context"
	"errors"
	"fmt"
	commonv1 "github.com/example/main-service/internal/platform/genproto/common/v1"
	dbv1 "github.com/example/main-service/internal/platform/genproto/db/v1"
)

func (s *HypothesisService) walkHypotheses(ctx context.Context, request *dbv1.ListHypothesesRequest,
	visit func([]*commonv1.Hypothesis) (bool, error),
) error {
	request.Limit = 500
	request.OrderBy = "created_at"
	request.Compact = true
	for {
		if err := ctx.Err(); err != nil {
			return fmt.Errorf("scan cancelled: %w", err)
		}
		page, err := s.cat.ListHypotheses(ctx, request)
		if err != nil {
			return err
		}
		if len(page) == 0 {
			return nil
		}
		more, err := visit(page)
		if err != nil {
			return err
		}
		if !more || len(page) < int(request.GetLimit()) {
			return nil
		}
		cursor := page[len(page)-1].GetId()
		if cursor == "" || cursor == request.GetBeforeId() {
			return errors.New("hypothesis cursor did not advance")
		}
		request.BeforeId = cursor
	}
}
