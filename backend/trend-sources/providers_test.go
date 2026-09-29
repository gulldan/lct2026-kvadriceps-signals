package main

import (
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"

	"golang.org/x/text/encoding/charmap"
)

func TestGazetteRejectsAliasesAndOtherIssueDates(t *testing.T) {
	body := []byte(`<p>LIST OF PATENTEES</p><div class="list_item1"><a name="A">Inventor, A, to Org</a> ` +
		`Solid-state batteries <a href="../html/1550-2/US12729125-20260908.html">12729125</a> Cl. H01M</div>` +
		`<div class="list_item2">Alias <a href="../html/1550-2/US12729125-20260908.html">12729125</a></div>` +
		`<div class="list_item1"><a>Old inventor</a> An older grant ` +
		`<a href="../html/1550-2/US12345678-20250908.html">12345678</a></div>` +
		``)
	rows, err := patentRows(body, issue{gazette + "/week36", "2026-09-08"})
	if err != nil || len(rows) != 1 || rows[0].Title != "Solid-state batteries" ||
		rows[0].URL != gazette+"/week36/OG/html/1550-2/US12729125-20260908.html" {
		t.Fatalf("wrong patents: %+v %v", rows, err)
	}
	if _, err = patentRows([]byte(`<html>Access denied</html>`), issue{}); err == nil {
		t.Fatal("access-denied page accepted")
	}
	// Имя компании может содержать запрос, а изобретение — нет.
	ambiguous, err := patentRows([]byte(`<p>LIST OF PATENTEES</p><div class="list_item1">Smith, A.; to CRISPR Inc. `+
		`Water filter <a href="../html/1550-2/US12729125-20260908.html">12729125</a></div>`),
		issue{gazette + "/week36", "2026-09-08"})
	if err != nil || len(ambiguous) != 1 || ambiguous[0].Citation == "" || matches(ambiguous[0].Title, []string{"CRISPR"}) {
		t.Fatalf("an ambiguous citation became a matched invention: %+v %v", ambiguous, err)
	}
}

func TestFeedDatesAndWholePhraseMatching(t *testing.T) {
	body := []byte(`<rss><channel><item><title>New gene-editing technology</title><link>https://example.org/one</link><` +
		`pubDate>Wed, 09 Sep 2026 23:00:00 -0400</pubDate></item><item><title>Future</title><` +
		`link>https://example.org/future</link><pubDate>Fri, 11 Sep 2026 10:00:00 +0000</pubDate></item><item><` +
		`title>Undated</title><link>https://example.org/unknown</link></item><item><title>Unsafe link</title><` +
		`link>javascript:alert(1)</link><pubDate>Wed, 09 Sep 2026 10:00:00 +0000</pubDate></item></channel></rss>`)
	rows, err := feedRows(body, feed{Name: "Lab"}, "2026-09-10")
	if err != nil || len(rows) != 1 || rows[0].Date != "2026-09-09" || rows[0].PublishedAt != "2026-09-10T03:00:00Z" {
		t.Fatalf("dates: %+v %v", rows, err)
	}
	if !matches(rows[0].Title, []string{"gene editing"}) || matches("Fragments of a battery", []string{"RAG"}) {
		t.Fatal("phrase match uses substrings or misses hyphens")
	}
}

func TestCutOffFeedKeepsWholeItems(t *testing.T) {
	item := func(n string) string {
		return `<item><title>Pilot ` + n + `</title><link>https://example.org/` + n + `</link><pubDate>` +
			`Tue, 08 Sep 2026 10:00:00 +0000</pubDate></item>`
	}
	for _, cut := range []string{
		`<rss><channel>` + item("1") + item("2") + `<item><title>Pilot 3</ti`,
		`<feed xmlns="http://www.w3.org/2005/Atom"><entry><title>Pilot 1</title><link href="https://example.org/1"/>` +
			`<published>2026-09-08T10:00:00Z</published></entry><entry><title>Pil`,
	} {
		rows, err := feedRows(feedPrefix([]byte(cut)), feed{Name: "Cut"}, "2026-09-10")
		if err != nil || len(rows) == 0 || rows[len(rows)-1].Title == "Pilot 3" {
			t.Fatalf("cut-off feed lost its whole items or kept a partial one: %+v %v", rows, err)
		}
	}
	if feedPrefix([]byte(`<rss><channel><item><title>Half`)) != nil {
		t.Fatal("a feed without a single whole item was salvaged")
	}
}

func TestSlowRetryPicksNetworkFailuresOnly(t *testing.T) {
	sources := []webSource{{Name: "cut"}, {Name: "late"}, {Name: "denied"}, {Name: "fine"}}
	covered := []coverage{
		{Source: "fine", StopReason: "RSS history is a sample; archive coverage unknown"},
		{Source: "cut", StopReason: errFeedCutOff.Error() + " after 16024 bytes"},
		{Source: "late", StopReason: "read source page: context deadline exceeded (Client.Timeout exceeded)"},
		{Source: "denied", StopReason: "source returned HTTP 403 ()"},
	}
	slow := slowSources(sources, covered)
	if len(slow) != 2 || slow[0].Name != "cut" || slow[1].Name != "late" {
		t.Fatalf("slow sources: %+v", slow)
	}
	merged := replaceCoverage(covered, []coverage{{Source: "cut", Received: 12}}, map[string]bool{"cut": true, "late": true})
	if len(merged) != 3 || merged[1].Source != "cut" || merged[1].Received != 12 || merged[2].Source != "denied" {
		t.Fatalf("coverage after retry: %+v", merged)
	}
}

func TestFeedItemCarriesItsText(t *testing.T) {
	article := strings.Repeat("A bank piloted voice liveness checks. ", 60)
	body := []byte(`<rss xmlns:content="http://purl.org/rss/1.0/modules/content/"><channel><item><title>Full</title>` +
		`<link>https://example.org/full</link><pubDate>Tue, 08 Sep 2026 10:00:00 +0000</pubDate><description>Short` +
		`</description><content:encoded><![CDATA[<p>` + article + `</p>]]></content:encoded></item><item><title>Teaser` +
		`</title><link>https://example.org/teaser</link><pubDate>Tue, 08 Sep 2026 10:00:00 +0000</pubDate>` +
		`<description><![CDATA[<p>A startup raised a seed round.</p>]]></description></item></channel></rss>`)
	rows, err := feedRows(body, feed{Name: "Media"}, "2026-09-10")
	if err != nil || len(rows) != 2 || rows[0].TextStatus != articleText || !strings.HasPrefix(rows[0].Text, "A bank") ||
		rows[1].TextStatus != feedAbstractText || rows[1].Text != "A startup raised a seed round." {
		t.Fatalf("feed text: %+v %v", rows, err)
	}
	fierce := []byte(`<rss><channel><item><title>Heidi lands $340M</title><link>https://example.org/heidi</link>` +
		`<pubDate>Sep 22, 2026 8:39pm</pubDate></item></channel></rss>`)
	if rows, err = feedRows(fierce, feed{Name: "Fierce"}, "2026-09-23"); err != nil || len(rows) != 1 ||
		rows[0].Date != "2026-09-22" {
		t.Fatalf("Fierce date: %+v %v", rows, err)
	}
}

func TestBingNewsRowsPointAtPublisher(t *testing.T) {
	item := func(title, description, target string) string {
		return `<item><title>` + title + `</title><link>http://www.bing.com/news/apiclick.aspx?ref=FexRss&amp;url=` +
			url.QueryEscape(target) + `&amp;c=1</link><description>` + description + `</description><pubDate>` +
			`Sun, 20 Sep 2026 06:25:00 GMT</pubDate><News:Source>ETV</News:Source></item>`
	}
	body := []byte(`<rss xmlns:News="https://www.bing.com/news"><channel>` +
		item("Neuromorphic chip gains traction", "A lab showed a prototype.", "https://www.etvbharat.com/chip?utm_source=x") +
		item("Chip market outlook", "Neuromorphic chip vendors raised funds.", "https://menafn.com/1") +
		item("Semiconductor roundup", "Other news entirely.", "https://example.org/roundup") + `</channel></rss>`)
	rows, err := bingRows(body, webSource{Name: "Bing News"}, "neuromorphic chip", "2026-09-23")
	if err != nil || len(rows) != 2 || rows[0].URL != "https://www.etvbharat.com/chip" || rows[1].URL != "https://menafn.com/1" ||
		rows[0].Source != "Bing News" || rows[0].TextStatus != feedAbstractText {
		t.Fatalf("Bing News rows: %+v %v", rows, err)
	}
}

func TestLegacyFeedCharsetFromProlog(t *testing.T) {
	raw, err := charmap.Windows1251.NewEncoder().String(`<?xml version="1.0" encoding="Windows-1251"?><rss ` +
		`version="2.0"><channel><item><title>Пилот платформы</title><link>https://example.org/1</link><pubDate>` +
		`Tue, 08 Sep 2026 10:00:00 +0300</pubDate></item></channel></rss>`)
	if err != nil {
		t.Fatal(err)
	}
	body := sourceUTF8([]byte(raw), "https://example.org/rss", "application/rss+xml charset=Windows-1251", &snapshot{})
	rows, err := feedRows(body, feed{Name: "IE"}, "2026-09-10")
	if err != nil || len(rows) != 1 || rows[0].Title != "Пилот платформы" {
		t.Fatalf("feed title lost its charset: %+v %v", rows, err)
	}
}

func TestQueryKeepsMissingHistoryUnknownAndValidatesCalendar(t *testing.T) {
	s := &service{data: &snapshot{Source: "USPTO", ObservedAt: "2026-09-10T12:00:00Z", Records: []record{{ID: "1",
		Title: "Carbon capture retrofit", Date: "2026-09-08", URL: "https://example.org/1",
		Text: "The utility signed a contract. A carbon capture pilot starts in May. Unrelated tail."}, {ID: "2",
		Title: "Grid outlook", Date: "2026-09-08", URL: "https://example.org/2", Text: "Carbon capture is mentioned."}}}}
	r := httptest.NewRequestWithContext(t.Context(), http.MethodPost, "/v1/query", strings.NewReader(`{"terms":["carbon `+
		`capture"],"titles":true,"periods":[{"start":"2026-08-01","end_exclusive":"2026-09-01"},{"start":"2026-09-01",`+
		`"end_exclusive":"2026-09-11"}]}`))
	w := httptest.NewRecorder()
	s.query(w, r)
	if w.Code != 200 || !strings.Contains(w.Body.String(), `"counts":[null,1]`) || !strings.Contains(w.Body.String(),
		`"partial":true`) {
		t.Fatal(w.Code, w.Body.String())
	}
	if !strings.Contains(w.Body.String(), `"excerpt":"A carbon capture pilot starts in May. Unrelated tail."`) ||
		strings.Contains(w.Body.String(), "signed a contract") || strings.Contains(w.Body.String(), "Grid outlook") {
		t.Fatal(w.Body.String())
	}
	if validQuery(request{Terms: []string{"x"}, Periods: []period{{"2026-09-11", "2026-09-01"}}}) {
		t.Fatal("reversed calendar accepted")
	}
}
