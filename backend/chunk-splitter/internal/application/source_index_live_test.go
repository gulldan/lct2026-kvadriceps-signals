package application_test

import (
	"bytes"
	"context"
	"encoding/json/v2"
	"errors"
	"io"
	"os"
	"strings"
	"testing"

	"github.com/example/chunk-splitter/internal/application"
	"github.com/example/chunk-splitter/internal/infrastructure/embedding"
	"github.com/example/chunk-splitter/internal/infrastructure/lexicalindex"
	"github.com/example/chunk-splitter/internal/infrastructure/splitter"
	"github.com/example/chunk-splitter/internal/infrastructure/vectorindex"
	"github.com/example/chunk-splitter/internal/platform/aiclients"
	"github.com/example/chunk-splitter/internal/platform/config"
	"github.com/example/chunk-splitter/internal/platform/contracts"
	"github.com/example/chunk-splitter/internal/platform/dbclient"
	commonv1 "github.com/example/chunk-splitter/internal/platform/genproto/common/v1"
	"github.com/example/chunk-splitter/internal/platform/indexdoc"
	"github.com/example/chunk-splitter/internal/platform/searchstore"
	"github.com/example/chunk-splitter/internal/platform/sourcedoc"
	"github.com/example/chunk-splitter/internal/platform/storage"
	"github.com/example/chunk-splitter/internal/platform/vectorstore"
	"google.golang.org/protobuf/proto"
)

type projectionFixture struct {
	RunID      string           `json:"run_id"`
	DocumentID string           `json:"document_id"`
	OwnerID    string           `json:"owner_id"`
	RawKey     string           `json:"raw_key"`
	Raw        string           `json:"raw"`
	BLAKE3     string           `json:"blake3"`
	Source     sourcedoc.Source `json:"source"`
}

type recordingObjects struct {
	*storage.Client
	keys []string
}

func (s *recordingObjects) Put(ctx context.Context, key string, body io.Reader, size int64, mime string) error {
	s.keys = append(s.keys, key)
	return s.Client.Put(ctx, key, body, size, mime)
}

type silentIndexEvents struct{}

func (silentIndexEvents) PublishProto(context.Context, string, string, proto.Message) error {
	return nil
}

type failingConfirmation struct {
	*lexicalindex.Adapter
	fail bool
}

func (s *failingConfirmation) Confirm(ctx context.Context, owner, doc, snapshot string, ids []string) error {
	if s.fail {
		return errors.New("injected missing lexical acknowledgement")
	}
	return s.Adapter.Confirm(ctx, owner, doc, snapshot, ids)
}

// TestSourceProjectionsLive проверяет публикацию и восстановление на заданных выходах парсера;
// это не бенчмарк разбора PDF.
func TestSourceProjectionsLive(t *testing.T) {
	filename := os.Getenv("TR_PROJECTION_MANIFEST")
	if filename == "" {
		t.Skip("predeclared projection fixture required")
	}
	raw, err := os.ReadFile(filename)
	if err != nil {
		t.Fatal(err)
	}
	var fixture projectionFixture
	if err = json.Unmarshal(raw, &fixture, json.RejectUnknownMembers(true)); err != nil {
		t.Fatal(err)
	}
	if !strings.HasPrefix(fixture.RunID, "tr-test-") || fixture.OwnerID != fixture.RunID ||
		fixture.Source.TestRunID != fixture.RunID || fixture.RawKey != "tests/"+fixture.RunID+"/original.txt" {
		t.Fatal("foreign test fixture")
	}
	db, err := dbclient.New(config.MustGet("DB_SERVICE_ADDR"))
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = db.Close() }()
	doc, err := db.GetDocument(t.Context(), fixture.DocumentID)
	if err != nil || doc.GetOwnerId() != fixture.OwnerID || doc.GetObjectKey() != fixture.RawKey {
		t.Fatal("only the prepared test document may be indexed", err)
	}
	source, err := sourcedoc.Decode([]byte(doc.GetSourceMetadata()))
	if err != nil || source.TestRunID != fixture.RunID || source.ContentHash != indexdoc.Hash([]byte(fixture.Raw)) {
		t.Fatal("test source provenance mismatch", err)
	}
	store, err := storage.New(t.Context(), storage.ConfigFromEnv())
	if err != nil {
		t.Fatal(err)
	}
	objects := &recordingObjects{Client: store}
	defer func() {
		encoded, encodeErr := json.Marshal(objects.keys)
		if encodeErr != nil {
			t.Error(encodeErr)
			return
		}
		t.Logf("EXACT_TEST_OBJECTS=%s", encoded)
	}()
	err = objects.Put(t.Context(), fixture.RawKey, strings.NewReader(fixture.Raw), int64(len(fixture.Raw)), "text/plain")
	if err != nil {
		t.Fatal(err)
	}
	qdrant := vectorstore.NewQdrant(config.MustGet("QDRANT_URL"), config.Get("QDRANT_COLLECTION", "documents"))
	lexical := searchstore.NewOpenSearch(config.MustGet("OPENSEARCH_URL"), config.Get("OPENSEARCH_INDEX", "chunks"))
	search := &failingConfirmation{Adapter: lexicalindex.New(lexical)}
	model := indexdoc.Model{Name: config.MustGet("EMBEDDINGS_MODEL"), Revision: config.MustGet("EMBEDDINGS_REVISION"),
		Dimension: config.GetInt("EMBEDDING_DIM", 1024)}
	embedder := aiclients.NewVersionedEmbedder(config.MustGet("EMBEDDINGS_URL"), model.Name, model.Revision, model.Dimension, nil)
	ix := application.New(splitter.NewRecursive(1000, 0), db, embedding.New(embedder), vectorindex.New(qdrant),
		search, silentIndexEvents{}, nil, objects, nil, 512, 1<<20, 64, 1<<20, 0, false, nil)
	ix.EnableSourceIndexes(db, objects, model)
	runProjectionChecks(t, ix, db, objects, search, fixture)
	points, err := qdrant.Scroll(t.Context(), map[string]string{
		"document_id": fixture.DocumentID, "owner_id": fixture.OwnerID,
	}, "chunk_index", 50)
	if err != nil || len(points) != 3 {
		t.Fatalf("physical vector count %d: %v", len(points), err)
	}
	hits, err := lexical.SearchFiltered(t.Context(), "evidence", fixture.OwnerID, 50, []string{fixture.DocumentID}, nil)
	if err != nil || len(hits) != 3 {
		t.Fatalf("physical lexical count %d: %v", len(hits), err)
	}
	t.Log("Physical stores each contain exactly 3 chunks; no obsolete or failed projection remains")
}

func runProjectionChecks(t *testing.T, ix *application.Indexer, db *dbclient.Client, objects *recordingObjects,
	search *failingConfirmation, fixture projectionFixture,
) {
	t.Helper()
	read := indexdoc.Read{ReaderID: fixture.OwnerID, DocumentIDs: []string{fixture.DocumentID}}
	manifests := make([]indexdoc.Manifest, 0, 3)
	for _, pages := range []int{10, 3, 3} {
		if err := ix.Process(t.Context(), projectionEvent(t, fixture, pages)); err != nil {
			t.Fatal(err)
		}
		states, err := db.ReadIndexes(t.Context(), read)
		if err != nil || len(states.Documents) != 1 || states.Documents[0].Active == nil {
			t.Fatal("no ready index", err)
		}
		manifest := *states.Documents[0].Active
		if manifest.ChunkCount != pages {
			t.Fatalf("want %d chunks, got %d", pages, manifest.ChunkCount)
		}
		manifests = append(manifests, manifest)
	}
	search.fail = true
	if err := ix.Process(t.Context(), projectionEvent(t, fixture, 10)); err == nil {
		t.Fatal("unconfirmed generation activated")
	}
	states, err := db.ReadIndexes(t.Context(), read)
	if err != nil || *states.Documents[0].Active != manifests[2] {
		t.Fatal("failed attempt replaced ready snapshot", err)
	}
	search.fail = false
	if err = ix.Process(t.Context(), projectionEvent(t, fixture, 3)); err != nil {
		t.Fatal(err)
	}
	for _, manifest := range manifests {
		read.SnapshotID = manifest.SnapshotID
		old, readErr := db.ReadIndexes(t.Context(), read)
		if readErr != nil || *old.Documents[0].Active != manifest {
			t.Fatal("historical manifest changed", readErr)
		}
		for _, ref := range []indexdoc.Object{manifest.Text, manifest.Data} {
			stream, getErr := objects.Get(t.Context(), ref.Key)
			if getErr != nil {
				t.Fatal(getErr)
			}
			content, getErr := io.ReadAll(io.LimitReader(stream, indexdoc.MaxSnapshotBytes+1))
			_ = stream.Close()
			if getErr != nil || indexdoc.Hash(content) != ref.SHA256 {
				t.Fatal("historical artifact changed", getErr)
			}
			if ref == manifest.Text && !bytes.Equal(content, []byte(fixture.Raw)) {
				t.Fatal("exact text lost")
			}
		}
	}
	t.Log("10 -> 3 -> 3; unconfirmed generation rejected; recovery -> 3; historical bytes unchanged")
}

func projectionEvent(t *testing.T, f projectionFixture, pages int) *commonv1.DocumentParsed {
	t.Helper()
	offsets := []int{0}
	for pos, r := range []rune(f.Raw) {
		if r == '\n' && pos+1 < len([]rune(f.Raw)) {
			offsets = append(offsets, pos+1)
		}
	}
	if len(offsets) != 10 {
		t.Fatal("fixture must contain ten source paragraphs")
	}
	if pages == 3 {
		offsets = []int{offsets[0], offsets[4], offsets[7]}
	}
	rawOffsets, err := json.Marshal(offsets)
	if err != nil {
		t.Fatal(err)
	}
	return &commonv1.DocumentParsed{DocumentId: f.DocumentID, OwnerId: f.OwnerID, ObjectKey: f.RawKey,
		Filename: "source-index-live.txt", MimeType: "text/plain", Source: "controlled-test-parser", Text: f.Raw,
		Metadata: contracts.ParsedMetadata(f.Raw, "controlled-page-output-v1", map[string]string{"page_offsets": string(rawOffsets)})}
}

// TestSourceProjectionCleanup удаляет только объекты и id чанков из манифеста; запускать после того,
// как живое доказательство сохранено вне корпуса.
func TestSourceProjectionCleanup(t *testing.T) {
	if os.Getenv("TR_PROJECTION_CLEANUP") != "true" {
		t.Skip("explicit cleanup mode required")
	}
	raw, err := os.ReadFile(os.Getenv("TR_PROJECTION_MANIFEST"))
	if err != nil {
		t.Fatal(err)
	}
	var f projectionFixture
	if err = json.Unmarshal(raw, &f); err != nil {
		t.Fatal(err)
	}
	raw, err = os.ReadFile(os.Getenv("TR_PROJECTION_KEYS"))
	if err != nil {
		t.Fatal(err)
	}
	var keys []string
	if err = json.Unmarshal(raw, &keys); err != nil || len(keys) > 32 {
		t.Fatal("invalid exact object list", err)
	}
	db, err := dbclient.New(config.MustGet("DB_SERVICE_ADDR"))
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = db.Close() }()
	doc, err := db.GetDocument(t.Context(), f.DocumentID)
	if err != nil || doc.GetOwnerId() != f.RunID || f.OwnerID != f.RunID {
		t.Fatal("foreign document", err)
	}
	source, err := sourcedoc.Decode([]byte(doc.GetSourceMetadata()))
	if err != nil || source.TestRunID != f.RunID || !strings.HasPrefix(f.RunID, "tr-test-") {
		t.Fatal("foreign source", err)
	}
	store, err := storage.New(t.Context(), storage.ConfigFromEnv())
	if err != nil {
		t.Fatal(err)
	}
	qdrant := vectorstore.NewQdrant(config.MustGet("QDRANT_URL"), config.Get("QDRANT_COLLECTION", "documents"))
	lexical := searchstore.NewOpenSearch(config.MustGet("OPENSEARCH_URL"), config.Get("OPENSEARCH_INDEX", "chunks"))
	ids := []string{}
	for _, key := range keys {
		if key != f.RawKey && !strings.HasPrefix(key, "source-index/v1/"+f.DocumentID+"/") {
			t.Fatal("foreign object key")
		}
		if !strings.HasSuffix(key, ".json") {
			continue
		}
		stream, getErr := store.Get(t.Context(), key)
		if getErr != nil {
			t.Fatal(getErr)
		}
		content, getErr := io.ReadAll(io.LimitReader(stream, indexdoc.MaxSnapshotBytes+1))
		_ = stream.Close()
		if getErr != nil {
			t.Fatal(getErr)
		}
		var snapshot indexdoc.Snapshot
		err = json.Unmarshal(content, &snapshot)
		if err != nil || snapshot.DocumentID != f.DocumentID || snapshot.OwnerID != f.OwnerID {
			t.Fatal("foreign snapshot", err)
		}
		for _, chunk := range snapshot.Chunks {
			ids = append(ids, chunk.ID)
		}
	}
	if err = qdrant.DeleteChunkIDs(t.Context(), f.OwnerID, f.DocumentID, ids); err != nil {
		t.Fatal(err)
	}
	if err = lexical.DeleteChunkIDs(t.Context(), f.OwnerID, f.DocumentID, ids); err != nil {
		t.Fatal(err)
	}
	for _, key := range keys {
		if err = store.Delete(t.Context(), key); err != nil {
			t.Fatal(err)
		}
	}
	t.Logf("Removed %d exact test objects and %d manifest-bound chunk IDs", len(keys), len(ids))
}

func TestSourceProjectionAbsentLive(t *testing.T) {
	if os.Getenv("TR_PROJECTION_VERIFY_ABSENT") != "true" {
		t.Skip("explicit fixture cleanup verification required")
	}
	raw, err := os.ReadFile(os.Getenv("TR_PROJECTION_MANIFEST"))
	if err != nil {
		t.Fatal(err)
	}
	var f projectionFixture
	if err = json.Unmarshal(raw, &f); err != nil {
		t.Fatal(err)
	}
	raw, err = os.ReadFile(os.Getenv("TR_PROJECTION_KEYS"))
	if err != nil {
		t.Fatal(err)
	}
	var keys []string
	if err = json.Unmarshal(raw, &keys); err != nil {
		t.Fatal(err)
	}
	objects, err := storage.New(t.Context(), storage.ConfigFromEnv())
	if err != nil {
		t.Fatal(err)
	}
	for _, key := range keys {
		stream, getErr := objects.Get(t.Context(), key)
		if stream != nil {
			_ = stream.Close()
		}
		if !errors.Is(getErr, storage.ErrNotFound) {
			t.Fatal("test object remains or absence not confirmed", key, getErr)
		}
	}
	qdrant := vectorstore.NewQdrant(config.MustGet("QDRANT_URL"), config.Get("QDRANT_COLLECTION", "documents"))
	scope := map[string]string{"document_id": f.DocumentID, "owner_id": f.OwnerID}
	points, err := qdrant.Scroll(t.Context(), scope, "chunk_index", 50)
	if err != nil || len(points) != 0 {
		t.Fatal("test vector projection remains", err)
	}
	lexical := searchstore.NewOpenSearch(config.MustGet("OPENSEARCH_URL"), config.Get("OPENSEARCH_INDEX", "chunks"))
	hits, err := lexical.SearchFiltered(t.Context(), "evidence", f.OwnerID, 50, []string{f.DocumentID}, nil)
	if err != nil || len(hits) != 0 {
		t.Fatal("test lexical projection remains", err)
	}
	t.Logf("Verified %d exact S3 keys absent; Qdrant=0, OpenSearch=0", len(keys))
}
