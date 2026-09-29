package application

import (
	"encoding/json"
	"errors"
	"strings"

	commonv1 "github.com/example/main-service/internal/platform/genproto/common/v1"
	"github.com/example/main-service/internal/platform/jsonx"
)

// genItem — плоская форма ответа LLM; вложенные assessment и detail собирает сервис.
type genItem struct {
	Title                   string         `json:"title"`
	Statement               string         `json:"statement"`
	Rationale               string         `json:"rationale"`
	TRL                     *int           `json:"trl"`
	TRLRationale            string         `json:"trl_rationale"`
	Novelty                 *float64       `json:"novelty"`
	NoveltyRationale        string         `json:"novelty_rationale"`
	Risk                    *float64       `json:"risk"`
	RiskRationale           string         `json:"risk_rationale"`
	Value                   *float64       `json:"value"`
	ValueRationale          string         `json:"value_rationale"`
	Confidence              *float64       `json:"confidence"`
	Measurable              *bool          `json:"measurable"`
	Organization            string         `json:"organization"`
	Function                string         `json:"function"`
	SourceType              string         `json:"source_type"`
	ResearchType            string         `json:"research_type"`
	Location                string         `json:"location"`
	Tags                    []string       `json:"tags"`
	Problem                 string         `json:"problem"`
	Drivers                 []string       `json:"drivers"`
	MaterialSystem          string         `json:"material_system"`
	CompositionChange       string         `json:"composition_change"`
	ProcessChange           string         `json:"process_change"`
	MicrostructureMechanism string         `json:"microstructure_mechanism"`
	TargetProperty          string         `json:"target_property"`
	CharacterizationMethods []string       `json:"characterization_methods"`
	TestMethods             []string       `json:"test_methods"`
	FailureModes            []string       `json:"failure_modes"`
	CausalChain             []causalStep   `json:"causal_chain"`
	Experiment              *experimentVal `json:"experiment_plan"`
	Feasibility             []feasibility  `json:"feasibility"`
}

type causalStep struct {
	Stage  string `json:"stage"`
	Change string `json:"change"`
}

type experimentVal struct {
	ExperimentType   string              `json:"experiment_type"`
	Sections         []experimentSection `json:"sections"`
	Materials        []string            `json:"materials"`
	ProcessParams    []experimentParam   `json:"process_parameters"`
	Characterization []string            `json:"characterization_methods"`
	TestMethods      []string            `json:"test_methods"`
	Controls         []string            `json:"controls"`
	SuccessCriteria  string              `json:"success_criteria"`
	EstimatedCost    string              `json:"estimated_cost"`
	EstimatedTime    string              `json:"estimated_time"`
	Risks            []string            `json:"risks"`
	Variables        []string            `json:"variables"`
	Methods          []string            `json:"methods"`
	Horizon          string              `json:"horizon"`
}

type feasibility struct {
	Aspect string `json:"aspect"`
	Level  string `json:"level"`
	Note   string `json:"note"`
}

func isStubGeneration(resp *commonv1.RagResponse) bool {
	model := strings.ToLower(strings.TrimSpace(resp.GetModel()))
	answer := strings.ToLower(strings.TrimSpace(resp.GetAnswer()))
	return model == "" || strings.Contains(model, "stub") || strings.Contains(answer, "[stub-llm]")
}

func usableGenItems(items []genItem) []genItem {
	out := make([]genItem, 0, len(items))
	seen := make(map[string]struct{}, len(items))
	for _, item := range items {
		item.Title = compactText(item.Title)
		item.Statement = compactText(item.Statement)
		item.Rationale = compactText(item.Rationale)
		if item.Title == "" || item.Statement == "" || item.Rationale == "" || !validHypothesisTitle(item.Title) {
			continue
		}
		key := hypothesisTitleKey(item.Title)
		if _, ok := seen[key]; ok {
			continue
		}
		seen[key] = struct{}{}
		out = append(out, item)
	}
	return out
}

// parseGenItems принимает формы, которые модели реально отдают: голый массив, обёртку hypotheses/items/results/data,
// одиночный объект, а также текст или ограждения вокруг массива.
func parseGenItems(answer string) ([]genItem, error) {
	var lastErr error
	for _, raw := range genJSONCandidates(answer) {
		items, err := decodeGenItems(raw)
		if err == nil {
			return items, nil
		}
		lastErr = err
	}
	if items := decodePartialGenItems(answer); len(items) > 0 {
		return items, nil
	}
	if lastErr != nil {
		return nil, lastErr
	}
	return nil, errors.New("no generated hypothesis JSON in model output")
}

func decodeGenItems(raw string) ([]genItem, error) {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return nil, errors.New("empty model output")
	}
	var items []genItem
	if err := unmarshalGenJSON(raw, &items); err == nil {
		return items, nil
	}
	var wrapped map[string]json.RawMessage
	if err := unmarshalGenJSON(raw, &wrapped); err == nil {
		for _, key := range []string{"hypotheses", keyItems, "results", "data"} {
			msg, ok := wrapped[key]
			if !ok {
				continue
			}
			if err := unmarshalGenJSON(string(msg), &items); err == nil {
				return items, nil
			}
		}
	}
	var single genItem
	if err := unmarshalGenJSON(raw, &single); err == nil {
		return []genItem{single}, nil
	}
	return nil, errors.New("parse generated hypotheses: unsupported JSON shape")
}

func decodePartialGenItems(answer string) []genItem {
	raw := stripJSONFence(answer)
	var items []genItem
	for _, obj := range completeJSONObjectCandidates(raw) {
		var item genItem
		if err := unmarshalGenJSON(obj, &item); err != nil {
			continue
		}
		items = append(items, item)
	}
	return items
}

func unmarshalGenJSON(raw string, v any) error {
	err := jsonx.Unmarshal([]byte(raw), v)
	if err == nil {
		return nil
	}
	// Модели пишут LaTeX с неэкранированными обратными слэшами (\ce, \frac) — это невалидный JSON:
	// одиночные слэши чинятся, разбор повторяется один раз.
	repaired := repairJSONBackslashes(raw)
	if repaired != raw {
		if err2 := jsonx.Unmarshal([]byte(repaired), v); err2 == nil {
			return nil
		}
	}
	return err
}

func advanceInString(r rune, escaped bool) (inString, newEscaped bool) {
	if escaped {
		return true, false
	}
	switch r {
	case '\\':
		return true, true
	case '"':
		return false, false
	}
	return true, false
}

func completeJSONObjectCandidates(s string) []string {
	startArray := strings.IndexByte(s, '[')
	if startArray < 0 {
		return nil
	}
	var out []string
	start, depth := -1, 0
	inString, escaped := false, false
	for i, r := range s[startArray+1:] {
		pos := startArray + 1 + i
		if inString {
			inString, escaped = advanceInString(r, escaped)
			continue
		}
		switch r {
		case '"':
			inString = true
		case '{':
			if depth == 0 {
				start = pos
			}
			depth++
		case '}':
			if depth == 0 {
				continue
			}
			depth--
			if depth == 0 && start >= 0 {
				out = append(out, s[start:pos+1])
				start = -1
			}
		case ']':
			if depth == 0 {
				return out
			}
		}
	}
	return out
}

func genJSONCandidates(answer string) []string {
	answer = strings.TrimSpace(answer)
	if answer == "" {
		return nil
	}
	arrays := jsonArrayCandidates(answer)
	candidates := make([]string, 0, len(arrays)+1)
	candidates = append(candidates, stripJSONFence(answer))
	candidates = append(candidates, arrays...)
	return uniqueStrings(candidates)
}

func stripJSONFence(s string) string {
	s = strings.TrimSpace(s)
	if !strings.HasPrefix(s, "```") || !strings.HasSuffix(s, "```") {
		return s
	}
	s = strings.TrimPrefix(s, "```")
	s = strings.TrimSuffix(s, "```")
	s = strings.TrimSpace(s)
	if i := strings.IndexByte(s, '\n'); i >= 0 {
		head := strings.ToLower(strings.TrimSpace(s[:i]))
		if head == "json" || head == "javascript" || head == "js" {
			s = strings.TrimSpace(s[i+1:])
		}
	}
	return s
}

func jsonArrayCandidates(s string) []string {
	var out []string
	start, depth := -1, 0
	inString, escaped := false, false
	for i, r := range s {
		if inString {
			inString, escaped = advanceInString(r, escaped)
			continue
		}
		switch r {
		case '"':
			inString = true
		case '[':
			if depth == 0 {
				start = i
			}
			depth++
		case ']':
			if depth == 0 {
				continue
			}
			depth--
			if depth == 0 && start >= 0 {
				out = append(out, s[start:i+1])
				start = -1
			}
		}
	}
	return out
}

func uniqueStrings(items []string) []string {
	out := make([]string, 0, len(items))
	seen := make(map[string]struct{}, len(items))
	for _, item := range items {
		item = strings.TrimSpace(item)
		if item == "" {
			continue
		}
		if _, ok := seen[item]; ok {
			continue
		}
		seen[item] = struct{}{}
		out = append(out, item)
	}
	return out
}
