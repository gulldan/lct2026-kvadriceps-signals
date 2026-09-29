package main

import (
	"context"
	"os"
	"testing"
	"time"
)

// Нужны TR_LIVE_SOURCES=1, EPO_OPS_KEY и EPO_OPS_SECRET; учитывается SOURCE_PROXY_URL.
func TestOPSHistoryLive(t *testing.T) {
	if os.Getenv("TR_LIVE_SOURCES") != "1" {
		t.Skip("TR_LIVE_SOURCES=1 runs the live EPO OPS check")
	}
	cfg := sourceConfig{HistoryMonths: 2, RequestBudget: 60, OPSDocuments: 400, PatentTerms: []string{"solid state batter"}}
	s := &service{kind: kindPatents, dir: t.TempDir(), config: cfg,
		http: newWebClient(40*time.Second, collectorDomains(cfg, kindPatents))}
	now := time.Now().UTC()
	data := snapshot{ObservedAt: now.Format(time.RFC3339), From: now.AddDate(0, -2, 0).Format("2006-01") + "-01",
		Through: now.Format(time.DateOnly), Records: []record{}, Pages: []provenance{}, Limits: []string{}}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Minute)
	defer cancel()
	s.opsHistory(ctx, &data)
	t.Logf("records=%d pages=%d requests=%d coverage=%+v", len(data.Records), len(data.Pages), s.requests, data.Coverage)
	t.Logf("throttle=%s", s.ops.throttle)
	if len(data.Records) == 0 || len(data.Coverage) != 1 {
		t.Fatalf("live OPS returned nothing: %+v", data.Coverage)
	}
	for _, row := range data.Records[:min(3, len(data.Records))] {
		t.Logf("%s %s %q text=%d family=%s filing=%s priority=%s cpc=%v", row.ID, row.Date, row.Title, len(row.Text),
			row.FamilyID, row.Filing, row.Priority, row.FullText != nil && len(row.FullText.CPC) > 0)
	}
}
