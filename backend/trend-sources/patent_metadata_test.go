package main

import (
	"encoding/json/v2"
	"errors"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
)

func publicSearchFixture(t *testing.T) ([]byte, patentSearchResponse, []record) {
	t.Helper()
	raw, e := os.ReadFile("testdata/uspto-public-search-39.json")
	if e != nil {
		t.Fatal(e)
	}
	var response patentSearchResponse
	if e = json.Unmarshal(raw, &response); e != nil {
		t.Fatal(e)
	}
	primary, e := os.ReadFile("testdata/uspto-primary-crosscheck39.json")
	if e != nil {
		t.Fatal(e)
	}
	var records []record
	if e = json.Unmarshal(primary, &records); e != nil {
		t.Fatal(e)
	}
	return raw, response, records
}
func TestPublicSearch39MissingTitlesAgainstPrimaryDocuments(t *testing.T) {
	raw, response, primary := publicSearchFixture(t)
	if response.Found != 39 || len(response.Patents) != 39 {
		t.Fatal("fixture must cover39 real missing titles")
	}
	page := provenance{URL: patentSearchBase + "/api/searches/searchWithBeFamily", SHA256: hashBytes(raw)}
	for _, original := range primary {
		t.Run(original.ID, func(t *testing.T) {
			i := slices.IndexFunc(response.Patents,
				func(r patentSearchRecord) bool { return normalizedPatentID(r.Number) == original.ID })
			if i < 0 {
				t.Fatal("ID absent")
			}
			found := response.Patents[i]
			row := record{ID: original.ID, Date: original.Date}
			enriched, e := applyPatentMetadata(row, found, page, "2026-09-11T12:20:00Z")
			if e != nil || enriched.Title != original.Title || enriched.Date != original.Date ||
				enriched.Filing != original.Filing || !metadataVerified(enriched) || enriched.ExcludedReason != "" {
				t.Fatalf("primary mismatch: %+v %v", enriched, e)
			}
			if found.FamilyID != nil && *found.FamilyID > 0 && enriched.FamilyBasis != "USPTO Public Search Family ID" {
				t.Fatal("family basis erased")
			}
			kept, e := applyPatentMetadata(original, found, page, "2026-09-11T12:20:00Z")
			if e != nil || kept.Priority != original.Priority || !slices.Equal(kept.PriorityIDs, original.PriorityIDs) {
				t.Fatal("primary priority lost", kept, e)
			}
			if found.FamilyID == nil && kept.FamilyID != original.FamilyID {
				t.Fatal("empty provider family erased primary")
			}
		})
	}
}
func TestStructuredGrantIdentityAndCertificateExclusion(t *testing.T) {
	_, response, _ := publicSearchFixture(t)
	original := response.Patents[0]
	row := record{ID: "US" + original.Number, Title: "old index title", Date: original.Published[:10]}
	badID := original
	badID.Number = "12728650"
	if _, err := applyPatentMetadata(row, badID, provenance{}, "2026-09-11T00:00:00Z"); err == nil {
		t.Fatal("different number accepted")
	}
	certificate := original
	certificate.Kind = []string{"C1"}
	certificate.GUID = "US-" + original.Number + "-C1"
	certificate.DocumentID = "US " + original.Number + " C1"
	excluded, err := applyPatentMetadata(row, certificate, provenance{}, "2026-09-11T00:00:00Z")
	if err != nil || !strings.Contains(excluded.ExcludedReason, "C1") {
		t.Fatal("certificate accepted as grant", excluded, err)
	}
	old := original
	old.Published = "2019-08-27T00:00:00Z"
	excluded, err = applyPatentMetadata(row, old, provenance{}, "2026-09-11T00:00:00Z")
	if err != nil || !strings.Contains(excluded.ExcludedReason, "2019-08-27") {
		t.Fatal("old grant counted as current", excluded, err)
	}
	source := &service{data: &snapshot{Records: []record{excluded}}}
	w := httptest.NewRecorder()
	source.exportRecords(w, httptest.NewRequestWithContext(t.Context(), "GET",
		"/v1/records?limit=10&from=2026-09-01&through=2026-09-11&query=*", nil))
	if w.Code != 200 || !strings.Contains(w.Body.String(), `"total":0`) {
		t.Fatal("excluded document exported", w.Body.String())
	}
	w = httptest.NewRecorder()
	query := `{"terms":["old index title"],"periods":[{"start":"2026-09-01","end_exclusive":"2026-09-12"}]}`
	source.query(w, httptest.NewRequestWithContext(t.Context(), "POST", "/v1/query", strings.NewReader(query)))
	if w.Code != 200 || !strings.Contains(w.Body.String(), `"total":0`) {
		t.Fatal("excluded document counted", w.Body.String())
	}
	short := original
	short.Title = "Cap"
	if got, e := applyPatentMetadata(row, short, provenance{}, ""); e != nil || got.Title != "Cap" {
		t.Fatal("valid short title lost", e)
	}
	for _, id := range []string{"USPP037681", "PP37681", "USPP37681"} {
		if normalizedPatentID(id) != "USPP37681" {
			t.Fatal("plant ID zero padding", id)
		}
	}
	for _, id := range []string{"US1272.pn.", "US0", "US1272 OR US4", "../US1272"} {
		if normalizedPatentID(id) != "" {
			t.Fatal("unsafe ID accepted", id)
		}
	}
}
func TestPublicSearchBatchCheckpointAndBudget(t *testing.T) {
	raw, _, records := publicSearchFixture(t)
	ids := make([]string, 0, len(records))
	for _, r := range records {
		ids = append(ids, r.ID)
	}
	calls := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls++
		if r.URL.Path == "/api/users/me/session" {
			w.Header().Set("X-Access-Token", "ephemeral-public-token")
			_, _ = w.Write([]byte(`{"userCase":{"caseId":1}}`))
			return
		}
		if r.Header.Get("X-Access-Token") != "ephemeral-public-token" {
			t.Error("session token missing")
		}
		var request map[string]any
		if err := json.UnmarshalRead(r.Body, &request); err != nil {
			t.Error(err)
		}
		if request["docFamilyFiltering"] != "noFiltering" || request["pageCount"] != float64(500) {
			t.Error("family filtering/page limit changes population", request)
		}
		_, _ = w.Write(raw)
	}))
	defer server.Close()
	s := &service{dir: t.TempDir(), config: sourceConfig{RequestBudget: 2}, http: server.Client()}
	client := &patentSearchClient{service: s, http: server.Client(), base: server.URL}
	body, receipt, err := s.metadataPage(t.Context(), client, ids, "2026-09-11T00:00:00Z")
	if err != nil || len(body) != len(raw) || calls != 2 || receipt.Found != 39 {
		t.Fatal("initial checkpoint", calls, receipt, err)
	}
	// Продолжение без бюджета запросов обязано взять сохранённые номера и хеш.
	_, saved, err := s.metadataPage(t.Context(), client, ids, "2026-09-12T00:00:00Z")
	if err != nil || saved.SHA256 != receipt.SHA256 || calls != 2 {
		t.Fatal("resume refetched", calls, err)
	}
	files, err := filepath.Glob(filepath.Join(s.dir, "patent-metadata", "*.json"))
	if err != nil || len(files) != 1 {
		t.Fatal(files, err)
	}
	receiptRaw, err := os.ReadFile(files[0])
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(receiptRaw), "ephemeral-public-token") || !strings.Contains(string(receiptRaw), "requested_ids") {
		t.Fatal("unsafe or unlinked receipt")
	}
	if _, _, err = s.metadataPage(t.Context(), client, []string{"US12729112"}, ""); err == nil || calls != 2 {
		t.Fatal("budget ignored", calls, err)
	}
	if err = os.WriteFile(filepath.Join(s.dir, "raw", receipt.SHA256), []byte("corrupt"), 0600); err != nil {
		t.Fatal(err)
	}
	if _, _, err = s.metadataPage(t.Context(), client, ids, ""); err == nil {
		t.Fatal("corrupt cached page accepted")
	}
}
func TestPublicSearchRejectsTruncatedBatch(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte(`{"numFound":2,"patents":[]}`))
	}))
	defer server.Close()
	s := &service{dir: t.TempDir(), config: sourceConfig{RequestBudget: 1}, http: server.Client()}
	c := &patentSearchClient{service: s, http: server.Client(), base: server.URL, token: "anonymous", caseID: 1}
	if _, _, err := s.metadataPage(t.Context(), c, []string{"US12729112"}, ""); err == nil {
		t.Fatal("truncated search certified")
	}
	files, err := filepath.Glob(filepath.Join(s.dir, "patent-metadata", "*.json"))
	if err != nil || len(files) != 0 {
		t.Fatal("truncated batch checkpointed", files, err)
	}
}

func TestWeeklyMetadataResumeAndCorrectDatedGrant(t *testing.T) {
	_, fixture, _ := publicSearchFixture(t)
	first, second := fixture.Patents[0], fixture.Patents[1]
	if first.Published != second.Published {
		t.Fatal("two same-week fixture records required")
	}
	firstRaw, _ := json.Marshal(patentSearchResponse{Found: 2, Patents: []patentSearchRecord{first}})
	secondRaw, _ := json.Marshal(patentSearchResponse{Found: 2, Patents: []patentSearchRecord{second}})
	starts := []int{}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/api/users/me/session" {
			w.Header().Set("X-Access-Token", "anonymous")
			_, _ = w.Write([]byte(`{"userCase":{"caseId":1}}`))
			return
		}
		var payload struct {
			Start int `json:"start"`
		}
		if e := json.UnmarshalRead(r.Body, &payload); e != nil {
			t.Error(e)
		}
		starts = append(starts, payload.Start)
		if payload.Start == 0 {
			_, _ = w.Write(firstRaw)
		} else {
			_, _ = w.Write(secondRaw)
		}
	}))
	defer server.Close()
	initial := []record{{ID: "US" + first.Number, Date: "2026-09-08"}, {ID: "US" + second.Number, Date: "2026-09-15"}}
	makeData := func() *snapshot { return &snapshot{Records: slices.Clone(initial), ObservedAt: "2026-09-11T00:00:00Z"} }
	index := map[string]int{initial[0].ID: 0, initial[1].ID: 1}
	dir := t.TempDir()
	data := makeData()
	progress := patentMetadataProgress{}
	run := func() *service {
		return &service{dir: dir, config: sourceConfig{RequestBudget: 2}, http: server.Client()}
	}
	s := run()
	client := &patentSearchClient{service: s, http: server.Client(), base: server.URL}
	if e := s.metadataWeek(t.Context(), client, data, index, "2026-09-08", &progress); !errors.Is(e, errBudget) {
		t.Fatal("bounded pass", e)
	}
	if metadataVerified(data.Records[1]) || !metadataVerified(data.Records[0]) {
		t.Fatal("unfetched page marked complete")
	}
	// Строки процесса потеряны: завершённый префикс восстанавливают неизменные сырые точки, а не одно смещение.
	data = makeData()
	s = run()
	client = &patentSearchClient{service: s, http: server.Client(), base: server.URL}
	if e := s.metadataWeek(t.Context(), client, data, index, "2026-09-08", &progress); e != nil {
		t.Fatal("resume", e)
	}
	if !slices.Equal(starts, []int{0, 1}) {
		t.Fatal("saved first page refetched or gap", starts)
	}
	if !metadataVerified(data.Records[1]) || data.Records[1].Date != "2026-09-08" || data.Records[1].ExcludedReason != "" {
		t.Fatal("dated original grant lost", data.Records[1])
	}
	if !strings.Contains(data.Records[1].DateBasis, "later Gazette occurrence") ||
		!strings.HasPrefix(data.Records[1].URL, patentSearchBase) {
		t.Fatal("wrong occurrence basis or source link")
	}
}

func TestPublicSearchSessionRefreshAndCooldown(t *testing.T) {
	raw, _, rows := publicSearchFixture(t)
	statuses := []int{http.StatusUnauthorized, http.StatusTooManyRequests, http.StatusServiceUnavailable}
	for _, status := range statuses {
		t.Run(http.StatusText(status), func(t *testing.T) {
			calls, searches := 0, 0
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				calls++
				if r.URL.Path == "/api/users/me/session" {
					w.Header().Set("X-Access-Token", "renewed")
					_, _ = w.Write([]byte(`{"userCase":{"caseId":42}}`))
					return
				}
				searches++
				if searches == 1 {
					w.Header().Set("Retry-After", "600")
					w.WriteHeader(status)
					return
				}
				var request struct {
					Query struct {
						Case int64 `json:"caseId"`
					} `json:"query"`
				}
				if e := json.UnmarshalRead(r.Body, &request); e != nil || request.Query.Case != 42 ||
					r.Header.Get("X-Access-Token") != "renewed" {
					t.Error("normal refreshed session request not used", request, e)
				}
				_, _ = w.Write(raw)
			}))
			defer server.Close()
			s := &service{dir: t.TempDir(), config: sourceConfig{RequestBudget: 3}, http: server.Client()}
			c := &patentSearchClient{service: s, http: server.Client(), base: server.URL, token: "expired", caseID: 1}
			_, receipt, err := s.metadataPage(t.Context(), c, []string{rows[0].ID}, "2026-09-11T00:00:00Z")
			if status == http.StatusUnauthorized {
				if err != nil || calls != 3 || searches != 2 ||
					receipt.SHA256 != hashBytes(raw) {
					t.Fatal("ordinary session refresh failed", calls, searches, err)
				}
				return
			}
			if err == nil || calls != 1 {
				t.Fatal("rate limit ignored", calls, err)
			}
			// Новый клиент или процесс тоже соблюдает сохранённую паузу провайдера.
			c = &patentSearchClient{service: s, http: server.Client(), base: server.URL}
			_, _, err = s.metadataPage(t.Context(), c, []string{rows[0].ID}, "")
			if err == nil || !strings.Contains(err.Error(), "postponed until") || calls != 1 || s.requests != 1 {
				t.Fatal("durable provider cooldown ignored", calls, s.requests, err)
			}
		})
	}
}

func TestWeeklyMetadataFailsClosedAndKeepsValidRows(t *testing.T) {
	_, response, _ := publicSearchFixture(t)
	first, second := response.Patents[0], response.Patents[1]
	day := first.Published[:10]
	for _, mode := range []string{"duplicate", "wrong_date", "invalid_title", "reissue"} {
		t.Run(mode, func(t *testing.T) {
			rows := []record{
				{ID: "US" + first.Number, Date: day, Source: "USPTO", ExcludedReason: "new-grant metadata not yet verified"},
				{ID: "US" + second.Number, Date: day, Source: "USPTO", ExcludedReason: "new-grant metadata not yet verified"},
			}
			bad := first
			switch mode {
			case "duplicate":
				bad = second
			case "wrong_date":
				bad.Published = "2026-09-01T00:00:00Z"
			case "invalid_title":
				bad.Title = ""
			case "reissue":
				bad.Number, bad.GUID, bad.DocumentID, bad.Kind = "RE51026", "US-RE51026-E", "US RE51026 E", []string{"E"}
				rows[0].ID = "USRE51026"
			}
			data := &snapshot{Records: rows, Coverage: []coverage{{Source: "USPTO", From: day, Through: day, Exhausted: true}}}
			index := map[string]int{rows[0].ID: 0, rows[1].ID: 1}
			page := provenance{URL: patentSearchBase + "/api/searches/searchWithBeFamily", SHA256: strings.Repeat("a", 64)}
			err := applyPatentWeekPage(data, index, day, map[string]bool{}, []patentSearchRecord{bad, second}, page, "")
			if mode == "duplicate" || mode == "wrong_date" {
				if err == nil {
					t.Fatal("invalid page certified", mode)
				}
				return
			}
			if err != nil || !metadataVerified(data.Records[1]) {
				t.Fatal("healthy row lost", err)
			}
			metadataCoverage(data, "pending")
			if !slices.Contains(data.Coverage[0].Pages, page) {
				t.Fatal("per-window raw provenance omitted")
			}
			progress := patentMetadataProgress{}
			updatePatentProgress(&progress, data, "", 0)
			if mode == "invalid_title" {
				if metadataVerified(data.Records[0]) || data.Coverage[0].Exhausted || progress.Exhausted ||
					progress.Pending != 1 {
					t.Fatal("malformed title certified", data.Records[0], progress)
				}
			} else if !metadataVerified(data.Records[0]) || data.Records[0].ExcludedReason != "" {
				t.Fatal("official E reissue kind rejected", data.Records[0])
			}
		})
	}
}

func TestPublicSearchTransportIsScoped(t *testing.T) {
	defaultTransport, ok := http.DefaultTransport.(*http.Transport)
	if !ok {
		t.Fatal("standard HTTP transport required")
	}
	transport := defaultTransport.Clone()
	tlsBefore, protocolsBefore, forceBefore := transport.TLSClientConfig, transport.Protocols, transport.ForceAttemptHTTP2
	original := &http.Client{Transport: transport}
	s := &service{http: original}
	c := newPatentSearchClient(s)
	changed, ok := c.http.Transport.(*http.Transport)
	if !ok {
		t.Fatal("PPS transport changed type")
	}
	if s.http != original || s.http.Transport != transport || transport.TLSClientConfig != tlsBefore ||
		transport.Protocols != protocolsBefore || transport.ForceAttemptHTTP2 != forceBefore {
		t.Fatal("shared scientific HTTP client changed")
	}
	if c.http == original || changed == transport || changed.ForceAttemptHTTP2 ||
		changed.Protocols.HTTP2() || !changed.Protocols.HTTP1() {
		t.Fatal("PPS transport does not preserve a separate HTTP1 route")
	}
}

func withdrawnFixture(t *testing.T) (patentSearchResponse, patentSearchRecord, patentSearchRecord, patentSearchRecord) {
	t.Helper()
	raw, e := os.ReadFile("testdata/uspto-public-search-withdrawn.json")
	if e != nil {
		t.Fatal(e)
	}
	var response patentSearchResponse
	if e = json.Unmarshal(raw, &response); e != nil {
		t.Fatal(e)
	}
	if len(response.Patents) != 3 {
		t.Fatal("fixture must hold a grant and both withdrawn shapes")
	}
	return response, response.Patents[0], response.Patents[1], response.Patents[2]
}

func TestWithdrawnNumbersAreExcludedAndLegacyTitleMarkupIsNotAConflict(t *testing.T) {
	response, grant, withdrawn, suppressed := withdrawnFixture(t)
	page := provenance{URL: patentSearchBase + "/api/searches/searchWithBeFamily", SHA256: "sha"}
	for _, found := range []patentSearchRecord{withdrawn, suppressed} {
		row := record{ID: patentRecordID(found), Date: "2025-09-16", Title: "pre-issue Gazette title"}
		got, err := applyPatentMetadata(row, found, page, "2026-09-12T00:00:00Z")
		if err != nil || !metadataVerified(got) || !strings.Contains(got.ExcludedReason, "withdrew") {
			t.Fatal("withdrawn number left pending", got.ID, got.ExcludedReason, err)
		}
		if got.KindCode != "" || got.PublishedAt != "" || got.Filing != "" || got.Priority != "" {
			t.Fatal("withdrawn number given grant facts", got)
		}
	}
	fake := withdrawn
	fake.Kind = []string{"B2"}
	if _, err := applyPatentMetadata(record{ID: patentRecordID(fake), Date: "2025-09-16"}, fake, page, ""); err == nil {
		t.Fatal("withdrawn record with grant fields accepted")
	}
	data := &snapshot{Records: []record{
		{ID: patentRecordID(suppressed), Date: "2025-11-25", Source: patentSource},
	}}
	if err := applyPatentTail(data, []int{0}, response.Patents, patentMetadataReceipt{URL: page.URL}); err != nil {
		t.Fatal(err)
	}
	if !metadataVerified(data.Records[0]) || data.Records[0].ExcludedReason == "" {
		t.Fatal("suppressed publication reference did not reach its row", data.Records[0])
	}
	primary := record{
		ID: patentRecordID(grant), Date: "2026-08-04", ParserVersion: primaryPatentParserVersion,
		TitleBasis: "USPTO primary document title",
		Title: "Methods and compositions for production of CO2-free hydrogen and carbon nanomaterials " +
			"by methane decomposition",
	}
	got, err := applyPatentMetadata(primary, grant, page, "2026-09-12T00:00:00Z")
	if err != nil || !metadataVerified(got) || got.ExcludedReason != "" {
		t.Fatal("legacy subscript markup treated as a title conflict", err, got.ExcludedReason)
	}
	if got.Title != primary.Title || got.TitleBasis != primary.TitleBasis {
		t.Fatal("verified primary title replaced by provider markup", got.Title, got.TitleBasis)
	}
	other := primary
	other.Title = "Apparatus for drying laundry"
	if _, err = applyPatentMetadata(other, grant, page, ""); err == nil {
		t.Fatal("genuinely different title accepted")
	}
}
