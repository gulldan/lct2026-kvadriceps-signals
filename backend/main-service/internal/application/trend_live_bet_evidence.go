package application

import (
	"cmp"
	"encoding/json/v2"
	"fmt"
	"slices"
	"strings"

	commonv1 "github.com/example/main-service/internal/platform/genproto/common/v1"
)

// Работ и цитат из detail.radar в загруженной базе обычно нет: цитаты идут в запрос проверки, работы — в привязку
// фрагментов «за» и «против».
const (
	// Запрос проверки обязан помещаться в окно рядом с выдержками корпуса.
	radarVerifySources = 8
	// Цитаты одной работы вложены друг в друга: берутся короткие, повторяющая их длинная отбрасывается.
	radarVerifyQuotes      = 2
	radarVerifyQuoteRunes  = 600
	radarVerifyContextNote = "\n\nДополнительный контекст — работы из технологического радара, по которым " +
		"аналитик предложил эту гипотезу. Используй их наравне с документами выше, дословные фрагменты «за» и " +
		"«против» бери и из них; не выдумывай источники сверх списка:\n"
)

type radarTransferQuote struct {
	Quote    string `json:"quote"`
	Verified bool   `json:"verified"`
}

type radarTransferDetail struct {
	Radar *struct {
		Sources []struct {
			Title      string               `json:"title"`
			Date       string               `json:"date"`
			SourceName string               `json:"source_name"`
			Quotes     []radarTransferQuote `json:"quotes"`
		} `json:"sources"`
	} `json:"radar"`
}

// Работа без подтверждённой цитаты не попадает: пересказ модели не доказательство.
func radarVerifyContext(detail string) (string, []*commonv1.Source) {
	var d radarTransferDetail
	if json.Unmarshal([]byte(detail), &d) != nil || d.Radar == nil {
		return "", nil
	}
	var b strings.Builder
	var sources []*commonv1.Source
	for _, src := range d.Radar.Sources {
		title := compactText(src.Title)
		quotes := radarVerifyQuotesOf(src.Quotes)
		if title == "" || len(quotes) == 0 {
			continue
		}
		sources = append(sources, &commonv1.Source{Filename: title, Snippet: title + ". " + strings.Join(quotes, " ")})
		fmt.Fprintf(&b, "[R%d] %s", len(sources), title)
		if meta := strings.Trim(src.Date+", "+src.SourceName, ", "); meta != "" {
			fmt.Fprintf(&b, " (%s)", meta)
		}
		for _, q := range quotes {
			fmt.Fprintf(&b, " — «%s»", q)
		}
		b.WriteString("\n")
		if len(sources) == radarVerifySources {
			break
		}
	}
	if len(sources) == 0 {
		return "", nil
	}
	return radarVerifyContextNote + b.String(), sources
}

func radarVerifyQuotesOf(quotes []radarTransferQuote) []string {
	var texts []string
	for _, q := range quotes {
		if text := compactText(q.Quote); q.Verified && text != "" {
			texts = append(texts, text)
		}
	}
	slices.SortStableFunc(texts, func(a, b string) int { return cmp.Compare(len(a), len(b)) })
	var out []string
	for _, text := range texts {
		if slices.ContainsFunc(out, func(kept string) bool { return strings.Contains(text, strings.TrimSuffix(kept, "…")) }) {
			continue
		}
		out = append(out, clipRunes(text, radarVerifyQuoteRunes))
		if len(out) == radarVerifyQuotes {
			break
		}
	}
	return out
}
