package application

import (
	"context"
	"errors"
	"fmt"
	"io"
	"log/slog"

	"github.com/example/main-service/internal/domain"
	"github.com/example/main-service/internal/platform/analysisdoc"
	"github.com/example/main-service/internal/platform/indexdoc"
)

type AnalysisInputs interface {
	FreezeAnalysis(context.Context, analysisdoc.Request) (analysisdoc.Frozen, error)
	ReadAnalysis(context.Context, analysisdoc.Read) (analysisdoc.Frozen, error)
}

// FreezeAnalysis проверяет опубликованный объект и повторно проверяет доступ.
// Большой вход читается из хранилища, а не из вложенной строки в RPC.
func FreezeAnalysis(ctx context.Context, db AnalysisInputs, store domain.ObjectStore,
	request analysisdoc.Request,
) (analysisdoc.Frozen, error) {
	frozen, err := db.FreezeAnalysis(ctx, request)
	if err != nil {
		return analysisdoc.Frozen{}, err
	}
	if frozen.Validate() != nil || frozen.ReaderID != request.OwnerID || frozen.SharedCorpus != request.SharedCorpus {
		return analysisdoc.Frozen{}, errors.New("invalid frozen analysis artifact")
	}
	stream, err := store.Get(ctx, frozen.Artifact)
	if err != nil {
		return analysisdoc.Frozen{}, fmt.Errorf("confirm analysis manifest: %w", err)
	}
	defer func() { _ = stream.Close() }()
	raw, err := io.ReadAll(io.LimitReader(stream, int64(frozen.Bytes)+1))
	if err != nil || len(raw) != frozen.Bytes || indexdoc.Hash(raw) != frozen.SHA256 {
		return analysisdoc.Frozen{}, errors.New("analysis manifest storage verification failed")
	}
	input, err := analysisdoc.Decode[analysisdoc.Input](string(raw))
	if err != nil || input.Version != analysisdoc.Version {
		return analysisdoc.Frozen{}, errors.New("obsolete analysis input; create a new request")
	}
	slog.InfoContext(ctx, "analysis input frozen", "run", frozen.RunID, "bytes", len(raw),
		"records", len(input.Records), "pages", len(input.Pages), "excluded", len(input.Excluded))
	current, err := db.ReadAnalysis(ctx, analysisdoc.Read{RunID: frozen.RunID, ReaderID: request.OwnerID,
		SharedCorpus: request.SharedCorpus})
	if err != nil {
		return analysisdoc.Frozen{}, err
	}
	if current.SHA256 != frozen.SHA256 || current.Artifact != frozen.Artifact || current.Bytes != frozen.Bytes {
		return analysisdoc.Frozen{}, errors.New("frozen analysis changed during publication")
	}
	frozen.JSON = string(raw)
	return frozen, nil
}
