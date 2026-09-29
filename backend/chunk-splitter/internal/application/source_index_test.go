package application

import (
	"bytes"
	"context"
	"encoding/hex"
	"encoding/json/v2"
	"errors"
	"io"
	"maps"
	"math"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/example/chunk-splitter/internal/infrastructure/splitter"
	"github.com/example/chunk-splitter/internal/platform/contracts"
	commonv1 "github.com/example/chunk-splitter/internal/platform/genproto/common/v1"
	"github.com/example/chunk-splitter/internal/platform/indexdoc"
	"github.com/example/chunk-splitter/internal/platform/sourcedoc"
	"lukechampine.com/blake3"
)

type indexMemory struct {
	document *commonv1.Document
	source   sourcedoc.Source
	active   *indexdoc.Manifest
	prepared *indexdoc.Manifest
	token    string
	denied   bool
}

func (m *indexMemory) GetDocument(context.Context, string) (*commonv1.Document, error) {
	return m.document, nil
}
func (m *indexMemory) BeginIndex(_ context.Context, lease indexdoc.Lease) (indexdoc.Job, error) {
	m.token = lease.Token
	job := indexdoc.Job{Lease: lease, Source: m.source, ObjectKey: m.document.GetObjectKey(),
		ContentHash: m.document.GetContentHash(), Active: m.active}
	if m.active != nil {
		job.Previous = []indexdoc.Previous{{SnapshotID: m.active.SnapshotID, ChunkCount: m.active.ChunkCount}}
	}
	return job, nil
}
func (m *indexMemory) RenewIndex(context.Context, indexdoc.Lease) error { return nil }
func (m *indexMemory) PrepareIndex(_ context.Context, a indexdoc.Activation) error {
	m.prepared = &a.Manifest
	return a.Manifest.Validate()
}
func (m *indexMemory) ActivateIndex(_ context.Context, a indexdoc.Activation) error {
	if m.denied || a.Lease.Token != m.token {
		return indexdoc.ErrConflict
	}
	if m.prepared == nil || *m.prepared != a.Manifest {
		return errors.New("activation without preparation")
	}
	m.active = &a.Manifest
	return nil
}
func (*indexMemory) FinishIndex(context.Context, indexdoc.Finish) error { return nil }

type indexObjects struct{ objects map[string][]byte }

func (m *indexObjects) Get(_ context.Context, key string) (io.ReadCloser, error) {
	raw, ok := m.objects[key]
	if !ok {
		return nil, errors.New("missing object")
	}
	return io.NopCloser(bytes.NewReader(raw)), nil
}
func (m *indexObjects) GetBytes(_ context.Context, key string) ([]byte, error) {
	raw, ok := m.objects[key]
	if !ok {
		return nil, errors.New("missing object")
	}
	return bytes.Clone(raw), nil
}
func (m *indexObjects) Put(_ context.Context, key string, reader io.Reader, _ int64, _ string) error {
	raw, err := io.ReadAll(reader)
	if err != nil {
		return err
	}
	m.objects[key] = raw
	return nil
}

type indexVectors struct{ points map[string]VectorPoint }

func (m *indexVectors) Upsert(_ context.Context, points []VectorPoint) error {
	for _, point := range points {
		m.points[point.ID] = point
	}
	return nil
}
func (m *indexVectors) Confirm(_ context.Context, owner, doc, snapshot string, ids []string, dim int) error {
	for _, id := range ids {
		p, ok := m.points[id]
		if !ok || p.OwnerID != owner || p.DocumentID != doc || p.Metadata["source_index_snapshot"] != snapshot || len(p.Vector) != dim {
			return errors.New("unconfirmed vector")
		}
	}
	return nil
}
func (m *indexVectors) DeleteIDs(_ context.Context, owner, doc string, ids []string) error {
	for _, id := range ids {
		p, ok := m.points[id]
		if ok && p.OwnerID == owner && p.DocumentID == doc {
			delete(m.points, id)
		}
	}
	return nil
}

type indexSearch struct {
	docs    map[string]SearchDoc
	fail    bool
	missing bool
}

func (m *indexSearch) Index(_ context.Context, docs []SearchDoc) error {
	if m.fail {
		return errors.New("lexical outage after vector write")
	}
	for _, doc := range docs {
		m.docs[doc.ID] = doc
	}
	return nil
}
func (m *indexSearch) Confirm(_ context.Context, owner, doc, snapshot string, ids []string) error {
	if m.missing {
		return errors.New("missing acknowledged chunk")
	}
	for _, id := range ids {
		d, ok := m.docs[id]
		if !ok || d.OwnerID != owner || d.DocumentID != doc || d.Metadata["source_index_snapshot"] != snapshot {
			return errors.New("unconfirmed lexical document")
		}
	}
	return nil
}
func (*indexSearch) Refresh(context.Context) error { return nil }
func (m *indexSearch) DeleteIDs(_ context.Context, owner, doc string, ids []string) error {
	for _, id := range ids {
		d, ok := m.docs[id]
		if ok && d.OwnerID == owner && d.DocumentID == doc {
			delete(m.docs, id)
		}
	}
	return nil
}

type indexHarness struct {
	ix      *Indexer
	control *indexMemory
	objects *indexObjects
	vectors *indexVectors
	search  *indexSearch
}

func newIndexHarness(t *testing.T) *indexHarness {
	t.Helper()
	raw := []byte("immutable source PDF original bytes")
	source := sourcedoc.Source{Version: 1, Kind: "paper", Provider: "manual", NativeID: "paper-1", CanonicalID: "paper-1",
		URL: "https://example.org/paper", ObservedAt: time.Now().UTC(), Revision: "v1", ContentHash: indexdoc.Hash(raw),
		HashAlgorithm: "sha256", AccessScope: "owner"}
	metadata, err := json.Marshal(source)
	if err != nil {
		t.Fatal(err)
	}
	hash := blake3.Sum256(raw)
	control := &indexMemory{source: source, document: &commonv1.Document{
		Id: "2825626c-e36d-4e7b-ae87-cd0fe7f81c87", OwnerId: "alice", ObjectKey: "raw/pdf",
		SourceMetadata: string(metadata), ContentHash: hex.EncodeToString(hash[:]),
	}}
	objects := &indexObjects{objects: map[string][]byte{"raw/pdf": raw}}
	vectors := &indexVectors{points: map[string]VectorPoint{}}
	search := &indexSearch{docs: map[string]SearchDoc{}}
	ix := New(splitter.NewRecursive(1000, 0), noopStatus{}, dummyEmbedder{}, vectors, search, noopPub{},
		nil, objects, nil, 0, 0, 64, 1<<20, 0, false, nil)
	ix.EnableSourceIndexes(control, objects, indexdoc.Model{Name: "test-embedding", Revision: "weights-v1", Dimension: 1})
	return &indexHarness{ix: ix, control: control, objects: objects, vectors: vectors, search: search}
}
func (h *indexHarness) event(t *testing.T, pages int, source string) *commonv1.DocumentParsed {
	t.Helper()
	var text strings.Builder
	offsets := make([]int, pages)
	for i := range pages {
		offsets[i] = len([]rune(text.String()))
		text.WriteString("М е т о д " + strconv.Itoa(i+1) + " описывает измеримое свойство.\n")
	}
	rawOffsets, err := json.Marshal(offsets)
	if err != nil {
		t.Fatal(err)
	}
	return &commonv1.DocumentParsed{DocumentId: h.control.document.GetId(), OwnerId: "alice", ObjectKey: "raw/pdf",
		Source: source, Filename: "source.pdf", MimeType: "application/pdf", Text: text.String(),
		Metadata: contracts.ParsedMetadata(text.String(), "parser-v1", map[string]string{"page_offsets": string(rawOffsets)})}
}

func TestSourceReindexPrunesTenToThreeAndPreservesOldEvidence(t *testing.T) {
	for _, ordinary := range []bool{false, true} {
		t.Run(strconv.FormatBool(ordinary), func(t *testing.T) {
			h := newIndexHarness(t)
			if ordinary {
				h.control.source = sourcedoc.Source{}
				h.control.document.SourceMetadata = ""
			}
			evt := h.event(t, 10, "pdf")
			if err := h.ix.Process(t.Context(), evt); err != nil {
				t.Fatal(err)
			}
			old := *h.control.active
			oldData := bytes.Clone(h.objects.objects[old.Data.Key])
			if old.ChunkCount != 10 {
				t.Fatalf("first snapshot: %d chunks", old.ChunkCount)
			}
			for range 2 {
				if err := h.ix.Process(t.Context(), h.event(t, 3, "pdf")); err != nil {
					t.Fatal(err)
				}
				if h.control.active.ChunkCount != 3 || len(h.vectors.points) != 3 || len(h.search.docs) != 3 {
					t.Fatal("reindex did not leave exactly three current chunks in both stores")
				}
				if !bytes.Equal(h.objects.objects[old.Data.Key], oldData) {
					t.Fatal("old analysis evidence was overwritten")
				}
			}
			var snapshot indexdoc.Snapshot
			if err := json.Unmarshal(oldData, &snapshot); err != nil {
				t.Fatal(err)
			}
			for i, chunk := range snapshot.Chunks {
				runes := []rune(evt.GetText())
				if chunk.Text != string(runes[chunk.CharStart:chunk.CharEnd]) || chunk.PageStart != i+1 ||
					chunk.PageEnd != i+1 || chunk.LocatorKind != "pdf_text_page" {
					t.Fatal("tracking collapse or reindex corrupted original page/rune anchors")
				}
			}
		})
	}
}

func TestSourceIndexFailuresKeepPreviousReadySnapshot(t *testing.T) {
	for _, failure := range []string{"lexical outage", "missing chunk", "missing raw", "tampered raw", "bad text hash",
		"dimension", "nonfinite", "stale lease", "missing parser"} {
		t.Run(failure, func(t *testing.T) {
			h := newIndexHarness(t)
			if err := h.ix.Process(t.Context(), h.event(t, 2, "pdf")); err != nil {
				t.Fatal(err)
			}
			previous := *h.control.active
			oldObjects := maps.Clone(h.objects.objects)
			evt := h.event(t, 3, "pdf")
			switch failure {
			case "lexical outage":
				h.search.fail = true
			case "missing chunk":
				h.search.missing = true
			case "missing raw":
				delete(h.objects.objects, "raw/pdf")
			case "tampered raw":
				h.objects.objects["raw/pdf"] = []byte("different revision")
			case "bad text hash":
				evt.Text += " changed"
			case "dimension":
				h.ix.sourceIndexes.model.Dimension = 2
			case "nonfinite":
				h.ix.embedder = nonfiniteEmbedder{}
			case "stale lease":
				h.control.denied = true
			case "missing parser":
				delete(evt.Metadata, "parser_revision")
			}
			if err := h.ix.Process(t.Context(), evt); err == nil {
				t.Fatal("incomplete or invalid index became ready")
			}
			if *h.control.active != previous {
				t.Fatal("failed attempt changed the published snapshot")
			}
			if !bytes.Equal(h.objects.objects[previous.Data.Key], oldObjects[previous.Data.Key]) {
				t.Fatal("old evidence changed")
			}
		})
	}
}

type nonfiniteEmbedder struct{}

func (nonfiniteEmbedder) Embed(_ context.Context, texts []string) ([][]float32, error) {
	vectors := make([][]float32, len(texts))
	for i := range vectors {
		vectors[i] = []float32{float32(math.NaN())}
	}
	return vectors, nil
}

func TestSourceOCRLocatorIsNotTextPDF(t *testing.T) {
	h := newIndexHarness(t)
	if err := h.ix.Process(t.Context(), h.event(t, 1, "ocr")); err != nil {
		t.Fatal(err)
	}
	for _, p := range h.vectors.points {
		if p.Metadata["locator_kind"] != "ocr_page" {
			t.Fatal("OCR locator mislabelled")
		}
	}
}

type unexpectedSourceTitleGeneration struct{}

func (unexpectedSourceTitleGeneration) Extract(context.Context, string) (string, string) {
	panic("source snapshot must reuse its recorded title")
}
func (unexpectedSourceTitleGeneration) SetDocumentTitle(context.Context, string, string) error {
	return errors.New("source title must not change during snapshot rebuild")
}

func TestSourceSnapshotReusesRecordedTitleWithoutGeneration(t *testing.T) {
	h := newIndexHarness(t)
	h.control.document.Title = "Recorded source title"
	h.ix.EnableTitles(unexpectedSourceTitleGeneration{}, unexpectedSourceTitleGeneration{})
	if err := h.ix.Process(t.Context(), h.event(t, 1, "pdf")); err != nil {
		t.Fatal(err)
	}
	var snapshot indexdoc.Snapshot
	raw := h.objects.objects[h.control.active.Data.Key]
	if err := json.Unmarshal(raw, &snapshot); err != nil {
		t.Fatal(err)
	}
	if snapshot.Title != "Recorded source title" {
		t.Fatal("snapshot did not preserve title")
	}
}
