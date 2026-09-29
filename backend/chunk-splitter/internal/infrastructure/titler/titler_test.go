package titler

import (
	"context"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/example/chunk-splitter/internal/platform/aiclients"
	"github.com/example/chunk-splitter/internal/platform/jsonx"
)

type fakeGen struct {
	text    string
	err     error
	gotCtx  string
	gotSys  string
	gotToks int
}

func (f *fakeGen) Generate(_ context.Context, req aiclients.GenRequest) (aiclients.GenResponse, error) {
	f.gotCtx, f.gotSys, f.gotToks = req.Context, req.System, req.MaxTokens
	return aiclients.GenResponse{Text: f.text, Model: "m"}, f.err
}

func (f *fakeGen) Stream(
	ctx context.Context, req aiclients.GenRequest, _ func(string) error,
) (aiclients.GenResponse, error) {
	return f.Generate(ctx, req)
}

func TestExtract(t *testing.T) {
	cases := []struct{ name, reply, want, wantKind string }{
		{"plain title", "Циклическая стабильность катодов", "Циклическая стабильность катодов", ""},
		{"quoted multiline", "\"Название статьи\"\nВторая строка", "Название статьи", ""},
		{"declined", "NONE", "", ""},
		{"too long", strings.Repeat("щ", 301), "", ""},
		{"labelled", "TITLE: Гипотезы КГМК\nKIND: hypotheses", "Гипотезы КГМК", "hypotheses"},
		{"labelled normal", "TITLE: NONE\nKIND: normal", "", ""},
		{"bare label line", "TITLE\nRetrieval Augmented Generation using Engineering Design Knowledge\nKIND\nnormal", "", ""},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			gen := &fakeGen{text: tc.reply}
			got, gotKind := New(gen, nil).Extract(context.Background(), "Заголовок и аннотация.")
			if got != tc.want || gotKind != tc.wantKind {
				t.Fatalf("Extract = %q, %q, want %q, %q", got, gotKind, tc.want, tc.wantKind)
			}
			if gen.gotSys == "" || gen.gotCtx == "" || gen.gotToks != 128 {
				t.Fatalf("request not shaped: sys=%q ctx=%q toks=%d", gen.gotSys, gen.gotCtx, gen.gotToks)
			}
		})
	}
}

func TestExtract_EmptyAndError(t *testing.T) {
	if got, kind := New(&fakeGen{text: "x"}, nil).Extract(context.Background(), "   "); got != "" || kind != "" {
		t.Fatalf("empty text must yield empty results, got %q, %q", got, kind)
	}
	gen := &fakeGen{err: errors.New("backend down")}
	if got, kind := New(gen, nil).Extract(context.Background(), "text"); got != "" || kind != "" {
		t.Fatalf("generator error must yield empty results, got %q, %q", got, kind)
	}
}

func TestExtract_OpeningIsRuneBounded(t *testing.T) {
	long := strings.Repeat("锂", openingRunes+100)
	gen := &fakeGen{text: "NONE"}
	_, _ = New(gen, nil).Extract(context.Background(), long)
	if got := len([]rune(gen.gotCtx)); got != openingRunes {
		t.Fatalf("opening runes = %d, want %d", got, openingRunes)
	}
}

func TestGenerator_SendsReasoningEffort(t *testing.T) {
	bodies := make(chan map[string]any, 1)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var body map[string]any
		_ = jsonx.NewDecoder(r.Body).Decode(&body)
		bodies <- body
		_, _ = io.WriteString(w, `{"choices":[{"message":{"content":"TITLE: Название\nKIND: normal"}}]}`)
	}))
	defer srv.Close()
	t.Setenv("VLLM_URL", srv.URL)
	t.Setenv("VLLM_REASONING_EFFORT", "none")
	if got, _ := New(Generator(), nil).Extract(t.Context(), "Название и аннотация."); got != "Название" {
		t.Fatalf("Extract = %q, want Название", got)
	}
	if body := <-bodies; body["reasoning_effort"] != "none" {
		t.Fatalf("reasoning_effort = %v, want none", body["reasoning_effort"])
	}
}
