package application

import (
	"cmp"
	"context"
	"regexp"
	"slices"
	"strings"
	"unicode/utf8"
)

// Словарь нарочно ставит бенчмарк выше внедрения: обратная ошибка стоила бы доверия к стадии.

type liveEventRule struct {
	kind    string
	pattern *regexp.Regexp
}

// liveEventRules — порядок значим: эксперимент раньше рынка, пилот и прототип раньше внедрения (занижение
// дешевле завышения), раунд после них — стадию доказывает клиент, а не деньги.
func liveEventRules() []liveEventRule {
	return []liveEventRule{
		{LiveEventLabResult, liveEventLabWords}, {LiveEventStandard, liveEventStandardWords},
		{LiveEventPilot, liveEventPilotWords}, {LiveEventPrototype, liveEventPrototypeWords},
		{LiveEventDeployment, liveEventDeploymentWords},
		{LiveEventFunding, liveEventFundingWords}, {LiveEventProductLaunch, liveEventLaunchWords},
		{LiveEventCodeRelease, liveEventCodeWords},
	}
}

var (
	liveEventLabWords = regexp.MustCompile(`(?i)\bbenchmark|state[- ]of[- ]the[- ]art|\bsota\b|outperform|\bbaselines?\b|` +
		`\bablation|on the [a-z0-9-]+ dataset|бенчмарк|превосход\p{L}* (базов|существующ|известн)|на наборе данных`)
	liveEventStandardWords = regexp.MustCompile(`(?i)\b(rfc ?\d{3,5}|iso(/iec)? ?\d{3,5}|ieee ?\d{3,5}|ietf|` +
		`w3c recommendation)\b|гост( р)? ?\d`)
	liveEventDeploymentWords = regexp.MustCompile(`(?i)\bdeploy(s|ed|ing|ment)? (at|by|across|with|for|in production)|` +
		`\bin production\b|\brolled out\b|\bwent live\b|\bgoes live\b|\bsigned (a |an )?(contract|agreement|deal)|` +
		`\bcontract (with|from|worth)|\bawarded (a |an )?contract|\bpurchase order|\badopted by\b|` +
		`\bcustomers? (include|such as)|внедрил|внедрен|внедрён|развернул|развёрнут|контракт|закупк|` +
		`в промышленн\p{L}* эксплуатац|ввел\p{L}* в эксплуатац|ввёл\p{L}* в эксплуатац`)
	liveEventPilotWords = regexp.MustCompile(`(?i)\bpilot(s|ed|ing)?\b|\bfield (test|trial)|\btrial (with|at|run)|` +
		`пилот|опытн\p{L}* эксплуатац`)
	liveEventPrototypeWords = regexp.MustCompile(`(?i)\bprototyp|proof[- ]of[- ]concept|\bpoc\b|\btestbed\b|` +
		`прототип|опытн\p{L}* образ|демонстратор`)
	liveEventFundingWords = regexp.MustCompile(`(?i)\braise[sd]?\b|\braising\b|\bfunding round|\bseries [a-f]\b|` +
		`\b(pre-)?seed (round|funding|investment)|\bventure (round|funding)|\bsecures? [$€£]|\bcloses? [$€£]|` +
		`привлек|привлёк|раунд|посевн\p{L}* инвестиц`)
	liveEventLaunchWords = regexp.MustCompile(`(?i)\blaunch(es|ed)?\b|\bunveil|\bintroduc(es|ed)\b|` +
		`\bgenerally available\b|\bgeneral availability\b|\bcommercially available\b|\bnow available\b|` +
		`выпустил|представил|запустил|вывел\p{L}* на рынок|анонсировал`)
	liveEventCodeWords = regexp.MustCompile(`(?i)\bopen[- ]sourced?\b|github\.com/|\breleased? (the )?(code|weights)|` +
		`\bv\d+\.\d+(\.\d+)?\b|открыт\p{L}* код|выложил\p{L}* код`)
)

func liveEventTypeOf(text string) string {
	for _, rule := range liveEventRules() {
		if rule.pattern.MatchString(text) {
			return rule.kind
		}
	}
	return ""
}

// liveNewsRules читаются только в рыночных записях: в научной работе «метод использует» — не внедрение.
func liveNewsRules() []liveEventRule {
	return []liveEventRule{{LiveEventPilot, liveNewsPilotWords}, {LiveEventDeployment, liveNewsDeploymentWords},
		{LiveEventProductLaunch, liveNewsLaunchWords}}
}

var (
	liveNewsPilotWords      = regexp.MustCompile(`(?i)тестиру\p{L}*|испытыва\p{L}*|опробова\p{L}*`)
	liveNewsDeploymentWords = regexp.MustCompile(`(?i)использу\p{L}*|применя\p{L}*|перешл\p{L}* на|перешёл на|` +
		`перев[её]л\p{L}* на|внедря\p{L}*|интегрир\p{L}*|подключ\p{L}*`)
	liveNewsLaunchWords = regexp.MustCompile(`(?i)выпуст\p{L}*|выпуск\p{L}*|начал\p{L}* продаж|стартова\p{L}*|` +
		`открыл\p{L}* доступ`)
)

func liveNewsTypeOf(text string) string {
	if kind := liveEventTypeOf(text); kind != "" {
		return kind
	}
	for _, rule := range liveNewsRules() {
		if rule.pattern.MatchString(text) {
			return rule.kind
		}
	}
	return ""
}

// liveEventBenchmark не перебивают ни модель, ни источник: бенчмарк внедрением не становится.
func liveEventBenchmark(text string) bool {
	return liveEventLabWords.MatchString(text)
}

var (
	liveEventAmount = regexp.MustCompile(`(?i)[$€£]\s?\d+(?:[.,]\d+)?\s?(?:million|billion|bn|m|b|k|млн|млрд)?\b|` +
		`\d+(?:[.,]\d+)?\s?(?:млн|млрд|тыс\.)\s?(?:руб\p{L}*|долл\p{L}*|евро|\$)?`)
	liveEventRound = regexp.MustCompile(`(?i)\b(pre-seed|seed|pre-series [a-f]|series [a-f])\b|посевн\p{L}*|раунд\p{L}* [a-f]\b`)
	liveEventActor = regexp.MustCompile(`^(.{2,60}?)\s+(?i:raises|raised|secures|closes|lands|nabs|bags|announces|` +
		`launches|unveils|deploys|pilots|привлекла|привлёк|привлек|запустила|представила|внедрила)\b`)
	liveEventSentence = regexp.MustCompile(`[.!?]\s+`)
)

// Одно-два предложения: весь фрагмент ленты длиннее и чаще говорит о другом.
const liveEventQuoteMax = 400

// liveEventFromText не перезаписывает поля, которые дал источник.
func liveEventFromText(event LiveAdoptionEvent) LiveAdoptionEvent {
	title, excerpt := event.Record.Title, event.Record.Excerpt
	text := title + ". " + excerpt
	if event.Type == "" {
		event.Type = liveNewsTypeOf(text)
	}
	if event.Quote == "" {
		event.Quote = liveEventQuote(title, excerpt, event.Type)
	}
	if event.Type == LiveEventFunding {
		if event.Amount == "" {
			event.Amount = strings.TrimSpace(liveEventAmount.FindString(text))
		}
		if event.Phase == "" {
			event.Phase = strings.TrimSpace(liveEventRound.FindString(text))
		}
	}
	if event.Actor == "" {
		if match := liveEventActor.FindStringSubmatch(title); match != nil {
			event.Actor = strings.Trim(match[1], " «»\"'—-:")
		}
	}
	return event
}

func liveEventQuote(title, excerpt, kind string) string {
	var pattern *regexp.Regexp
	for _, rule := range slices.Concat(liveNewsRules(), liveEventRules()) {
		if rule.kind == kind {
			pattern = rule.pattern
		}
	}
	quote := title
	for _, sentence := range liveEventSentence.Split(excerpt, -1) {
		if pattern != nil && pattern.MatchString(sentence) {
			quote = strings.TrimSpace(sentence)
			break
		}
	}
	if utf8.RuneCountInString(quote) > liveEventQuoteMax {
		quote = string([]rune(quote)[:liveEventQuoteMax]) + "…"
	}
	return quote
}

// liveEventNamed: совпадение в полном тексте документа ещё не значит, что заметка о технологии.
func liveEventNamed(event LiveAdoptionEvent, query LiveEventQuery) bool {
	for _, repository := range query.Repos {
		if repository != "" && strings.HasPrefix(strings.ToLower(event.Record.URL), strings.ToLower(repository)) {
			return true
		}
	}
	words := liveWords(event.Record.Title + " " + event.Quote)
	stemmed := liveStems(slices.Clone(words))
	if english := liveStems(liveWords(query.Term)); len(english) > 0 &&
		!slices.ContainsFunc(english, func(word string) bool { return !slices.Contains(stemmed, word) }) {
		return true
	}
	stems := []string{}
	for _, word := range liveWords(query.Russian) {
		if runes := []rune(word); len(runes) >= 4 {
			stems = append(stems, string(runes[:max(4, len(runes)-2)]))
		}
	}
	return len(stems) > 0 && !slices.ContainsFunc(stems, func(stem string) bool {
		return !slices.ContainsFunc(words, func(word string) bool { return strings.HasPrefix(word, stem) })
	})
}

func (s *TrendLiveService) SetEventCheck(check LiveEventCheck) { s.checkEvents = check }

// liveEventCheckMax — событий на одно обращение: длиннее ответ модели в волне, а стадию решают первые.
const liveEventCheckMax = 12

// eventCheckJobs: работы пишут только в свои ячейки, события меняются после волны — карточка той же
// технологии пишется параллельно.
func (s *TrendLiveService) eventCheckJobs(ctx context.Context, topics []LiveTopic) (jobs []func(), apply func()) {
	if s.checkEvents == nil {
		return nil, func() {}
	}
	picked := make([][]int, len(topics))
	verdicts := make([][]LiveEventVerdict, len(topics))
	for i := range topics {
		for k, event := range topics[i].Events {
			if event.Type != "" && event.Count == nil && len(picked[i]) < liveEventCheckMax {
				picked[i] = append(picked[i], k)
			}
		}
		if len(picked[i]) == 0 {
			continue
		}
		events := make([]LiveAdoptionEvent, len(picked[i]))
		for n, k := range picked[i] {
			events[n] = topics[i].Events[k]
		}
		identity := liveEventIdentity(topics[i])
		jobs = append(jobs, func() {
			if found, err := s.checkEvents(ctx, identity, events); err == nil {
				verdicts[i] = found
			}
		})
	}
	return jobs, func() {
		for i := range topics {
			if verdicts[i] != nil {
				topics[i].Events = liveApplyEventVerdicts(topics[i].Events, picked[i], verdicts[i])
			}
		}
	}
}

func liveEventIdentity(topic LiveTopic) LiveCardIdentity {
	name := liveCandidateName(topic)
	identity := LiveCardIdentity{Label: name, Mechanism: name}
	if qualification := topic.Qualification; qualification != nil && qualification.Mechanism != "" {
		identity = LiveCardIdentity{Label: cmp.Or(qualification.Label, name), Mechanism: qualification.Mechanism}
	}
	return identity
}

// liveApplyEventVerdicts: тип от источника модель не меняет, тип словаря меняет — кроме бенчмарка.
func liveApplyEventVerdicts(events []LiveAdoptionEvent, picked []int, verdicts []LiveEventVerdict) []LiveAdoptionEvent {
	out := slices.Clone(events)
	drop := map[int]bool{}
	for n, k := range picked {
		verdict := verdicts[n]
		kind := verdict.Type
		if !verdict.About || (kind == liveEventNone && !out[k].typed) {
			drop[k] = true
			continue
		}
		switch {
		case out[k].typed:
		case LiveEventStage(kind) > 1 && liveEventBenchmark(out[k].Record.Title+". "+out[k].Quote):
			out[k].Type = LiveEventLabResult
		case slices.Contains(liveEventTypes(), kind):
			out[k].Type = kind
		}
		out[k].Verified = true
		out[k].Basis = "привязка к механизму и тип события проверены моделью: " + verdict.Reason
	}
	kept := out[:0]
	for k, event := range out {
		if !drop[k] {
			kept = append(kept, event)
		}
	}
	return kept
}

func liveEventTypes() []string {
	return []string{LiveEventGrantEarly, LiveEventGrantLate, LiveEventCodeRelease, LiveEventPrototype, LiveEventProductLaunch,
		LiveEventPilot, LiveEventDeployment, LiveEventFunding, LiveEventHiring, LiveEventStandard, LiveEventLabResult}
}
