package dbclient

import (
	"context"
	"errors"
	"fmt"
	"github.com/example/rag-mvp/pkg/contracts"
	commonv1 "github.com/example/rag-mvp/pkg/genproto/common/v1"
)

type DocumentLister interface {
	ListDocuments(context.Context, string, ...contracts.DocumentScan) ([]*commonv1.Document, error)
}

// WalkDocuments: false из visit останавливает обход.
func WalkDocuments(ctx context.Context, catalog DocumentLister, owner string, scan contracts.DocumentScan,
	visit func([]*commonv1.Document) (bool, error),
) error {
	scan.Limit = 500
	for {
		if err := ctx.Err(); err != nil {
			return fmt.Errorf("scan cancelled: %w", err)
		}
		docs, err := catalog.ListDocuments(ctx, owner, scan)
		if err != nil {
			return err
		}
		if len(docs) == 0 {
			return nil
		}
		more, err := visit(docs)
		if err != nil {
			return err
		}
		if !more || len(docs) < scan.Limit {
			return nil
		}
		cursor := docs[len(docs)-1].GetId()
		if cursor == "" || cursor == scan.BeforeID {
			return errors.New("document scan cursor did not advance")
		}
		scan.BeforeID = cursor
	}
}
