package main

import "testing"

func TestFeedLanguage(t *testing.T) {
	for _, tc := range []struct{ declared, want string }{
		{"ru", "ru"}, {"RU", "ru"}, {"ru-RU", "ru"}, {"en-US", "en"},
		{"", ""}, {"unknown", ""}, {"x1", ""}, {"  en  ", "en"},
	} {
		got, basis := feedLanguage(tc.declared)
		if got != tc.want {
			t.Errorf("%q: получили %q, ожидали %q", tc.declared, got, tc.want)
		}
		if (basis == "") != (got == "") {
			t.Errorf("%q: основание и код рассогласованы: %q / %q", tc.declared, got, basis)
		}
	}
}

func TestFeedRowsCarryDeclaredLanguage(t *testing.T) {
	body := []byte(`<?xml version="1.0" encoding="UTF-8"?>
<rss version="2.0"><channel><title>Тест</title><language>ru-RU</language>
<item><title>Новая технология</title><link>https://example.org/a</link>
<pubDate>Mon, 01 Sep 2026 10:00:00 +0000</pubDate></item>
</channel></rss>`)
	rows, err := feedRows(body, feed{}, "2026-09-30")
	if err != nil {
		t.Fatalf("разбор ленты: %v", err)
	}
	if len(rows) != 1 {
		t.Fatalf("записей %d, ожидали 1", len(rows))
	}
	if rows[0].Language != "ru" || rows[0].LanguageBasis == "" {
		t.Fatalf("язык не проставлен: %q / %q", rows[0].Language, rows[0].LanguageBasis)
	}
}
