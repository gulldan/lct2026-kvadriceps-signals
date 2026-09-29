// Command backfill проставляет заголовки статей уже проиндексированным документам: начало текста
// берётся из чанков через llm-service, оригиналы не перечитываются.
package main

import (
	"context"
	"errors"
	"fmt"
	"github.com/example/chunk-splitter/internal/platform/contracts"
	commonv1 "github.com/example/chunk-splitter/internal/platform/genproto/common/v1"
	"os"
	"os/signal"
	"strings"
	"syscall"

	"github.com/rs/zerolog"

	"github.com/example/chunk-splitter/internal/infrastructure/titler"
	"github.com/example/chunk-splitter/internal/platform/config"
	"github.com/example/chunk-splitter/internal/platform/dbclient"
	"github.com/example/chunk-splitter/internal/platform/grpcx"
	"github.com/example/chunk-splitter/internal/platform/logger"

	llmv1 "github.com/example/chunk-splitter/internal/platform/genproto/llm/v1"
)

// Заголовок стоит в самом начале, поэтому экстрактору хватает первых символов.
const openingChars = 4000

func main() {
	log := logger.New("backfill-titles", config.Get("LOG_LEVEL", "info"))
	if err := run(log); err != nil {
		log.Error().Err(err).Msg("backfill stopped with error")
		os.Exit(1)
	}
}

func run(log zerolog.Logger) error {
	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()

	if config.Get("VLLM_URL", "") == "" {
		return errors.New("VLLM_URL is required: title extraction needs a generation backend")
	}

	db, err := dbclient.New(config.MustGet("DB_SERVICE_ADDR"))
	if err != nil {
		return err
	}
	defer func() { _ = db.Close() }()

	conn, err := grpcx.Dial(config.MustGet("LLM_SERVICE_ADDR"))
	if err != nil {
		return err
	}
	defer func() { _ = conn.Close() }()
	rag := llmv1.NewRagServiceClient(conn)

	return backfillTitles(ctx, log, db, rag, titler.New(titler.Generator(), nil))
}

func backfillTitles(
	ctx context.Context, log zerolog.Logger, db *dbclient.Client,
	rag llmv1.RagServiceClient, tl *titler.Titler,
) error {
	var scanned, titled, skipped, total int
	err := dbclient.WalkDocuments(ctx, db, "", contracts.DocumentScan{}, func(docs []*commonv1.Document) (bool, error) {
		total += len(docs)
		for _, d := range docs {
			if contextErr := ctx.Err(); contextErr != nil {
				return false, fmt.Errorf("backfill cancelled: %w", contextErr)
			}
			if strings.TrimSpace(d.GetTitle()) != "" {
				continue
			}
			scanned++
			if backfillTitle(ctx, log, db, rag, tl, d) {
				titled++
			} else {
				skipped++
			}
		}
		return true, nil
	})
	if err != nil {
		return err
	}

	log.Info().
		Int("total", total).Int("scanned", scanned).Int("titled", titled).Int("skipped", skipped).
		Msg("backfill complete")
	return nil
}

func openingText(ctx context.Context, rag llmv1.RagServiceClient, docID, ownerID string) (string, error) {
	resp, err := rag.DocumentChunks(ctx, &llmv1.DocumentChunksRequest{DocumentId: docID, OwnerId: ownerID})
	if err != nil {
		return "", err
	}
	var b strings.Builder
	for _, c := range resp.GetChunks() {
		if b.Len() >= openingChars {
			break
		}
		b.WriteString(c.GetText())
		b.WriteByte('\n')
	}
	return b.String(), nil
}

func backfillTitle(ctx context.Context, log zerolog.Logger, db *dbclient.Client,
	rag llmv1.RagServiceClient, tl *titler.Titler, d *commonv1.Document,
) bool {
	text, terr := openingText(ctx, rag, d.GetId(), d.GetOwnerId())
	if terr != nil {
		log.Warn().Err(terr).Str("document_id", d.GetId()).Msg("read chunks failed")
		return false
	}
	title, kind := tl.Extract(ctx, text)
	if kind != "" {
		if kerr := db.SetDocumentKind(ctx, d.GetId(), kind); kerr != nil {
			log.Warn().Err(kerr).Str("document_id", d.GetId()).Msg("set kind failed")
		}
	}
	if title == "" {
		log.Info().Str("document_id", d.GetId()).Str("filename", d.GetFilename()).Msg("no title extracted; left empty")
		return false
	}
	if serr := db.SetDocumentTitle(ctx, d.GetId(), title); serr != nil {
		log.Warn().Err(serr).Str("document_id", d.GetId()).Msg("set title failed")
		return false
	}
	log.Info().Str("document_id", d.GetId()).Str("title", title).Msg("title set")
	return true
}
