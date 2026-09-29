package trendsemantic

import (
	"context"
	"fmt"
	"github.com/example/main-service/internal/platform/openalex"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"
	"time"

	"github.com/example/main-service/internal/application"
)

func TestAffiliationsRequireExactDOIAndKeepMultipleOrganizations(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/works/doi:10.1/paper" && r.URL.Path != "/works/doi:10.1/other" {
			t.Errorf("incorrect exact identifier %s", r.URL.Path)
		}
		fmt.Fprint(w, `{"id":"https://openalex.org/W1","doi":"https://doi.org/10.1/paper",`+
			`"authorships":[{"author":{"id":"https://openalex.org/A1","display_name":"J. Smith"},`+
			`"institutions":[{"id":"https://openalex.org/I1","display_name":"One"},`+
			`{"id":"https://openalex.org/I2","display_name":"Two"}]}]}`)
	}))
	defer server.Close()
	client := NewOpenAlex(testOpenAlex(t, server.URL))
	work, hash, status := client.lookup(t.Context(), "10.1/paper")
	if work == nil || len(hash) != 64 || status != "provider_attributed" || len(authorships(work, "2026-09-11")) != 2 {
		t.Fatalf("lost verified attribution: %+v %s %s", work, hash, status)
	}
	if work, _, status := client.lookup(t.Context(), "10.1/other"); work != nil || status != "identity_mismatch" {
		t.Fatal("accepted mismatched DOI")
	}
	_, limits := client.Enrich(t.Context(), application.LiveAnalysisRequest{
		Snapshot: application.LiveSnapshot{Periods: []application.LivePeriod{{EndExclusive: "2020-01-01"}}},
	}, 10)
	if len(limits) != 1 {
		t.Fatal("historical observation accepted present affiliations")
	}
}

func TestAffiliationsNegotiateHTTP1WithHTTP2CapableProvider(t *testing.T) {
	server := httptest.NewUnstartedServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.ProtoMajor != 1 {
			t.Errorf("provider request negotiated %s", r.Proto)
		}
		fmt.Fprint(w, `{"id":"https://openalex.org/W1","doi":"https://doi.org/10.1/paper"}`)
	}))
	server.EnableHTTP2 = true
	server.StartTLS()
	defer server.Close()
	client := NewOpenAlex(testOpenAlex(t, server.URL))
	transport, ok := client.Client.HTTP.Transport.(*http.Transport)
	if !ok {
		t.Fatal("OpenAlex client must use a configurable HTTP transport")
	}
	providerTransport, ok := server.Client().Transport.(*http.Transport)
	if !ok {
		t.Fatal("test server must provide its TLS transport")
	}
	transport.TLSClientConfig.RootCAs = providerTransport.TLSClientConfig.RootCAs
	if work, _, status := client.lookup(t.Context(), "10.1/paper"); work == nil || status != "provider_attributed" {
		t.Fatalf("provider TLS negotiation failed: %s", status)
	}
}

func TestAffiliationObservationCannotBeBackdatedToPassStart(t *testing.T) {
	var received atomic.Bool
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		received.Store(true)
		fmt.Fprint(w, `{"id":"https://openalex.org/W1","doi":"https://doi.org/10.1/paper",`+
			`"authorships":[{"author":{"id":"https://openalex.org/A1"},`+
			`"institutions":[{"id":"https://openalex.org/I1"}]}]}`)
	}))
	defer server.Close()
	client := NewOpenAlex(testOpenAlex(t, server.URL))
	start := time.Date(2026, 9, 11, 23, 59, 0, 0, time.UTC)
	client.Now = func() time.Time {
		if received.Load() {
			return start.Add(2 * time.Minute)
		}
		return start
	}
	input := application.LiveAnalysisRequest{EnrichMetadata: true, StrictAvailability: true,
		Snapshot: application.LiveSnapshot{Periods: []application.LivePeriod{{Start: "2026-09-01", EndExclusive: "2026-09-12"}},
			Papers: []application.StudyPaper{{ID: "p", Title: "Paper", Date: "2026-09-09"}},
			Topics: []application.LiveTopic{{ID: "a", PaperIDs: []string{"p"}}}},
		Metadata: map[string]application.LivePaperMetadata{"p": {DOI: "10.1/paper"}}}
	metadata, _ := client.Enrich(t.Context(), input, 1)
	if metadata["p"].AffiliationCheckedAt != "2026-09-12T00:01:00Z" || len(metadata["p"].Authorships) != 1 ||
		metadata["p"].Authorships[0].ObservedAt != "2026-09-12T00:01:00Z" {
		t.Fatal("provider response was backdated to the earlier request start")
	}
	input.Metadata = metadata
	result, err := application.AnalyzeLive(t.Context(), nil, input)
	if err != nil || result.Topics["a"].Evidence.AttributedPapers != 0 {
		t.Fatal("response after midnight leaked into the previous historical day")
	}
}

func testOpenAlex(t *testing.T, base string) *openalex.Client {
	t.Helper()
	client, err := openalex.New(openalex.Config{BaseURL: base})
	if err != nil {
		t.Fatal(err)
	}
	client.Sleep = func(context.Context, time.Duration) error { return nil }
	return client
}
