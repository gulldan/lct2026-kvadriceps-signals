package main

import (
	"strings"
	"testing"
	"time"
	"unicode/utf8"
)

func TestArticlePreservesPublisherLanguage(t *testing.T) {
	text := "研究チームは新しい量子材料を開発しました。実験では電力消費を抑えながら計算精度を維持し、再現性を確認しました。" +
		"論文には測定方法、比較条件、誤差の範囲が公開されています。材料の構造と性能の関係を調べ、将来の製造工程に応用することを目指しています。"
	raw := []byte(`<html lang="ja"><head><title>量子材料の研究</title></head><body><main><article><p>` + text +
		`</p></article></main></body></html>`)
	row, err := articleDocument(raw, record{URL: "https://lab.example/research"}, "2026-09-13T00:00:00Z")
	if err != nil || row.Language != "ja" || !strings.Contains(row.Text, text) {
		t.Fatalf("publisher language or original Japanese text lost: language=%q text=%q err=%v", row.Language, row.Text, err)
	}
}

func TestFeedPreservesPublisherCalendarDay(t *testing.T) {
	raw := []byte(`<rss><channel><item><title>量子材料の研究</title><link>https://lab.example/research</link>` +
		`<pubDate>Thu, 10 Sep 2026 00:00:00 +0900</pubDate></item></channel></rss>`)
	rows, err := feedRows(raw, feed{Name: "Japanese laboratory", URL: "https://lab.example/feed"}, "2026-09-10")
	if err != nil || len(rows) != 1 || rows[0].Date != "2026-09-10" || rows[0].PublishedAt != "2026-09-09T15:00:00Z" {
		t.Fatalf("publication calendar day or UTC instant changed: rows=%+v err=%v", rows, err)
	}
}

// Сеть — только по явному включению; TR_SOURCE_PROOF_DIR сохраняет сырые байты и записи для разбора.
func TestLiveResearchSources(t *testing.T) {
	for _, source := range researchSources() {
		t.Run(source.Name, func(t *testing.T) {
			s, data, ctx := liveSourceService(t, "web")
			source.Pages = 2
			s.config.Web = []webSource{source}
			s.config.AllowedDomains = nil
			s.config.RequestBudget = 8
			s.http = newWebClient(40*time.Second, s.config.domains())
			data.Source, data.SourceSet = source.Name, s.config.fingerprint("web")
			data.From = time.Now().UTC().AddDate(0, -13, 0).Format(time.DateOnly)
			s.feedHistory(ctx, source, data, map[string]record{})
			withText := 0
			for _, row := range data.Records {
				if row.TextStatus != articleText {
					continue
				}
				verifyLiveRecord(t, row)
				if utf8.RuneCountInString(row.Text) < 200 {
					t.Errorf("publisher article too short to verify: %s (%d characters)", row.URL, utf8.RuneCountInString(row.Text))
				}
				withText++
				t.Logf("date=%s title=%q characters=%d anchors=%d url=%s", row.Date, row.Title,
					utf8.RuneCountInString(row.Text), len(row.Anchors), row.URL)
			}
			if withText == 0 {
				t.Fatalf("no useful publisher article; coverage=%+v", data.Coverage)
			}
			liveExport(t, s, data, "research")
		})
	}
}
