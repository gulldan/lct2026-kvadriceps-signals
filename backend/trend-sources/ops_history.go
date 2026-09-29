package main

import (
	"cmp"
	"context"
	"errors"
	"fmt"
	"os"
	"time"
)

// opsHistory перечитывает два последних месяца: DOCDB дополняет их с задержкой ведомств; ранние кэшируются.
func (s *service) opsHistory(ctx context.Context, data *snapshot) {
	if s.config.OPSDocuments <= 0 {
		return
	}
	c := coverage{Source: opsSource, From: data.From, Through: data.Through, ObservedAt: data.ObservedAt}
	if key, secret := opsCredentials(); key == "" || secret == "" {
		c.StopReason = "EPO OPS credentials (EPO_OPS_KEY, EPO_OPS_SECRET) are not configured"
		data.Coverage = append(data.Coverage, c)
		data.Limits = append(data.Limits, "The worldwide patent scope is off: no EPO OPS credentials are configured.")
		return
	}
	through, _ := time.Parse(time.DateOnly, data.Through)
	frozen := time.Date(through.Year(), through.Month(), 1, 0, 0, 0, 0, time.UTC).AddDate(0, -1, 0).Format(time.DateOnly)
	sweep := opsSweep{coverage: &c, seen: map[string]bool{}, frozen: frozen}
outer:
	for _, term := range s.config.PatentTerms {
		for _, month := range monthWindows(data.From, data.Through) {
			if stop := s.opsMonth(ctx, data, &sweep, term, month); stop != "" {
				c.StopReason = stop
				break outer
			}
		}
	}
	sweep.finish("every configured term and month was read from EPO OPS")
	data.Coverage = append(data.Coverage, c)
	data.Limits = append(data.Limits,
		"Worldwide patent records come from the EPO OPS DOCDB bibliographic search over the configured terms "+
			"(title or abstract), so they are a term sample of 100+ offices, not the whole population; each office "+
			"reaches DOCDB with its own delay, a record carries an abstract at most, and families are DOCDB simple "+
			"families. Fair use: 4 GB a week per registration and the per-minute allowance OPS announces per service"+
			opsThrottleNote(s.ops.throttle)+".")
}

func opsThrottleNote(state string) string {
	if state == "" {
		return ""
	}
	return " (last announced: " + state + ")"
}

type opsSweep struct {
	coverage *coverage
	seen     map[string]bool
	rejected map[string]bool
	frozen   string
	read     int
}

// stop возвращает причину остановки обхода. Отказ по одному термину раньше тоже останавливал обход, и термины
// после него не читались ни в одном проходе; теперь пропускается только сам термин.
func (s *opsSweep) stop(term, month string, offset int, err error) string {
	if !errors.Is(err, errQueryRejected) {
		s.coverage.Cursor = fmt.Sprintf("%s|%s|%d", term, month, offset)
		return err.Error()
	}
	if s.rejected == nil {
		s.rejected = map[string]bool{}
	}
	s.rejected[term] = true
	s.coverage.Limitations = append(s.coverage.Limitations, "term skipped: "+term+": "+err.Error())
	return ""
}

// Пропущенный термин не прочитан, поэтому охват не объявляется полным.
func (s *opsSweep) finish(done string) {
	if s.coverage.StopReason == "" && len(s.rejected) > 0 {
		s.coverage.StopReason = "the source rejected some configured terms; they are listed in limitations"
	}
	s.coverage.Exhausted = s.coverage.StopReason == ""
	s.coverage.StopReason = cmp.Or(s.coverage.StopReason, done)
}

// opsMonth возвращает причину остановки или пустую строку, если месяц дочитан.
func (s *service) opsMonth(ctx context.Context, data *snapshot, sweep *opsSweep, term string, month period) string {
	cql := opsCQL(term, month)
	if sweep.rejected[term] {
		return ""
	}
	for start := 1; start <= opsMaxResults; start += opsPage {
		if sweep.read >= s.config.OPSDocuments {
			sweep.coverage.Cursor = fmt.Sprintf("%s|%s|%d", term, month.Start, start)
			return "EPO OPS record budget reached; the sweep continues from this term and month"
		}
		raw, cached, err := s.opsPage(ctx, data, cql, start, month.End <= sweep.frozen)
		if err != nil {
			return sweep.stop(term, month.Start, start, err)
		}
		if raw == nil {
			return ""
		}
		rows, total, err := opsRecords(raw, data.ObservedAt)
		if err != nil {
			sweep.coverage.Limitations = append(sweep.coverage.Limitations, "EPO OPS page not readable: "+cql+": "+err.Error())
			return ""
		}
		digest := hashBytes(raw)
		for _, row := range rows {
			if sweep.seen[row.ID] {
				continue
			}
			sweep.seen[row.ID] = true
			if !cached {
				sweep.read++
			}
			row.Provenance = []provenance{{URL: opsQueryURL(cql, start), SHA256: digest}}
			data.Records = append(data.Records, row)
		}
		if start+opsPage > total {
			return ""
		}
	}
	sweep.coverage.Limitations = append(sweep.coverage.Limitations,
		"EPO OPS returns at most 2000 results per query; the term and month were cut there: "+cql)
	return ""
}

func opsQueryURL(cql string, start int) string {
	return fmt.Sprintf("%s/rest-services/published-data/search/biblio?q=%s&Range=%d-%d", opsBase, cql, start, start+opsPage-1)
}

// opsPage кэширует страницы только замороженного месяца; nil без ошибки — у запроса нет результатов.
func (s *service) opsPage(ctx context.Context, data *snapshot, cql string, start int, frozen bool) ([]byte, bool, error) {
	address := opsQueryURL(cql, start)
	name := s.cachePath("ops:v1", address)
	if frozen {
		if b, err := os.ReadFile(name); err == nil {
			data.Pages = append(data.Pages, provenance{URL: address, SHA256: hashBytes(b)})
			return b, true, nil
		}
	}
	raw, _, err := s.opsSearch(ctx, cql, start)
	if err != nil || raw == nil {
		return nil, false, err
	}
	if err = s.storeRaw(raw, address, "", data); err != nil {
		return nil, false, err
	}
	if frozen {
		if err = cachePut(name, raw); err != nil {
			return nil, false, err
		}
	}
	return raw, false, nil
}
