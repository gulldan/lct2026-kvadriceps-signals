package application

import (
	"cmp"
	"context"
	"regexp"
	"slices"
	"strings"
	"unicode"
	"unicode/utf8"
)

const liveRejectCard = "incomplete_card"

// Общий отбор заканчивается после фактического допуска всех волн, а не после первой проверки сущности.
func (s *TrendLiveService) finishCandidates(ctx context.Context, query string, umbrella *LiveTopic,
	candidates []LiveTopic, documents []StudyPaper, metadata map[string]LivePaperMetadata,
	periods []LivePeriod, rejected []LiveRejection,
) ([]LiveTopic, []LiveRejection, []StudyPaper) {
	ctx = liveWithCardContext(ctx)
	candidates, rejected, documents = s.selectCandidateWaves(ctx, query, umbrella, candidates, documents,
		metadata, periods, rejected)
	for i := range candidates {
		candidates[i].Label = liveNamedLabel(candidates[i])
		if candidates[i].TZ == nil {
			liveAssessTZ(&candidates[i], nil)
		}
	}
	return candidates, rejected, documents
}

// Пересказы источников не держат волну карточек: они дополняют уже проверенный ответ в фоне.
func (s *TrendLiveService) writeWave(ctx context.Context, query string, candidates []LiveTopic,
	documents []StudyPaper, metadata map[string]LivePaperMetadata,
) {
	byID := make(map[string]StudyPaper, len(documents))
	for _, document := range documents {
		byID[document.ID] = document
	}
	names, naming := s.russianNames(ctx, candidates, byID)
	checks, applyChecks := s.eventCheckJobs(ctx, candidates)
	jobs := slices.Concat(s.cardJobs(ctx, query, candidates, documents, metadata), checks, naming)
	s.eachConcurrently(ctx, len(jobs), func(i int) { jobs[i]() })
	applyChecks()
	for i := range candidates {
		candidates[i].Label = cmp.Or(names[i], candidates[i].Label)
	}
}

// Имя метода обязано встречаться во фразе кандидата или в цитатах, иначе оно может быть выдумано моделью.
func liveNamedLabel(topic LiveTopic) string {
	label := topic.Label
	if topic.Card == nil || liveRussianText(label) == "" || strings.Contains(label, ":") {
		return label
	}
	name, cited := liveLeadName(topic)
	// Русское слово в начале определения — род технологии, а не имя.
	if name == "" || strings.ContainsFunc(name, func(letter rune) bool { return unicode.Is(unicode.Cyrillic, letter) }) ||
		liveTechnologyLabel(name) != name || strings.Contains(strings.ToLower(label), strings.ToLower(name)) {
		return label
	}
	cited = cited || strings.Contains(topic.Name, name) || slices.ContainsFunc(topic.Card.Evidence,
		func(evidence LiveClaimEvidence) bool { return strings.Contains(evidence.Quote, name) })
	if !cited {
		return label
	}
	// Строчная после двоеточия, но аббревиатура в начале остаётся: «…: БПЛА в логистике».
	first, size := utf8.DecodeRuneInString(label)
	if second, _ := utf8.DecodeRuneInString(label[size:]); unicode.Is(unicode.Cyrillic, first) && unicode.IsLower(second) {
		label = string(unicode.ToLower(first)) + label[size:]
	}
	return name + ": " + label
}

// «Edge attribution patching (EAP) — метод…»: многословное имя с аббревиатурой.
var liveLeadAbbreviation = regexp.MustCompile(`^([A-Za-z][A-Za-z0-9 -]{2,60}?)\s+\(([A-Z][A-Za-z0-9-]{1,15})\)\s+[—–-]\s`)

func liveLeadName(topic LiveTopic) (name string, own bool) {
	if match := liveLeadAbbreviation.FindStringSubmatch(topic.Card.Summary); match != nil &&
		strings.EqualFold(match[1], topic.Name) {
		return match[2], true
	}
	words := strings.Fields(topic.Card.Summary)
	if len(words) < 2 || !slices.Contains([]string{"—", "–", "-"}, words[1]) && !strings.HasPrefix(words[1], "(") {
		return "", false
	}
	return words[0], false
}

func (s *TrendLiveService) russianNames(ctx context.Context, topics []LiveTopic,
	byID map[string]StudyPaper,
) ([]string, []func()) {
	labels := make([]string, len(topics))
	if s.name == nil {
		return labels, nil
	}
	jobs := []func(){}
	for k, topic := range topics {
		if liveRussianText(topic.Label) != "" {
			continue
		}
		titles := []string{}
		for _, id := range slices.Concat(topic.PaperIDs, topic.EvidenceIDs) {
			if title := byID[id].Title; title != "" && len(titles) < liveCardWorks {
				titles = append(titles, title)
			}
		}
		if len(titles) == 0 {
			continue
		}
		jobs = append(jobs, func() {
			if name, err := s.name(ctx, titles); err == nil {
				labels[k] = name
			}
		})
	}
	return labels, jobs
}

func liveFallbackSummaries(candidates []LiveTopic, metadata map[string]LivePaperMetadata) {
	for i := range candidates {
		candidate := &candidates[i]
		if candidate.Card != nil && candidate.Card.Summary != "" {
			continue
		}
		summary, basis := "", ""
		if qualification := candidate.Qualification; qualification != nil && qualification.Mechanism != "" {
			summary, basis = qualification.Mechanism, "описание — механизм из проверки сущности по цитатам источников"
		}
		for _, id := range slices.Concat(candidate.PaperIDs, candidate.EvidenceIDs) {
			if digest := metadata[id].Digest; summary == "" && digest != "" {
				summary, basis = digest, "описание — русский пересказ основного источника"
			}
		}
		if summary == "" {
			continue
		}
		card := LiveCard{}
		if candidate.Card != nil {
			card = *candidate.Card
		}
		card.Summary = summary
		card.Basis = strings.TrimPrefix(card.Basis+"; "+basis+", отдельной проверкой утверждения не подтверждено", "; ")
		card.Missing = slices.DeleteFunc(slices.Clone(card.Missing), func(missing string) bool {
			return strings.HasPrefix(missing, "описание технологии")
		})
		candidate.Card = &card
	}
}

type LiveShortfall struct {
	Code   string `json:"code"`
	Reason string `json:"reason"`
}

func liveOffTopicAnswer(umbrella LiveTopic) bool {
	return slices.ContainsFunc(umbrella.Limits, func(limit LiveLimit) bool { return limit.Code == liveRejectOffTopic })
}

// Допуск — после генерации: до чтения источников неизвестно, найдутся ли кейс, преимущество и оценка рынка.
func liveAdmitCards(candidates []LiveTopic, rejected []LiveRejection) ([]LiveTopic, []LiveRejection) {
	kept := make([]LiveTopic, 0, len(candidates))
	for _, candidate := range candidates {
		market := liveMarketMaturity(candidate, liveAbsoluteBar())
		candidate.Market = market.field()
		candidate.Confidence = new(liveConfidence(candidate, liveAbsoluteBar()))
		evidence := liveTopicEvidence(candidate)
		switch {
		case market.formed:
			rejected = append(rejected, liveRejection(candidate, liveRejectMarketFormed, market.reason()))
		case market.mature:
			rejected = append(rejected, liveRejection(candidate, liveRejectMature, market.reason()))
		case liveNoiseOnly(evidence):
			rejected = append(rejected, liveRejection(candidate, liveRejectHype, liveHypeReason(len(evidence))))
		case !liveVerifiedMechanism(candidate):
			rejected = append(rejected, liveRejection(candidate, liveRejectUnqualified,
				"механизм технологии и прямые источники не прошли проверку"))
		case liveRussianText(candidate.Label) == "":
			rejected = append(rejected, liveRejection(candidate, liveRejectCard,
				"русское название технологии не сформировано"))
		case !liveCardAdmissible(candidate):
			reason := "нет проверенного описания и хотя бы одного из «преимущество / кейс» с цитатой"
			if candidate.Card != nil && len(candidate.Card.Missing) > 0 {
				reason += ": " + strings.Join(candidate.Card.Missing, ", ")
			}
			rejected = append(rejected, liveRejection(candidate, liveRejectCard, reason))
		default:
			kept = append(kept, candidate)
		}
	}
	return kept, rejected
}
