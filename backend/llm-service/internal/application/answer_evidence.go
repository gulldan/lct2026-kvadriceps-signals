package application

import (
	"fmt"
	"github.com/example/llm-service/internal/domain"
	"github.com/rs/zerolog"
	"regexp"
	"strconv"
	"strings"
)

func sanitizeAnswerCitations(
	log zerolog.Logger, answer string, sources []domain.Source, tr *domain.Trace,
) string {
	answer, rep := sanitizeCitations(answer, len(sources))
	tr.Cited = citationMarkers(rep.Cited)
	tr.UncitedRemoved = citationMarkers(rep.OutOfRange)
	if len(rep.OutOfRange) > 0 {
		log.Warn().Ints("dropped", rep.OutOfRange).Int("sources", len(sources)).
			Msg("stripped citations to sources that were not retrieved")
	}
	if len(sources) > 0 && len(rep.Cited) == 0 {
		log.Warn().Msg("answer cited no retrieved source (ungrounded)")
	}
	return answer
}

func citationMarkers(indices []int) []string {
	if len(indices) == 0 {
		return nil
	}
	out := make([]string, len(indices))
	for i, n := range indices {
		out[i] = "[S" + strconv.Itoa(n) + "]"
	}
	return out
}

// Контекст и источники строятся в одном проходе, чтобы метки [Sn] совпадали с источниками один к одному.
func buildEvidence(chunks []domain.RetrievedChunk, fullSnippets bool) (string, []domain.Source) {
	parts := make([]string, 0, len(chunks))
	sources := make([]domain.Source, 0, len(chunks))
	for _, c := range chunks {
		t := strings.TrimSpace(c.Text)
		if t == "" {
			continue
		}
		var header strings.Builder
		fmt.Fprintf(&header, "[S%d]", len(parts)+1)
		if c.Filename != "" {
			fmt.Fprintf(&header, " Document: %s.", c.Filename)
		}
		if c.SectionHeading != "" {
			fmt.Fprintf(&header, " Section: %s.", c.SectionHeading)
		}
		if c.PageStart > 0 {
			fmt.Fprintf(&header, " Page: %d.", c.PageStart)
		}
		parts = append(parts, header.String()+"\n"+t)
		sourceText := t
		if !fullSnippets {
			sourceText = snippet(t)
		}
		sources = append(sources, domain.Source{
			DocumentID:      c.DocumentID,
			Filename:        c.Filename,
			ChunkID:         c.ID,
			Snippet:         sourceText,
			Score:           c.Score,
			CharStart:       c.CharStart,
			CharEnd:         c.CharEnd,
			PageStart:       c.PageStart,
			PageEnd:         c.PageEnd,
			SectionHeading:  c.SectionHeading,
			Origin:          c.Origin,
			RaptorSummaryID: c.RaptorSummaryID,
			IndexSnapshot:   c.IndexSnapshot,
		})
	}
	return strings.Join(parts, "\n\n"), sources
}

func snippet(text string) string {
	text = strings.TrimSpace(text)
	runes := []rune(text)
	if len(runes) <= snippetLen {
		return text
	}
	return strings.TrimSpace(string(runes[:snippetLen])) + "…"
}

// Страховка чата: поиск нашёл опору, а генератор недоступен — отдаём выдержки с источниками вместо 500.
func extractiveFallbackAnswer(sources []domain.Source) string {
	var b strings.Builder
	b.WriteString("Генерация ответа сейчас недоступна, поэтому показываю наиболее релевантные фрагменты из базы знаний.\n")
	limit := len(sources)
	if limit > extractiveFallbackMaxSources {
		limit = extractiveFallbackMaxSources
	}
	for i := range limit {
		src := sources[i]
		title := strings.TrimSpace(src.Filename)
		if title == "" {
			title = strings.TrimSpace(src.DocumentID)
		}
		if title == "" {
			title = "источник"
		}
		snip := strings.TrimSpace(src.Snippet)
		if snip == "" {
			continue
		}
		fmt.Fprintf(&b, "\n[S%d] %s: %s", i+1, title, snip)
	}
	return strings.TrimSpace(b.String())
}

var citationRef = regexp.MustCompile(`\[S(\d+)\]`)

// Переводы строк не трогаем, чтобы абзацы пережили вырезание ссылок.
var (
	multiSpace       = regexp.MustCompile(`[ \t]{2,}`)
	spaceBeforePunct = regexp.MustCompile(`[ \t]+([.,;:!?…])`)
)

type citationReport struct {
	Cited      []int
	OutOfRange []int
}

func validateCitations(answer string, nSources int) citationReport {
	seen := make(map[int]bool)
	var rep citationReport
	for _, m := range citationRef.FindAllStringSubmatch(answer, -1) {
		n, err := strconv.Atoi(m[1])
		if err != nil || n < 1 || seen[n] {
			continue
		}
		seen[n] = true
		if n > nSources {
			rep.OutOfRange = append(rep.OutOfRange, n)
		} else {
			rep.Cited = append(rep.Cited, n)
		}
	}
	return rep
}

func sanitizeCitations(answer string, nSources int) (string, citationReport) {
	rep := validateCitations(answer, nSources)
	if len(rep.OutOfRange) == 0 {
		return answer, rep
	}
	cleaned := citationRef.ReplaceAllStringFunc(answer, func(tok string) string {
		m := citationRef.FindStringSubmatch(tok)
		if n, _ := strconv.Atoi(m[1]); n > nSources {
			return ""
		}
		return tok
	})
	cleaned = multiSpace.ReplaceAllString(cleaned, " ")
	cleaned = spaceBeforePunct.ReplaceAllString(cleaned, "$1")
	return strings.TrimSpace(cleaned), rep
}
