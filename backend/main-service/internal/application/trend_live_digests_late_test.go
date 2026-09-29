package application

import (
	"context"
	"encoding/json/v2"
	"net/http"
	"net/http/httptest"
	"slices"
	"sync/atomic"
	"testing"
	"time"
)

func TestLiveDigestsPreservePublisherRevision(t *testing.T) {
	const revised = "Revised description: the validation claim is withdrawn; this is an untested proposal."
	const original = "Original description for the unrelated source."
	for _, mode := range []string{"fresh_stale", "fresh_empty", "cached_stale", "cached_mixed", "cached_all_revised"} {
		t.Run(mode, func(t *testing.T) {
			service := NewTrendLiveService(nil, nil)
			detail := LiveDetail{RunID: "preserved-run", Candidates: []LiveTopic{{PaperIDs: []string{"revised", "other"}}},
				Papers:          []StudyPaper{{ID: "revised", Title: "Revised source"}, {ID: "other", Title: "Other source"}},
				Passports:       map[string]LivePaperPassport{"revised": {Language: "en"}, "other": {Language: "en"}},
				SourceRevisions: map[string]LivePublisherRevision{"revised": {Text: revised}}}
			metadata := map[string]LivePaperMetadata{
				"revised": {Abstract: "Obsolete description: validation succeeded."}, "other": {Abstract: original}}
			if mode == "fresh_empty" {
				metadata["revised"] = LivePaperMetadata{}
			}
			if mode == "cached_all_revised" {
				detail.SourceRevisions["other"] = LivePublisherRevision{Text: original}
			}
			want := map[string]string{"Revised source": revised, "Other source": original}
			var calls, requests atomic.Int32
			service.digest = func(_ context.Context, title, abstract string) (string, string, error) {
				calls.Add(1)
				if abstract != want[title] || want[title] == "" {
					t.Errorf("digest lost the persisted source revision: %q / %q", title, abstract)
				}
				return "Пересказ: " + abstract, "digest-model", nil
			}
			collector := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				requests.Add(1)
				if mode == "cached_all_revised" {
					http.Error(w, "collector unavailable", http.StatusServiceUnavailable)
					return
				}
				var request struct {
					IDs []string `json:"ids"`
				}
				if err := json.UnmarshalRead(r.Body, &request); err != nil || !slices.Equal(request.IDs, []string{"other"}) {
					t.Errorf("collector must only receive unresolved IDs: %+v %v", request, err)
				}
				// Даже лишний ответ сборщика не вправе подменить сохранённую редакцию.
				if err := json.MarshalWrite(w, map[string]string{"revised": metadata["revised"].Abstract, "other": original}); err != nil {
					t.Error(err)
				}
			}))
			t.Cleanup(collector.Close)
			service.collectorURL = collector.URL
			var filled LiveDetail
			switch mode {
			case "fresh_stale", "fresh_empty":
				service.startLateDigests(t.Context(), &detail, metadata)
				if !detail.DigestsPending || detail.lateDigests == nil {
					t.Fatal("fresh source digests did not start")
				}
				filled = liveWithDigests(detail, <-detail.lateDigests)
			default:
				if mode == "cached_stale" {
					service.windowMeta = metadata
				}
				raw, err := json.Marshal(detail)
				if err != nil {
					t.Fatal(err)
				}
				var reopened LiveDetail
				if err = json.Unmarshal(raw, &reopened); err != nil {
					t.Fatal(err)
				}
				var changed bool
				filled, changed, err = service.backfillSourceDigests(t.Context(), reopened)
				if err != nil || !changed {
					t.Fatalf("persisted revision could not be replayed: changed=%v err=%v", changed, err)
				}
			}
			wantRequests := int32(0)
			if mode == "cached_mixed" {
				wantRequests = 1
			}
			if requests.Load() != wantRequests || calls.Load() != 2 || filled.DigestsPending ||
				len(liveUndigested(filled)) != 0 || filled.RunID != detail.RunID {
				t.Fatalf("unexpected digest replay: requests=%d calls=%d pending=%v missing=%v run=%q",
					requests.Load(), calls.Load(), filled.DigestsPending, liveUndigested(filled), filled.RunID)
			}
			if filled.Passports["revised"].Digest != "Пересказ: "+revised || detail.Passports["revised"].Digest != "" ||
				filled.SourceRevisions["revised"].Text != revised {
				t.Fatal("repair changed the source revision or the original answer")
			}
		})
	}
}

func TestLiveUndigested(t *testing.T) {
	topic := LiveTopic{Card: &LiveCard{Evidence: []LiveClaimEvidence{{SourceID: "rec:quote"}}}}
	topic.PaperIDs = []string{"arxiv:en", "arxiv:ru", "arxiv:done"}
	detail := LiveDetail{Candidates: []LiveTopic{topic},
		Papers: []StudyPaper{{ID: "arxiv:en", Title: "A"}, {ID: "arxiv:ru", Title: "Б"},
			{ID: "arxiv:done", Title: "C"}, {ID: "rec:quote", Title: "D"}},
		Passports: map[string]LivePaperPassport{"arxiv:en": {Language: "en"}, "arxiv:ru": {Language: "ru"},
			"arxiv:done": {Language: "en", Digest: "пересказ", DigestModel: "m"}}}
	missing := liveUndigested(detail)
	if !slices.Equal(missing, []string{"arxiv:en", "rec:quote"}) {
		t.Fatalf("undigested sources: %v", missing)
	}
	detail.DigestsPending = true
	filled := liveWithDigests(detail, map[string]liveDigest{"arxiv:en": {text: "о чём", model: "m"}})
	if filled.DigestsPending || filled.Passports["arxiv:en"].Digest != "о чём" || filled.Passports["arxiv:en"].Language != "en" ||
		detail.Passports["arxiv:en"].Digest != "" {
		t.Fatalf("digests must land in a copy of the passports: %+v", filled.Passports)
	}
	bare := LiveTopic{}
	bare.PaperIDs = []string{"arxiv:en"}
	detail.Candidates = []LiveTopic{bare}
	filled = liveWithDigests(detail, map[string]liveDigest{"arxiv:en": {text: "о чём", model: "m"}})
	if filled.Candidates[0].Card == nil || filled.Candidates[0].Card.Summary != "о чём" || detail.Candidates[0].Card != nil {
		t.Fatalf("a top-up without a card must get the late digest as its description: %+v", filled.Candidates[0].Card)
	}
}

func TestLiveOpenTopicDigestsOnce(t *testing.T) {
	store := &channelTestStore{}
	live := NewTrendLiveService(store, nil)
	var calls atomic.Int32
	live.digest = func(context.Context, string, string) (string, string, error) {
		calls.Add(1)
		return "пересказ", "m", nil
	}
	topic := LiveTopic{StudyTopic: StudyTopic{ID: "d"}, Name: "D"}
	topic.PaperIDs = []string{"p"}
	candidate := LiveTopic{StudyTopic: StudyTopic{ID: "t"}, Name: "T", TZ: &LiveTZAssessment{StageCode: 1}}
	candidate.PaperIDs = []string{"p"}
	live.snapshot = &LiveSnapshot{ObservedAt: "2026-09-25T00:00:00Z", Topics: []LiveTopic{topic},
		Papers:        []StudyPaper{{ID: "p", Title: "A paper"}},
		PaperMetadata: map[string]LivePaperMetadata{"p": {Abstract: "About", Language: "en"}}}
	candidate.Qualification = storedCandidateFixture("t").Qualification
	scoped := liveScopedDetailFixture(topic.Name, LiveDetail{Candidates: []LiveTopic{candidate}})
	live.directions["d"] = liveDirectionAnswer{RunID: "r", Scope: scoped.Scope, Candidates: scoped.Candidates}
	if detail, _ := live.OpenTopic(t.Context(), "d"); !detail.DigestsPending {
		t.Fatal("the page must poll while digests are written")
	}
	for deadline := time.Now().Add(5 * time.Second); ; time.Sleep(10 * time.Millisecond) {
		live.mu.RLock()
		done := live.directionDigests["d"].done
		live.mu.RUnlock()
		if done {
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("digests were not written")
		}
	}
	detail, _ := live.OpenTopic(t.Context(), "d")
	if detail.DigestsPending || detail.Passports["p"].Digest != "пересказ" || calls.Load() != 1 || len(store.raw) == 0 ||
		detail.Candidates[0].Card == nil || detail.Candidates[0].Card.Summary != candidate.Qualification.Mechanism {
		t.Fatalf("pending=%v digest=%q calls=%d stored=%d", detail.DigestsPending, detail.Passports["p"].Digest,
			calls.Load(), len(store.raw))
	}
}
