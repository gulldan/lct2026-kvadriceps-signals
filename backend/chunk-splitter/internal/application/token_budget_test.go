package application

import (
	"context"
	"errors"
	"strings"
	"sync/atomic"
	"testing"
	"unicode/utf8"

	"github.com/example/chunk-splitter/internal/infrastructure/splitter"
	commonv1 "github.com/example/chunk-splitter/internal/platform/genproto/common/v1"
)

// wordTokenizer считает слова вместо токенов F2LLM-v2; calls доказывает, что работает кэш.
type wordTokenizer struct{ calls int64 }

func (w *wordTokenizer) CountTokens(_ context.Context, text string) (int, error) {
	atomic.AddInt64(&w.calls, 1)
	return len(strings.Fields(text)), nil
}

type failingTokenizer struct{ calls int64 }

func (f *failingTokenizer) CountTokens(_ context.Context, _ string) (int, error) {
	atomic.AddInt64(&f.calls, 1)
	return 0, errors.New("tokenizer unavailable")
}

func TestProcess_TokenBudget_RespectedAndProvenanceHolds(t *testing.T) {
	page1 := "Первая страница про буровые растворы и плотность жидкости, длинная фраза для нескольких чанков подряд."
	page2 := "Вторая страница описывает коррозию насосно-компрессорных труб и защиту от сероводорода в скважине надёжно."
	page3 := "Третья страница посвящена композитным материалам и температурной стойкости при высоких механических нагрузках."
	full := page1 + "\n" + page2 + "\n" + page3

	r2 := utf8.RuneCountInString(page1 + "\n")
	r3 := utf8.RuneCountInString(page1 + "\n" + page2 + "\n")
	raw := mustJSON(t, []int{0, r2, r3})

	const tokenBudget = 6 // «токены» (слова) на чанк

	tok := &wordTokenizer{}
	vecs := &capturingVectors{}
	ix := New(
		// Игнорируется при токенизаторе; огромный размер доказывает, что работает путь по токенам.
		splitter.NewRecursive(10000, 0),
		noopStatus{}, dummyEmbedder{}, vecs, noopSearch{}, noopPub{},
		nil, nil, // pacer, store
		tok, tokenBudget, // tokenizer, maxTokens — бюджет по токенам включён
		0, 8, // textMax, batchSize
		120, 4, // splitWindow в байтах, overlap в рунах
		true, // headers
		nil,  // metrics
	)

	evt := &commonv1.DocumentParsed{
		DocumentId: "doc-tok",
		OwnerId:    "u1",
		Filename:   "report.pdf",
		Text:       full,
		Metadata:   map[string]string{"page_offsets": string(raw)},
	}
	if err := ix.Process(context.Background(), evt); err != nil {
		t.Fatalf("Process: %v", err)
	}
	if len(vecs.points) == 0 {
		t.Fatal("no vector points captured — token-budget Process produced nothing")
	}
	if tok.calls == 0 {
		t.Fatal("tokenizer was never called — token-budget path not taken")
	}

	// Посимвольный сплиттер дал бы один чанк. Префикс перекрытия мешает мерить последующие,
	// поэтому бюджет проверяется только на первом.
	if len(vecs.points) < 3 {
		t.Fatalf("token budget %d should split this text into several chunks, got %d", tokenBudget, len(vecs.points))
	}
	if got := len(strings.Fields(vecs.points[0].Text)); got > tokenBudget {
		t.Fatalf("first chunk has %d word-tokens > budget %d: %q", got, tokenBudget, vecs.points[0].Text)
	}

	spans, pages := 0, 0
	for _, p := range vecs.points {
		if p.CharStart == 0 && p.CharEnd == 0 {
			if p.PageStart != 0 || p.PageEnd != 0 {
				t.Fatalf("unlocated chunk carries page %d-%d: %q", p.PageStart, p.PageEnd, p.Text)
			}
			continue
		}
		if got, want := normWS(sliceRunesAbs(full, p.CharStart, p.CharEnd)), normWS(p.Text); got != want {
			t.Fatalf("token-path offsets wrong: span %q != chunk %q", got, want)
		}
		spans++
		if p.PageStart != p.PageEnd {
			t.Fatalf("chunk spans pages %d..%d: %q", p.PageStart, p.PageEnd, p.Text)
		}
		if p.PageStart < 1 || p.PageStart > 3 {
			t.Fatalf("page %d out of range: %q", p.PageStart, p.Text)
		}
		pages++
	}
	if spans == 0 || pages == 0 {
		t.Fatal("nothing verified — expected located, page-annotated chunks under token budget")
	}
	t.Logf("token-budget Process: %d chunks, %d offset spans reproduced, %d single-page chunks, %d tokenizer calls",
		len(vecs.points), spans, pages, tok.calls)
}

func TestProcess_TokenizerError_FallsBackToRunes(t *testing.T) {
	full := strings.Join([]string{
		"Первый абзац о буровых растворах содержит двойные  пробелы и тире — вот так здесь.",
		"Второй абзац начинается здесь и описывает коррозию насосно-компрессорных труб подробно.",
	}, "\n\n")

	tok := &failingTokenizer{}
	vecs := &capturingVectors{}
	ix := New(
		splitter.NewRecursive(10000, 0), // игнорируется: задан токенизатор
		noopStatus{}, dummyEmbedder{}, vecs, noopSearch{}, noopPub{},
		nil, nil,
		tok, 40, // tokenizer с ошибками, maxTokens; запасная единица — руны
		0, 8,
		200, 8,
		true, // headers
		nil,
	)

	evt := &commonv1.DocumentParsed{
		DocumentId: "doc-fallback",
		OwnerId:    "u1",
		Filename:   "f.txt",
		Text:       full,
	}
	if err := ix.Process(context.Background(), evt); err != nil {
		t.Fatalf("Process must not fail when the tokenizer errors: %v", err)
	}
	if tok.calls == 0 {
		t.Fatal("failing tokenizer was never consulted — fallback path not exercised")
	}
	if len(vecs.points) == 0 {
		t.Fatal("tokenizer-error fallback produced no chunks; ingestion should still index")
	}
	if len(vecs.points) < 2 {
		t.Fatalf("expected the rune-fallback budget to split the text, got %d chunks", len(vecs.points))
	}
	for _, p := range vecs.points {
		if p.CharStart == 0 && p.CharEnd == 0 {
			continue
		}
		if got, want := normWS(sliceRunesAbs(full, p.CharStart, p.CharEnd)), normWS(p.Text); got != want {
			t.Fatalf("fallback offsets wrong: span %q != chunk %q", got, want)
		}
	}
	t.Logf("tokenizer-error fallback: indexed %d chunks via rune length, %d tokenizer attempts", len(vecs.points), tok.calls)
}

func TestProcess_TokenizerMemoized(t *testing.T) {
	// Повтор одинаковых предложений гарантирует одинаковые куски при слиянии.
	sentence := "буровой раствор плотность вязкость фильтрация скважина"
	full := strings.Join([]string{sentence, sentence, sentence, sentence, sentence}, "\n\n")

	tok := &wordTokenizer{}
	vecs := &capturingVectors{}
	ix := New(
		splitter.NewRecursive(10000, 0),
		noopStatus{}, dummyEmbedder{}, vecs, noopSearch{}, noopPub{},
		nil, nil,
		tok, 4, // маленький бюджет — много замеров повторяющихся кусков
		0, 8,
		400, 0, // одно окно без перекрытия: повторы только от текста
		true, // headers
		nil,
	)
	evt := &commonv1.DocumentParsed{DocumentId: "doc-memo", OwnerId: "u1", Filename: "f.txt", Text: full}
	if err := ix.Process(context.Background(), evt); err != nil {
		t.Fatalf("Process: %v", err)
	}
	// Без кэша токенизатор звался бы на каждый замер всех пяти копий; запас щедрый, но уложится только рабочий кэш.
	if tok.calls == 0 {
		t.Fatal("tokenizer never called")
	}
	if tok.calls > 30 {
		t.Fatalf("memoization appears ineffective: %d tokenizer calls for a 5x-repeated sentence", tok.calls)
	}
	t.Logf("memoized token-budget: %d tokenizer calls, %d chunks", tok.calls, len(vecs.points))
}
