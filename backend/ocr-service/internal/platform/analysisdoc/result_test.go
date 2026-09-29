package analysisdoc_test

import (
	"context"
	"encoding/json/jsontext"
	"encoding/json/v2"
	"errors"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/example/ocr-service/internal/platform/analysisdoc"
	"github.com/example/ocr-service/internal/platform/indexdoc"
)

type resultObjects map[string]string

func (s resultObjects) Get(_ context.Context, key string) (io.ReadCloser, error) {
	raw, ok := s[key]
	if !ok {
		return nil, errors.New("missing artifact")
	}
	return io.NopCloser(strings.NewReader(raw)), nil
}

func resultFixture() (analysisdoc.Input, analysisdoc.Job, analysisdoc.Result, resultObjects) {
	text := "Проблема. Method reduces latency by 10%. Research example."
	input := analysisdoc.Input{RunID: "input", Request: analysisdoc.Request{Query: "RAG", Cutoff: "2025-12-31T23:59:59Z"},
		Records: []analysisdoc.Record{{ID: "doc1", Index: indexdoc.Manifest{SnapshotID: "s1", SourceRevision: "v1",
			Text: indexdoc.Object{Key: "text1", SHA256: indexdoc.Hash([]byte(text))}}}, {ID: "doc2"}}}
	job := analysisdoc.Job{ID: "job", InputRunID: "input", InputSHA256: strings.Repeat("a", 64),
		MethodSHA256: strings.Repeat("b", 64)}
	c := analysisdoc.Candidate{ID: "candidate", Title: "RAG", WorkIDs: []string{"w1", "w2"},
		Measurements: jsontext.Value(`{"series":[]}`),
		Assessment:   jsontext.Value(`{"eligible":true,"order":[1,0.2,2,2]}`), CompleteForBrief: true,
		Relevance: analysisdoc.Relevance{Version: "test", Relevant: true, LexicalOverlap: 1, Mode: "lexical_fallback"},
		Brief:     analysisdoc.Brief{DescriptionStatus: "template", EvidenceSHA256: strings.Repeat("c", 64)}}
	for i, field := range []string{"problem", "advantage", "example"} {
		e := analysisdoc.Evidence{ID: field, Field: field, WorkID: "w1", DocumentID: "doc1", SnapshotID: "s1", SourceRevision: "v1",
			TextSHA256: input.Records[0].Index.Text.SHA256, Start: 0, End: len([]rune(text)), Quote: text, ClaimType: "research_claim"}
		c.Evidence = append(c.Evidence, e)
		f := &analysisdoc.BriefField{Text: text, EvidenceIDs: []string{field}}
		switch i {
		case 0:
			c.Brief.Problem = f
		case 1:
			c.Brief.Advantage = f
		case 2:
			c.Brief.Example = f
		}
	}
	r := analysisdoc.Result{Version: analysisdoc.ResultVersion, JobID: job.ID, InputRunID: input.RunID, InputSHA256: job.InputSHA256,
		MethodSHA256: job.MethodSHA256, Query: input.Request.Query, Cutoff: input.Request.Cutoff,
		Candidates: []analysisdoc.Candidate{c},
		Works: []analysisdoc.Work{{ID: "w1", Kind: "paper", DocumentIDs: []string{"doc1"}},
			{ID: "w2", Kind: "paper", DocumentIDs: []string{"doc2"}}}, Coverage: jsontext.Value(`{"status":"controlled_test"}`),
		Ranking: analysisdoc.Ranking{OrderedIDs: []string{"candidate"}, Completeness: "insufficient_coverage",
			Reasons: []string{"only_one"}}}
	return input, job, r, resultObjects{"text1": text}
}

func TestResultPublicationBoundary(t *testing.T) {
	input, job, result, store := resultFixture()
	raw, err := analysisdoc.MarshalResult(result)
	if err != nil || analysisdoc.ValidateResult(raw, job, input) != nil ||
		analysisdoc.VerifyResultSources(t.Context(), store, input, raw) != nil {
		t.Fatal("valid source-bound result rejected", err)
	}
	cases := map[string]func(*analysisdoc.Result){
		"mixed run":    func(r *analysisdoc.Result) { r.InputRunID = "other" },
		"missing work": func(r *analysisdoc.Result) { r.Works = r.Works[:1] },
		"overlapping candidates": func(r *analysisdoc.Result) {
			c := r.Candidates[0]
			c.ID = "second"
			r.Candidates = append(r.Candidates, c)
		},
		"missing advantage":       func(r *analysisdoc.Result) { r.Candidates[0].Brief.Advantage = nil },
		"duplicate ranked topic":  func(r *analysisdoc.Result) { r.Ranking.OrderedIDs = append(r.Ranking.OrderedIDs, "candidate") },
		"false full list":         func(r *analysisdoc.Result) { r.Ranking.Completeness = "complete" },
		"wrong revision":          func(r *analysisdoc.Result) { r.Candidates[0].Evidence[0].SnapshotID = "latest" },
		"draft description":       func(r *analysisdoc.Result) { r.Candidates[0].Brief.DescriptionStatus = "draft" },
		"invented template claim": func(r *analysisdoc.Result) { r.Candidates[0].Brief.Advantage.Text = "10x faster" },
		"nonfinite numeric":       func(r *analysisdoc.Result) { r.Candidates[0].Measurements = jsontext.Value(`{"rate":1e999}`) },
	}
	for name, change := range cases {
		t.Run(name, func(t *testing.T) {
			var copyResult analysisdoc.Result
			if err := json.Unmarshal([]byte(raw), &copyResult); err != nil {
				t.Fatal(err)
			}
			change(&copyResult)
			changed, err := analysisdoc.MarshalResult(copyResult)
			if err == nil && analysisdoc.ValidateResult(changed, job, input) == nil {
				t.Fatal("invalid projection accepted")
			}
		})
	}
}

func TestResultQuoteIsVerifiedIndependently(t *testing.T) {
	input, job, result, store := resultFixture()
	result.Candidates[0].Evidence[0].Quote = strings.ReplaceAll(result.Candidates[0].Evidence[0].Quote, "10%", "10x")
	result.Candidates[0].Brief.Problem.Text = result.Candidates[0].Evidence[0].Quote
	raw, err := analysisdoc.MarshalResult(result)
	if err != nil || analysisdoc.ValidateResult(raw, job, input) != nil {
		t.Fatal("shape should remain valid", err)
	}
	if analysisdoc.VerifyResultSources(t.Context(), store, input, raw) == nil {
		t.Fatal("real citation accepted a changed number")
	}
	result.Candidates[0].Evidence[0].Quote = strings.ReplaceAll(result.Candidates[0].Evidence[0].Quote, "10x", "10%")
	result.Candidates[0].Brief.Problem.Text = result.Candidates[0].Evidence[0].Quote
	raw, err = analysisdoc.MarshalResult(result)
	if err != nil {
		t.Fatal(err)
	}
	delete(store, "text1")
	if analysisdoc.VerifyResultSources(t.Context(), store, input, raw) == nil {
		t.Fatal("missing source artifact accepted")
	}
}

func TestMMRMethodCannotOmitIndependentDiversityVerification(t *testing.T) {
	input, job, result, _ := resultFixture()
	job.MethodJSON = `{"parameters":{"ranking_version":"source-centroid-mmr-v1-development"}}`
	raw, err := analysisdoc.MarshalResult(result)
	if err != nil {
		t.Fatal(err)
	}
	if analysisdoc.ValidateResult(raw, job, input) == nil {
		t.Fatal("new method bypassed source-vector verification by omitting diversity")
	}
	job.MethodJSON = `{"parameters":{}}`
	if analysisdoc.ValidateResult(raw, job, input) != nil {
		t.Fatal("old immutable method is no longer readable")
	}
}

func TestRecordedPythonProjection(t *testing.T) {
	dir := os.Getenv("TR_PRODUCER_CAPTURE")
	if dir == "" {
		t.Skip("captured real producer input/result required")
	}
	inputRaw, err := os.ReadFile(filepath.Join(dir, "producer-input.json"))
	if err != nil {
		t.Fatal(err)
	}
	input, err := analysisdoc.Decode[analysisdoc.Input](string(inputRaw))
	if err != nil {
		t.Fatal(err)
	}
	jobRaw, err := os.ReadFile(filepath.Join(dir, "producer-job.json"))
	if err != nil {
		t.Fatal(err)
	}
	job, err := analysisdoc.Decode[analysisdoc.Job](string(jobRaw))
	if err != nil {
		t.Fatal(err)
	}
	resultRaw, err := os.ReadFile(filepath.Join(dir, "producer-result.json"))
	if err != nil {
		t.Fatal(err)
	}
	result, err := analysisdoc.Decode[analysisdoc.Result](string(resultRaw))
	if err != nil {
		t.Fatal(err)
	}
	err = analysisdoc.ValidateResult(string(resultRaw), job, input)
	if os.Getenv("TR_PRODUCER_EXPECT_INVALID") == "true" {
		if err == nil {
			t.Fatal("known incomplete producer artifact accepted")
		}
		return
	}
	if err != nil {
		t.Fatal("real producer rejected", err)
	}
	t.Logf("real producer: records=%d candidates=%d relations=%d", len(input.Records), len(result.Candidates), len(result.Relations))
}

func TestRunVersions(t *testing.T) {
	method, err := analysisdoc.ParseMethod(analysisdoc.DevelopmentMethod)
	if err != nil {
		t.Fatal(err)
	}
	input := analysisdoc.Input{
		Records: []analysisdoc.Record{
			{ID: "doc1", Index: indexdoc.Manifest{ParserRevision: "pdf-v4"}},
			{ID: "doc2", Index: indexdoc.Manifest{ParserRevision: "html-v2"}},
		},
		Model: &indexdoc.Model{Name: "f2llm", Revision: "r1", Dimension: 1024},
	}

	versions, err := analysisdoc.Versions(method, input)
	if err != nil {
		t.Fatal(err)
	}
	if versions.Parser != "html-v2+pdf-v4" || versions.Embedding != "f2llm@r1/1024" ||
		versions.Extraction == "" || versions.Identity == "" || versions.Clustering == "" ||
		!strings.Contains(versions.Metrics, "/") {
		t.Fatalf("incomplete run versions: %+v", versions)
	}

	input.Model = nil
	input.Records[0].Index.ParserRevision, input.Records[1].Index.ParserRevision = "", ""
	unknown, err := analysisdoc.Versions(method, input)
	if err != nil || unknown.Embedding != "no_embedding_model" || unknown.Parser != "no_parsed_records" {
		t.Fatalf("missing components must be named, not empty: %+v %v", unknown, err)
	}

	method.Parameters = jsontext.Value(`{"brief_version":"b","candidates_version":"c"}`)
	if _, err = analysisdoc.Versions(method, input); err == nil {
		t.Fatal("a method without its metrics and identity versions produced a run")
	}
}
