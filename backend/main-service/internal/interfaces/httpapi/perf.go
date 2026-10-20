package httpapi

import (
	"context"
	"errors"
	"fmt"
	"math"
	"net/http"
	"net/url"
	"os"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/example/main-service/internal/application"
	"github.com/example/main-service/internal/platform/httpx"
	"github.com/example/main-service/internal/platform/jsonx"
)

type stageLatency struct {
	P50 *float64 `json:"p50,omitempty"`
	P95 *float64 `json:"p95,omitempty"`
}

// perfResponse — живые задержки из гистограмм Prometheus, а не оценки качества.
type perfResponse struct {
	Pipeline       map[string]*stageLatency        `json:"pipeline"`
	Ingestion      map[string]*stageLatency        `json:"ingestion"`
	HypothesisJobs []application.HypothesisJobStat `json:"hypothesis_jobs"`
	Telemetry      string                          `json:"telemetry"`
	ObservedAt     time.Time                       `json:"observed_at"`
}

const perfJobsWindow = 24 * time.Hour

// Отказ сбора метрик не означает, что конвейер не работал; состояние источника возвращается отдельно.
func (a *API) getPerf(w http.ResponseWriter, r *http.Request) {
	if !a.requireRole(w, r, "admin", "operator") {
		return
	}
	resp := perfResponse{Telemetry: "available", ObservedAt: time.Now().UTC()}
	ctx := r.Context()
	var pipelineErr, ingestionErr error
	var wg sync.WaitGroup
	wg.Go(func() { resp.Pipeline, pipelineErr = promStages(ctx, "rag_stage_duration_seconds", "stage") })
	wg.Go(func() {
		resp.Ingestion, ingestionErr = promStages(ctx, "rag_processing_duration_seconds", "queue")
	})
	wg.Wait()
	if pipelineErr != nil || ingestionErr != nil {
		resp.Telemetry = "partial"
		if pipelineErr != nil && ingestionErr != nil {
			resp.Telemetry = "unavailable"
		}
	}
	jobs := a.hypJobs.ListRecentAll(r.Context(), 0)
	resp.HypothesisJobs = application.SummarizeHypothesisJobs(jobs, time.Now(), perfJobsWindow)
	httpx.JSON(w, http.StatusOK, resp)
}

func promURL() string {
	if v := os.Getenv("PROM_URL"); v != "" {
		return strings.TrimRight(v, "/")
	}
	return "http://prometheus:9090"
}

func promStages(ctx context.Context, metric, group string) (map[string]*stageLatency, error) {
	out := map[string]*stageLatency{}
	queries := []struct {
		quant float64
		set   func(*stageLatency, float64)
	}{
		{0.5, func(s *stageLatency, v float64) { s.P50 = &v }},
		{0.95, func(s *stageLatency, v float64) { s.P95 = &v }},
	}
	results := make([]map[string]float64, len(queries))
	errs := make([]error, len(queries))
	var wg sync.WaitGroup
	for i, q := range queries {
		wg.Go(func() {
			promql := "histogram_quantile(" + strconv.FormatFloat(q.quant, 'f', -1, 64) +
				", sum by (" + group + ", le) (rate(" + metric + "_bucket[15m])))"
			results[i], errs[i] = promQuery(ctx, promql, group)
		})
	}
	wg.Wait()
	for i, result := range results {
		for label, v := range result {
			s := out[label]
			if s == nil {
				s = &stageLatency{}
				out[label] = s
			}
			queries[i].set(s, v)
		}
	}
	return out, errors.Join(errs...)
}

func promQuery(ctx context.Context, promql, group string) (map[string]float64, error) {
	res := map[string]float64{}
	cctx, cancel := context.WithTimeout(ctx, 4*time.Second)
	defer cancel()
	u := promURL() + "/api/v1/query?query=" + url.QueryEscape(promql)
	req, err := http.NewRequestWithContext(cctx, http.MethodGet, u, nil)
	if err != nil {
		return res, fmt.Errorf("create metrics query: %w", err)
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return res, fmt.Errorf("query metrics: %w", err)
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != http.StatusOK {
		return res, fmt.Errorf("metrics source status: %d", resp.StatusCode)
	}
	var body struct {
		Status string `json:"status"`
		Data   struct {
			Result []struct {
				Metric map[string]string `json:"metric"`
				Value  []any             `json:"value"`
			} `json:"result"`
		} `json:"data"`
	}
	if derr := jsonx.NewDecoder(resp.Body).Decode(&body); derr != nil {
		return res, fmt.Errorf("decode metrics: %w", derr)
	}
	if body.Status != "success" {
		return res, errors.New("metrics source returned an unsuccessful query")
	}
	for _, r := range body.Data.Result {
		if len(r.Value) != 2 {
			continue
		}
		s, ok := r.Value[1].(string)
		if !ok {
			continue
		}
		v, perr := strconv.ParseFloat(s, 64)
		if perr != nil || math.IsNaN(v) || math.IsInf(v, 0) {
			continue
		}
		key := r.Metric[group]
		if key == "" {
			key = "?"
		}
		res[key] = math.Round(v*1000) / 1000
	}
	return res, nil
}
