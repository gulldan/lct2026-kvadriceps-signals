package worker

import (
	"strings"

	"github.com/abadojack/whatlanggo"
	"github.com/example/source-harvester/internal/platform/harvest"
)

// detectorVersion привязывает решение к детектору: новый детектор даёт новое решение, а не переписывает старое.
const detectorVersion = "whatlang-v1.0.1"

// minLanguageRunes низок, потому что детектор сам отказывается от нечитаемого; порог лишь не даёт решать по названию.
const minLanguageRunes = 40

// documentLanguage решает по всему тексту, чтобы переведённый реферат не перемаркировал документ.
func documentLanguage(text string) (code, basis string) {
	if len([]rune(text)) < minLanguageRunes {
		return "", ""
	}
	whole := detectLanguage(text)
	if whole == "" {
		return "", ""
	}
	return whole, detectorVersion
}

func detectLanguage(text string) string {
	info := whatlanggo.Detect(text)
	if !info.IsReliable() {
		return ""
	}
	code := strings.ToLower(whatlanggo.LangToStringShort(info.Lang))
	if len(code) != 2 {
		return ""
	}
	return code
}

func applyLanguage(prepared *harvest.Prepared) {
	if prepared.Source.Language == "" {
		prepared.Source.Language, prepared.Source.LanguageBasis =
			documentLanguage(prepared.Text)
	}
	if prepared.Source.Language == "" {
		// Неизвестный язык — не чужой: материал идёт в извлечение, а причина пишется, чтобы пробел был виден.
		prepared.Limitations = append(prepared.Limitations,
			"language_undetermined; too little text to decide, so the material is examined anyway")
	}
}
