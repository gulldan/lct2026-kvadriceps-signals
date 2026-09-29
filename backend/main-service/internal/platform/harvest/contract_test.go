package harvest

import (
	"encoding/json/v2"
	"strings"
	"testing"
	"time"
	"uuid"

	"github.com/example/main-service/internal/platform/sourcedoc"
)

func TestArxivCategorySeparatesCheckpointAndRoundTrips(t *testing.T) {
	s := startFixture()
	s.Provider = "arxiv"
	// Контрольные точки, созданные до появления необязательной категории, должны сохраниться.
	const legacy = "5f19676409a953269cb87a80af7a3ccac1a108ac948cd55da7d9b579277f18f2"
	if s.ScopeID() != legacy {
		t.Fatal("an unfiltered scope no longer uses its existing checkpoint")
	}
	for _, category := range []string{"cs.IR", "math.AG", "hep-th"} {
		s.Scope.Category = category
		if err := s.Validate(); err != nil {
			t.Fatal(err)
		}
		if s.ScopeID() == legacy {
			t.Fatal("category filter reused the unfiltered cursor")
		}
		raw, err := json.Marshal(s)
		if err != nil {
			t.Fatal(err)
		}
		decoded, err := Decode[Start](string(raw))
		if err != nil || decoded.Scope.Category != category || decoded.ScopeID() != s.ScopeID() {
			t.Fatalf("category lost across the strict control plane: %v", err)
		}
	}
	for _, category := range []string{"cs.*", "cs.IR OR all:biology", " cs.IR", "cs.IR\n", `cs.IR"`, "cs.IR.cs.AI"} {
		s.Scope.Category = category
		if err := s.Validate(); err == nil {
			t.Fatalf("category expression accepted: %q", category)
		}
	}
	s.Scope.Category = "cs.IR"
	for _, provider := range []string{"crossref", "github", "patent", "web"} {
		s.Provider, s.Scope.PageSize = provider, 1
		if err := s.Validate(); err == nil {
			t.Fatalf("unsupported category accepted by %s", provider)
		}
	}
}

func startFixture() Start {
	return Start{
		OwnerID:   "owner",
		Provider:  "crossref",
		Scope:     Scope{Query: "RAG", From: "2023-01-01", Through: "2026-08-31", PageSize: 2},
		RunID:     uuid.NewV4().String(),
		Token:     uuid.NewV4().String(),
		Limit:     10,
		TestRunID: "tr-test-contract",
	}
}

func TestStartBoundaries(t *testing.T) {
	good := startFixture()
	if err := good.Validate(); err != nil {
		t.Fatal(err)
	}
	for name, change := range map[string]func(*Start){
		"partial page":      func(s *Start) { s.Limit = 3 },
		"unbounded":         func(s *Start) { s.Limit = 202 },
		"test budget":       func(s *Start) { s.Limit = 52 },
		"path escape":       func(s *Start) { s.TestRunID = "tr-test-../../other" },
		"blank owner":       func(s *Start) { s.OwnerID = "" },
		"date order":        func(s *Start) { s.Scope.From = "2027-01-01" },
		"unknown provider":  func(s *Start) { s.Provider = "untrusted" },
		"other source sort": func(s *Start) { s.Scope.Sort = "stars" },
		"github page bound": func(s *Start) { s.Provider = "github" },
	} {
		t.Run(
			name,
			func(t *testing.T) {
				s := good
				change(&s)
				if err := s.Validate(); err == nil {
					t.Fatal("invalid collection accepted")
				}
			},
		)
	}
	other := good
	other.OwnerID = "other"
	if good.ScopeID() == other.ScopeID() {
		t.Fatal("scope cache crosses owners")
	}
	other = good
	other.TestRunID = "tr-test-another"
	if good.ScopeID() == other.ScopeID() {
		t.Fatal("test run shares production scope")
	}
}

func TestGitHubSortStartsSeparateCheckpoint(t *testing.T) {
	s := startFixture()
	s.Provider, s.Scope.PageSize = "github", 1
	before := s.ScopeID()
	s.Scope.Sort = "stars"
	if err := s.Validate(); err != nil {
		t.Fatal(err)
	}
	if before == s.ScopeID() {
		t.Fatal("changed ordering reused an existing cursor")
	}
	s.Scope.Sort = "unknown"
	if err := s.Validate(); err == nil {
		t.Fatal("unknown ordering accepted")
	}
}

func TestStrictEnvelope(t *testing.T) {
	for _, raw := range []string{
		"",
		`{"owner_id":"a","owner_id":"b"}`,
		`{"extra":true}`,
		strings.Repeat(" ", MaxPageBytes+1),
	} {
		if _, err := Decode[Start](raw); err == nil {
			t.Fatal("malformed envelope accepted")
		}
	}
}

func TestRepositoryObservationDoesNotDuplicateFile(t *testing.T) {
	s := sourcedoc.Source{
		Provider:    "github",
		NativeID:    "123",
		Revision:    "sha@today",
		ContentHash: strings.Repeat("a", 64),
		GitHub:      &sourcedoc.GitHub{CommitSHA: strings.Repeat("b", 40)},
	}
	r := Prepared{Source: s, MIME: "text/plain"}
	before := DocumentKey("owner", r)
	r.Source.ObservedAt = time.Now()
	r.Source.Revision = "sha@tomorrow"
	r.Source.GitHub.Stars = new(int64(500))
	if before != DocumentKey("owner", r) {
		t.Fatal("observation duplicated pinned content")
	}
	r.Source.GitHub.CommitSHA = strings.Repeat("c", 40)
	if before == DocumentKey("owner", r) {
		t.Fatal("new commit overwrote pinned evidence")
	}
	if DocumentKey("owner", r) == DocumentKey("other", r) {
		t.Fatal("immutable object key crosses owners")
	}
}
