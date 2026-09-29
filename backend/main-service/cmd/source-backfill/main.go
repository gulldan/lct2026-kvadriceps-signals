// Command source-backfill переиндексирует явно зафиксированные ревизии документов.
package main

import (
	"context"
	"encoding/json/v2"
	"errors"
	"flag"
	"fmt"
	"os"
	"time"
	"uuid"

	"github.com/example/main-service/internal/application"
	"github.com/example/main-service/internal/platform/config"
	"github.com/example/main-service/internal/platform/dbclient"
	"github.com/example/main-service/internal/platform/sourcedoc"
	"github.com/example/main-service/internal/platform/storage"
)

type entry struct {
	DocumentID  string `json:"document_id"`
	Revision    string `json:"revision"`
	ContentHash string `json:"content_hash"`
}
type manifest struct {
	OwnerID   string  `json:"owner_id"`
	Documents []entry `json:"documents"`
}

func main() {
	filename := flag.String("manifest", "", "frozen document revisions (maximum 300)")
	force := flag.Bool("force", false, "also queue revisions with a ready index")
	flag.Parse()
	if err := run(*filename, *force); err != nil {
		_, _ = fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}

func run(filename string, force bool) error {
	raw, err := os.ReadFile(filename) // #nosec G304 -- operator explicitly selects the frozen local manifest.
	if err != nil {
		return fmt.Errorf("read source manifest: %w", err)
	}
	var m manifest
	if err = json.Unmarshal(raw, &m, json.RejectUnknownMembers(true)); err != nil {
		return fmt.Errorf("decode source manifest: %w", err)
	}
	if _, err = uuid.Parse(m.OwnerID); err != nil || len(m.Documents) < 1 || len(m.Documents) > 300 {
		return errors.New("invalid bounded source manifest")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Minute)
	defer cancel()
	db, err := dbclient.New(config.MustGet("DB_SERVICE_ADDR"))
	if err != nil {
		return err
	}
	defer func() { _ = db.Close() }()
	if err = validateDocuments(ctx, db, m); err != nil {
		return err
	}
	objects, err := storage.New(ctx, storage.ConfigFromEnv())
	if err != nil {
		return err
	}
	svc := application.NewIngestionService(objects, db, nil)
	svc.EnableSourceReads(db, config.GetBool("RAG_SHARED_CORPUS", true), true)
	for _, e := range m.Documents {
		outcome, queueErr := svc.ReindexSource(ctx, m.OwnerID, e.DocumentID, force)
		if queueErr != nil {
			return fmt.Errorf("source %s: %w", e.DocumentID, queueErr)
		}
		_, _ = fmt.Fprintf(os.Stdout, "%s %s\n", e.DocumentID, outcome)
	}
	return nil
}

func validateDocuments(ctx context.Context, db *dbclient.Client, m manifest) error {
	seen := map[string]bool{}
	for _, e := range m.Documents {
		if _, err := uuid.Parse(e.DocumentID); err != nil || seen[e.DocumentID] {
			return errors.New("invalid or repeated document ID")
		}
		seen[e.DocumentID] = true
		doc, err := db.GetDocument(ctx, e.DocumentID)
		if err != nil {
			return err
		}
		if doc.GetOwnerId() != m.OwnerID {
			return fmt.Errorf("document %s owner differs", e.DocumentID)
		}
		if doc.GetSourceMetadata() == "" {
			if e.ContentHash == "" || e.ContentHash != doc.GetContentHash() || e.Revision != e.ContentHash {
				return fmt.Errorf("document %s differs from frozen manifest", e.DocumentID)
			}
			continue
		}
		source, err := sourcedoc.Decode([]byte(doc.GetSourceMetadata()))
		if err != nil || doc.GetOwnerId() != m.OwnerID || source.TestRunID != "" ||
			source.Revision != e.Revision || source.ContentHash != e.ContentHash {
			return fmt.Errorf("source %s differs from frozen manifest", e.DocumentID)
		}
	}
	return nil
}
