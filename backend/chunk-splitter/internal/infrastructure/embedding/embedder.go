package embedding

import (
	"context"

	"github.com/example/chunk-splitter/internal/platform/aiclients"
)

type Adapter struct {
	client aiclients.Embedder
}

func New(client aiclients.Embedder) *Adapter {
	return &Adapter{client: client}
}

func (a *Adapter) Embed(ctx context.Context, texts []string) ([][]float32, error) {
	return a.client.Embed(ctx, texts)
}
