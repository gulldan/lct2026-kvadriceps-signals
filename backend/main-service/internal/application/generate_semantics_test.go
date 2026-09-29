package application

import (
	"context"
	"errors"
	"strings"
	"testing"
	"testing/synctest"
	"time"

	commonv1 "github.com/example/main-service/internal/platform/genproto/common/v1"
)

func TestGenerateNeverInventsHypothesesAfterModelFailure(t *testing.T) {
	for _, response := range []answer{
		reply("[stub-llm] no backend"), reply("invalid JSON"), reply("[]"),
		failReply(errors.New("model unavailable")),
	} {
		t.Run(response.resp.GetAnswer(), func(t *testing.T) {
			cat := newCatalog()
			cat.clusters["battery"] = &commonv1.Cluster{
				Id: "battery", Label: "Electrode capacity", DocumentCount: 100,
				Representatives: `[{"document_id":"battery","filename":"cathode.pdf","snippet":"Anion redox improves capacity."}]`,
			}
			if response.resp != nil {
				response.resp.Sources = []*commonv1.Source{{DocumentId: "battery", Snippet: "Anion redox improves capacity."}}
			}
			kpi := &commonv1.KPI{
				Id: "crypto", Title: "Постквантовая криптография для банковских платежей",
				Description: "Тестовый тезис приёмки: найти технологию с пилотом до конца 2027 года.",
			}
			answerer := newAnswerer(response)
			created, err := newHypService(cat, answerer).Generate(t.Context(), "alice", kpi, 3, "")
			if err == nil || len(created) != 0 || len(cat.hypotheses) != 0 {
				t.Fatalf("model failure invented or persisted hypotheses: created=%v error=%v", created, err)
			}
			if response.resp.GetAnswer() == "[]" && (answerer.callCount() != 1 || !strings.Contains(err.Error(), "недостаточно данных")) {
				t.Fatalf("insufficient evidence was retried as a broken model: calls=%d error=%v", answerer.callCount(), err)
			}
		})
	}
}

type slowGeneration struct{ calls int }

func (a *slowGeneration) Answer(ctx context.Context, _ *commonv1.RagRequest) (*commonv1.RagResponse, error) {
	a.calls++
	select {
	case <-time.After(110 * time.Second):
		return &commonv1.RagResponse{Answer: genArray, Model: "test-model"}, nil
	case <-ctx.Done():
		return nil, ctx.Err()
	}
}

func TestGenerationUsesSharedBudgetWithoutRestartingHealthyRequest(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		ctx, cancel := context.WithTimeout(t.Context(), 300*time.Second)
		defer cancel()
		answerer := &slowGeneration{}
		got := newHypService(newCatalog(), answerer).generateItems(ctx, 1, &commonv1.RagRequest{Query: "coating"})
		if len(got.items) != 1 || answerer.calls != 1 {
			t.Fatalf("healthy generation was restarted: items=%d calls=%d error=%v", len(got.items), answerer.calls, got.lastErr)
		}
	})
}
