package httpapi

import (
	"encoding/json"
	"fmt"
	"net/http"
	"regexp"
	"strings"
)

// Служебный id модели с суффиксом «abliterated» перед банком читается как модель со снятыми ограничениями.
// Имя подменяется на выходе API, а не при записи: в базе уже сотни ответов с сырым id. Журналы и настройки
// админа видят сырой id — в настройках его вводят. Имя встаёт после «моделью», поэтому начинается с названия.
const (
	modelDisplayKey     = "LLM_MODEL_DISPLAY_NAME"
	modelDisplayDefault = "GLM 5.3 Flash (локальная)"
)

// modelNamedPath не включает потоковый каталог технологий: имени модели в нём нет, а копить его в памяти дорого.
func modelNamedPath(path string) bool {
	switch {
	case strings.HasPrefix(path, "/technologies/"):
		return strings.HasSuffix(path, "/brief")
	case strings.HasPrefix(path, "/trends/"), strings.HasPrefix(path, "/hypotheses"),
		strings.HasPrefix(path, "/hypothesis-jobs"), strings.HasPrefix(path, "/kpis"),
		strings.HasPrefix(path, "/chats"), path == "/admin/llm-usage":
		return true
	}
	return false
}

// displayModelName меняет только JSON-строку целиком: тот же id внутри текста документа — содержание,
// а не подпись.
func displayModelName(body []byte, id, name string) []byte {
	if id == "" || name == "" || id == name {
		return body
	}
	to, err := json.Marshal(name)
	if err != nil {
		return body
	}
	// Префикс провайдера меняется от настройки к настройке, имя модели — нет.
	base := id[strings.LastIndex(id, "/")+1:]
	pattern := regexp.MustCompile(`"(?:[A-Za-z0-9_.-]+/)*` + regexp.QuoteMeta(base) + `"`)
	return pattern.ReplaceAllLiteral(body, to)
}

// ModelDisplayNames подменяет имя модели в JSON-ответах; картинки, CSV и потоки проходят как есть.
func (a *API) ModelDisplayNames(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		id := strings.TrimSpace(a.ovr.Get(r.Context(), "VLLM_MODEL", ""))
		if id == "" || !modelNamedPath(r.URL.Path) {
			next.ServeHTTP(w, r)
			return
		}
		ids := append([]string{id}, strings.Split(a.ovr.Get(r.Context(), "LLM_MODEL_DISPLAY_ALIASES", ""), ",")...)
		named := &modelNameWriter{ResponseWriter: w, ids: ids,
			name: strings.TrimSpace(a.ovr.Get(r.Context(), modelDisplayKey, modelDisplayDefault))}
		next.ServeHTTP(named, r)
		named.finish()
	})
}

// modelNameWriter копит JSON-тело целиком: кодировщик пишет большой ответ
// кусками, и id мог бы разрезаться на границе двух записей.
type modelNameWriter struct {
	http.ResponseWriter
	ids       []string
	name      string
	status    int
	body      []byte
	buffering bool
	decided   bool
}

func (w *modelNameWriter) decide() {
	if w.decided {
		return
	}
	w.decided = true
	w.buffering = strings.HasPrefix(w.Header().Get("Content-Type"), "application/json")
}

func (w *modelNameWriter) WriteHeader(status int) {
	w.decide()
	if !w.buffering {
		w.ResponseWriter.WriteHeader(status)
		return
	}
	w.status = status
}

func (w *modelNameWriter) Write(p []byte) (int, error) {
	w.decide()
	if w.buffering {
		w.body = append(w.body, p...)
		return len(p), nil
	}
	n, err := w.ResponseWriter.Write(p)
	if err != nil {
		return n, fmt.Errorf("write response: %w", err)
	}
	return n, nil
}

func (w *modelNameWriter) Unwrap() http.ResponseWriter { return w.ResponseWriter }

func (w *modelNameWriter) finish() {
	if !w.buffering {
		return
	}
	// Длину тела выставил обработчик по сырому id; новое имя другой длины.
	w.Header().Del("Content-Length")
	if w.status != 0 {
		w.ResponseWriter.WriteHeader(w.status)
	}
	if len(w.body) > 0 {
		body := w.body
		for _, id := range w.ids {
			body = displayModelName(body, strings.TrimSpace(id), w.name)
		}
		_, _ = w.ResponseWriter.Write(body)
	}
}
