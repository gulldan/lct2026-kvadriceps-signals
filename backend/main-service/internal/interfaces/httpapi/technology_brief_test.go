package httpapi_test

import (
	"context"
	"errors"
	"net/http"
	"strings"
	"sync"
	"testing"

	"github.com/example/main-service/internal/application"
	"github.com/example/main-service/internal/platform/extractdoc"
)

type briefCatalogue struct{ technology extractdoc.Technology }

func (c briefCatalogue) ReadTechnology(_ context.Context, read extractdoc.ReadTechnology) (extractdoc.Technology, error) {
	if read.TechnologyID != c.technology.TechnologyID {
		return extractdoc.Technology{}, extractdoc.ErrNotFound
	}
	return c.technology, nil
}

func (briefCatalogue) ReadRevision(context.Context, extractdoc.ReadRevision) (extractdoc.RevisionTechnologies, error) {
	return extractdoc.RevisionTechnologies{}, errors.ErrUnsupported
}

func (briefCatalogue) Search(context.Context, extractdoc.SearchTechnologies) (extractdoc.TechnologyPage, error) {
	return extractdoc.TechnologyPage{}, errors.ErrUnsupported
}

func (briefCatalogue) Review(context.Context, extractdoc.Review) (extractdoc.Review, error) {
	return extractdoc.Review{}, errors.ErrUnsupported
}

func (briefCatalogue) Reviews(context.Context, extractdoc.ReadReviews) ([]extractdoc.Review, error) {
	return nil, errors.ErrUnsupported
}

type briefWriter struct {
	mu    sync.Mutex
	calls int
	input application.TechnologyBriefInput
}

func (w *briefWriter) write(_ context.Context, in application.TechnologyBriefInput) (application.TechnologyBrief, error) {
	w.mu.Lock()
	defer w.mu.Unlock()
	w.calls++
	w.input = in
	return application.TechnologyBrief{TechnologyID: in.TechnologyID, Hypothesis: "Если …, то …, потому что …"}, nil
}

func TestTechnologyBriefIsWrittenOnPostAndSurvivesRestart(t *testing.T) {
	h := newHarness(t)
	h.api.SetTechnologies(briefCatalogue{technology: extractdoc.Technology{TechnologyID: "t1",
		CanonicalName: "Voice replay detection", Aliases: []extractdoc.Alias{{Value: "nonce challenge"}},
		Mentions: []extractdoc.Mention{
			{MentionID: "m1", SourceRevisionID: "r1", SourceTitle: "Paper", Problem: "replay attacks",
				Evidence: []extractdoc.Quote{{Quote: "nonce challenges detect replay"}}},
			{MentionID: "m2", SourceRevisionID: "r1", Inputs: []string{"audio"}},
		}}})
	writer := &briefWriter{}
	h.api.SetTechnologyBrief(writer.write, h.store)
	token := h.token(t, "u1", "expert")
	if r := h.do(t, http.MethodGet, "/technologies/t1/brief", token, ""); r.code != http.StatusNoContent {
		t.Fatalf("brief before writing: %d %s", r.code, r.body)
	}
	if r := h.do(t, http.MethodPost, "/technologies/t1/brief", token, ""); r.code != http.StatusOK ||
		!strings.Contains(r.body, `"hypothesis":"Если`) {
		t.Fatalf("write brief: %d %s", r.code, r.body)
	}
	// Новая установка — как после выкладки: память пуста, бриф приходит из хранилища без вызова модели.
	h.api.SetTechnologyBrief(writer.write, h.store)
	if r := h.do(t, http.MethodGet, "/technologies/t1/brief", token, ""); r.code != http.StatusOK ||
		!strings.Contains(r.body, `"hypothesis":"Если`) {
		t.Fatalf("stored brief: %d %s", r.code, r.body)
	}
	writer.mu.Lock()
	defer writer.mu.Unlock()
	in := writer.input
	if writer.calls != 1 || len(in.Sources) != 1 || in.Aliases[0] != "nonce challenge" ||
		in.Sources[0].Problem != "replay attacks" || len(in.Sources[0].Inputs) != 1 ||
		in.Sources[0].Quotes[0] != "nonce challenges detect replay" {
		t.Fatalf("writer calls = %d, input = %+v", writer.calls, in)
	}
}
