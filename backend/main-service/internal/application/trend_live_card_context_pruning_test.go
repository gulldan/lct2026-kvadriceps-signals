package application

import (
	"bytes"
	"encoding/json/v2"
	"reflect"
	"testing"

	"github.com/example/main-service/internal/platform/analysisdoc"
)

func TestLiveCardContextsFollowRetainedSourcesFreshAndStored(t *testing.T) {
	const primary = "openalex:10.1016/j.ensm.2026.105169"
	const retained = "rec:datacite:10.6084/m9.figshare.34533795"
	const removed = "rec:https://doi.org/10.18103/mra.v14i5.7521"
	for _, mode := range []string{"fresh", "stored"} {
		t.Run(mode, func(t *testing.T) {
			candidate := storedCandidateFixture("transport pathways")
			candidate.PaperIDs = []string{primary}
			candidate.EvidenceIDs = []string{retained, removed}
			candidate.DocumentsCount, candidate.RecordsCount = 3, 2
			candidate.Qualification.Evidence[0].SourceID = primary
			candidate.marketIDs = []string{removed}
			candidate.Web = &LiveChannel{Total: 1, Records: []LiveSourceRecord{{ID: removed[4:]}}}
			contexts := []LiveCardSourceContext{
				{SourceID: primary, DOI: "10.1016/j.ensm.2026.105169", Coverage: liveCardContextAbstract,
					Text: "A polymer preserves contact in solid-state batteries.", TextSHA256: "primary-hash"},
				{SourceID: retained, DOI: "10.6084/m9.figshare.34533795", Coverage: liveCardContextAbstract,
					Text: "Retained scientific dataset.", TextSHA256: "dataset-hash"},
				{SourceID: removed, DOI: "10.18103/mra.v14i5.7521", Coverage: liveCardContextAbstract,
					Text: "Nuclear transport pathways in fungal infections.", TextSHA256: "removed-hash"},
			}
			original := &LiveCard{Summary: "Научный результат", Advantage: "Сохраняется контакт",
				Missing: []string{"кейс-пример: нет подтверждённого утверждения и цитаты"},
				Evidence: []LiveClaimEvidence{{Field: liveClaimSummary, SourceID: primary, Verified: true},
					{Field: liveClaimAdvantage, SourceID: retained, Verified: true}},
				SourceContexts: contexts, Basis: "Проверено по источникам" + liveCardContextBasis(contexts)}
			candidate.Card = original
			candidate.ModelScore = &LiveModelScore{Label: liveClassifierInsufficient, DataGap: liveGapTooEarly,
				Value: .9, Threshold: .23}
			score := *candidate.ModelScore
			if mode == "fresh" {
				liveKeepCitedMarket(&candidate)
			} else {
				// Старый ответ уже отбросил запись, но сохранил её контекст; чтение не запускает новый сбор.
				candidate.EvidenceIDs = []string{retained}
				candidate.DocumentsCount, candidate.RecordsCount = 2, 1
				candidate.marketIDs, candidate.Web = nil, nil
				detail := liveScopedDetailFixture("аккумуляторы", LiveDetail{Candidates: []LiveTopic{candidate}})
				raw, err := json.Marshal(detail)
				if err != nil {
					t.Fatal(err)
				}
				store := newFakeSearchStore()
				key := liveStoreKey("аккумуляторы")
				store.stored[key] = analysisdoc.LiveSearchSave{Query: key, Detail: raw}
				restored, ok := storeService(t, store).storedSearch(t.Context(), "аккумуляторы", nil)
				if !ok || len(restored.Candidates) != 1 || !bytes.Equal(store.stored[key].Detail, raw) {
					t.Fatal("reading the old answer failed or rewrote its stored evidence")
				}
				candidate = restored.Candidates[0]
			}
			want := *original
			want.SourceContexts = contexts[:2]
			want.Basis = "Проверено по источникам" + liveCardContextBasis(contexts[:2])
			if !reflect.DeepEqual(candidate.Card, &want) || !reflect.DeepEqual(*candidate.ModelScore, score) {
				t.Fatalf("pruning lost retained proof, changed the verdict or kept a discarded context: %+v", candidate.Card)
			}
			if len(original.SourceContexts) != 3 || original.SourceContexts[2].SourceID != removed ||
				original.Basis != "Проверено по источникам"+liveCardContextBasis(contexts) {
				t.Fatal("pruning mutated a shared card or its source context slice")
			}
		})
	}
}
