package application

import (
	"errors"
	"strings"
)

var errNoJSONObject = errors.New("no JSON object")

// ErrUnparsableModelOutput отделяет ответ не по формату от недоступной модели: в задаче пользователь
// должен увидеть, что повтор может помочь, а не общий сбой.
var ErrUnparsableModelOutput = errors.New("unparsable model output")

// decodeJSONObject при двух неудачах возвращает исходную ошибку разбора, а не ошибку после починки.
func decodeJSONObject(answer string, dest any, unmarshal func([]byte, any) error) error {
	_, body, found := strings.Cut(answer, "{")
	if !found {
		return errNoJSONObject
	}
	body, _, found = strings.CutLast(body, "}")
	if !found {
		return errNoJSONObject
	}
	raw := "{" + body + "}"
	if err := unmarshal([]byte(raw), dest); err != nil {
		if repairErr := unmarshal([]byte(repairJSONBackslashes(raw)), dest); repairErr != nil {
			return err
		}
	}
	return nil
}

// repairJSONBackslashes: модель пишет LaTeX (\ce{…}, \frac) в строках JSON, забывая экранировать слеш.
// Только запасной путь после строгого разбора; побайтовый обход безопасен — 0x5C не встречается внутри UTF-8.
func repairJSONBackslashes(s string) string {
	if !strings.ContainsRune(s, '\\') {
		return s
	}
	var b strings.Builder
	b.Grow(len(s) + 16)
	for i := 0; i < len(s); i++ {
		c := s[i]
		if c != '\\' {
			b.WriteByte(c)
			continue
		}
		if i+1 < len(s) && isJSONEscapeByte(s[i+1]) {
			b.WriteByte(c)
			b.WriteByte(s[i+1])
			i++
			continue
		}
		b.WriteString(`\\`)
	}
	return b.String()
}

func isJSONEscapeByte(c byte) bool {
	switch c {
	case '"', '\\', '/', 'b', 'f', 'n', 'r', 't', 'u':
		return true
	default:
		return false
	}
}
