package searchstore

import (
	"crypto/sha256"
	"encoding/hex"
)

// TechnologyIndex отделён от индекса чанков: смешение сломало бы разом поиск, подсчёт и фильтр доступа.
func TechnologyIndex(version string) string {
	sum := sha256.Sum256([]byte(version))
	return "technologies-" + hex.EncodeToString(sum[:])
}

// TechnologyAccessQuery допускает технологию, открытую всем ИЛИ принадлежащую читателю: И молча скрыло бы
// чужие. Строится здесь, потому что фильтр, который вызывающий может забыть, — не правило доступа.
func TechnologyAccessQuery(reader string, sharedCorpus bool) map[string]any {
	scopes := []any{"public"}
	if sharedCorpus {
		scopes = append(scopes, "shared")
	}
	should := []any{map[string]any{osTerms: map[string]any{"access_scope": scopes}}}
	if reader != "" {
		should = append(should, map[string]any{osTerm: map[string]any{osOwnerID: reader}})
	}
	return map[string]any{osBool: map[string]any{"should": should, "minimum_should_match": 1}}
}

// TechnologyNarrowing только сужает ответ и никогда не расширяет допущенное запросом доступа.
func TechnologyNarrowing(languages, roles, stages []string) []any {
	out := []any{}
	for field, values := range map[string][]string{
		osLanguage: languages, "role": roles, "stage": stages,
	} {
		if len(values) == 0 {
			continue
		}
		terms := make([]any, 0, len(values))
		for _, value := range values {
			terms = append(terms, value)
		}
		out = append(out, map[string]any{osTerms: map[string]any{field: terms}})
	}
	return out
}
