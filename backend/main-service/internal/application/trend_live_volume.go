package application

import (
	"cmp"
	"context"
	"encoding/json/v2"
	"sync"
	"time"
	"unicode/utf8"

	"github.com/example/main-service/internal/platform/aiclients"
)

const liveCharsPerPage = 1800

// liveCharsPerToken — оценка по английским аннотациям, а не замер токенизатора; для русского завышена.
const liveCharsPerToken = 4

type LiveVolume struct {
	// Scanned — размер стога, а не выдачи: окно просматривается целиком на каждый запрос.
	Scanned   int `json:"scanned"`
	Fetched   int `json:"fetched"`
	Processed int `json:"processed"`
	Stored    int `json:"stored"`
	// Chars — объём первичного сбора; дополнительный контекст карточек учитывается во входе генерации.
	Chars      int                 `json:"chars"`
	Pages      int                 `json:"pages"`
	Tokens     int                 `json:"tokens"`
	Channels   []LiveVolumeChannel `json:"channels,omitempty"`
	Model      LiveModelUsage      `json:"model,omitzero"`
	Analysis   LiveAnalysisUsage   `json:"analysis,omitzero"`
	Classifier LiveClassifierUsage `json:"classifier,omitzero"`
}

// LiveModelUsage считает вызовы без отчёта шлюза отдельно: неизвестный расход не бесплатный.
type LiveModelUsage struct {
	Calls            int `json:"calls"`
	Failed           int `json:"failed"`
	Unreported       int `json:"unreported"`
	PromptChars      int `json:"prompt_chars"`
	PromptTokens     int `json:"prompt_tokens"`
	CompletionTokens int `json:"completion_tokens"`
	// DurationMs — сумма вызовов, и она больше времени поиска: вызовы идут параллельно.
	DurationMs int                  `json:"duration_ms"`
	SlowestMs  int                  `json:"slowest_ms"`
	Operations []LiveModelOperation `json:"operations,omitempty"`
}

type LiveModelOperation struct {
	Operation        string `json:"operation"`
	Calls            int    `json:"calls"`
	Failed           int    `json:"failed"`
	PromptTokens     int    `json:"prompt_tokens"`
	CompletionTokens int    `json:"completion_tokens"`
	DurationMs       int    `json:"duration_ms"`
	SlowestMs        int    `json:"slowest_ms"`
}

type LiveAnalysisUsage struct {
	Requests   int `json:"requests"`
	Papers     int `json:"papers"`
	Chars      int `json:"chars"`
	Tokens     int `json:"tokens"`
	DurationMs int `json:"duration_ms"`
}

// LiveClassifierUsage считается и при отключённом классификаторе: ML-коллеге нужен объём заранее.
type LiveClassifierUsage struct {
	Connected  bool `json:"connected"`
	Candidates int  `json:"candidates"`
	Sources    int  `json:"sources"`
	Chars      int  `json:"chars"`
	Tokens     int  `json:"tokens"`
	// Bytes — сумма размеров запросов; потолок отдельного входа классификатора 4 МиБ.
	Bytes int `json:"bytes"`
}

type LiveVolumeChannel struct {
	Channel string `json:"channel"`
	Records int    `json:"records"`
	Chars   int    `json:"chars"`
}

// liveVolume считает по собранному ответу, чтобы число на экране совпадало с ответом.
func liveVolume(detail LiveDetail, metadata map[string]LivePaperMetadata, scanned int,
	tally *liveRunTally,
) LiveVolume {
	volume := LiveVolume{Scanned: scanned, Stored: len(liveStoredRecords(detail))}
	if tally != nil {
		volume.Model, volume.Analysis, volume.Classifier = tally.read()
	}
	seen := make(map[string]bool, len(detail.Papers)*2)
	byChannel := map[string]*LiveVolumeChannel{}
	order := []string{}
	add := func(channel, address, text string) {
		volume.Fetched++
		if address == "" || seen[address] {
			return
		}
		seen[address] = true
		volume.Processed++
		chars := utf8.RuneCountInString(text)
		volume.Chars += chars
		stat, ok := byChannel[channel]
		if !ok {
			stat = &LiveVolumeChannel{Channel: channel}
			byChannel[channel] = stat
			order = append(order, channel)
		}
		stat.Records++
		stat.Chars += chars
	}
	for _, paper := range detail.Papers {
		meta := metadata[paper.ID]
		add("science", cmp.Or(paper.URL, meta.SourceURL), paper.Title+" "+meta.Abstract)
	}
	for _, channel := range []struct {
		name string
		body *LiveChannel
	}{
		{"patents", detail.Topic.Patents}, {"patents_cn", detail.Topic.PatentsCN},
		{"registry", detail.Topic.Registry}, {"web", detail.Topic.Web}, {"corpus", detail.Topic.Corpus},
		{"research", detail.Topic.Research},
	} {
		if channel.body == nil {
			continue
		}
		for _, record := range channel.body.Records {
			add(channel.name, cmp.Or(record.URL, record.CanonicalURL), record.Title)
		}
	}
	for _, event := range detail.Topic.External {
		add(cmp.Or(event.Kind, "external"), event.URL, event.Title)
	}
	volume.Pages = (volume.Chars + liveCharsPerPage - 1) / liveCharsPerPage
	volume.Tokens = volume.Chars / liveCharsPerToken
	for _, name := range order {
		volume.Channels = append(volume.Channels, *byChannel[name])
	}
	return volume
}

type liveRunTallyKey struct{}

type liveRunTally struct {
	mu         sync.Mutex
	model      LiveModelUsage
	operations map[string]*LiveModelOperation
	order      []string
	analysis   LiveAnalysisUsage
	classifier LiveClassifierUsage
}

// liveWithTally заводит накопитель только поиску: у ночного сбора его нет, отсюда проверки на nil.
func liveWithTally(ctx context.Context) (context.Context, *liveRunTally) {
	tally := &liveRunTally{operations: map[string]*LiveModelOperation{}}
	return context.WithValue(ctx, liveRunTallyKey{}, tally), tally
}

func liveTallyOf(ctx context.Context) *liveRunTally {
	tally, _ := ctx.Value(liveRunTallyKey{}).(*liveRunTally)
	return tally
}

func (t *liveRunTally) addLLM(operation string, request aiclients.GenRequest, response aiclients.GenResponse,
	succeeded bool, elapsed time.Duration,
) {
	if t == nil {
		return
	}
	usage := response.Usage
	reported := usage.TotalTokens > 0 || usage.PromptTokens > 0 || usage.CompletionTokens > 0
	millis := int(elapsed.Milliseconds())
	t.mu.Lock()
	defer t.mu.Unlock()
	t.model.Calls++
	t.model.PromptChars += utf8.RuneCountInString(request.System) + utf8.RuneCountInString(request.Query)
	t.model.DurationMs += millis
	t.model.SlowestMs = max(t.model.SlowestMs, millis)
	step, ok := t.operations[operation]
	if !ok {
		step = &LiveModelOperation{Operation: operation}
		t.operations[operation] = step
		t.order = append(t.order, operation)
	}
	step.Calls++
	step.DurationMs += millis
	step.SlowestMs = max(step.SlowestMs, millis)
	if !succeeded {
		t.model.Failed++
		step.Failed++
	}
	if !reported {
		t.model.Unreported++
		return
	}
	t.model.PromptTokens += usage.PromptTokens
	t.model.CompletionTokens += usage.CompletionTokens
	step.PromptTokens += usage.PromptTokens
	step.CompletionTokens += usage.CompletionTokens
}

func (t *liveRunTally) addAnalysis(papers, chars int, elapsed time.Duration) {
	if t == nil {
		return
	}
	t.mu.Lock()
	defer t.mu.Unlock()
	t.analysis.Requests++
	t.analysis.Papers += papers
	t.analysis.Chars += chars
	t.analysis.Tokens += chars / liveCharsPerToken
	t.analysis.DurationMs += int(elapsed.Milliseconds())
}

// addClassifier меряет вход тем же запросом, что уходит модели, чтобы оценка не разошлась с телом.
func (t *liveRunTally) addClassifier(input LiveClassifierRequest, connected bool) {
	if t == nil {
		return
	}
	usage := LiveClassifierUsage{Connected: connected, Candidates: len(input.Candidates)}
	for _, candidate := range input.Candidates {
		usage.Sources += len(candidate.Sources)
		usage.Chars += utf8.RuneCountInString(candidate.Name)
		for _, source := range candidate.Sources {
			usage.Chars += utf8.RuneCountInString(source.Title) + utf8.RuneCountInString(source.Abstract)
		}
	}
	usage.Tokens = usage.Chars / liveCharsPerToken
	if body, err := json.Marshal(input); err == nil {
		usage.Bytes = len(body)
	}
	t.mu.Lock()
	defer t.mu.Unlock()
	// Готовые кандидаты приходят несколькими пакетами: последний не заменяет расход предыдущих.
	t.classifier.Connected = t.classifier.Connected || usage.Connected
	t.classifier.Candidates += usage.Candidates
	t.classifier.Sources += usage.Sources
	t.classifier.Chars += usage.Chars
	t.classifier.Tokens += usage.Tokens
	t.classifier.Bytes += usage.Bytes
}

func (t *liveRunTally) read() (LiveModelUsage, LiveAnalysisUsage, LiveClassifierUsage) {
	t.mu.Lock()
	defer t.mu.Unlock()
	model := t.model
	for _, operation := range t.order {
		model.Operations = append(model.Operations, *t.operations[operation])
	}
	return model, t.analysis, t.classifier
}

// liveAnalysisSize меряет весь снимок, а не выдачу: по нему ML-коллега считает нагрузку.
func liveAnalysisSize(snapshot *LiveSnapshot) (papers, chars int) {
	if snapshot == nil {
		return 0, 0
	}
	for _, paper := range snapshot.Papers {
		chars += utf8.RuneCountInString(paper.Title) +
			utf8.RuneCountInString(snapshot.PaperMetadata[paper.ID].Abstract)
	}
	return len(snapshot.Papers), chars
}
