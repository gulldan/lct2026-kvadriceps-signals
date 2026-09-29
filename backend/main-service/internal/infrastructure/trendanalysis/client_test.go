package trendanalysis

import (
	"encoding/json/v2"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/example/main-service/internal/application"
)

func TestAnalysisClientOmitsDuplicateMetadataAndRejectsMissingResults(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var input application.LiveAnalysisRequest
		if json.UnmarshalRead(r.Body, &input) != nil || r.URL.Path != "/v1/analyze" ||
			input.Snapshot.PaperMetadata != nil || input.Metadata["p"].Abstract != "abstract" {
			t.Error("invalid bounded request")
		}
		_ = json.MarshalWrite(w, application.LiveAnalysisResult{
			Version: application.LiveAnalysisVersion,
			Topics:  map[string]application.LiveTopicAnalysis{}, Semantics: map[string]*application.LiveSemantic{},
		})
	}))
	defer server.Close()
	input := application.LiveAnalysisRequest{Snapshot: application.LiveSnapshot{
		Papers:        []application.StudyPaper{{ID: "p"}},
		PaperMetadata: map[string]application.LivePaperMetadata{"p": {Abstract: "abstract"}},
	}}
	if _, err := New(server.URL).Analyze(t.Context(), input); err != nil {
		t.Fatal(err)
	}
	if input.Snapshot.PaperMetadata["p"].Abstract != "abstract" {
		t.Fatal("client mutated caller metadata")
	}
}
