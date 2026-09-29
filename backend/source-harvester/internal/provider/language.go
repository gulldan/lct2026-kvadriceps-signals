package provider

import "strings"

// normalizeProviderLanguage возвращает пустое для всего, что не сводится к двум буквам: решает детектор.
func normalizeProviderLanguage(value string) string {
	value = strings.ToLower(strings.TrimSpace(value))
	value, _, _ = strings.Cut(value, "-")
	value, _, _ = strings.Cut(value, "_")
	if len(value) == 3 {
		value = map[string]string{"eng": "en", "rus": "ru", "deu": "de", "ger": "de", "fra": "fr",
			"fre": "fr", "spa": "es", "zho": "zh", "chi": "zh", "jpn": "ja", "kor": "ko"}[value]
	}
	if len(value) != 2 {
		return ""
	}
	for _, r := range value {
		if r < 'a' || r > 'z' {
			return ""
		}
	}
	return value
}
