package analysisdoc_test

import (
	"encoding/json/v2"
	"slices"
	"strings"
	"testing"
	"time"
	"uuid"

	"github.com/example/rag-mvp/pkg/analysisdoc"
	"github.com/example/rag-mvp/pkg/sourcedoc"
)

func TestHistoricalSourceEligibility(t *testing.T) {
	observed := time.Date(2026, 9, 9, 0, 0, 0, 0, time.UTC)
	available := time.Date(2024, 2, 29, 12, 0, 0, 0, time.UTC)
	source := sourcedoc.Source{Kind: "paper", ObservedAt: observed, AvailableAt: &available,
		AvailableBasis: "arXiv version", Publication: &sourcedoc.Date{Value: "2024-02", Precision: "month", Basis: "provider"},
		ArXiv: &sourcedoc.ArXiv{VersionUpdated: available, DOI: "10.1234/late", JournalRef: "2026 journal"}}
	request := analysisdoc.Request{Mode: "reconstruction", Cutoff: "2024-02-29T23:59:59Z"}
	clean, limits, reason := analysisdoc.EligibleSource(source, request)
	if reason != "" || len(limits) != 2 || clean.ArXiv.DOI != "" || source.ArXiv.DOI == "" {
		t.Fatal("exact month boundary must pass with late metadata removed from a copy", reason)
	}
	for _, test := range []struct {
		name, mode, cutoff, reason string
	}{
		{"strict observation", "as_observed", request.Cutoff, "not_observed_at_cutoff"},
		{"incomplete month", "reconstruction", "2024-02-28T23:59:59Z", "publication_interval_after_cutoff"},
	} {
		t.Run(test.name, func(t *testing.T) {
			request.Mode, request.Cutoff = test.mode, test.cutoff
			if _, _, got := analysisdoc.EligibleSource(source, request); got != test.reason {
				t.Fatalf("got %q, want %q", got, test.reason)
			}
		})
	}
	source.Publication, source.AvailableAt, source.ArXiv = nil, nil, nil
	request.Mode, request.Cutoff = "reconstruction", "2024-12-31T23:59:59Z"
	if _, _, reason = analysisdoc.EligibleSource(source, request); reason != "revision_unavailable_at_cutoff" {
		t.Fatal("current abstract cannot supply unproven historical text", reason)
	}
}

func TestYearPrecisionUnknownAndNonProductSources(t *testing.T) {
	source := sourcedoc.Source{Kind: "paper", ObservedAt: time.Date(2023, 1, 1, 0, 0, 0, 0, time.UTC),
		Publication: &sourcedoc.Date{Value: "2023", Precision: "year", Basis: "provider"}}
	request := analysisdoc.Request{Mode: "as_observed", Cutoff: "2023-06-30T23:59:59Z"}
	if _, _, reason := analysisdoc.EligibleSource(source, request); reason != "publication_interval_after_cutoff" {
		t.Fatal("year-only source must not invent a January publication")
	}
	request.Cutoff = "2023-12-31T23:59:59Z"
	if _, _, reason := analysisdoc.EligibleSource(source, request); reason != "" {
		t.Fatal("completed year rejected", reason)
	}
	source.Publication = nil
	if _, limits, reason := analysisdoc.EligibleSource(source, request); reason != "" || len(limits) != 1 {
		t.Fatal("unknown date must remain usable text with no temporal support")
	}
	source.TestRunID = "tr-test-" + uuid.NewV4().String()
	if _, _, reason := analysisdoc.EligibleSource(source, request); reason != "non_product_source" {
		t.Fatal("test source entered the product input")
	}
	request.TestRunID = source.TestRunID
	if _, limits, reason := analysisdoc.EligibleSource(source, request); reason != "" || len(limits) != 2 {
		t.Fatal("matching isolated test source must retain its limitation", reason)
	}
	request.TestRunID = "tr-test-" + uuid.NewV4().String()
	if _, _, reason := analysisdoc.EligibleSource(source, request); reason != "non_product_source" {
		t.Fatal("another test namespace entered the input")
	}
	source.Kind, request.TestRunID = "hypothesis", source.TestRunID
	if _, _, reason := analysisdoc.EligibleSource(source, request); reason != "non_product_source" {
		t.Fatal("test namespace must not turn a hypothesis into scientific support")
	}
}

func TestRequestBoundariesAndNormalization(t *testing.T) {
	now := time.Date(2026, 9, 9, 0, 0, 0, 0, time.UTC)
	r := analysisdoc.Request{OwnerID: uuid.NewV4().String(), RequestID: uuid.NewV4().String(),
		Query: "  методы   RAG  ", Start: "2023-01-01", Cutoff: "2026-08-31T23:59:59Z", Mode: "reconstruction"}
	got, err := r.Normalize(now)
	if err != nil || got.Query != "методы RAG" {
		t.Fatal("query normalization", err)
	}
	r.Cutoff = "2027-01-01T00:00:00Z"
	if _, err = r.Normalize(now); err == nil {
		t.Fatal("future cutoff accepted")
	}
	if _, err = analysisdoc.Decode[analysisdoc.Request](`{"owner_id":"x","admin":true}`); err == nil {
		t.Fatal("unknown authority field accepted")
	}
}

func TestExplicitCollectionScopeNormalization(t *testing.T) {
	now := time.Date(2026, 9, 11, 12, 0, 0, 0, time.UTC)
	r := analysisdoc.Request{OwnerID: uuid.NewV4().String(), RequestID: uuid.NewV4().String(),
		Query: "RAG", Start: "2017-01-01", Cutoff: "2026-09-11T11:00:00Z", Mode: "as_observed"}
	legacy, err := json.Marshal(r)
	if err != nil || strings.Contains(string(legacy), "collection_scope_ids") {
		t.Fatal("unfiltered request serialization changed", err)
	}
	a, b := strings.Repeat("a", 64), strings.Repeat("b", 64)
	r.CollectionScopeIDs = []string{b, a}
	got, err := r.Normalize(now)
	if err != nil || !slices.Equal(got.CollectionScopeIDs, []string{a, b}) || r.CollectionScopeIDs[0] != b {
		t.Fatal("scope normalization mutated its caller or did not canonicalize order", err)
	}
	raw, err := json.Marshal(got)
	if err != nil {
		t.Fatal(err)
	}
	decoded, err := analysisdoc.Decode[analysisdoc.Request](string(raw))
	if err != nil || !slices.Equal(decoded.CollectionScopeIDs, got.CollectionScopeIDs) {
		t.Fatal("strict request decoding lost the selected scope", err)
	}
	for _, ids := range [][]string{{a, a}, {"../scope"}, {strings.Repeat("A", 64)}, make([]string, 17)} {
		r.CollectionScopeIDs = ids
		if _, err := r.Normalize(now); err == nil {
			t.Fatalf("invalid scope list accepted: %v", ids)
		}
	}
}
