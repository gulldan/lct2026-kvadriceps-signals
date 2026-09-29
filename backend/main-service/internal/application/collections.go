package application

import (
	"context"

	"github.com/example/main-service/internal/platform/extractdoc"
	"github.com/example/main-service/internal/platform/harvest"
)

// Collections только заказывает сбор и читает прогресс: сам сбор идёт в сборщике, не внутри HTTP-запроса.
type Collections interface {
	Request(context.Context, harvest.Request) (harvest.Collection, error)
	Progress(context.Context, harvest.Progress) (harvest.Collection, error)
}

type Technologies interface {
	ReadRevision(context.Context, extractdoc.ReadRevision) (extractdoc.RevisionTechnologies, error)
	ReadTechnology(context.Context, extractdoc.ReadTechnology) (extractdoc.Technology, error)
	Search(context.Context, extractdoc.SearchTechnologies) (extractdoc.TechnologyPage, error)
	Review(context.Context, extractdoc.Review) (extractdoc.Review, error)
	Reviews(context.Context, extractdoc.ReadReviews) ([]extractdoc.Review, error)
}
