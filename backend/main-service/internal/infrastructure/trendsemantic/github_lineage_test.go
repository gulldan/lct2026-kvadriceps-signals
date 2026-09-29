package trendsemantic

import (
	"fmt"
	"net/http"
	"net/http/httptest"
	"reflect"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/example/main-service/internal/application"
)

func TestGitHubForkAcquisitionGroupsEventsAndCachesAliases(t *testing.T) {
	var calls atomic.Int64
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls.Add(1)
		if r.Header.Get("Authorization") != "Bearer test-token" || r.Header.Get("X-Github-Api-Version") == "" {
			t.Error("missing configured auth or pinned API version")
		}
		switch r.URL.Path {
		case "/repos/team/fork":
			fmt.Fprint(w, `{"id":11,"html_url":"https://github.com/team/fork","fork":true,`+
				`"parent":{"id":20,"html_url":"https://github.com/middle/project"},`+
				`"source":{"id":30,"html_url":"https://github.com/upstream/project"}}`)
		case "/repos/upstream/project":
			fmt.Fprint(w, `{"id":30,"html_url":"https://github.com/upstream/project","fork":false}`)
		default:
			t.Errorf("unexpected lookup %s", r.URL.Path)
		}
	}))
	defer server.Close()
	client := NewGitHubLineage(server.URL, "test-token")
	client.Now = func() time.Time { return time.Date(2026, 9, 11, 10, 0, 0, 0, time.UTC) }
	input := application.LiveAnalysisRequest{EnrichMetadata: true, EvidenceObservedAt: "2026-09-11T10:01:00Z",
		Snapshot: application.LiveSnapshot{
			Periods: []application.LivePeriod{{Start: "2026-08-01", EndExclusive: "2026-09-12"}},
			Topics: []application.LiveTopic{{ID: "a", External: []application.StudyEvent{
				{URL: "https://github.com/team/fork/releases/tag/v1", Date: "2026-09-01"},
				{URL: "https://github.com/upstream/project", Date: "2026-09-01"},
				{URL: "https://github.com/TEAM/fork", Date: "2026-09-01"},
			}}},
		}}
	metadata, limits := client.Enrich(t.Context(), input, 32)
	fork := metadata["https://github.com/team/fork/releases/tag/v1"]
	if calls.Load() != 2 || len(limits) != 0 || fork.LineageStatus != "provider_linked" || len(fork.Links) != 2 ||
		len(fork.LineageHash) != 64 || fork.Links[0].SourceHash != fork.LineageHash ||
		metadata["https://github.com/upstream/project"].LineageStatus != "not_fork_not_independence" {
		t.Fatalf("fork acquisition lost explicit provider data: %+v %v", fork, limits)
	}
	input.Metadata = metadata
	result, err := application.AnalyzeLive(t.Context(), nil, input)
	if err != nil {
		t.Fatal(err)
	}
	evidence := result.Topics["a"].Evidence
	if evidence.RecordsExamined != 3 || evidence.DistinctEvents != 1 ||
		evidence.DependentRecords != 2 || evidence.IndependentImplementations != nil {
		t.Fatalf("actual acquired links did not group fork/release events: %+v", evidence)
	}
	metadata, limits = client.Enrich(t.Context(), input, 32)
	if calls.Load() != 2 || len(limits) != 0 || !reflect.DeepEqual(metadata, input.Metadata) {
		t.Fatal("seven-day cache repeated lookups or lost recorded proof")
	}
}

func TestGitHubLineageBoundsRequestsAndRetainsUnknownEvidence(t *testing.T) {
	var calls atomic.Int64
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		calls.Add(1)
		w.Header().Set("X-Ratelimit-Remaining", "0")
		http.Error(w, "rate limit", http.StatusForbidden)
	}))
	defer server.Close()
	client := NewGitHubLineage(server.URL, "")
	input := application.LiveAnalysisRequest{EnrichMetadata: true,
		Snapshot: application.LiveSnapshot{Topics: []application.LiveTopic{{External: []application.StudyEvent{
			{URL: "https://github.com/a/repo"}, {URL: "https://github.com/b/repo"},
		}}}},
		Metadata: map[string]application.LivePaperMetadata{"https://github.com/a/repo": {
			Links: []application.LiveEvidenceLink{{Kind: "fork", ParentURL: "https://github.com/original/repo"}},
		}},
	}
	got, limits := client.Enrich(t.Context(), input, 0)
	if calls.Load() != 0 || len(limits) != 1 || !reflect.DeepEqual(got, input.Metadata) {
		t.Fatal("zero budget made network requests or changed prior observations")
	}
	got, limits = client.Enrich(t.Context(), input, 32)
	if calls.Load() != 1 || len(limits) != 1 || !strings.Contains(limits[0].Text, "rate_limited") ||
		!reflect.DeepEqual(got, input.Metadata) {
		t.Fatal("rate-limited provider was retried or erased verified history")
	}
	input.StrictAvailability = true
	client.Enrich(t.Context(), input, 32)
	input.StrictAvailability, input.EvidenceOnly = false, true
	client.Enrich(t.Context(), input, 32)
	if calls.Load() != 1 {
		t.Fatal("historical or evidence-only analysis made current GitHub requests")
	}
}

func TestGitHubLineageRejectsUnrelatedRepositoryResponse(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		fmt.Fprint(w, `{"id":1,"html_url":"https://github.com/unrelated/repo","fork":true,`+
			`"parent":{"id":2,"html_url":"https://github.com/owner/origin"}}`)
	}))
	defer server.Close()
	links, _, status := NewGitHubLineage(server.URL, "").lookup(t.Context(), "https://github.com/requested/repo")
	if len(links) != 0 || status != "identity_mismatch" {
		t.Fatal("accepted a fork assertion for a different repository")
	}
}
