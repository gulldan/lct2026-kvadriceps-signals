package httpapi

import (
	"context"
	"encoding/csv"
	"encoding/json/jsontext"
	"encoding/json/v2"
	"errors"
	"net/http/httptest"
	"slices"
	"strings"
	"testing"

	"github.com/example/main-service/internal/platform/analysisdoc"
	"github.com/example/main-service/internal/platform/extractdoc"
)

// Ноль вместо отсутствия превратил бы «не измерено» в «измерено как ноль».
func TestExportKeepsAbsencesAndTheirReasons(t *testing.T) {
	measurements := jsontext.Value(`{
"velocity":{"value":null,"status":"unavailable","reason_codes":["insufficient_quarters"]},
"acceleration":{"value":null,"status":"unavailable","reason_codes":["insufficient_quarters"]},
"hhi":{"value":null,"status":"not_comparable","reason_codes":["affiliations_missing"]},
"active_quarters":{"value":3,"status":"measured","reason_codes":[]}}`)
	result := analysisdoc.Result{Candidates: []analysisdoc.Candidate{
		{ID: "d-1", Title: "Водород", CompleteForBrief: true, Measurements: measurements},
	}, Limitations: []string{"coverage is a lower bound"}}

	rows, err := profileCSVRows(briefProfiles(result))
	if err != nil {
		t.Fatal(err)
	}
	if len(rows) != 2 {
		t.Fatalf("expected a header and one direction, got %d rows", len(rows))
	}
	header, row := rows[0], rows[1]
	cells := map[string]string{}
	for i, name := range header {
		cells[name] = row[i]
	}
	for _, name := range []string{"velocity", "acceleration", "hhi"} {
		if cells[name] != "" {
			t.Fatalf("an unmeasured metric was written as %q instead of an empty cell", cells[name])
		}
		if strings.TrimSpace(cells[name+"_reason"]) == "" {
			t.Fatalf("an empty cell for %s carries no reason", name)
		}
	}
	if cells["active_quarters"] != "3" {
		t.Fatalf("a measured value was not written: %q", cells["active_quarters"])
	}
	if cells["hhi_reason"] != "not_comparable affiliations_missing" {
		t.Fatalf("the reason lost its status: %q", cells["hhi_reason"])
	}
}

func TestTechnologyCSVReadsItsOwnContractAndMissingMetrics(t *testing.T) {
	result := analysisdoc.Result{Version: analysisdoc.ResultVersion,
		Directions: []analysisdoc.ResultDirection{
			{DirectionID: "d-1", Title: "Первое", Stage: "early_signal",
				Measurements: jsontext.Value(`{"velocity":{"value":null,"reason_codes":["insufficient_quarters"]}}`)},
			{DirectionID: "d-2", Title: "Второе", Stage: "candidate_for_review"},
		}}
	raw, err := json.Marshal(result)
	if err != nil {
		t.Fatal(err)
	}
	job := analysisdoc.Job{ID: "j-1", ResultJSON: string(raw),
		Request: &analysisdoc.Request{AnalysisKind: analysisdoc.KindBriefV1}}
	recorder := httptest.NewRecorder()
	(&API{}).exportAnalysisCSV(recorder, job)
	if recorder.Code != 200 {
		t.Fatalf("v1 CSV read the wrong contract: %d %s", recorder.Code, recorder.Body.String())
	}
	rows, err := csv.NewReader(recorder.Body).ReadAll()
	if err != nil || len(rows) != 3 {
		t.Fatalf("CSV rows: %+v %v", rows, err)
	}
	if rows[1][3] != "" || rows[1][4] != "insufficient_quarters" || rows[2][4] != "not_reported" {
		t.Fatalf("CSV lost null/missing measurement reasons: %+v", rows)
	}
}

func TestAbsenceWithoutAReasonSaysSo(t *testing.T) {
	result := analysisdoc.Result{Candidates: []analysisdoc.Candidate{{ID: "d-1", Title: "Водород",
		Measurements: jsontext.Value(`{"velocity":{"value":null,"reason_codes":[]}}`)}}}
	rows, err := profileCSVRows(briefProfiles(result))
	if err != nil {
		t.Fatal(err)
	}
	if rows[1][4] != "unavailable_without_reason" {
		t.Fatalf("an unexplained absence was written as %q", rows[1][4])
	}
}

func TestUnreadableMeasurementsAreNotBlanks(t *testing.T) {
	result := analysisdoc.Result{Candidates: []analysisdoc.Candidate{{ID: "d-1", Title: "Водород",
		Measurements: jsontext.Value(`{"velocity":"not an object"}`)}}}
	if _, err := profileCSVRows(briefProfiles(result)); err == nil {
		t.Fatal("an unreadable measurement block was written as blanks")
	}
}

// signalgraph.temporal.measure смешивает нечисловой провенанс (history_kind, series) с метриками.
func TestPublishedTemporalV1CSV(t *testing.T) {
	measurements := jsontext.Value(`{
		"history_kind":"retrospective_distribution_of_current_membership",
		"earliest":{"date":{"value":"2023-10-18","precision":"day","basis":"arXiv Atom published (v1)"}},
		"earliest_meaning":"earliest_found_in_examined_corpus",
		"stars_history":"unknown_before_first_observation",
		"unallocated":[{"work_id":"paper-undated","reason":"publication_date_unknown"}],
		"unscoped_scientific_work_ids":["paper-unscoped-a","paper-unscoped-b"],
		"scientific_scope_ids":["scope-a","scope-b"],
		"units":["scientific_works"],
		"series":[
			{"period":"2023-Q1","complete_period":true,"coverage":"unknown_or_partial",
			 "found":{"scientific_works":0},"found_exposure_scientific_works":98,
			 "comparable_scientific_works":null,"comparable_exposure":null,"normalized_activity":null,
			 "coverage_basis":[],"covered_scientific_scope_ids":["scope-b"]},
			{"period":"2023-Q2","complete_period":true,"coverage":"examined_bounded_population",
			 "found":{"scientific_works":0},"found_exposure_scientific_works":0,
			 "comparable_scientific_works":0,"comparable_exposure":0,"normalized_activity":null,
			 "coverage_basis":[{"meaning":"exhausted_bounded_provider_search"}]},
			{"period":"2023-Q3","complete_period":true,"coverage":"examined_bounded_population",
			 "found":{"scientific_works":2},"found_exposure_scientific_works":10,
			 "comparable_scientific_works":2,"comparable_exposure":10,"normalized_activity":0.2}
		]}`)
	result := analysisdoc.Result{Version: analysisdoc.ResultVersion,
		Candidates: []analysisdoc.Candidate{{ID: "candidate-1", Title: "Published V1", Measurements: measurements}}}
	raw, err := json.Marshal(result)
	if err != nil {
		t.Fatal(err)
	}
	recorder := httptest.NewRecorder()
	(&API{}).exportAnalysisCSV(recorder, analysisdoc.Job{ID: "job-1", ResultJSON: string(raw)})
	if recorder.Code != 200 {
		t.Fatalf("published V1 CSV: HTTP %d %s", recorder.Code, recorder.Body.String())
	}
	rows, err := csv.NewReader(recorder.Body).ReadAll()
	if err != nil || len(rows) != 4 {
		t.Fatalf("expected one row per quarter: %v %v", rows, err)
	}
	for name, expected := range map[string][]string{
		"period":                     {"2023-Q1", "2023-Q2", "2023-Q3"},
		"found.scientific_works":     {"0", "0", "2"},
		"normalized_activity":        {"", "", "0.2"},
		"normalized_activity_reason": {"unknown_or_partial", "zero_exposure", ""},
		"comparable_exposure":        {"", "0", "10"},
	} {
		column := slices.Index(rows[0], name)
		if column < 0 {
			t.Fatalf("column %s is missing", name)
		}
		for i, want := range expected {
			if got := rows[i+1][column]; got != want {
				t.Errorf("row %d %s = %q, want %q", i+1, name, got, want)
			}
		}
	}
	contextColumn := slices.Index(rows[0], "measurement_context")
	if contextColumn < 0 {
		t.Fatal("documentary context is missing")
	}
	context := rows[1][contextColumn]
	if !jsontext.Value(context).IsValid() || !strings.Contains(context, "publication_date_unknown") ||
		!strings.Contains(context, "arXiv Atom published (v1)") || !strings.Contains(context, "scope-a") {
		t.Fatalf("CSV lost date or coverage provenance: %s", context)
	}
	for _, row := range rows[1:] {
		var context struct {
			Profile struct {
				Unscoped []string `json:"unscoped_scientific_work_ids"`
				Scopes   []string `json:"scientific_scope_ids"`
			} `json:"profile"`
		}
		if err := json.Unmarshal([]byte(row[contextColumn]), &context); err != nil ||
			!slices.Equal(context.Profile.Unscoped, []string{"paper-unscoped-a", "paper-unscoped-b"}) ||
			!slices.Equal(context.Profile.Scopes, []string{"scope-a", "scope-b"}) {
			t.Fatalf("CSV lost profile-level coverage provenance: %v %+v", err, context)
		}
	}
	if !strings.Contains(context, `"covered_scientific_scope_ids":["scope-b"]`) {
		t.Fatal("CSV lost period-level coverage provenance")
	}
}

func TestMalformedTemporalMeasurementsFailCSV(t *testing.T) {
	for _, raw := range []string{
		`{"series":"not an array"}`,
		`{"series":[{"period":"2023-Q1","coverage":"unknown_or_partial","found":{"scientific_works":"invalid"}}]}`,
		`{"series":[{"period":"2023-Q1"}]}`,
	} {
		if _, err := profileCSVRows([]exportProfile{{id: "broken", measurements: jsontext.Value(raw)}}); err == nil {
			t.Errorf("malformed temporal measurements became an empty table: %s", raw)
		}
	}
}

// pagedCatalogue: незнакомый курсор означает каталог, изменившийся под выгрузкой.
type pagedCatalogue struct {
	pages map[string]extractdoc.TechnologyPage
}

func (c pagedCatalogue) Search(_ context.Context, s extractdoc.SearchTechnologies) (extractdoc.TechnologyPage, error) {
	page, ok := c.pages[s.Cursor]
	if !ok {
		return page, extractdoc.ErrConflict
	}
	return page, nil
}

func (pagedCatalogue) ReadRevision(context.Context, extractdoc.ReadRevision) (extractdoc.RevisionTechnologies, error) {
	return extractdoc.RevisionTechnologies{}, errors.ErrUnsupported
}

func (pagedCatalogue) ReadTechnology(context.Context, extractdoc.ReadTechnology) (extractdoc.Technology, error) {
	return extractdoc.Technology{}, errors.ErrUnsupported
}

func (pagedCatalogue) Review(context.Context, extractdoc.Review) (extractdoc.Review, error) {
	return extractdoc.Review{}, errors.ErrUnsupported
}

func (pagedCatalogue) Reviews(context.Context, extractdoc.ReadReviews) ([]extractdoc.Review, error) {
	return nil, errors.ErrUnsupported
}

func TestCatalogueExportStreamsEveryPageAndMarksAShortFile(t *testing.T) {
	hit := func(id string) extractdoc.TechnologyHit {
		return extractdoc.TechnologyHit{TechnologyID: id, CanonicalName: "Tech " + id, ValidationStatus: "candidate",
			Description: "a, \"quoted\" description", MatchReasons: []string{"name", "alias"}}
	}
	first := extractdoc.TechnologyPage{Technologies: []extractdoc.TechnologyHit{hit("1"), hit("2")}, NextCursor: "c2",
		CatalogueVersion: "v1", SchemaVersion: extractdoc.CatalogueSchemaVersion}
	whole := pagedCatalogue{pages: map[string]extractdoc.TechnologyPage{
		"": first, "c2": {Technologies: []extractdoc.TechnologyHit{hit("3")}}}}
	search := extractdoc.SearchTechnologies{ReaderID: "u", IncludeAll: true, Limit: 2}

	readCSV := func(text string) [][]string {
		reader := csv.NewReader(strings.NewReader(text))
		reader.FieldsPerRecord = -1
		rows, err := reader.ReadAll()
		if err != nil {
			t.Fatalf("csv: %v", err)
		}
		return rows
	}
	var out strings.Builder
	catalogueCSV(t.Context(), &out, whole, search, first)
	rows := readCSV(out.String())
	if len(rows) != 4 || rows[3][0] != "3" || rows[1][4] != "name alias" || rows[1][3] != hit("1").Description {
		t.Fatalf("csv: %q", rows)
	}

	out.Reset()
	catalogueJSON(t.Context(), &out, whole, search, first)
	var decoded struct {
		SchemaVersion string                     `json:"schema_version"`
		Technologies  []extractdoc.TechnologyHit `json:"technologies"`
		Limitations   []string                   `json:"limitations"`
	}
	if err := json.Unmarshal([]byte(out.String()), &decoded); err != nil || len(decoded.Technologies) != 3 ||
		decoded.SchemaVersion != extractdoc.CatalogueSchemaVersion || len(decoded.Limitations) != 0 {
		t.Fatalf("json: %v %s", err, out.String())
	}

	// Каталог пересчитан после первой страницы: файл кончается отметкой, а не страницами другого каталога.
	changed := pagedCatalogue{pages: map[string]extractdoc.TechnologyPage{"": first}}
	out.Reset()
	catalogueCSV(t.Context(), &out, changed, search, first)
	rows = readCSV(out.String())
	if len(rows) != 5 || rows[3][0] != "limitations" || !strings.HasPrefix(rows[4][0], "export_incomplete") {
		t.Fatalf("short csv must say so: %q", rows)
	}
	out.Reset()
	catalogueJSON(t.Context(), &out, changed, search, first)
	if err := json.Unmarshal([]byte(out.String()), &decoded); err != nil || len(decoded.Technologies) != 2 ||
		len(decoded.Limitations) != 1 {
		t.Fatalf("short json must say so: %v %s", err, out.String())
	}

	// Пустой каталог — пустой файл с причиной.
	empty := extractdoc.TechnologyPage{Reason: extractdoc.ReasonEmptyCatalogue, SchemaVersion: extractdoc.CatalogueSchemaVersion}
	out.Reset()
	catalogueCSV(t.Context(), &out, pagedCatalogue{}, search, empty)
	if !strings.Contains(out.String(), extractdoc.ReasonEmptyCatalogue) {
		t.Fatalf("empty catalogue lost its reason: %q", out.String())
	}
}
