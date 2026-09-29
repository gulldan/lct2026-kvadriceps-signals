package domain

import "context"

type ImageDescriber interface {
	Describe(ctx context.Context, data []byte, mime string) (string, error)
}
