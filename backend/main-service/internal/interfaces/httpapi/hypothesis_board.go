package httpapi

import (
	"context"
	"encoding/json"
	"net/http"
	"strings"
	"sync"

	"google.golang.org/protobuf/proto"

	"github.com/example/main-service/internal/platform/httpx"

	commonv1 "github.com/example/main-service/internal/platform/genproto/common/v1"
)

const (
	autoVerifyEnqueuePerBoard = 4
	autoTRLEnqueuePerBoard    = 4
	autoEnrichEnqueuePerBoard = 4

	boardEvidenceConcurrency = 8
)

func groupHypothesesByOwner(hs []*commonv1.Hypothesis) map[string][]*commonv1.Hypothesis {
	out := map[string][]*commonv1.Hypothesis{}
	for _, h := range hs {
		if h == nil || h.GetOwnerId() == "" {
			continue
		}
		out[h.GetOwnerId()] = append(out[h.GetOwnerId()], h)
	}
	return out
}

func (a *API) hypothesisBoard(w http.ResponseWriter, r *http.Request) {
	ownerID, ok := ownerFromContext(r)
	if !ok {
		httpx.Error(w, http.StatusUnauthorized, "missing claims")
		return
	}
	q := r.URL.Query()
	req := hypothesisFilters(r)
	privileged := a.hypothesesPrivileged(r)
	limit := queryInt(r, "limit", 60)
	if limit <= 0 || limit > 200 {
		limit = 60
	}
	offset := max(queryInt(r, "offset", 0), 0)
	req.Search = strings.TrimSpace(q.Get("q"))
	req.Queue = q.Get("queue")
	req.Limit = int32(limit)
	req.Offset = int32(offset)
	result, err := a.hypotheses.HypothesisBoard(r.Context(), ownerID, privileged, req)
	if err != nil {
		a.fail(w, err)
		return
	}
	var view hypothesisBoardView
	if err = json.Unmarshal(result.GetAggregates(), &view); err != nil {
		a.fail(w, err)
		return
	}
	page := result.GetHypotheses()
	if a.hypJobs != nil {
		for owner, group := range groupHypothesesByOwner(page) {
			a.hypJobs.EnsureVerify(r.Context(), owner, group, autoVerifyEnqueuePerBoard)
			a.hypJobs.EnsureAssessTRL(r.Context(), owner, group, autoTRLEnqueuePerBoard)
			// Только после проверки: EnsureEnrich ставит лишь строки с уже вынесенным вердиктом.
			a.hypJobs.EnsureEnrich(r.Context(), owner, group, autoEnrichEnqueuePerBoard)
		}
	}
	page = a.hydrateBoardEvidence(r.Context(), ownerID, privileged, page)
	view.Items = newHypothesisBoardItemViews(page)
	view.Limit = limit
	view.Offset = offset
	a.cachedJSON(w, r, view)
}

func (a *API) hydrateBoardEvidence(
	ctx context.Context,
	ownerID string,
	privileged bool,
	page []*commonv1.Hypothesis,
) []*commonv1.Hypothesis {
	if len(page) == 0 {
		return page
	}
	out := make([]*commonv1.Hypothesis, len(page))
	sem := make(chan struct{}, boardEvidenceConcurrency)
	var wg sync.WaitGroup
	for i, h := range page {
		if h == nil || len(h.GetEvidence()) > 0 {
			out[i] = h
			continue
		}
		wg.Go(func() {
			sem <- struct{}{}
			defer func() { <-sem }()
			ev, err := a.hypotheses.ListEvidence(ctx, ownerID, privileged, h.GetId())
			if err != nil || len(ev) == 0 {
				out[i] = h
				return
			}
			clone, _ := proto.Clone(h).(*commonv1.Hypothesis)
			clone.Evidence = ev
			out[i] = clone
		})
	}
	wg.Wait()
	return out
}

type hypothesisBoardView struct {
	Items       []hypothesisView    `json:"items"`
	Total       int                 `json:"total"`
	Limit       int                 `json:"limit"`
	Offset      int                 `json:"offset"`
	QueueCounts map[string]int      `json:"queue_counts"`
	Facets      map[string][]string `json:"facets"`
}

const (
	keyITC     = "itc"
	keyTRL     = "trl"
	keySignals = "signals"
)

// boardAssessmentPaths — белый список веток assessment, которые читают сортировки, очереди и бейджи
// борда. Полный объект отдаёт только GET /hypotheses/{id}, поэтому новые ветки оценки сюда не добавляются.
func boardAssessmentPaths() [][]string {
	return [][]string{
		{"ranking", "score"},
		{"learning_priority"},
		{keyITC, "scope"}, {keyITC, "score"}, {keyITC, "techscore"}, {keyITC, "band"}, {keyITC, "axes"},
		{"check", "verdict"},
		{"scores"},
		{"schema"},
		{keyTRL, "level"}, {keyTRL, "name"},
	}
}

func projectJSONBranches(s string, paths ...[]string) json.RawMessage {
	var src map[string]any
	if err := json.Unmarshal([]byte(s), &src); err != nil || src == nil {
		return rawJSON(s)
	}
	dst := map[string]any{}
	for _, path := range paths {
		node, ok := any(src), true
		for _, key := range path {
			m, isMap := node.(map[string]any)
			if !isMap || m[key] == nil {
				ok = false
				break
			}
			node = m[key]
		}
		if !ok {
			continue
		}
		cur := dst
		for _, key := range path[:len(path)-1] {
			next, isMap := cur[key].(map[string]any)
			if !isMap {
				next = map[string]any{}
				cur[key] = next
			}
			cur = next
		}
		cur[path[len(path)-1]] = node
	}
	out, err := json.Marshal(dst)
	if err != nil {
		return rawJSON(s)
	}
	return out
}

func newHypothesisBoardItemView(h *commonv1.Hypothesis) hypothesisView {
	v := newHypothesisViewWithEvidence(h, false)
	v.Detail = rawJSON("")
	v.Generation = generationRef(h.GetGeneration())
	v.Assessment = projectJSONBranches(h.GetAssessment(), boardAssessmentPaths()...)
	return v
}

func newHypothesisBoardItemViews(hs []*commonv1.Hypothesis) []hypothesisView {
	out := make([]hypothesisView, 0, len(hs))
	for _, h := range hs {
		out = append(out, newHypothesisBoardItemView(h))
	}
	return out
}
