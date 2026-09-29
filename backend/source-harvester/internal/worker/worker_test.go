package worker

import (
	"context"
	"encoding/json/v2"
	"errors"
	"io"
	"slices"
	"strconv"
	"strings"
	"testing"
	"time"
	"uuid"

	"github.com/example/source-harvester/internal/platform/harvest"
	"github.com/example/source-harvester/internal/platform/sourcedoc"
	"github.com/example/source-harvester/internal/provider"
)

type memoryControl struct {
	job        harvest.Job
	pending    []harvest.Dispatch
	documents  map[string]bool
	failCommit bool
	failAck    bool
	finishErr  error
	commits    int
}

func (c *memoryControl) Start(_ context.Context, in harvest.Start) (harvest.Job, error) {
	c.job = harvest.Job{
		Lease:     harvest.Lease{ScopeID: in.ScopeID(), RunID: in.RunID, Token: in.Token},
		OwnerID:   in.OwnerID,
		Provider:  in.Provider,
		Scope:     in.Scope,
		Limit:     in.Limit,
		Cursor:    c.job.Cursor,
		Exhausted: c.job.Exhausted,
		Status:    "running",
		TestRunID: in.TestRunID,
	}
	return c.job, nil
}
func (c *memoryControl) Quota(context.Context, harvest.Quota) (harvest.Grant, error) {
	return harvest.Grant{Allowed: true}, nil
}
func (c *memoryControl) CommitPage(_ context.Context, p harvest.CommitPage) (harvest.Job, error) {
	if c.failCommit {
		return c.job, errors.New("commit unavailable")
	}
	if err := p.Validate(c.job); err != nil {
		return c.job, err
	}
	c.commits++
	for _, r := range p.Records {
		key := harvest.DocumentKey(c.job.OwnerID, r)
		if c.documents[key] {
			continue
		}
		c.documents[key] = true
		c.pending = append(
			c.pending,
			harvest.Dispatch{
				DocumentID: r.DocumentID,
				OwnerID:    c.job.OwnerID,
				Text:       r.Text,
				MIME:       r.MIME,
				ObjectKey:  r.ObjectKey,
			},
		)
	}
	c.job.Received += p.Received
	c.job.PageNumber++
	c.job.Cursor = p.Next
	c.job.Exhausted = p.Exhausted
	return c.job, nil
}
func (c *memoryControl) Pending(context.Context, harvest.Pending) (harvest.PendingResult, error) {
	return harvest.PendingResult{Records: c.pending}, nil
}
func (c *memoryControl) Ack(_ context.Context, a harvest.Ack) (harvest.PendingResult, error) {
	if c.failAck {
		return harvest.PendingResult{}, errors.New("ack unavailable")
	}
	for i, r := range c.pending {
		if r.DocumentID == a.DocumentID {
			c.pending = append(c.pending[:i], c.pending[i+1:]...)
			break
		}
	}
	return harvest.PendingResult{}, nil
}
func (*memoryControl) Requested(context.Context, harvest.Requested) ([]harvest.Collection, error) {
	return nil, nil
}

func (c *memoryControl) Finish(_ context.Context, f harvest.Finish) (harvest.Job, error) {
	if c.finishErr != nil {
		return c.job, c.finishErr
	}
	c.job.Status = f.Status
	if f.ResetCursor {
		c.job.Cursor = ""
		c.job.Exhausted = false
	}
	return c.job, nil
}

type memoryObjects struct {
	data map[string][]byte
	fail bool
}

func (o *memoryObjects) Put(_ context.Context, key string, r io.Reader, _ int64, _ string) error {
	if o.fail {
		return errors.New("S3 unavailable")
	}
	b, err := io.ReadAll(r)
	o.data[key] = b
	return err
}

type memoryPublisher struct {
	fail bool
	ids  []string
}

func (p *memoryPublisher) Publish(_ context.Context, d harvest.Dispatch) error {
	if p.fail {
		return errors.New("publish not confirmed")
	}
	p.ids = append(p.ids, d.DocumentID)
	return nil
}

type pages struct {
	records  []provider.Page
	calls    int
	gate     provider.Gate
	pdfErr   error
	fetchErr error
	pdf      []byte
}

func (p *pages) SetRequestGate(g provider.Gate) { p.gate = g }
func (p *pages) Fetch(context.Context, provider.Scope, string) (provider.Page, error) {
	if p.fetchErr != nil {
		return provider.Page{}, p.fetchErr
	}
	i := p.calls
	p.calls++
	if i >= len(p.records) {
		return provider.Page{}, errors.New("no recorded page")
	}
	return p.records[i], nil
}
func (p *pages) PDF(context.Context, sourcedoc.Source) ([]byte, error) { return p.pdf, p.pdfErr }

func TestExpiredCursorReturnsConfirmedCheckpoint(t *testing.T) {
	finishFailure := errors.New("finish unavailable")
	for _, failedFinish := range []bool{false, true} {
		t.Run(strconv.FormatBool(failedFinish), func(t *testing.T) {
			w, in, control, adapter, publisher, _ := fixture(t)
			control.job.Cursor = "old-snapshot:200"
			adapter.fetchErr = provider.ErrCursorExpired
			if failedFinish {
				control.finishErr = finishFailure
			}
			job, err := w.Run(t.Context(), in)
			if !errors.Is(err, provider.ErrCursorExpired) || errors.Is(err, finishFailure) != failedFinish {
				t.Fatalf("lost collection or checkpoint error: %v", err)
			}
			if failedFinish {
				if job.Cursor != "old-snapshot:200" || job.Status != "running" {
					t.Fatalf("unconfirmed reset presented as durable: %+v", job)
				}
			} else if job.Cursor != "" || job.Status != "failed" || job.Exhausted {
				t.Fatalf("returned stale checkpoint after durable reset: %+v", job)
			}
			if len(control.documents) != 0 || len(publisher.ids) != 0 {
				t.Fatal("cursor reset ingested documents")
			}
		})
	}
}

func fixture(t *testing.T) (*Worker, harvest.Start, *memoryControl, *pages, *memoryPublisher, *memoryObjects) {
	t.Helper()
	in := harvest.Start{
		OwnerID:  "fixture-owner",
		Provider: "crossref",
		Scope: harvest.Scope{
			Query:    "retrieval",
			From:     "2023-01-01",
			Through:  "2026-08-31",
			PageSize: 1,
			WorkType: "journal-article",
		},
		Limit:     3,
		RunID:     uuid.NewV4().String(),
		Token:     uuid.NewV4().String(),
		TestRunID: "tr-test-worker",
	}
	s := sourcedoc.Source{
		Version:       1,
		Kind:          "paper",
		Provider:      "crossref",
		NativeID:      "10.5555/example",
		CanonicalID:   "doi:10.5555/example",
		URL:           "https://doi.org/10.5555/example",
		ObservedAt:    time.Date(2026, 9, 8, 0, 0, 0, 0, time.UTC),
		Revision:      "metadata:fixture",
		HashAlgorithm: "sha256",
		ContentHash:   sha([]byte("saved abstract")),
		AccessScope:   "public",
	}
	p := &pages{
		records: []provider.Page{
			{
				Provider:  "crossref",
				Scope:     provider.Scope(in.Scope),
				Received:  1,
				Exhausted: true,
				Raw:       []byte(`{"fixture":true}`),
				RawHash:   sha([]byte(`{"fixture":true}`)),
				Records: []provider.Record{
					{Source: s, Title: "A recorded paper", Text: "saved abstract", TextStatus: "title_and_abstract"},
				},
			},
		},
	}
	c := &memoryControl{documents: map[string]bool{}}
	o := &memoryObjects{data: map[string][]byte{}}
	pub := &memoryPublisher{}
	w := &Worker{
		Control:   c,
		Objects:   o,
		Publisher: pub,
		Adapter:   p,
		BeforeCommit: func(page harvest.CommitPage) error {
			if _, ok := o.data[page.ManifestKey]; !ok {
				t.Fatal("DB commit preceded immutable manifest")
			}
			return nil
		},
	}
	return w, in, c, p, pub, o
}

func TestRestartDispatchAndOverlap(t *testing.T) {
	for _, failure := range []string{"publish", "ack"} {
		t.Run(
			failure,
			func(t *testing.T) {
				w, in, c, p, pub, o := fixture(t)
				pub.fail = failure == "publish"
				c.failAck = failure == "ack"
				if _, err := w.Run(t.Context(), in); err == nil {
					t.Fatal("lost confirmation succeeded")
				}
				if len(c.pending) != 1 || c.commits != 1 {
					t.Fatalf("lost durable pending: %+v", c)
				}
				pendingID := c.pending[0].DocumentID
				pub.fail = false
				c.failAck = false
				in.RunID, in.Token = uuid.NewV4().String(), uuid.NewV4().String()
				job, err := w.Run(t.Context(), in)
				if err != nil || job.Status != "complete" || len(c.pending) != 0 || p.calls != 1 {
					t.Fatalf("restart fetched past checkpoint: %+v, %v", job, err)
				}
				for _, id := range pub.ids {
					if id != pendingID {
						t.Fatal("redelivery changed document identity")
					}
				}
				c.job.Exhausted = false
				p.calls = 0
				in.RunID, in.Token = uuid.NewV4().String(), uuid.NewV4().String()
				if _, err := w.Run(t.Context(), in); err != nil {
					t.Fatal(err)
				}
				if len(c.documents) != 1 {
					t.Fatal("overlap duplicated document")
				}
				for key, raw := range o.data {
					if len(raw) == 0 {
						t.Fatalf("empty source object %s", key)
					}
				}
			},
		)
	}
}

func TestCheckpointRequiresAllStorage(t *testing.T) {
	for _, failure := range []string{"s3", "manifest", "database"} {
		t.Run(
			failure,
			func(t *testing.T) {
				w, in, c, _, pub, o := fixture(t)
				switch failure {
				case "s3":
					o.fail = true
				case "manifest":
					w.BeforeCommit = func(harvest.CommitPage) error { return errors.New("manifest failed") }
				case "database":
					c.failCommit = true
				}
				if _, err := w.Run(t.Context(), in); err == nil {
					t.Fatal("failure succeeded")
				}
				if c.commits != 0 || len(pub.ids) != 0 || c.job.Cursor != "" {
					t.Fatal("failed storage advanced or published")
				}
			},
		)
	}
}

func TestArXivPDF404PreservesAbstract(t *testing.T) {
	testArXivFallback(t, &provider.HTTPError{Status: 404})
}

func TestArXivOversizedPDFPreservesAbstract(t *testing.T) {
	testArXivFallback(t, &provider.RequestError{Err: provider.ErrResponseTooLarge})
}

func TestArXivPDFReplacesAbstractStructure(t *testing.T) {
	w, in, _, p, _, objects := fixture(t)
	record := p.records[0].Records[0]
	record.Source.Provider = "arxiv"
	record.Source.Sections = []sourcedoc.Section{{Name: "abstract", Start: 0, End: len(record.Text)}}
	record.Limitations = []string{"pdf_not_fetched", "preprint_not_verified_peer_review"}
	p.pdf = []byte("%PDF-1.7\nfixture PDF bytes")
	prepared, err := w.prepareRecord(t.Context(), harvest.Job{OwnerID: in.OwnerID, Provider: "arxiv"}, record)
	if err != nil {
		t.Fatal(err)
	}
	if prepared.TextStatus != "pdf" || len(prepared.Source.Sections) != 0 || len(prepared.Source.Anchors) != 0 ||
		slices.Contains(prepared.Limitations, "pdf_not_fetched") ||
		!slices.Contains(prepared.Limitations, "preprint_not_verified_peer_review") {
		t.Fatalf("PDF inherited obsolete abstract structure or lost limitations: %+v", prepared)
	}
	if string(objects.data[prepared.ObjectKey]) != string(p.pdf) {
		t.Fatal("PDF bytes were not stored")
	}
	if len(record.Source.Sections) != 1 || !slices.Contains(record.Limitations, "pdf_not_fetched") {
		t.Fatal("preparing a PDF mutated the saved metadata page")
	}
}

func testArXivFallback(t *testing.T, failure error) {
	t.Helper()
	w, in, c, p, _, _ := fixture(t)
	in.Provider = "arxiv"
	s := &p.records[0].Records[0].Source
	s.Provider, s.NativeID, s.CanonicalID, s.Revision = "arxiv", "2301.00001", "arxiv:2301.00001", "2301.00001v1"
	s.Publication = &sourcedoc.Date{Value: "2023-01-01", Precision: "day", Basis: "published"}
	s.ArXiv = &sourcedoc.ArXiv{
		BaseID:         s.NativeID,
		Version:        1,
		FirstPublished: time.Date(2023, 1, 1, 0, 0, 0, 0, time.UTC),
		VersionUpdated: time.Date(2023, 1, 1, 0, 0, 0, 0, time.UTC),
	}
	p.pdfErr = failure
	w.BeforeCommit = func(page harvest.CommitPage) error {
		r := page.Records[0]
		if r.MIME != "text/plain" || r.Text != "saved abstract" ||
			!slices.ContainsFunc(r.Limitations, func(v string) bool { return strings.HasPrefix(v, "arxiv_pdf_") }) {
			t.Fatalf("unavailable PDF erased available evidence: %+v", r.Limitations)
		}
		raw, err := json.Marshal(r.Source)
		if err != nil {
			t.Fatal(err)
		}
		if _, err := sourcedoc.Decode(raw); err != nil {
			t.Fatal(err)
		}
		return nil
	}
	if _, err := w.Run(t.Context(), in); err != nil {
		t.Fatal(err)
	}
	if len(c.documents) != 1 {
		t.Fatal("abstract was not retained")
	}
}

func TestPatentAndWebUseStoredSourceAndResumeWithoutDuplicates(t *testing.T) {
	for _, kind := range []string{"patent", "web"} {
		t.Run(kind, func(t *testing.T) {
			w, in, c, p, pub, o := fixture(t)
			in.Provider = kind
			in.Limit = 1
			first := p.records[0]
			first.Provider = kind
			first.Exhausted = false
			first.Next = "second"
			first.Records[0].Source.Provider = kind
			first.Records[0].Source.Kind = "analysis"
			if kind == "patent" {
				first.Records[0].Source.Kind = "patent"
			}
			first.Records[0].TextStatus = "metadata_only"
			second := first
			second.Records = append([]provider.Record(nil), first.Records...)
			second.Records[0].Source.NativeID = "second"
			second.Exhausted = true
			second.Cursor = "second"
			second.Next = ""
			second.Raw = []byte(`{"page":2}`)
			second.RawHash = sha(second.Raw)
			p.records = []provider.Page{first, second}
			job, e := w.Run(t.Context(), in)
			if e != nil || job.Status != "partial" || job.Cursor != "second" {
				t.Fatalf("first batch %+v %v", job, e)
			}
			in.RunID, in.Token = uuid.NewV4().String(), uuid.NewV4().String()
			job, e = w.Run(t.Context(), in)
			if e != nil || job.Status != "complete" || len(c.documents) != 2 || len(pub.ids) != 2 {
				t.Fatalf("resume lost/duplicated records %+v %v", job, e)
			}
			c.job.Exhausted = false
			p.calls = 0
			in.RunID, in.Token = uuid.NewV4().String(), uuid.NewV4().String()
			if _, e = w.Run(t.Context(), in); e != nil {
				t.Fatal(e)
			}
			if len(c.documents) != 2 || len(pub.ids) != 2 || len(o.data) == 0 {
				t.Fatal("overlap duplicated stored sources")
			}
		})
	}
}
