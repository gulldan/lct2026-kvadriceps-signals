package amqp

import (
	"context"
	"fmt"

	"google.golang.org/protobuf/proto"

	"github.com/example/db-parser/internal/application"

	commonv1 "github.com/example/db-parser/internal/platform/genproto/common/v1"
)

func Handler(p *application.Processor) func(ctx context.Context, body []byte) error {
	return func(ctx context.Context, body []byte) error {
		var evt commonv1.DocumentUploaded
		if err := proto.Unmarshal(body, &evt); err != nil {
			return fmt.Errorf("unmarshal DocumentUploaded: %w", err)
		}
		return p.Process(ctx, &evt)
	}
}
