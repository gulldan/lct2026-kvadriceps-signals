package application

import (
	"context"
	"encoding/json/v2"
	"io"
	"net/http"
	"slices"
	"strconv"
	"strings"
	"sync/atomic"
	"testing"
	"testing/synctest"
	"time"
)

func publisherRevisionFixture(t *testing.T, id int64, concept string) string {
	t.Helper()
	record := liveZenodoRecord{ID: id, DOI: liveZenodoDOIPrefix + strconv.FormatInt(id, 10),
		ConceptDOI: liveZenodoDOIPrefix + concept, Updated: time.Date(2026, 8, 28, 8, 45, 0, 0, time.UTC)}
	record.Metadata.DOI = record.DOI
	record.Metadata.Description = "<p>Correction: pruning reduced accuracy by 6 percentage points.</p>" +
		"<p>Earlier description retained for the record: pruning improved accuracy.</p>"
	raw, err := json.Marshal(record)
	if err != nil {
		t.Fatal(err)
	}
	return string(raw)
}

func publisherPapers(t *testing.T, count int) ([]StudyPaper, map[string]LivePaperMetadata) {
	t.Helper()
	papers := make([]StudyPaper, 0, count)
	metadata := make(map[string]LivePaperMetadata, count)
	for i := range count {
		doi := liveZenodoDOIPrefix + strconv.Itoa(i+1)
		paper := StudyPaper{ID: "openalex:" + doi, CanonicalID: doi, Title: "Selective synaptic pruning research",
			Date: "2026-08-01", URL: "https://doi.org/" + doi, ContentHash: "old-bibliographic-hash"}
		papers = append(papers, paper)
		metadata[paper.ID] = LivePaperMetadata{DOI: doi, Abstract: "Pruning improved accuracy.", SourceURL: paper.URL,
			ContentHash: paper.ContentHash, Digest: "Старый положительный результат", DigestModel: "old-model",
			ObservedAt: "2026-08-02T00:00:00Z", AffiliationCheckedAt: "2026-08-03T00:00:00Z"}
	}
	return papers, metadata
}

func publisherResponse(req *http.Request, status int, body string) *http.Response {
	return &http.Response{Request: req, StatusCode: status,
		Header: http.Header{"Content-Type": {"application/json"}}, Body: io.NopCloser(strings.NewReader(body))}
}

func TestPublisherRevisionReachesCandidateEvidence(t *testing.T) {
	for _, mode := range []string{"concept_redirect", "concept_direct", "pinned"} {
		t.Run(mode, func(t *testing.T) {
			papers, metadata := publisherPapers(t, 1)
			if mode == "pinned" {
				papers[0].CanonicalID = liveZenodoDOIPrefix + "2"
				meta := metadata[papers[0].ID]
				meta.DOI = papers[0].CanonicalID
				metadata[papers[0].ID] = meta
			}
			original := papers[0]
			old := metadata[original.ID]
			raw := publisherRevisionFixture(t, 2, "1")
			var calls atomic.Int32
			client := livePublisherClient()
			client.Transport = liveRetryTransport(func(req *http.Request) (*http.Response, error) {
				calls.Add(1)
				if !livePublisherAddress(req.URL) || req.Header.Get("Authorization") != "" || req.URL.RawQuery != "" {
					t.Errorf("unsafe metadata request: %s", req.URL)
				}
				if mode == "concept_redirect" && req.URL.Path == "/api/records/1" {
					response := publisherResponse(req, http.StatusFound, "")
					response.Header.Set("Location", "/api/records/2")
					return response, nil
				}
				return publisherResponse(req, http.StatusOK, raw), nil
			})
			papers, limits := liveRefreshPublisherRevisions(t.Context(), papers, metadata, client)
			if len(papers) != 1 || len(limits) != 0 {
				t.Fatalf("revision failed: papers=%d calls=%d limits=%v", len(papers), calls.Load(), limits)
			}
			meta := metadata[original.ID]
			revision := meta.SourceRevision
			if revision == nil || revision.RequestedDOI != old.DOI || revision.ResolvedDOI != liveZenodoDOIPrefix+"2" ||
				revision.ConceptDOI != liveZenodoDOIPrefix+"1" || revision.Coverage != livePublisherCoverage ||
				revision.RawSHA256 != liveCardTextHash(raw) || revision.TextSHA256 != liveCardTextHash(meta.Abstract) ||
				revision.FetchedAt.IsZero() || revision.SourceUpdatedAt.IsZero() {
				t.Fatalf("revision identity or hashes lost: %+v", revision)
			}
			if papers[0].ID != original.ID || papers[0].CanonicalID != original.CanonicalID ||
				papers[0].Date != original.Date || meta.ObservedAt != old.ObservedAt ||
				meta.AffiliationCheckedAt != old.AffiliationCheckedAt || meta.Digest != "" || meta.DigestModel != "" ||
				papers[0].URL != revision.SourceURL || meta.SourceURL != revision.SourceURL ||
				papers[0].ContentHash == original.ContentHash {
				t.Fatal("update changed historical identity or reused a stale digest")
			}
			candidate := LiveTopic{PaperIDs: []string{original.ID}}
			byID := map[string]StudyPaper{original.ID: papers[0]}
			qualification := liveQualificationInputs(candidate, byID, metadata)
			cards, _ := liveCardInput(candidate, byID, metadata)
			extraction := liveExtractBatches(papers, metadata, liveExtractAbstract)
			for _, text := range []string{qualification[0].Text, cards[0], extraction[0][0].Text} {
				if !strings.Contains(text, "Correction: pruning reduced accuracy by 6") ||
					!strings.Contains(text, "Earlier description retained") {
					t.Fatalf("consumer received stale or silently rewritten evidence: %s", text)
				}
			}
			for _, accepted := range [][]LiveTopic{nil, {candidate}} {
				detail := liveDetail(&LiveSnapshot{}, candidate, papers, nil, accepted, nil, metadata)
				if got := detail.SourceRevisions[original.ID]; got.TextSHA256 != revision.TextSHA256 {
					t.Fatal("answer lost the exact author revision")
				}
			}
			if calls.Load() != 1 && mode != "concept_redirect" || mode == "concept_redirect" && calls.Load() != 2 {
				t.Fatalf("unexpected request count: %d", calls.Load())
			}
		})
	}
}

func TestPublisherRevisionRejectsUnboundOrUnavailableSources(t *testing.T) {
	for _, mode := range []string{"invalid_doi", "wrong_parent", "wrong_record", "wrong_metadata_doi", "pinned_changed",
		"offhost", "port", "query", "fragment", "encoded_path", "loop", "forbidden", "invalid_json", "oversize",
		"empty_description", "wrong_media", "oversized_response"} {
		t.Run(mode, func(t *testing.T) {
			papers, metadata := publisherPapers(t, 1)
			raw := publisherRevisionFixture(t, 2, "1")
			var calls atomic.Int32
			client := livePublisherClient()
			client.Transport = liveRetryTransport(func(req *http.Request) (*http.Response, error) {
				calls.Add(1)
				response := publisherResponse(req, http.StatusOK, raw)
				locations := map[string]string{"offhost": "https://evil.example/api/records/2",
					"port": "https://zenodo.org:444/api/records/2", "query": "/api/records/2?url=secret",
					"fragment": "/api/records/2#part", "encoded_path": "/api/records/%32", "loop": "/api/records/1"}
				if location := locations[mode]; location != "" {
					response.StatusCode = http.StatusFound
					response.Header.Set("Location", location)
				}
				if mode == "forbidden" {
					response.StatusCode = http.StatusForbidden
				}
				if mode == "wrong_media" {
					response.Header.Set("Content-Type", "text/html")
				}
				return response, nil
			})
			switch mode {
			case "invalid_doi", "pinned_changed":
				id := "3"
				if mode == "invalid_doi" {
					id = "1/../../secret"
				}
				papers[0].CanonicalID = liveZenodoDOIPrefix + id
			case "wrong_parent":
				raw = publisherRevisionFixture(t, 2, "3")
			case "wrong_record":
				raw = strings.Replace(raw, `"id":2`, `"id":3`, 1)
			case "wrong_metadata_doi":
				raw = strings.Replace(raw, `"metadata":{"doi":"10.5281/zenodo.2"`,
					`"metadata":{"doi":"10.5281/zenodo.3"`, 1)
			case "invalid_json":
				raw = `{`
			case "oversize":
				raw = strings.Replace(raw, "Correction:", strings.Repeat("a", liveCardAbstract+1), 1)
			case "empty_description":
				var record liveZenodoRecord
				if err := json.Unmarshal([]byte(raw), &record); err != nil {
					t.Fatal(err)
				}
				record.Metadata.Description = "<script>hidden text</script>"
				body, err := json.Marshal(record)
				if err != nil {
					t.Fatal(err)
				}
				raw = string(body)
			case "oversized_response":
				raw = strings.Repeat(" ", livePublisherMaxBody+1)
			}
			kept, limits := liveRefreshPublisherRevisions(t.Context(), papers, metadata, client)
			if len(kept) != 0 || len(metadata) != 0 || len(limits) != 1 || limits[0].Code != livePublisherLimit {
				t.Fatalf("unverified source survived: papers=%d metadata=%d limits=%v", len(kept), len(metadata), limits)
			}
			want := int32(1)
			switch mode {
			case "invalid_doi":
				want = 0
			case "loop":
				want = 2
			}
			if calls.Load() != want {
				t.Fatalf("unexpected network followup: %d want%d", calls.Load(), want)
			}
		})
	}
}

func TestPublisherRevisionBudgetAndCancellation(t *testing.T) {
	for _, mode := range []string{"unique_cap", "total_deadline", "redirect_deadline", "parent_cancel", "rate_limit",
		"forbidden_one", "dedup_private", "deadline_before_cancel"} {
		t.Run(mode, func(t *testing.T) {
			synctest.Test(t, func(t *testing.T) {
				papers, metadata := publisherPapers(t, 40)
				if mode == "dedup_private" {
					papers = slices.Clone(papers[:1])
					duplicate := papers[0]
					duplicate.ID = "openalex:alias"
					metadata[duplicate.ID] = metadata[papers[0].ID]
					papers = append(papers, duplicate)
					private := papers[0]
					private.ID, private.DocumentID, private.SnapshotID = "openalex:private", "document", "snapshot"
					metadata[private.ID] = metadata[papers[0].ID]
					papers = append(papers, private)
				}
				parent := t.Context()
				if mode == "deadline_before_cancel" {
					parent = livePublisherPendingDeadline{deadline: time.Now()}
				}
				ctx, cancel := context.WithCancel(parent)
				defer cancel()
				if mode == "parent_cancel" {
					go func() { time.Sleep(time.Second); cancel() }()
				}
				var calls atomic.Int32
				client := livePublisherClient()
				client.Transport = liveRetryTransport(func(req *http.Request) (*http.Response, error) {
					calls.Add(1)
					if mode == "total_deadline" || mode == "parent_cancel" || mode == "redirect_deadline" {
						wait := 10 * time.Second
						if mode == "redirect_deadline" {
							wait = 6 * time.Second
						}
						select {
						case <-req.Context().Done():
							return nil, req.Context().Err()
						case <-time.After(wait):
						}
						response := publisherResponse(req, http.StatusFound, "")
						response.Header.Set("Location", "/api/records/2")
						return response, nil
					}
					if mode == "rate_limit" {
						return publisherResponse(req, http.StatusTooManyRequests, ""), nil
					}
					id := strings.TrimPrefix(req.URL.Path, "/api/records/")
					if mode == "forbidden_one" && id == "1" {
						return publisherResponse(req, http.StatusForbidden, ""), nil
					}
					number, _ := strconv.ParseInt(id, 10, 64)
					return publisherResponse(req, http.StatusOK, publisherRevisionFixture(t, number, id)), nil
				})
				if mode == "redirect_deadline" {
					papers = papers[:1]
				}
				started := time.Now()
				kept, limits := liveRefreshPublisherRevisions(ctx, papers, metadata, client)
				elapsed := time.Since(started)
				switch mode {
				case "unique_cap":
					if calls.Load() != 32 || len(kept) != 32 || len(limits) != 1 {
						t.Fatalf("unique bound lost: calls%d kept%d limits%v", calls.Load(), len(kept), limits)
					}
				case "total_deadline":
					if elapsed != livePublisherBudget || calls.Load() > 10 || len(kept) != 0 {
						t.Fatalf("overall budget lost: %s calls%d kept%d", elapsed, calls.Load(), len(kept))
					}
				case "redirect_deadline":
					if elapsed != livePublisherRequest || calls.Load() != 2 || len(kept) != 0 {
						t.Fatalf("redirect restarted deadline: %s calls%d kept%d", elapsed, calls.Load(), len(kept))
					}
				case "parent_cancel":
					if elapsed != time.Second || calls.Load() > 2 || len(kept) != 0 {
						t.Fatalf("parent cancellation ignored: %s calls%d", elapsed, calls.Load())
					}
				case "rate_limit":
					if calls.Load() > 2 || len(kept) != 0 || ctx.Err() != nil {
						t.Fatalf("429 continued dispatch or cancelled parent: calls%d kept%d", calls.Load(), len(kept))
					}
				case "forbidden_one":
					if calls.Load() != 32 || len(kept) != 31 || len(limits) != 1 || ctx.Err() != nil {
						t.Fatalf("one forbidden record blocked others: calls%d kept%d limits%v", calls.Load(), len(kept), limits)
					}
				case "dedup_private":
					if calls.Load() != 1 || len(kept) != 3 || metadata["openalex:private"].SourceRevision != nil ||
						len(liveSourceRevisions(kept, metadata)) != 2 {
						t.Fatal("duplicate fetched twice or private evidence exposed")
					}
				case "deadline_before_cancel":
					if calls.Load() != 0 || len(kept) != 0 || elapsed != 0 || ctx.Err() != nil {
						t.Fatalf("expired deadline started HTTP before cancellation: calls%d kept%d elapsed%s err%v",
							calls.Load(), len(kept), elapsed, ctx.Err())
					}
				}
			})
		})
	}
	for _, address := range []string{"127.0.0.1:443", "[::1]:443", "10.0.0.1:443", "100.64.0.1:443"} {
		if livePublisherPublicAddress("tcp", address, nil) == nil {
			t.Fatalf("private network destination allowed: %s", address)
		}
	}
}

// Таймер достиг срока, но уведомление об отмене ещё не получило процессорное время.
type livePublisherPendingDeadline struct {
	deadline time.Time
}

func (c livePublisherPendingDeadline) Deadline() (time.Time, bool) { return c.deadline, true }
func (livePublisherPendingDeadline) Done() <-chan struct{}         { return nil }
func (livePublisherPendingDeadline) Err() error                    { return nil }
func (livePublisherPendingDeadline) Value(any) any                 { return nil }
