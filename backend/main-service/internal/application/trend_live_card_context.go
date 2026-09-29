package application

import (
	"cmp"
	"context"
	"crypto/sha256"
	"fmt"
	"net/url"
	"regexp"
	"strings"
	"time"
	"unicode/utf8"
)

const (
	liveCardContextSourceRunes = 12000
	liveCardContextTotalRunes  = 36000
	liveCardContextParser      = "arxiv-html-blocks-v2"
	liveCardContextAbstract    = "abstract"
	liveCardContextFull        = "fulltext"
	liveCardContextExcerpted   = "excerpted_fulltext"
	liveCardArxivHost          = "arxiv.org"
	liveCardSecureScheme       = "https"
	liveCardLegacyScheme       = "http"
)

// LiveCardSourceContext хранит неизменённое основание обоих обращений к модели, а не вердикт об истинности.
type LiveCardSourceContext struct {
	SourceID       string                 `json:"source_id"`
	SourceURL      string                 `json:"source_url"`
	ArxivID        string                 `json:"arxiv_id"`
	Coverage       string                 `json:"coverage"`
	Limitation     string                 `json:"limitation,omitempty"`
	Text           string                 `json:"text"`
	TextSHA256     string                 `json:"text_sha256"`
	RawSHA256      string                 `json:"raw_sha256,omitempty"`
	BodySHA256     string                 `json:"body_sha256,omitempty"`
	ParserRevision string                 `json:"parser_revision,omitempty"`
	ObservedAt     time.Time              `json:"observed_at,omitzero"`
	Ranges         []LiveCardContextRange `json:"ranges,omitempty"`
}

// LiveCardContextRange задаёт полуоткрытые диапазоны в рунах нормализованного тела с BodySHA256, не байтах HTML.
type LiveCardContextRange struct {
	StartRune int `json:"start_rune"`
	EndRune   int `json:"end_rune"`
}

var liveCardArxivPattern = regexp.MustCompile(`^[0-9]{4}\.[0-9]{4,5}(?:v[1-9][0-9]{0,3})?$`)

func liveCardArxivID(paper StudyPaper, meta LivePaperMetadata) (string, string) {
	if paper.DocumentID != "" || paper.SnapshotID != "" {
		return "", ""
	}
	id, own := strings.CutPrefix(paper.ID, "arxiv:")
	if !own {
		id = ""
	}
	found := false
	for _, address := range []string{meta.SourceURL, paper.URL} {
		next := liveCardArxivAddress(address)
		if next == "" {
			continue
		}
		base, _, versioned := strings.Cut(id, "v")
		nextBase, _, nextVersioned := strings.Cut(next, "v")
		if id != "" && (base != nextBase || versioned && nextVersioned && id != next) {
			return "", ""
		}
		if !versioned {
			id = next
		}
		found = true
	}
	if found {
		return id, "https://arxiv.org/abs/" + id
	}
	return "", ""
}

func liveCardArxivAddress(address string) string {
	u, err := url.Parse(address)
	if err != nil || u.Host != liveCardArxivHost || u.User != nil || u.RawQuery != "" || u.Fragment != "" ||
		u.RawPath != "" || (u.Scheme != liveCardSecureScheme && u.Scheme != liveCardLegacyScheme) {
		return ""
	}
	for _, prefix := range []string{"/abs/", "/html/", "/pdf/"} {
		if id, ok := strings.CutPrefix(u.Path, prefix); ok && liveCardArxivPattern.MatchString(id) {
			return id
		}
	}
	return ""
}

// Чужие URL и приватный корпус не попадают ни в загрузчик, ни в общий сохранённый контекст.
func liveCardContexts(ctx context.Context, works, ids []string, documents map[string]StudyPaper,
	metadata map[string]LivePaperMetadata,
) []LiveCardSourceContext {
	budget, _ := ctx.Value(liveCardContextKey{}).(*liveCardContextBudget)
	remaining := liveCardContextTotalRunes
	for _, work := range works {
		remaining -= utf8.RuneCountInString(work)
	}
	contexts := make([]LiveCardSourceContext, 0, len(ids))
	for index, sourceID := range ids {
		id, address := liveCardArxivID(documents[sourceID], metadata[sourceID])
		if id == "" {
			continue
		}
		source := LiveCardSourceContext{SourceID: sourceID, SourceURL: address, ArxivID: id,
			Coverage: liveCardContextAbstract, Text: works[index]}
		limit := min(liveCardContextSourceRunes, remaining+utf8.RuneCountInString(source.Text))
		liveExpandCardContext(ctx, budget, &source, limit)
		remaining -= utf8.RuneCountInString(source.Text) - utf8.RuneCountInString(works[index])
		source.TextSHA256 = liveCardTextHash(source.Text)
		works[index] = source.Text
		contexts = append(contexts, source)
	}
	return contexts
}

func liveExpandCardContext(ctx context.Context, budget *liveCardContextBudget, source *LiveCardSourceContext, limit int) {
	if budget == nil {
		source.Limitation = "полный текст не загружался"
		return
	}
	if limit <= 0 {
		source.Limitation = "полный текст не загружен: исчерпан объём контекста карточки"
		return
	}
	document, err := budget.get(ctx, source.ArxivID)
	if err != nil {
		source.Limitation = "полный текст недоступен: " + liveCardContextFailure(err)
		return
	}
	text, ranges := liveSelectCardContext(document, limit)
	if text != document.text {
		text, ranges = liveSelectCardContext(document, limit-utf8.RuneCountInString(document.lead)-2)
		if text != "" && document.lead != "" {
			text = document.lead + "\n\n" + text
		}
	}
	if text == "" {
		source.Limitation = "целые фрагменты полного текста не поместились в контекст карточки"
		return
	}
	source.ArxivID, source.SourceURL = document.arxivID, document.address
	source.RawSHA256, source.BodySHA256 = document.rawHash, liveCardTextHash(document.text)
	source.ParserRevision, source.ObservedAt = liveCardContextParser, document.observed
	source.Ranges = ranges
	source.Text = text
	source.Coverage = liveCardContextFull
	if text != document.text {
		source.Coverage = liveCardContextExcerpted
		source.Limitation = "переданы цельные фрагменты полного текста; остальные части не помещаются в контекст"
	}
}

func liveCardTextHash(text string) string {
	return fmt.Sprintf("%x", sha256.Sum256([]byte(text)))
}

func liveCardContextBasis(contexts []LiveCardSourceContext) string {
	if len(contexts) == 0 {
		return ""
	}
	full, excerpted, abstracts := 0, 0, 0
	for _, source := range contexts {
		switch source.Coverage {
		case liveCardContextFull:
			full++
		case liveCardContextExcerpted:
			excerpted++
		default:
			abstracts++
		}
	}
	return fmt.Sprintf("; контекст arXiv: полное доступное HTML-тело — %d, цельные фрагменты тела — %d, "+
		"только аннотация — %d; покрытие остальных источников этим не подтверждается", full, excerpted, abstracts)
}

func liveCardContextFailure(err error) string {
	return cmp.Or(liveCardBudgetFailure(err), "ошибка загрузки или непроверенное происхождение HTML")
}
