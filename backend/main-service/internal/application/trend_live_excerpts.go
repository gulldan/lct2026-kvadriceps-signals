package application

import (
	"strings"
	"unicode"
)

type liveIndexedExcerpt struct {
	Index int    `json:"index"`
	Text  string `json:"text"`
}

// Явные номера не заставляют модель самостоятельно пересчитывать длинный массив.
func liveIndexedSourceExcerpts(text string) []liveIndexedExcerpt {
	parts := liveSourceExcerpts(text)
	excerpts := make([]liveIndexedExcerpt, len(parts))
	for i, part := range parts {
		excerpts[i] = liveIndexedExcerpt{Index: i + 1, Text: part}
	}
	return excerpts
}

// Фрагменты вместе дают исходный текст: сервер копирует выбранный моделью диапазон дословно.
func liveSourceExcerpts(text string) []string {
	excerpts := []string{}
	start := 0
	var previous rune
	for i, current := range text {
		if unicode.IsSpace(current) && strings.ContainsRune(".!?。！？", previous) {
			excerpts = append(excerpts, text[start:i])
			start = i
		}
		previous = current
	}
	if start < len(text) {
		excerpts = append(excerpts, text[start:])
	}
	return excerpts
}
