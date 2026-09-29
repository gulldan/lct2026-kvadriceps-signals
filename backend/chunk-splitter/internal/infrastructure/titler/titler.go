package titler

import (
	"context"
	"strings"
	"unicode/utf8"

	"github.com/example/chunk-splitter/internal/platform/aiclients"
	"github.com/example/chunk-splitter/internal/platform/config"
	"github.com/example/chunk-splitter/internal/platform/llmusage"
	"github.com/example/chunk-splitter/internal/platform/logger"
)

// Generator строится через GenTarget, а не позиционный NewGenerator: только цель несёт VLLM_REASONING_EFFORT.
// Без него рассуждающая модель шлюза тратит все 128 токенов на скрытые рассуждения и отдаёт пустой заголовок.
func Generator() aiclients.Generator {
	target := aiclients.GenTarget{
		URL:             config.Get("VLLM_URL", ""),
		Model:           config.Get("VLLM_MODEL", ""),
		APIKey:          config.Get("VLLM_API_KEY", ""),
		RPM:             config.GetInt("VLLM_RPM", 0),
		ReasoningEffort: config.Get("VLLM_REASONING_EFFORT", ""),
	}
	return aiclients.NewDynamicGenerator(func(context.Context) aiclients.GenTarget { return target }, nil)
}

const titleOperation = "title_extract"

// Заголовок и титульные данные умещаются в начале; больше — только трата токенов.
const openingRunes = 1500

// Ответ длиннее — это предложение или аннотация, а не заголовок, и он отбрасывается.
const maxTitleRunes = 300

const systemPrompt = "You analyse the opening text of a document (often a scientific article). " +
	"Reply with EXACTLY two lines and nothing else:\n" +
	"TITLE: <the document's title as plain text in its original language, no quotes, " +
	"no markdown, no translation; NONE if the text has no discernible title>\n" +
	"KIND: <hypotheses if the document is essentially a list of ready-made hypotheses, " +
	"improvement ideas or brainstorm results (e.g. \"гипотезы по результатам мозгового " +
	"штурма\"); normal for everything else — articles, reports, data, methods>"

const query = "Return the document's title and kind."

const kindHypotheses = "hypotheses"

type Titler struct {
	gen aiclients.Generator
	kv  llmusage.KV
}

func New(gen aiclients.Generator, kv llmusage.KV) *Titler {
	return &Titler{gen: gen, kv: kv}
}

// Extract возвращает заголовок и вид («hypotheses» или ""); ошибок нет — извлечение не должно валить индексацию.
func (t *Titler) Extract(ctx context.Context, text string) (string, string) {
	opening := strings.TrimSpace(headRunes(text, openingRunes))
	if opening == "" {
		return "", ""
	}
	resp, err := t.gen.Generate(ctx, aiclients.GenRequest{
		System:      systemPrompt,
		Context:     opening,
		Query:       query,
		MaxTokens:   128,
		Temperature: 0,
	})
	if err != nil {
		logger.From(ctx).Warn().Err(err).Msg("title extraction failed")
		return "", ""
	}
	if t.kv != nil {
		// Токены потрачены, даже если sanitize отбросит заголовок.
		_ = llmusage.Record(ctx, t.kv, resp.Model, titleOperation,
			resp.Usage.PromptTokens, resp.Usage.CompletionTokens, resp.Usage.CostUSD)
	}
	return parseReply(resp.Text)
}

// Ответ без строк TITLE/KIND считается голым заголовком: так отвечают старые модели.
func parseReply(raw string) (string, string) {
	title, kind, labelled := "", "", false
	for _, line := range strings.Split(raw, "\n") {
		l := strings.TrimSpace(line)
		switch {
		case len(l) >= 6 && strings.EqualFold(l[:6], "TITLE:"):
			title, labelled = strings.TrimSpace(l[6:]), true
		case len(l) >= 5 && strings.EqualFold(l[:5], "KIND:"):
			kind, labelled = strings.TrimSpace(l[5:]), true
		}
	}
	if !labelled {
		title = raw
	}
	if !strings.EqualFold(kind, kindHypotheses) {
		kind = ""
	} else {
		kind = kindHypotheses
	}
	return sanitize(title), kind
}

func headRunes(s string, n int) string {
	if utf8.RuneCountInString(s) <= n {
		return s
	}
	i, count := 0, 0
	for i < len(s) && count < n {
		_, sz := utf8.DecodeRuneInString(s[i:])
		i += sz
		count++
	}
	return s[:i]
}

func sanitize(raw string) string {
	title := strings.TrimSpace(raw)
	if nl := strings.IndexAny(title, "\r\n"); nl >= 0 {
		title = strings.TrimSpace(title[:nl])
	}
	title = strings.TrimSpace(strings.Trim(title, "\"'«»“”`"))
	if title == "" || strings.EqualFold(title, "NONE") {
		return ""
	}
	if utf8.RuneCountInString(title) > maxTitleRunes {
		return ""
	}
	if len(strings.Fields(title)) == 1 && title == strings.ToUpper(title) {
		return ""
	}
	return title
}
