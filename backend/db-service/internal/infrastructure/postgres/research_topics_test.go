package postgres_test

import (
	"encoding/json/v2"
	"os"
	"testing"
)

func TestResearchTopicMatchesSharedFrontendCases(t *testing.T) {
	db := newTestDB(t)
	raw, err := os.ReadFile("testdata/research_topics.json")
	if err != nil {
		t.Fatal(err)
	}
	var cases []struct {
		Text string `json:"text"`
		Want string `json:"want"`
	}
	if err = json.Unmarshal(raw, &cases); err != nil {
		t.Fatal(err)
	}
	for _, tc := range cases {
		var got string
		if err = db.Pool.QueryRow(t.Context(), `SELECT research_topic($1)`, tc.Text).Scan(&got); err != nil {
			t.Fatal(err)
		}
		if got == "" {
			got = "unknown"
		}
		if got != tc.Want {
			t.Errorf("%q: got %q, want %q", tc.Text, got, tc.Want)
		}
	}
}
