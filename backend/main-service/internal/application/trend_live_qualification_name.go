package application

import (
	"errors"
	"slices"
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
	if !liveTechnicalPhrase(name) {
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

// Целое предложение буквально есть в статье, но поиск его как названия создаёт ложную нехватку работ.
func liveTechnicalPhrase(name string) bool {
	words := liveWords(name)
	return len(words) >= 2 && len(words) <= 12 && utf8.RuneCountInString(name) <= 160 &&
		!slices.ContainsFunc(words, func(word string) bool {
			return slices.Contains([]string{"is", "are", "was", "were", "we", "our", "demonstrates", "achieves"}, word)
		})
}

// Подтверждённое имя уже сверено с полными текстами при допуске; в сохранённом ответе аннотаций нет, и сверка
// по одним заголовкам подменяла бы при дооценке фразу исходного поиска словами из ID кандидата.
func liveClassifierCanonicalPhrase(candidate LiveTopic) string {
	qualification := candidate.Qualification
	if qualification == nil || !qualification.Verified || !liveTechnicalPhrase(qualification.CanonicalName) {
		return ""
	}
	return strings.Join(liveWords(qualification.CanonicalName), " ")
}
