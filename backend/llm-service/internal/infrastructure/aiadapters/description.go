package aiadapters

import (
	"context"
	"encoding/json/v2"
	"strings"
	"time"

	"github.com/example/llm-service/internal/platform/aiclients"
	"github.com/example/llm-service/internal/platform/analysisdoc"
)

type Describer struct {
	client aiclients.Generator
	usage  UsageRecorder
}

func NewDescriber(client aiclients.Generator, usage UsageRecorder) *Describer {
	return &Describer{client: client, usage: usage}
}

// Describe отдаёт проверенную атрибуцию или точный шаблон, но никогда — сырой черновик модели.
func (d *Describer) Describe(ctx context.Context, b analysisdoc.FactBundle) analysisdoc.DescriptionResult {
	if b.Validate() != nil {
		return b.Template("unverified_paraphrase")
	}
	if len(b.Evidence) == 0 {
		return b.Template("no_facts")
	}
	ctx, cancel := context.WithTimeout(ctx, analysisdoc.DescriptionTimeout)
	defer cancel()
	raw, _ := b.Bytes()
	started := time.Now()
	response, err := d.client.Generate(ctx, aiclients.GenRequest{System: analysisdoc.DescriptionPrompt, Context: string(raw),
		Query:     "Format the supplied source facts using the required JSON schema.",
		MaxTokens: 4096, Temperature: 0, JSONMode: true})
	ms := min(time.Since(started).Milliseconds(), 90000)
	fallback := b.Template("model_unavailable")
	fallback.LatencyMS = ms
	if err != nil {
		return fallback
	}
	d.record(ctx, response)
	if len(response.Model) <= 200 && !strings.ContainsAny(response.Model, "\r\n\x00") {
		fallback.Model = response.Model
	}
	var draft analysisdoc.DescriptionFields
	var keys map[string]any
	result := analysisdoc.DescriptionResult{BundleSHA256: b.SHA256(), Status: "validated", Fields: draft,
		Model: response.Model, PromptSHA256: b.PromptSHA256, LatencyMS: ms}
	switch {
	case len(response.Text) > analysisdoc.MaxDescriptionBytes ||
		json.Unmarshal([]byte(response.Text), &draft, json.RejectUnknownMembers(true)) != nil ||
		json.Unmarshal([]byte(response.Text), &keys) != nil || len(keys) != 3:
		fallback.Reason = "invalid_json"
		result = fallback
	default:
		result.Fields = draft
		if result.Check(b) != nil {
			fallback.Reason = "unverified_paraphrase"
			result = fallback
		}
	}
	// Название и пересказ опираются только на цитаты, поэтому их получает и отклонённая атрибуция;
	// точные факты читатель видит в любом случае.
	d.summarize(ctx, b, &result)
	result.LatencyMS = min(time.Since(started).Milliseconds(), 90000)
	return result
}

func (d *Describer) record(ctx context.Context, response aiclients.GenResponse) {
	if d.usage != nil {
		d.usage.Record(ctx, response.Model, "signal_description",
			response.Usage.PromptTokens, response.Usage.CompletionTokens, response.Usage.CostUSD)
	}
}

type summaryQuote struct {
	Field string `json:"field"`
	Quote string `json:"quote"`
}

type summaryFacts struct {
	Quotes  []summaryQuote `json:"quotes"`
	Sources []string       `json:"sources"`
}

func (d *Describer) summarize(ctx context.Context, b analysisdoc.FactBundle, result *analysisdoc.DescriptionResult) {
	facts := summaryFacts{Quotes: make([]summaryQuote, 0, len(b.Evidence)), Sources: make([]string, 0, len(b.Sources))}
	for _, e := range b.Evidence {
		facts.Quotes = append(facts.Quotes, summaryQuote{Field: e.Field, Quote: e.Quote})
	}
	for _, s := range b.Sources {
		facts.Sources = append(facts.Sources, s.Title)
	}
	raw, err := json.Marshal(facts)
	if err != nil {
		return
	}
	response, err := d.client.Generate(ctx, aiclients.GenRequest{System: analysisdoc.DescriptionSummaryPrompt,
		Context: string(raw), Query: "Name and summarise the topic of the supplied quotes in Russian using the required JSON schema.",
		MaxTokens: 1024, Temperature: 0, JSONMode: true})
	if err != nil {
		return
	}
	d.record(ctx, response)
	start, end := strings.Index(response.Text, "{"), strings.LastIndex(response.Text, "}")
	var draft struct {
		Label   string `json:"label"`
		Summary string `json:"summary"`
	}
	if len(response.Text) > analysisdoc.MaxDescriptionBytes || start < 0 || end <= start ||
		json.Unmarshal([]byte(response.Text[start:end+1]), &draft) != nil {
		return
	}
	label := strings.Trim(strings.Join(strings.Fields(draft.Label), " "), `"«»“” .`)
	candidate := *result
	candidate.Label = clip(label, analysisdoc.MaxLabelRunes, " ")
	candidate.Summary = clip(strings.Join(strings.Fields(draft.Summary), " "), analysisdoc.MaxSummaryRunes, ".!? ")
	if candidate.Check(b) == nil {
		*result = candidate
	}
}

func clip(s string, limit int, ends string) string {
	runes := []rune(s)
	if len(runes) <= limit {
		return s
	}
	cut := string(runes[:limit])
	if i := strings.LastIndexAny(cut, ends); i > 0 {
		cut = cut[:i+1]
	}
	return strings.TrimSpace(cut)
}
