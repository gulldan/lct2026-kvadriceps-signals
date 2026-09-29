package main

import (
	"testing"
	"time"
)

func TestHuggingFaceRowsParseCatalogueJSON(t *testing.T) {
	source := webSource{Name: "HF"}
	papers := []byte(`[{"paper":{"id":"2609.21788","title":" Real-World  Subtask RL ","summary":"Abstract.",
		"publishedAt":"2026-09-18T00:00:00.000Z","upvotes":3}},
		{"paper":{"id":"2609.99999","title":"Future","publishedAt":"2026-09-30T00:00:00.000Z"}},
		{"paper":{"id":"","title":"No id","publishedAt":"2026-09-18T00:00:00.000Z"}}]`)
	rows, err := hfPaperRows(papers, source, "2026-09-21")
	if err != nil || len(rows) != 1 {
		t.Fatalf("статьи: %v %+v", err, rows)
	}
	if rows[0].URL != "https://huggingface.co/papers/2609.21788" || rows[0].Date != "2026-09-18" ||
		rows[0].Title != "Real-World Subtask RL" || rows[0].Text != "Abstract. Голосов сообщества Hugging Face: 3." {
		t.Fatalf("запись статьи: %+v", rows[0])
	}
	models := []byte(`[{"id":"org/laya","likes":1623,"trendingScore":1585,"createdAt":"2026-09-18T05:05:55.000Z",
		"pipeline_tag":"text-classification","library_name":"transformers","tags":["guardrails","routing"]},
		{"id":"org/private","private":true,"createdAt":"2026-09-18T05:05:55.000Z"}]`)
	rows, err = hfModelRows(models, source, "2026-09-21")
	if err != nil || len(rows) != 1 || rows[0].URL != "https://huggingface.co/org/laya" || rows[0].Date != "2026-09-18" {
		t.Fatalf("модели: %v %+v", err, rows)
	}
	if rows[0].Text != "Задача: text-classification. Библиотека: transformers. Теги: guardrails, routing. "+
		"Отметок «нравится»: 1623, индекс тренда: 1585, загрузок: 0." {
		t.Fatalf("текст модели: %q", rows[0].Text)
	}
}

func TestMergeRetainedKeepsRecordsOfConfiguredSources(t *testing.T) {
	now := time.Date(2026, 9, 21, 12, 0, 0, 0, time.UTC)
	web := &service{kind: kindWeb, config: sourceConfig{Web: []webSource{{Name: "N+1"}}}}
	old := &snapshot{SourceSet: "old", ObservedAt: "2026-09-20T00:00:00Z", Records: []record{
		{ID: "a", Source: "N+1", Date: "2026-09-10"}, {ID: "b", Source: "Ушедшая лента", Date: "2026-09-10"}}}
	data := &snapshot{SourceSet: "new", ObservedAt: "2026-09-21T00:00:00Z", From: "2026-01-01", Through: "2026-09-21"}
	mergeRetained(data, old, now, web.retained)
	if len(data.Records) != 1 || data.Records[0].ID != "a" {
		t.Fatalf("удержание по лентам: %+v", data.Records)
	}
	patents := &service{kind: kindCNPatents}
	data = &snapshot{SourceSet: "new", ObservedAt: "2026-09-21T00:00:00Z", From: "2026-01-01", Through: "2026-09-21"}
	mergeRetained(data, old, now, patents.retained)
	if len(data.Records) != 2 {
		t.Fatalf("патентные записи потеряны при смене состава: %+v", data.Records)
	}
}
