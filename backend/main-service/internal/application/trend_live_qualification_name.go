package application

import (
	"errors"
	"strings"
	"unicode/utf8"
)

// Необязательное имя уточняет сбор признаков, но его ошибка не отменяет проверку самого механизма.
// В независимую проверку и классификатор попадает только буквальное имя из выбранных источников.
func liveLiteralQualificationName(qualification LiveQualification, documents []LiveQualificationDocument) string {
	if liveValidateQualificationName(qualification, documents) != nil {
		return ""
	}
	return strings.Join(strings.Fields(qualification.CanonicalName), " ")
}

// Исходное имя связывает признаки с термином статьи: слитное имя с сокращением может не встречаться в тексте.
// Проверка буквальности не заменяет независимую проверку тождества имени и механизма.
func liveValidateQualificationName(qualification LiveQualification, documents []LiveQualificationDocument) error {
	name := strings.Join(strings.Fields(qualification.CanonicalName), " ")
	if name == "" {
		return nil
	}
	words := liveWords(name)
	if len(words) < 2 || len(words) > 12 || utf8.RuneCountInString(name) > 160 {
		return errors.New("qualification: canonical name is not a bounded technical phrase")
	}
	selected := make(map[string]bool, len(qualification.Evidence))
	for _, evidence := range qualification.Evidence {
		selected[evidence.SourceID] = true
	}
	phrase := " " + strings.Join(words, " ") + " "
	for _, document := range documents {
		if !selected[document.ID] {
			continue
		}
		text := strings.Join(strings.Fields(document.Text), " ")
		if strings.Contains(text, name) && strings.Contains(" "+strings.Join(liveWords(text), " ")+" ", phrase) {
			return nil
		}
	}
	return errors.New("qualification: canonical name is absent from selected sources")
}

func liveClassifierCanonicalPhrase(candidate LiveTopic, documents map[string]StudyPaper,
	metadata map[string]LivePaperMetadata,
) string {
	qualification := candidate.Qualification
	if qualification == nil || !qualification.Verified || qualification.CanonicalName == "" {
		return ""
	}
	selected := make([]LiveQualificationDocument, 0, len(qualification.Evidence))
	for _, evidence := range qualification.Evidence {
		if paper, ok := documents[evidence.SourceID]; ok {
			selected = append(selected, LiveQualificationDocument{ID: paper.ID, Text: liveWorkLine(paper, metadata[paper.ID])})
		}
	}
	if liveValidateQualificationName(*qualification, selected) != nil {
		return ""
	}
	return strings.Join(liveWords(qualification.CanonicalName), " ")
}
