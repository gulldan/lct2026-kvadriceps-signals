package postgres_test

import (
	"context"
	"encoding/json/v2"
	"errors"
	"os"
	"strings"
	"sync"
	"testing"
	"time"
	"uuid"

	"github.com/example/db-service/internal/domain"
	"github.com/example/db-service/internal/infrastructure/postgres"
	dbv1 "github.com/example/db-service/internal/platform/genproto/db/v1"
	"github.com/example/db-service/internal/platform/sourcedoc"
	"github.com/rs/zerolog"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/credentials/insecure"
	"google.golang.org/grpc/status"
)

// Нужен явный манифест; на общей базе трогает только свои ID.
func TestDocumentSourceLive(t *testing.T) {
	manifestFile := os.Getenv("TR_TEST_MANIFEST")
	if manifestFile == "" {
		t.Skip("TR_TEST_MANIFEST required for bounded live test")
	}
	raw, readErr := os.ReadFile(manifestFile)
	if readErr != nil {
		t.Fatal(readErr)
	}
	var m struct {
		Version int      `json:"version"`
		RunID   string   `json:"run_id"`
		OwnerID string   `json:"owner_id"`
		Prefix  string   `json:"object_prefix"`
		IDs     []string `json:"document_ids"`
	}
	if err := json.Unmarshal(raw, &m, json.RejectUnknownMembers(true)); err != nil {
		t.Fatal(err)
	}
	if m.Version != 1 || !strings.HasPrefix(m.RunID, "tr-test-") || m.OwnerID != m.RunID ||
		m.Prefix != "tests/"+m.RunID+"/" || len(m.IDs) != 3 {
		t.Fatal("invalid test manifest")
	}
	if _, err := uuid.Parse(strings.TrimPrefix(m.RunID, "tr-test-")); err != nil {
		t.Fatal("invalid run UUID")
	}
	seen := map[string]bool{}
	for _, id := range m.IDs {
		if _, err := uuid.Parse(id); err != nil || seen[id] {
			t.Fatal("invalid/duplicate record ID")
		}
		seen[id] = true
	}
	dsn := os.Getenv("DB_TEST_DSN")
	if dsn == "" {
		dsn = os.Getenv("POSTGRES_DSN")
	}
	if dsn == "" {
		t.Fatal("DB_TEST_DSN or POSTGRES_DSN required")
	}
	db, connectErr := postgres.Connect(t.Context(), dsn, zerolog.Nop())
	if connectErr != nil {
		t.Fatal("connect to existing PostgreSQL failed")
	}
	t.Cleanup(db.Close)
	// Миграции применяет выкладка; тесты схему неявно не меняют.
	var n int
	err := db.Pool.QueryRow(t.Context(), "SELECT count(*) FROM documents WHERE id = ANY($1)", m.IDs).Scan(&n)
	if err != nil || n != 0 {
		t.Fatalf("test IDs already exist: count=%d error=%v", n, err)
	}
	t.Cleanup(func() {
		ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		_, err := db.Pool.Exec(ctx, `DELETE FROM documents WHERE id = ANY($1) AND owner_id=$2
AND object_key LIKE $3 AND status='uploaded' AND chunk_count=0`, m.IDs, m.OwnerID, m.Prefix+"%")
		if err != nil {
			t.Error("test cleanup failed; use saved manifest")
		}
	})
	repo := postgres.NewDocumentRepo(db)
	for _, id := range m.IDs {
		if err := repo.Create(t.Context(), &domain.Document{ID: id, OwnerID: m.OwnerID,
			ObjectKey: m.Prefix + id, Status: "uploaded",
			CreatedAt: time.Now().UTC(), UpdatedAt: time.Now().UTC()}); err != nil {
			t.Fatal(err)
		}
	}
	legacy, legacyErr := repo.Get(t.Context(), m.IDs[0])
	if legacyErr != nil || len(legacy.SourceMetadata) != 0 {
		t.Fatalf("legacy read failed: %v", legacyErr)
	}
	s := sourcedoc.Source{Version: 1, Kind: "paper", Provider: "manual", NativeID: "native-1",
		CanonicalID: "work-1", URL: "https://example.org/paper",
		Publication: &sourcedoc.Date{Value: "2025", Precision: "year", Basis: "fixture cover"},
		ObservedAt:  time.Now().UTC(), Revision: "v1", ContentHash: strings.Repeat("a", 64),
		HashAlgorithm: "sha256", AccessScope: "owner", TestRunID: m.RunID}
	metadata, marshalErr := json.Marshal(s)
	if marshalErr != nil {
		t.Fatal(marshalErr)
	}
	if err := repo.SetSource(t.Context(), m.IDs[0], "foreign", metadata); !errors.Is(err, domain.ErrNotFound) {
		t.Fatalf("foreign owner: %v", err)
	}
	for range 2 {
		if err := repo.SetSource(t.Context(), m.IDs[0], m.OwnerID, metadata); err != nil {
			t.Fatal(err)
		}
	}
	if err := repo.SetSource(t.Context(), m.IDs[1], m.OwnerID, metadata); !errors.Is(err, domain.ErrConflict) {
		t.Fatalf("duplicate revision: %v", err)
	}
	if err := repo.UpdateMeta(t.Context(), m.IDs[0], "parser author", "1601-01-01T00:00:00Z", "pdf"); err != nil {
		t.Fatal(err)
	}
	d, getErr := repo.Get(t.Context(), m.IDs[0])
	if getErr != nil {
		t.Fatal(getErr)
	}
	stored, decodeErr := sourcedoc.Decode(d.SourceMetadata)
	if decodeErr != nil || stored.Publication.Value != "2025" {
		t.Fatalf("source changed by legacy writer: %v", decodeErr)
	}
	s.ContentHash = strings.Repeat("b", 64)
	conflict, conflictErr := json.Marshal(s)
	if conflictErr != nil {
		t.Fatal(conflictErr)
	}
	if err := repo.SetSource(t.Context(), m.IDs[0], m.OwnerID, conflict); !errors.Is(err, domain.ErrConflict) {
		t.Fatalf("hash overwrite: %v", err)
	}
	if err := repo.SetSource(t.Context(), m.IDs[2], m.OwnerID, []byte(`{"version":1}`)); err == nil {
		t.Fatal("incomplete SQL contract accepted")
	}
	// Конкурирующие ревизии одного документа: SQL-обновление выигрывает ровно одна.
	s.NativeID = "native-2"
	a, aErr := json.Marshal(s)
	if aErr != nil {
		t.Fatal(aErr)
	}
	s.ContentHash = strings.Repeat("c", 64)
	b, bErr := json.Marshal(s)
	if bErr != nil {
		t.Fatal(bErr)
	}
	results := make(chan error, 2)
	var wg sync.WaitGroup
	for _, body := range [][]byte{a, b} {
		wg.Go(func() { results <- repo.SetSource(t.Context(), m.IDs[1], m.OwnerID, body) })
	}
	wg.Wait()
	close(results)
	succeeded := 0
	for err := range results {
		if err == nil {
			succeeded++
		} else if !errors.Is(err, domain.ErrConflict) {
			t.Fatal(err)
		}
	}
	if succeeded != 1 {
		t.Fatalf("competing writes succeeded=%d", succeeded)
	}
	checkDocumentSourceRPC(t, m.IDs[2], m.OwnerID, s)
}

func checkDocumentSourceRPC(t *testing.T, id, ownerID string, source sourcedoc.Source) {
	t.Helper()
	conn, err := grpc.NewClient("127.0.0.1:9091", grpc.WithTransportCredentials(insecure.NewCredentials()))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = conn.Close() })
	client := dbv1.NewDbServiceClient(conn)
	ctx, cancel := context.WithTimeout(t.Context(), 10*time.Second)
	defer cancel()
	bad := &dbv1.SetDocumentSourceRequest{Id: id, OwnerId: ownerID, SourceMetadata: `{}`}
	if _, callErr := client.SetDocumentSource(ctx, bad); status.Code(callErr) != codes.InvalidArgument {
		t.Fatalf("invalid source RPC: %v", callErr)
	}
	source.NativeID = "native-3"
	raw, marshalErr := json.Marshal(source)
	if marshalErr != nil {
		t.Fatal(marshalErr)
	}
	request := &dbv1.SetDocumentSourceRequest{Id: id, OwnerId: "foreign", SourceMetadata: string(raw)}
	if _, callErr := client.SetDocumentSource(ctx, request); status.Code(callErr) != codes.NotFound {
		t.Fatalf("foreign source RPC: %v", callErr)
	}
	request.OwnerId = ownerID
	for range 2 {
		if _, callErr := client.SetDocumentSource(ctx, request); callErr != nil {
			t.Fatal(callErr)
		}
	}
	doc, getErr := client.GetDocument(ctx, &dbv1.GetDocumentRequest{Id: id})
	if getErr != nil {
		t.Fatal(getErr)
	}
	stored, decodeErr := sourcedoc.Decode([]byte(doc.GetSourceMetadata()))
	if decodeErr != nil || stored.NativeID != source.NativeID {
		t.Fatalf("source RPC roundtrip: %v", decodeErr)
	}
	request.SourceMetadata = strings.ReplaceAll(request.SourceMetadata, source.ContentHash, strings.Repeat("d", 64))
	if _, callErr := client.SetDocumentSource(ctx, request); status.Code(callErr) != codes.AlreadyExists {
		t.Fatalf("immutable source RPC: %v", callErr)
	}
}
