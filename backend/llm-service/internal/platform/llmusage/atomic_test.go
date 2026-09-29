package llmusage_test

import (
	"math"
	"strconv"
	"sync"
	"testing"
	"time"

	"github.com/alicebob/miniredis/v2"
	"github.com/example/llm-service/internal/platform/llmusage"
	"github.com/example/llm-service/internal/platform/valkey"
)

func TestUsageIsAtomicUnderConcurrencyAndCorruptCounter(t *testing.T) {
	r := miniredis.RunT(t)
	kv, err := valkey.New(t.Context(), r.Addr(), "", 0)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = kv.Close() })
	var workers sync.WaitGroup
	for range 40 {
		workers.Go(func() {
			if recordErr := llmusage.Record(t.Context(), kv, "model", "answer", 7, 3, 0.001); recordErr != nil {
				t.Error(recordErr)
			}
		})
	}
	workers.Wait()
	cells, err := llmusage.ReadDay(t.Context(), kv, time.Now())
	if err != nil || len(cells) != 1 {
		t.Fatalf("cells: %+v %v", cells, err)
	}
	if cells[0].Requests != 40 ||
		cells[0].PromptTokens != 280 ||
		cells[0].CompletionTokens != 120 ||
		cells[0].CostNanoUSD != 40_000_000 {
		t.Fatalf("partial tally: %+v", cells[0])
	}
	for _, cost := range []float64{-1, math.NaN(), math.Inf(1)} {
		if recordErr := llmusage.Record(t.Context(), kv, "model", "answer", 1, 1, cost); recordErr == nil {
			t.Fatal("invalid cost accepted")
		}
	}
	minute := "rag:llm:reqmin:" + strconv.FormatInt(time.Now().Unix()/60, 10)
	r.Set(minute, "corrupt")
	if recordErr := llmusage.Record(t.Context(), kv, "model", "answer", 7, 3, 0.001); recordErr == nil {
		t.Fatal("corruption ignored")
	}
	after, err := llmusage.ReadDay(t.Context(), kv, time.Now())
	if err != nil || len(after) != 1 || after[0] != cells[0] {
		t.Fatalf("failed write was partial: %+v %v", after, err)
	}
	if r.TTL(llmusage.DayKey(time.Now())) <= 0 {
		t.Fatal("missing day TTL")
	}
}
