package sourcedoc

import (
	"bytes"
	"encoding/json/v2"
	"os"
	"strings"
	"testing"
	"time"
)

func TestInvestorGenreContract(t *testing.T) {
	legacy := sampleSource()
	before, err := json.Marshal(legacy)
	if err != nil {
		t.Fatal(err)
	}
	decoded, err := Decode(before)
	if err != nil {
		t.Fatal(err)
	}
	after, err := json.Marshal(decoded)
	if err != nil || !bytes.Equal(before, after) || bytes.Contains(after, []byte(`"genre"`)) {
		t.Fatalf("legacy payload changed: %s, %v", after, err)
	}
	for _, genre := range []string{GenreInvestmentThesis, GenreInvestorCommentary} {
		for _, kind := range []string{kindAnalysis, kindCompany} {
			s := legacy
			s.Provider, s.Kind, s.Genre, s.Publication = "web", kind, genre, nil
			raw, err := json.Marshal(s)
			if err != nil {
				t.Fatal(err)
			}
			got, err := Decode(raw)
			if err != nil || got.Genre != genre || got.Publication != nil || !got.ObservedAt.Equal(s.ObservedAt) {
				t.Fatalf("investor provenance lost: %+v, %v", got, err)
			}
		}
	}
	for _, fields := range [][3]string{
		{"web", kindAnalysis, "funding"}, {"web", kindPaper, GenreInvestmentThesis},
		{providerManual, kindCompany, GenreInvestorCommentary}, {"crossref", kindPaper, GenreInvestmentThesis},
		{"web", kindCompany, ""},
	} {
		s := legacy
		s.Provider, s.Kind, s.Genre = fields[0], fields[1], fields[2]
		if err := s.Validate(); err == nil {
			t.Fatalf("invalid investor provenance accepted: %v", fields)
		}
	}
}

func sampleSource() Source {
	return Source{Version: 1, Kind: "paper", Provider: "manual", NativeID: "paper-1", URL: "https://example.org/paper",
		CanonicalID: "work-1", Publication: &Date{Value: "2024", Precision: "year", Basis: "report cover"},
		ObservedAt: time.Date(2026, 8, 31, 0, 0, 0, 0, time.UTC), Revision: "v1", ContentHash: strings.Repeat("a", 64),
		HashAlgorithm: "sha256", AccessScope: "public"}
}

func TestSourceContract(t *testing.T) {
	valid := sampleSource()
	raw, err := json.Marshal(valid)
	if err != nil {
		t.Fatal(err)
	}
	got, err := Decode(raw)
	if err != nil || got.Publication.Value != "2024" {
		t.Fatalf("year precision lost: %v %v", got, err)
	}
	for name, mutate := range map[string]func(*Source){
		"unknown date": func(s *Source) { s.Publication = nil },
		"leap day": func(s *Source) {
			s.Publication = &Date{Value: "2024-02-29", Precision: "day", Basis: "provider publication_date"}
		},
		"patent": func(s *Source) {
			s.Kind = "patent"
			s.Priority = &Date{Value: "2021", Precision: "year", Basis: "patent priority"}
			s.Filing = &Date{Value: "2022-03", Precision: "month", Basis: "patent filing"}
		},
		"patent without grant": func(s *Source) {
			s.Kind = "patent"
			s.Priority = &Date{Value: "2021-04-02", Precision: "day", Basis: "USPTO priority claim"}
			s.Filing = &Date{Value: "2022-03-11", Precision: "day", Basis: "USPTO filing line"}
		},
		"revised work": func(s *Source) {
			s.Publication = &Date{Value: "2024-01-15", Precision: "day", Basis: "arXiv Atom published (v1)"}
			s.RevisionPublication = &Date{Value: "2025-06-01", Precision: "day", Basis: "arXiv Atom updated"}
		},
		"imported patent": func(s *Source) {
			s.Kind = "patent"
			s.NativeID, s.CanonicalID = "RU2789123", "patent:RU2789123"
			s.Patent = &Patent{PublicationNumber: "RU2789123", Jurisdiction: "RU", KindCode: "C1",
				ApplicationNumber: "2023101234", FamilyID: "ru-2023101234",
				FamilyBasis: "same application only; wider family unknown"}
		},
		"imported translation": func(s *Source) {
			s.Kind = "patent"
			s.NativeID, s.CanonicalID = "EP3456789", "patent:RU2789123"
			s.Patent = &Patent{PublicationNumber: "EP3456789", Jurisdiction: "EP", KindCode: "B1",
				TranslationOf: "RU2789123"}
		},
		"undated page": func(s *Source) {
			s.Kind = "analysis"
			s.Provider = "web"
			s.Publication = nil
			s.AvailableAt = new(s.ObservedAt.Add(-time.Hour))
			s.AvailableBasis = "first successful source observation"
		},
		"hypothesis": func(s *Source) { s.Kind = "hypothesis"; s.Provider = "generated"; s.Publication = nil },
	} {
		t.Run(name, func(t *testing.T) {
			s := sampleSource()
			mutate(&s)
			if err := s.Validate(); err != nil {
				t.Fatal(err)
			}
		})
	}
	for name, mutate := range map[string]func(*Source){
		"impossible day":     func(s *Source) { s.Publication = &Date{Value: "2023-02-29", Precision: "day", Basis: "api"} },
		"sentinel":           func(s *Source) { s.Publication.Value = "1601" },
		"invented precision": func(s *Source) { s.Publication.Precision = "day" },
		"missing basis":      func(s *Source) { s.Publication.Basis = "" },
		"scope":              func(s *Source) { s.AccessScope = "admin" },
		"hash":               func(s *Source) { s.ContentHash = "not-a-hash" },
		"native ID":          func(s *Source) { s.NativeID = " " },
		"provider":           func(s *Source) { s.Provider = "github" },
		"date for repo":      func(s *Source) { s.Kind = "software_repository" },
		"priority for paper": func(s *Source) { s.Priority = s.Publication },
		"grant for paper":    func(s *Source) { s.Grant = s.Publication },
		"patent identity for paper": func(s *Source) {
			s.Patent = &Patent{PublicationNumber: "RU2789123", Jurisdiction: "RU"}
		},
		"patent under another identity": func(s *Source) {
			s.Kind = "patent"
			s.Patent = &Patent{PublicationNumber: "RU2789123", Jurisdiction: "RU"}
		},
		"family without basis": func(s *Source) {
			s.Kind = "patent"
			s.NativeID, s.CanonicalID = "RU2789123", "patent:RU2789123"
			s.Patent = &Patent{PublicationNumber: "RU2789123", Jurisdiction: "RU", FamilyID: "ru-1"}
		},
		"translation of itself": func(s *Source) {
			s.Kind = "patent"
			s.NativeID, s.CanonicalID = "RU2789123", "patent:RU2789123"
			s.Patent = &Patent{PublicationNumber: "RU2789123", Jurisdiction: "RU", TranslationOf: "RU2789123"}
		},
		"future publication": func(s *Source) {
			s.Publication = &Date{Value: "2027-01-01", Precision: "day", Basis: "provider publication_date"}
		},
		"revision before work": func(s *Source) {
			s.Publication = &Date{Value: "2024-06-01", Precision: "day", Basis: "first publication"}
			s.RevisionPublication = &Date{Value: "2024-05-31", Precision: "day", Basis: "version update"}
		},
		"revision without work": func(s *Source) {
			s.Publication = nil
			s.RevisionPublication = &Date{Value: "2025-01-01", Precision: "day", Basis: "version update"}
		},
		"available before publication": func(s *Source) {
			s.Publication = &Date{Value: "2026-01-01", Precision: "day", Basis: "provider publication_date"}
			s.AvailableAt = new(time.Date(2025, 12, 31, 0, 0, 0, 0, time.UTC))
			s.AvailableBasis = "first successful source observation"
		},
		"generated paper": func(s *Source) { s.Provider = "generated" },
		"availability": func(s *Source) {
			s.AvailableAt = new(s.ObservedAt.Add(time.Hour))
			s.AvailableBasis = "snapshot"
		},
		"unknown availability basis": func(s *Source) { s.AvailableAt = new(s.ObservedAt) },
		"observation":                func(s *Source) { s.ObservedAt = time.Time{} },
		"credential URL":             func(s *Source) { s.URL = "https://user:password@example.org/" },
		"executable URL":             func(s *Source) { s.URL = "javascript:alert(1)" },
	} {
		t.Run(name, func(t *testing.T) {
			s := sampleSource()
			mutate(&s)
			if err := s.Validate(); err == nil {
				t.Fatal("invalid source accepted")
			}
		})
	}
	for _, bad := range []string{
		`null`, `{}`, strings.Repeat(" ", MaxMetadataBytes+1),
		string(raw[:len(raw)-1]) + `,"version":1}`, string(raw[:len(raw)-1]) + `,"unexpected":true}`,
	} {
		if _, err := Decode([]byte(bad)); err == nil {
			t.Fatalf("invalid envelope accepted: %.80s", bad)
		}
	}
}

func TestProviderFixtures(t *testing.T) {
	raw, err := os.ReadFile("testdata/v1/corpus.json")
	if err != nil {
		t.Fatal(err)
	}
	var fixture struct {
		Records []struct {
			ID     string `json:"id"`
			Source Source `json:"source"`
		} `json:"records"`
	}
	if err := json.Unmarshal(raw, &fixture); err != nil {
		t.Fatal(err)
	}
	for _, record := range fixture.Records {
		t.Run(record.ID, func(t *testing.T) {
			if err := record.Source.Validate(); err != nil {
				t.Fatal(err)
			}
		})
	}
}

func TestDateRangeAndQuarter(t *testing.T) {
	for _, c := range []struct {
		date    Date
		quarter string
		ok      bool
	}{
		{Date{Value: "2026-02-14", Precision: "day", Basis: "b"}, "2026-Q1", true},
		{Date{Value: "2026-03", Precision: "month", Basis: "b"}, "2026-Q1", true},
		{Date{Value: "2026-04", Precision: "month", Basis: "b"}, "2026-Q2", true},
		{Date{Value: "2026", Precision: "year", Basis: "b"}, "", false},
		{Date{Value: "2026", Precision: "decade", Basis: "b"}, "", false},
	} {
		quarter, ok := c.date.Quarter()
		if quarter != c.quarter || ok != c.ok {
			t.Fatalf("%+v allocated to %q (%v), want %q (%v)", c.date, quarter, ok, c.quarter, c.ok)
		}
	}
	start, end := Date{Value: "2026", Precision: "year", Basis: "b"}.Range()
	if start.Format(time.DateOnly) != "2026-01-01" || end.Format(time.DateOnly) != "2027-01-01" {
		t.Fatal("year precision lost its span", start, end)
	}
}

func TestKnownNoLaterThan(t *testing.T) {
	s := sampleSource()
	s.Publication = nil
	if !s.KnownNoLaterThan().Equal(s.ObservedAt) {
		t.Fatal("undated source lost its observation bound")
	}
	available := s.ObservedAt.Add(-48 * time.Hour)
	s.AvailableAt, s.AvailableBasis = new(available), "first successful source observation"
	if !s.KnownNoLaterThan().Equal(available) {
		t.Fatal("recorded availability is the tighter bound")
	}
	if _, ok := (Date{}).Quarter(); ok {
		t.Fatal("an absent date was allocated to a quarter")
	}
}
