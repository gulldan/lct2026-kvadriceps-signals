package application

import (
	"strings"
	"testing"
)

func TestRadarVerifyContextAnchorsFragments(t *testing.T) {
	detail := `{"radar":{"sources":[
		{"title":"Personalized Keyword Spotting","date":"2026-06-18","source_name":"arXiv","quotes":[
			{"quote":"ZP-KWS reduces target-only FRR by up to 60% for edge deployment.","verified":true},
			{"quote":"Abstract. ZP-KWS reduces target-only FRR by up to 60% for edge deployment. More text.","verified":true}]},
		{"title":"Unverified work","quotes":[{"quote":"model paraphrase","verified":false}]}]}}`
	note, sources := radarVerifyContext(detail)
	if len(sources) != 1 || sources[0].GetFilename() != "Personalized Keyword Spotting" {
		t.Fatalf("expected one radar source with a verified quote, got %v", sources)
	}
	if !strings.Contains(note, "[R1] Personalized Keyword Spotting (2026-06-18, arXiv)") || strings.Contains(note, "Abstract.") {
		t.Fatalf("note must list the source once with its shortest quote:\n%s", note)
	}
	rows := verifierEvidence(nil, sources, verifyResult{Supporting: []string{"reduces target-only FRR by up to 60%"}})
	if len(rows) != 1 || rows[0].GetFilename() != "Personalized Keyword Spotting" || rows[0].GetStance() != stanceSupports {
		t.Fatalf("fragment must anchor to the radar source, got %v", rows)
	}
	if note, sources := radarVerifyContext(`{"title":"corpus hypothesis"}`); note != "" || sources != nil {
		t.Fatal("hypothesis without radar provenance must not change the verify prompt")
	}
}
