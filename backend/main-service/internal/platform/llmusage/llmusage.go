// Package llmusage пишет и читает дневной расход LLM в хэшах Valkey; формат ключей и полей побайтно
// общий с воркерами на Python.
package llmusage

import (
	"context"
	"errors"
	"math"
	"strconv"
	"strings"
	"time"
)

const (
	dayPrefix    = "rag:llm:usage:"  // + ГГГГММДД (UTC) → хэш
	reqMinPrefix = "rag:llm:reqmin:" // + минута Unix → счётчик
	fieldSep     = "\x1f"            // разделитель: модель \x1f операция \x1f метрика
	// DayTTL держит ~35 дней, чтобы 30-дневное окно дашборда всегда было в Valkey.
	DayTTL    = 35 * 24 * time.Hour
	reqMinTTL = 120 * time.Second
)

type KV interface {
	AddUsage(context.Context, string, string, string, int64, int64, int64, time.Duration, time.Duration) error
	HGetAll(ctx context.Context, key string) (map[string]string, error)
	GetInt(ctx context.Context, key string) (int64, error)
}

func DayKey(day time.Time) string { return dayPrefix + day.UTC().Format("20060102") }

// Record: вызывающие игнорируют ошибку — запись метрик не должна ронять путь LLM.
func Record(ctx context.Context, kv KV, model, operation string, promptTokens, completionTokens int, costUSD float64) error {
	if kv == nil || model == "" || operation == "" {
		return nil
	}
	if promptTokens < 0 || completionTokens < 0 || costUSD < 0 || math.IsNaN(costUSD) || math.IsInf(costUSD, 0) ||
		costUSD*1e9 > 1<<53-1 || strings.Contains(model+operation, fieldSep) || len(model) > 256 || len(operation) > 128 {
		return errors.New("invalid usage record")
	}
	now := time.Now()
	return kv.AddUsage(ctx, DayKey(now), reqMinPrefix+strconv.FormatInt(now.Unix()/60, 10),
		model+fieldSep+operation+fieldSep, int64(promptTokens), int64(completionTokens),
		int64(math.Round(costUSD*1e9)), DayTTL, reqMinTTL)
}

type Cell struct {
	Model, Operation                                      string
	Requests, PromptTokens, CompletionTokens, CostNanoUSD int64
}

func ReadDay(ctx context.Context, kv KV, day time.Time) ([]Cell, error) {
	m, err := kv.HGetAll(ctx, DayKey(day))
	if err != nil {
		return nil, err
	}
	type acc struct{ req, pt, ct, cost int64 }
	byKey := map[[2]string]*acc{}
	for f, v := range m {
		parts := strings.SplitN(f, fieldSep, 3)
		if len(parts) != 3 {
			continue
		}
		n, _ := strconv.ParseInt(v, 10, 64)
		k := [2]string{parts[0], parts[1]}
		a := byKey[k]
		if a == nil {
			a = &acc{}
			byKey[k] = a
		}
		switch parts[2] {
		case "req":
			a.req = n
		case "pt":
			a.pt = n
		case "ct":
			a.ct = n
		case "cost":
			a.cost = n
		}
	}
	cells := make([]Cell, 0, len(byKey))
	for k, a := range byKey {
		cells = append(cells, Cell{
			Model: k[0], Operation: k[1],
			Requests: a.req, PromptTokens: a.pt, CompletionTokens: a.ct, CostNanoUSD: a.cost,
		})
	}
	return cells, nil
}

func ReqPerMinute(ctx context.Context, kv KV) int64 {
	n, err := kv.GetInt(ctx, reqMinPrefix+strconv.FormatInt(time.Now().Unix()/60, 10))
	if err != nil {
		return 0
	}
	return n
}
