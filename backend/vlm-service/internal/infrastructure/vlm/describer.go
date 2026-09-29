package vlm

import (
	"context"
	"fmt"
	"net/http"
	"strings"
	"time"

	"github.com/example/vlm-service/internal/platform/aiclients"
)

const describePrompt = "Опиши изображение подробно, плотным текстом для поискового индекса. " +
	"Если это технологическая схема или чертёж: перечисли все блоки и оборудование с марками " +
	"и обозначениями, опиши потоки по стрелкам (что, откуда и куда идёт), приведи все подписи, " +
	"параметры и единицы измерения. Для остальных изображений опиши содержимое, видимый текст " +
	"и ключевые детали."

// StyleOpenAI включает диалект OpenAI vision chat/completions; иначе — родной протокол {model,image_b64,mime,prompt}.
const StyleOpenAI = "openai"

type Describer struct {
	client aiclients.VLM
}

// NewDescriber с пустым url отдаёт детерминированную заглушку платформы: воркер работает без модели.
func NewDescriber(url, model, apiKey, style string) *Describer {
	if url != "" && style == StyleOpenAI {
		return &Describer{client: &openAIVLM{
			url:    strings.TrimRight(url, "/"),
			model:  model,
			apiKey: apiKey,
			httpc:  &http.Client{Timeout: 120 * time.Second},
		}}
	}
	return &Describer{client: aiclients.NewVLM(url, model, nil)}
}

func (d *Describer) Describe(ctx context.Context, data []byte, mime string) (string, error) {
	text, err := d.client.Describe(ctx, data, mime, describePrompt)
	if err != nil {
		return "", fmt.Errorf("vlm describe: %w", err)
	}
	return text, nil
}
