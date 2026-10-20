package application

import (
	"context"
	"encoding/json/v2"
	"errors"
	"fmt"
	"slices"
	"testing"
	"testing/synctest"
	"time"

	"github.com/example/main-service/internal/platform/aiclients"
)

type liveTimedCardGenerator struct {
	liveCardTestGenerator
	budgets []time.Duration
}

func (g *liveTimedCardGenerator) Generate(ctx context.Context, request aiclients.GenRequest) (aiclients.GenResponse, error) {
	deadline, _ := ctx.Deadline()
	g.budgets = append(g.budgets, time.Until(deadline))
	delay := 55 * time.Second
	if request.System == liveCardVerifySystem+liveQueryScopeInstruction {
		delay = 70 * time.Second
	}
	select {
	case <-time.After(delay):
		return g.liveCardTestGenerator.Generate(ctx, request)
	case <-ctx.Done():
		return aiclients.GenResponse{}, fmt.Errorf("delayed card generation: %w", ctx.Err())
	}
}

func TestLiveCardCallBudgetsRespectParent(t *testing.T) {
	for _, tc := range []struct {
		name        string
		parent      time.Duration
		wantElapsed time.Duration
		wantBudgets []time.Duration
		wantErr     error
	}{
		{name: "separate calls", parent: 12 * time.Minute, wantElapsed: 125 * time.Second,
			wantBudgets: []time.Duration{liveCardCallTimeout, liveCardCallTimeout}},
		{name: "parent deadline", parent: 100 * time.Second, wantElapsed: 100 * time.Second,
			wantBudgets: []time.Duration{100 * time.Second, 45 * time.Second}, wantErr: context.DeadlineExceeded},
	} {
		t.Run(tc.name, func(t *testing.T) {
			synctest.Test(t, func(t *testing.T) {
				const source = "Nonce challenges detect replayed speech in a laboratory trial."
				draft := liveCardDraft{Summary: "Одноразовое задание выявляет запись голоса.",
					Advantage: "Проверка позволяет выявлять повторное воспроизведение речи.",
					Example:   "Проведено лабораторное испытание защиты от повторов."}
				fields := []string{liveClaimSummary, liveClaimAdvantage, liveClaimExample}
				for _, field := range fields {
					draft.Evidence = append(draft.Evidence, liveCardSelection{Field: field, Source: 1, From: 1, Through: 1})
				}
				raw, err := json.Marshal(draft)
				if err != nil {
					t.Fatal(err)
				}
				gen := &liveTimedCardGenerator{draft: string(raw),
					verified: liveCardTestReview(t, draft, []string{source}, fields...)}
				ctx, cancel := context.WithTimeout(t.Context(), tc.parent)
				defer cancel()
				started := time.Now()
				card, err := NewCandidateCard(gen, func(context.Context) bool { return true })(
					liveFixtureScopeContext(ctx, "Проверка живости голоса"),
					"Проверка живости голоса", LiveCardIdentity{Label: "Одноразовое задание",
						Mechanism: "Произнесение одноразового задания выявляет повторное воспроизведение речи."}, []string{source})
				if !errors.Is(err, tc.wantErr) || time.Since(started) != tc.wantElapsed || !slices.Equal(gen.budgets, tc.wantBudgets) {
					t.Fatalf("card budgets: elapsed=%s calls=%v error=%v", time.Since(started), gen.budgets, err)
				}
				if tc.wantErr != nil {
					if card.Summary != "" || card.Advantage != "" || card.Example != "" || len(card.Evidence) != 0 {
						t.Fatalf("canceled verification returned a card: %+v", card)
					}
					return
				}
				if len(gen.requests) != 2 || !liveCardComplete(LiveTopic{PaperIDs: []string{"1"}, Card: &card}) {
					t.Fatalf("card was not verified in exactly two calls: %+v", card)
				}
			})
		})
	}
}

func TestLiveCardCompleteNeedsAllThreeVerifiedFields(t *testing.T) {
	card := func(fields ...string) LiveTopic {
		c := LiveCard{VerificationModel: "fixture", ExampleID: "p1"}
		for _, field := range fields {
			c.Evidence = append(c.Evidence, LiveClaimEvidence{Field: field, SourceID: "p1", Quote: "цитата", Verified: true})
			switch field {
			case liveClaimSummary:
				c.Summary = "описание"
			case liveClaimAdvantage:
				c.Advantage = "преимущество"
			case liveClaimExample:
				c.Example = "кейс"
			}
		}
		return LiveTopic{StudyTopic: StudyTopic{PaperIDs: []string{"p1"}}, Card: &c}
	}
	for _, tc := range []struct {
		fields []string
		want   bool
	}{
		{[]string{liveClaimSummary, liveClaimAdvantage, liveClaimExample}, true},
		{[]string{liveClaimSummary, liveClaimAdvantage}, false},
		{[]string{liveClaimSummary, liveClaimExample}, false},
		{[]string{liveClaimSummary}, false},
		{[]string{liveClaimAdvantage, liveClaimExample}, false},
	} {
		if got := liveCardComplete(card(tc.fields...)); got != tc.want {
			t.Fatalf("fields %v: complete=%v, want %v", tc.fields, got, tc.want)
		}
	}
}
