package application

import (
	"context"
	"fmt"
	"regexp"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/example/main-service/internal/platform/aiclients"
)

const (
	liveDigestTimeout = 60 * time.Second
	// Русский пересказ расходует больше токенов: прежний предел обрывал второе предложение.
	liveDigestTokens = 384
)

const liveDigestSystem = "Ты пересказываешь один источник для русскоязычного аналитика. Тебе дают название и " +
	"аннотацию. Ответь одним-двумя предложениями по-русски: о чём этот материал и что в нём заявлено. " +
	"Не оценивай, не обобщай на отрасль, не добавляй ничего, чего нет в тексте. Только пересказ."

const liveDigestRussianOnly = "Пиши только по-русски: английскими оставь лишь названия продуктов, " +
	"моделей и устоявшиеся аббревиатуры, служебные слова вроде «this» или «based» не используй. " +
	"Уложись в два коротких завершённых предложения, не обрывай мысль."

type SourceDigest func(ctx context.Context, title, abstract string) (digest, model string, err error)

// NewSourceDigest не трогает заголовок и ссылку: ТЗ требует русскую интерпретацию рядом с оригиналом, а не вместо него.
func NewSourceDigest(gen aiclients.Generator, configured func(context.Context) bool) SourceDigest {
	return func(ctx context.Context, title, abstract string) (string, string, error) {
		if !configured(ctx) {
			return "", "", errLLMNotConfigured
		}
		ctx, cancel := context.WithTimeout(ctx, liveDigestTimeout)
		defer cancel()
		if utf8.RuneCountInString(abstract) > liveCardAbstract {
			abstract = string([]rune(abstract)[:liveCardAbstract])
		}
		system := liveDigestSystem
		for range 2 {
			out, err := gen.Generate(ctx, aiclients.GenRequest{System: system,
				Query:     "Название: " + title + "\n\nАннотация: " + abstract,
				MaxTokens: liveDigestTokens, Temperature: 0})
			if err != nil {
				return "", "", err
			}
			if digest := liveCompletedDigest(out); digest != "" {
				return digest, out.Model, nil
			}
			// Оборванный или смешанный пересказ нельзя сохранять как готовое объяснение источника.
			system = liveDigestSystem + " " + liveDigestRussianOnly
		}
		return "", "", fmt.Errorf("unusable digest for %q", title)
	}
}

func liveCompletedDigest(out aiclients.GenResponse) string {
	if out.FinishReason != "" && out.FinishReason != "stop" {
		return ""
	}
	digest := liveRussianText(out.Text)
	if digest == "" || liveEnglishLeak.MatchString(digest) {
		return ""
	}
	// Некоторые шлюзы не передают причину остановки: незаконченную строку всё равно не публикуем.
	last, _ := utf8.DecodeLastRuneInString(strings.TrimRight(digest, "»”’\"')]"))
	if !strings.ContainsRune(".!?", last) {
		return ""
	}
	return digest
}

// «Based на», «This материал»; термины через дефис («LLM-based система») не задеваются.
var liveEnglishLeak = regexp.MustCompile(
	`(?:^|[\s(«"])(?i:the|this|that|these|based|which|is|are|was|were|of|and|with)\s+[а-яёА-ЯЁ]`)
