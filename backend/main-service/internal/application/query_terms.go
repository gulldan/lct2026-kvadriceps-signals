package application

import (
	"context"
	"errors"
	"fmt"
	"strconv"
	"strings"
	"time"
	"unicode"
	"unicode/utf8"

	"github.com/example/main-service/internal/platform/aiclients"
)

// Запрос часто описывает, ЧТО найти («слабые сигналы в кибербезопасности»), а не предмет. Без снятия рамки
// в источники уходит «weak signal detection» и возвращаются работы про ЭЭГ вместо кибербезопасности.
const searchTermsSystem = "You turn a technology topic typed by a user, possibly in Russian, into English search " +
	"phrases as they appear in arXiv titles and abstracts. Answer with a JSON array of 6 to 8 short phrases, " +
	"each at most three words (for example: \"llm inference\", \"kv cache\", \"speculative decoding\"): " +
	"the topic itself first, then its main sub-areas and the common wording of paper titles in that area, " +
	"nothing else. A long descriptive phrase such as \"efficient inference of large language models\" is " +
	"never found verbatim in papers, so never write one. Prefer full names over abbreviations " +
	"(artificial intelligence, not AI) unless the abbreviation is the usual title wording. " +
	"The user often describes what they want found rather than the subject: phrases like \"weak signals\", " +
	"\"слабые сигналы\", \"emerging trends\", \"зарождающиеся технологии\", \"перспективные решения\", " +
	"\"promising solutions\", \"early indicators\" are that framing. Drop them and translate only the " +
	"technology area they are asked about, then expand that area."

// liveGatewayRPMKey — клиентский предел обращений к шлюзу в минуту; ноль по умолчанию снимает троттлинг:
// шлюз держит 256 одновременных запросов без квот, а предел растягивал волны вызовов одной выдачи.
const liveGatewayRPMKey = "RADAR_LLM_RPM"

func liveGatewayRPM(ctx context.Context, get func(ctx context.Context, key, def string) string) int {
	rpm, err := strconv.Atoi(strings.TrimSpace(get(ctx, liveGatewayRPMKey, "0")))
	if err != nil {
		return 0
	}
	return rpm
}

var errLLMNotConfigured = errors.New("llm gateway not configured")

type QueryTranslator func(ctx context.Context, query string) ([]string, error)

// LLMTarget читает настройки шлюза на каждый вызов, чтобы правка оператора применялась без перезапуска.
func LLMTarget(get func(ctx context.Context, key, def string) string) func(context.Context) aiclients.GenTarget {
	return func(ctx context.Context) aiclients.GenTarget {
		return aiclients.GenTarget{
			URL:             get(ctx, "VLLM_URL", ""),
			Model:           get(ctx, "VLLM_MODEL", ""),
			ReasoningEffort: get(ctx, "VLLM_REASONING_EFFORT", ""),
			APIKey:          get(ctx, "VLLM_API_KEY", ""),
			RPM:             liveGatewayRPM(ctx, get),
		}
	}
}

// NewQueryTranslator без настроенного шлюза отказывает, а не угадывает: вызывающий называет это ограничение.
func NewQueryTranslator(gen aiclients.Generator, configured func(context.Context) bool, limit int) QueryTranslator {
	return func(ctx context.Context, query string) ([]string, error) {
		if !configured(ctx) {
			return nil, errLLMNotConfigured
		}
		ctx, cancel := context.WithTimeout(ctx, 20*time.Second)
		defer cancel()
		out, err := gen.Generate(ctx, aiclients.GenRequest{System: searchTermsSystem, Query: query, MaxTokens: 160,
			Temperature: 0})
		if err != nil {
			return nil, err
		}
		terms := ParseSearchTerms(out.Text, limit)
		if len(terms) == 0 {
			return nil, fmt.Errorf("unusable translation: %q", strings.TrimSpace(out.Text))
		}
		return terms, nil
	}
}

const topicNameSystem = "You name one research direction from the titles of its recent arXiv papers. Answer with a " +
	"Russian name of 2 to 6 words that a non-specialist reads at a glance, no quotes, no period, nothing else."

func NewTopicNamer(gen aiclients.Generator, configured func(context.Context) bool,
) func(context.Context, []string) (string, error) {
	return func(ctx context.Context, titles []string) (string, error) {
		if !configured(ctx) {
			return "", errLLMNotConfigured
		}
		// Под нагрузкой шлюза имя приходит в среднем за 12 с и нередко упирается в 20 с; 45 с — запас.
		ctx, cancel := context.WithTimeout(ctx, 45*time.Second)
		defer cancel()
		out, err := gen.Generate(ctx, aiclients.GenRequest{System: topicNameSystem,
			Query: "Titles:\n- " + strings.Join(titles, "\n- "), MaxTokens: 60, Temperature: 0})
		if err != nil {
			return "", err
		}
		label := strings.Trim(strings.TrimSpace(out.Text), "«»\"'.")
		if label == "" || strings.ContainsAny(label, "\n\r") || utf8.RuneCountInString(label) > 60 ||
			!strings.ContainsFunc(label, func(r rune) bool { return unicode.Is(unicode.Cyrillic, r) }) {
			return "", fmt.Errorf("unusable topic name: %q", label)
		}
		return label, nil
	}
}
