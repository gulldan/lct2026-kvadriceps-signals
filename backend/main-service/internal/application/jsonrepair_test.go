package application

import (
	"encoding/json"
	"errors"
	"strings"
	"testing"

	commonv1 "github.com/example/main-service/internal/platform/genproto/common/v1"
	"github.com/example/main-service/internal/platform/jsonx"
)

func TestDecodeJSONObject(t *testing.T) {
	cases := []struct {
		name, answer, want, invalidRaw string
		missing                        bool
	}{
		{name: "wrapped", answer: "prose ```json\n{\"r\":\"result\"}\n``` trailing", want: "result"},
		{name: "latex", answer: `{"r":"\ce{H2O}"}`, want: `\ce{H2O}`},
		{name: "valid escapes", answer: `{"r":"a\nb \"q\" \\ A"}`, want: "a\nb \"q\" \\ A"},
		{name: "empty object", answer: "{}"},
		{name: "unknown field", answer: `{"unknown":true,"r":"kept"}`, want: "kept"},
		{name: "duplicate key", answer: `{"r":"first","r":"last"}`, want: "last"},
		{name: "no object", answer: "no JSON", missing: true},
		{name: "missing close", answer: `{"r":"value"`, missing: true},
		{name: "reversed braces", answer: "} text {", missing: true},
		{
			name: "first error after failed repair", answer: `{"r":"\ce","n":"bad"}`,
			invalidRaw: `{"r":"\ce","n":"bad"}`,
		},
		{
			name: "multiple objects", answer: `before {"r":"first"} {"r":"last"} after`,
			invalidRaw: `{"r":"first"} {"r":"last"}`,
		},
	}
	for name, unmarshal := range map[string]func([]byte, any) error{
		"json": json.Unmarshal, "jsonx": jsonx.Unmarshal,
	} {
		t.Run(name, func(t *testing.T) {
			for _, tc := range cases {
				t.Run(tc.name, func(t *testing.T) {
					var got struct {
						R string `json:"r"`
						N int    `json:"n"`
					}
					err := decodeJSONObject(tc.answer, &got, unmarshal)
					if tc.missing {
						if !errors.Is(err, errNoJSONObject) {
							t.Fatalf("error = %v, want missing-object sentinel", err)
						}
						return
					}
					if tc.invalidRaw != "" {
						wantErr := unmarshal([]byte(tc.invalidRaw), &got)
						if err == nil || wantErr == nil || err.Error() != wantErr.Error() {
							t.Fatalf("error = %v, want original error %v", err, wantErr)
						}
						return
					}
					if err != nil || got.R != tc.want {
						t.Fatalf("result = %q, error = %v; want %q, nil", got.R, err, tc.want)
					}
				})
			}
		})
	}
}

func TestRepairJSONBackslashes(t *testing.T) {
	cases := []struct{ name, in, want string }{
		{"latex chem", `{"r":"\ce{H2O}"}`, `{"r":"\\ce{H2O}"}`},
		{"latex sigma", `{"r":"\sigma=\alpha"}`, `{"r":"\\sigma=\\alpha"}`},
		{"valid escapes kept", `{"r":"a\nb \"q\" \\ A"}`, `{"r":"a\nb \"q\" \\ A"}`},
		{"already escaped", `{"r":"\\ce{CO2}"}`, `{"r":"\\ce{CO2}"}`},
		{"no backslash", `{"r":"plain text"}`, `{"r":"plain text"}`},
	}
	for _, c := range cases {
		if got := repairJSONBackslashes(c.in); got != c.want {
			t.Errorf("%s: repairJSONBackslashes(%q) = %q, want %q", c.name, c.in, got, c.want)
		}
	}
}

func TestRepairedJSONUnmarshals(t *testing.T) {
	// Неэкранированные слеши LaTeX ломают JSON; после починки он разбирается.
	bad := `[{"title":"T","statement":"S","rationale":"$E=mc^2$, \ce{H2O}"}]`
	var items []genItem
	if err := json.Unmarshal([]byte(bad), &items); err == nil {
		t.Fatal("expected raw output with unescaped LaTeX to be invalid JSON")
	}
	if err := json.Unmarshal([]byte(repairJSONBackslashes(bad)), &items); err != nil {
		t.Fatalf("repaired JSON should parse: %v", err)
	}
	if len(items) != 1 || items[0].Rationale != `$E=mc^2$, \ce{H2O}` {
		t.Fatalf("unexpected parse result: %+v", items)
	}
}

func TestParseGenItemsRecoversLatex(t *testing.T) {
	answer := `prose before [{"title":"T","statement":"S","rationale":"\ce{CO2} + \pu{5 g}"}] trailing`
	items, err := parseGenItems(answer)
	if err != nil {
		t.Fatalf("parseGenItems: %v", err)
	}
	if len(items) != 1 || items[0].Rationale != `\ce{CO2} + \pu{5 g}` {
		t.Fatalf("unexpected items: %+v", items)
	}
}

func TestParseGenItemsAcceptsWrappedAndSingleObjects(t *testing.T) {
	wrapped := "```json\n" +
		`{"hypotheses":[{"title":"T","statement":"S","rationale":"R"}]}` +
		"\n```"
	items, err := parseGenItems(wrapped)
	if err != nil || len(items) != 1 || items[0].Title != "T" {
		t.Fatalf("wrapped output should parse, items=%+v err=%v", items, err)
	}
	single := `{"title":"One","statement":"S","rationale":"R"}`
	items, err = parseGenItems(single)
	if err != nil || len(items) != 1 || items[0].Title != "One" {
		t.Fatalf("single object should parse, items=%+v err=%v", items, err)
	}
}

func TestParseGenItemsRecoversCompletedObjectsFromTruncatedArray(t *testing.T) {
	answer := "```json\n[" +
		`{"title":"One","statement":"S1","rationale":"R1","causal_chain":[{"stage":"KPI","change":"x"}]},` +
		`{"title":"Two","statement":"S2","rationale":"R2"},` +
		`{"title":"Broken","statement":"S3","rationale":"`
	items, err := parseGenItems(answer)
	if err != nil {
		t.Fatalf("truncated array should yield completed items: %v", err)
	}
	if len(items) != 2 || items[0].Title != "One" || items[1].Title != "Two" {
		t.Fatalf("unexpected partial items: %+v", items)
	}
}

func TestGenerationPromptsUseValidJSONExamples(t *testing.T) {
	genPrompt := buildGenPrompt(&commonv1.KPI{Title: "Повысить точность прогноза"}, 3, "", false)
	judgePrompt := buildJudgePrompt([]genItem{{
		Title:     "Модель X снизит ошибку прогноза",
		Statement: "Если применить модель X, то ошибка прогноза снизится.",
	}})
	for name, prompt := range map[string]string{"generator": genPrompt, "judge": judgePrompt} {
		if strings.Contains(prompt, "0..1") || strings.Contains(prompt, "1-9") {
			t.Fatalf("%s prompt must not contain invalid JSON numeric ranges", name)
		}
		if !strings.Contains(prompt, `"novelty":0.5`) || !strings.Contains(prompt, `"confidence":0.6`) {
			t.Fatalf("%s prompt should show concrete JSON numbers", name)
		}
	}
}

func TestFirstSentenceIsRuneSafe(t *testing.T) {
	got := firstSentence("Точность сегментации повышается при адаптации модели.", 12)
	if got == "" || got == "Точность сег�" {
		t.Fatalf("firstSentence produced broken unicode: %q", got)
	}
}

func TestSelectEvidenceGroupsChunksByArticle(t *testing.T) {
	item := genItem{
		Statement: "Проверить SAM сегментацию КТ и экономный inference",
		Rationale: "SAM segmentation CT inference",
	}
	sources := []*commonv1.Source{
		{DocumentId: "doc-1", ChunkId: "c1", Filename: "sam.pdf", Snippet: "SAM segmentation CT", Score: 0.95},
		{DocumentId: "doc-1", ChunkId: "c2", Filename: "sam.pdf", Snippet: "SAM CT inference", Score: 0.90},
		{DocumentId: "doc-1", ChunkId: "c3", Filename: "sam.pdf", Snippet: "SAM extra CT", Score: 0.85},
		{DocumentId: "doc-2", ChunkId: "c4", Filename: "inference.pdf", Snippet: "economic inference CT", Score: 0.80},
	}

	got := selectEvidence(sources, item)
	if len(got) != 3 {
		t.Fatalf("selectEvidence() len = %d, want 3 (2 chunks from doc-1 + 1 from doc-2)", len(got))
	}
	perDoc := map[string]int{}
	for _, ev := range got {
		perDoc[ev.GetDocumentId()]++
	}
	if perDoc["doc-1"] != genEvidenceChunks || perDoc["doc-2"] != 1 {
		t.Fatalf("unexpected grouping: %+v", perDoc)
	}
}
