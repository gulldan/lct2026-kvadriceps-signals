package httpapi_test

import (
	"context"
	"encoding/json"
	"errors"
	commonv1 "github.com/example/main-service/internal/platform/genproto/common/v1"
	dbv1 "github.com/example/main-service/internal/platform/genproto/db/v1"
	"google.golang.org/protobuf/proto"
	"maps"
	"slices"
	"strconv"
	"strings"
)

const (
	queueAll          = "all"
	queueNeedsVerify  = "needs_verify"
	queueNeedsTRL     = "needs_trl"
	queueReady        = "ready"
	queueRisk         = "risk"
	queueInsufficient = "insufficient"
	keyTRL            = "trl"
)

func orEmptyStrings(s []string) []string {
	if s == nil {
		return []string{}
	}
	return s
}
func boardFilterHypotheses(hs []*commonv1.Hypothesis, search, queue string) []*commonv1.Hypothesis {
	q := strings.ToLower(strings.TrimSpace(search))
	out := make([]*commonv1.Hypothesis, 0, len(hs))
	for _, h := range hs {
		if q != "" && !boardSearchMatch(h, q) {
			continue
		}
		if queue != "" && queue != queueAll && !boardQueueMatch(h, queue) {
			continue
		}
		out = append(out, h)
	}
	return out
}

func boardSearchMatch(h *commonv1.Hypothesis, q string) bool {
	text := strings.ToLower(strings.Join([]string{
		h.GetTitle(), h.GetStatement(), h.GetRationale(), h.GetLocation(), h.GetOrganization(),
		h.GetFunctionArea(), h.GetSourceType(), strings.Join(h.GetTags(), " "),
	}, " "))
	return strings.Contains(text, q)
}

// boardQueueMatch делит очереди по вердикту проверки, так что гипотеза попадает ровно в одну;
// закрытые — только во «Все».
func boardQueueMatch(h *commonv1.Hypothesis, queue string) bool {
	closed := h.GetStatus() == "rejected" || h.GetStatus() == "archived"
	switch queue {
	case queueAll:
		return true
	case queueNeedsTRL:
		return !closed && h.Trl == nil
	}
	if closed {
		return false
	}
	return boardPrimaryQueue(h.GetStatus(), boardVerdict(h.GetAssessment())) == queue
}

func boardPrimaryQueue(status, verdict string) string {
	if status == "approved" || verdict == "supported" {
		return queueReady
	}
	switch verdict {
	case "refuted", "mixed":
		return queueRisk
	case "insufficient":
		return queueInsufficient
	}
	return queueNeedsVerify
}

func boardVerdict(assessment string) string {
	if assessment == "" {
		return ""
	}
	var m struct {
		Check struct {
			Verdict string `json:"verdict"`
		} `json:"check"`
	}
	if err := json.Unmarshal([]byte(assessment), &m); err != nil {
		return ""
	}
	return m.Check.Verdict
}

func boardQueueCounts(hs []*commonv1.Hypothesis) map[string]int {
	queues := []string{queueNeedsVerify, queueNeedsTRL, queueReady, queueRisk, queueInsufficient}
	counts := map[string]int{queueAll: len(hs)}
	for _, q := range queues {
		counts[q] = 0
	}
	for _, h := range hs {
		for _, q := range queues {
			if boardQueueMatch(h, q) {
				counts[q]++
			}
		}
	}
	return counts
}

func boardFacets(hs []*commonv1.Hypothesis) map[string][]string {
	values := map[string]map[string]bool{
		"company":  {},
		"func":     {},
		"source":   {},
		keyTRL:     {},
		"research": {},
	}
	for _, h := range hs {
		addFacet(values["company"], h.GetOrganization())
		addFacet(values["func"], h.GetFunctionArea())
		addFacet(values["source"], h.GetSourceType())
		if h.Trl != nil {
			addFacet(values[keyTRL], strconv.Itoa(int(h.GetTrl())))
		}
		for _, tag := range h.GetTags() {
			v := strings.ToLower(strings.TrimSpace(tag))
			if v == "теоретическое исследование" || v == "практическое" {
				addFacet(values["research"], v)
			}
		}
	}
	out := make(map[string][]string, len(values))
	for key, set := range values {
		out[key] = sortedSet(set)
	}
	return out
}

func addFacet(set map[string]bool, value string) {
	v := strings.TrimSpace(value)
	low := strings.ToLower(v)
	if v == "" || v == "-" || v == "—" || low == "n/a" {
		return
	}
	set[v] = true
}

func sortedSet(set map[string]bool) []string {
	return orEmptyStrings(slices.Sorted(maps.Keys(set)))
}

func (c *fakeCatalog) HypothesisBoard(
	ctx context.Context,
	req *dbv1.ListHypothesesRequest,
) (*dbv1.ListHypothesesResponse, error) {
	copyReq, ok := proto.Clone(req).(*dbv1.ListHypothesesRequest)
	if !ok {
		return nil, errors.New("invalid board request")
	}
	copyReq.Limit = 0
	copyReq.Offset = 0
	hs, err := c.ListHypotheses(ctx, copyReq)
	if err != nil {
		return nil, err
	}
	base := boardFilterHypotheses(hs, req.GetSearch(), "")
	filtered := boardFilterHypotheses(base, "", req.GetQueue())
	raw, err := json.Marshal(
		map[string]any{
			"total": len(
				filtered,
			),
			"queue_counts": boardQueueCounts(
				base,
			),
			"facets": boardFacets(
				filtered,
			),
		},
	)
	start := min(int(req.GetOffset()), len(filtered))
	end := min(start+int(req.GetLimit()), len(filtered))
	return &dbv1.ListHypothesesResponse{Hypotheses: filtered[start:end], Aggregates: raw}, err
}
