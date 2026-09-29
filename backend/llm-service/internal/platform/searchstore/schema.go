package searchstore

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"reflect"
)

func indexDefinition() map[string]any {
	return map[string]any{
		"settings": map[string]any{
			osIndex: map[string]any{"refresh_interval": refreshInterval},
			// Стеммеры сводят словоформы («балансир» и «балансира»), которые пропускает стандартный анализатор;
			// фильтры каждого языка не трогают чужую письменность. Анализ статичен: старый индекс нужно переиндексировать.
			"analysis": map[string]any{
				osFilter: map[string]any{
					"ru_stop":    map[string]any{osType: "stop", "stopwords": "_russian_"},
					"ru_stemmer": map[string]any{osType: "stemmer", osLanguage: "russian"},
					"en_stop":    map[string]any{osType: "stop", "stopwords": "_english_"},
					"en_stemmer": map[string]any{osType: "stemmer", osLanguage: "english"},
				},
				osAnalyzer: map[string]any{
					// cjk_bigram: китайский ищется биграммами, не-CJK токены проходят
					// фильтр нетронутыми (ru/en стемминг сохраняется).
					"ru_en": map[string]any{
						osType:      "custom",
						"tokenizer": "standard",
						osFilter: []any{
							"lowercase", "cjk_width", "cjk_bigram",
							"en_stop", "en_stemmer", "ru_stop", "ru_stemmer",
						},
					},
					// Без стемминга и стоп-слов: формулы (LiFePO4), единицы (MPa, S/cm) и марки сплавов остаются целыми.
					osExact: map[string]any{
						osType:      "custom",
						"tokenizer": "whitespace",
						osFilter:    []any{"lowercase"},
					},
				},
			},
		},
		"mappings": map[string]any{
			"properties": map[string]any{
				osText: map[string]any{
					osType: osText, osAnalyzer: "ru_en",
					"fields": map[string]any{osExact: map[string]any{osType: osText, osAnalyzer: osExact}},
				},
				osDocumentID: map[string]any{osType: osKeyword},
				osOwnerID:    map[string]any{osType: osKeyword},
				"filename":   map[string]any{osType: osKeyword},
			},
		},
	}
}

func (o *OpenSearch) validateSchema(ctx context.Context) error {
	var mappings map[string]struct {
		Mappings map[string]any `json:"mappings"`
	}
	if err := o.do(ctx, http.MethodGet, "/"+o.index+"/_mapping", nil, &mappings); err != nil {
		return err
	}
	expected := indexDefinition()
	if len(mappings) == 0 {
		return fmt.Errorf("opensearch index %s has no mapping", o.index)
	}
	for name, actual := range mappings {
		if !schemaIncludes(actual.Mappings, expected["mappings"]) {
			return fmt.Errorf("opensearch index %s has incompatible mappings; create a versioned index and reindex", name)
		}
	}
	var settings map[string]struct {
		Settings map[string]map[string]any `json:"settings"`
	}
	if err := o.do(ctx, http.MethodGet, "/"+o.index+"/_settings", nil, &settings); err != nil {
		return err
	}
	expectedSettings, ok := expected["settings"].(map[string]any)
	if !ok {
		return errors.New("invalid expected search settings")
	}
	wanted := expectedSettings["analysis"]
	for name := range mappings {
		if !schemaIncludes(settings[name].Settings["index"]["analysis"], wanted) {
			return fmt.Errorf("opensearch index %s has incompatible analysis; create a versioned index and reindex", name)
		}
	}
	return nil
}

func schemaIncludes(actual, expected any) bool {
	if fields, ok := expected.(map[string]any); ok {
		values, ok := actual.(map[string]any)
		if !ok {
			return false
		}
		for key, value := range fields {
			if !schemaIncludes(values[key], value) {
				return false
			}
		}
		return true
	}
	return reflect.DeepEqual(actual, expected)
}
