package aiclients

import (
	"sync"
	"testing"
	"time"
)

// При 600 запросах в минуту слоты идут через 100 мс; три вызова ждут
// одновременно и укладываются примерно в два интервала, а не в сумму ожиданий.
func TestThrottleReservesSlotsWithoutHoldingLock(t *testing.T) {
	g := &httpGenerator{rpm: 600}
	start := time.Now()
	var wg sync.WaitGroup
	for range 3 {
		wg.Go(func() {
			if err := g.throttle(t.Context()); err != nil {
				t.Error(err)
			}
		})
	}
	wg.Wait()
	if took := time.Since(start); took < 150*time.Millisecond || took > 400*time.Millisecond {
		t.Fatalf("three calls took %v, want about 200ms", took)
	}
}
