package httpapi

import (
	"bytes"
	"cmp"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json/v2"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net/http"

	"github.com/example/main-service/internal/application"
	"github.com/example/main-service/internal/domain"
	"github.com/example/main-service/internal/platform/extractdoc"
	"github.com/example/main-service/internal/platform/httpx"
	"github.com/example/main-service/internal/platform/storage"
)

func (a *API) technologyRoutes(mux *http.ServeMux) {
	mux.HandleFunc("GET /documents/{id}/technologies", a.documentTechnologies)
	mux.HandleFunc("GET /technologies", a.searchTechnologies)
	mux.HandleFunc("GET /technologies/export", a.exportTechnologies)
	mux.HandleFunc("GET /technologies/{id}", a.readTechnology)
	mux.HandleFunc("POST /technologies/{id}/reviews", a.submitReview)
	mux.HandleFunc("GET /technologies/{id}/reviews", a.readReviews)
	mux.HandleFunc("GET /technologies/{id}/brief", a.technologyBrief)
	mux.HandleFunc("POST /technologies/{id}/brief", a.technologyBrief)
}

// SetTechnologyBrief принимает хранилище объектов: бриф пишется моделью около 40 с, и без хранилища
// каждая выкладка заставляла первого читателя карточки ждать его заново.
func (a *API) SetTechnologyBrief(write application.TechnologyBriefWriter, store domain.ObjectStore) {
	a.brief, a.briefs, a.briefStore = write, map[string]application.TechnologyBrief{}, store
}

// technologyBrief читает технологию в проекции читателя: бриф по недоступным ему источникам утёк бы
// через кэш.
func (a *API) technologyBrief(w http.ResponseWriter, r *http.Request) {
	reader, ok := ownerFromContext(r)
	if !ok {
		httpx.Error(w, http.StatusUnauthorized, "missing claims")
		return
	}
	if a.technologies == nil || a.brief == nil {
		httpx.Error(w, http.StatusServiceUnavailable, "technology brief unavailable")
		return
	}
	technology, err := a.technologies.ReadTechnology(r.Context(), extractdoc.ReadTechnology{
		ReaderID: reader, TechnologyID: r.PathValue("id"), SharedCorpus: a.sharedCorpus,
	})
	if err != nil {
		a.reviewError(w, err)
		return
	}
	in := briefInput(technology)
	key, err := briefKey(in)
	if err != nil {
		httpx.Error(w, http.StatusInternalServerError, "technology brief key")
		return
	}
	w.Header().Set("Cache-Control", "no-store")
	if r.Method == http.MethodGet {
		brief, cached := a.storedBrief(r.Context(), key)
		if !cached {
			// «Ещё не написан» — обычное состояние карточки, как отсутствие обложки: 404 оставлял
			// ошибку в консоли на каждом открытии.
			w.WriteHeader(http.StatusNoContent)
			return
		}
		httpx.JSON(w, http.StatusOK, brief)
		return
	}
	// Карточка просит бриф сама при открытии, поэтому уход со страницы не отменяет вызов модели;
	// срок ограничивает писатель.
	// ponytail: одновременные открытия несчитанной технологии дают по вызову модели на каждое — общей
	// записи «уже считаю» нет; если станет заметно, хватит канала ожидания в briefs.
	brief, err := a.brief(context.WithoutCancel(r.Context()), in)
	switch {
	case errors.Is(err, application.ErrLLMNotConfigured):
		httpx.Error(w, http.StatusServiceUnavailable, "llm gateway not configured")
		return
	case err != nil:
		slog.Warn("technology brief failed", "technology", technology.TechnologyID, "error", err)
		httpx.Error(w, http.StatusBadGateway, "technology brief failed")
		return
	}
	a.briefMu.Lock()
	a.briefs[key] = brief
	a.briefMu.Unlock()
	if raw, merr := json.Marshal(brief); merr == nil && a.briefStore != nil {
		if perr := a.briefStore.Put(context.WithoutCancel(r.Context()), key, bytes.NewReader(raw), int64(len(raw)),
			"application/json"); perr != nil {
			slog.Warn("technology brief store", "technology", technology.TechnologyID, "error", perr)
		}
	}
	httpx.JSON(w, http.StatusOK, brief)
}

// storedBrief читает бриф сначала из памяти, затем из хранилища: после выкладки память пуста.
func (a *API) storedBrief(ctx context.Context, key string) (application.TechnologyBrief, bool) {
	a.briefMu.Lock()
	brief, cached := a.briefs[key]
	a.briefMu.Unlock()
	if cached || a.briefStore == nil {
		return brief, cached
	}
	body, err := a.briefStore.Get(ctx, key)
	if err != nil {
		if !errors.Is(err, storage.ErrNotFound) {
			slog.Warn("technology brief read", "key", key, "error", err)
		}
		return brief, false
	}
	defer func() { _ = body.Close() }()
	raw, err := io.ReadAll(io.LimitReader(body, 1<<20))
	if err != nil || json.Unmarshal(raw, &brief) != nil || brief.Hypothesis == "" {
		return application.TechnologyBrief{}, false
	}
	a.briefMu.Lock()
	a.briefs[key] = brief
	a.briefMu.Unlock()
	return brief, true
}

// technologyBriefContract поднимается, когда меняется запрос к модели: иначе из хранилища вернётся бриф
// по прежнему запросу.
const technologyBriefContract = "brief-v1"

// briefKey — отпечаток всего, что видит модель, вместе с id технологии: две проекции одной технологии
// не делят бриф, а новое упоминание или правка описания делают старый бриф чужим.
func briefKey(in application.TechnologyBriefInput) (string, error) {
	raw, err := json.Marshal(in, json.Deterministic(true))
	if err != nil {
		return "", fmt.Errorf("encode technology brief input: %w", err)
	}
	sum := sha256.Sum256(raw)
	return "technologies/briefs/" + technologyBriefContract + "/" + hex.EncodeToString(sum[:]) + ".json", nil
}

// briefInput собирает упоминания по ревизиям источника: карточка перечисляет
// документы, а не упоминания, и модель должна считать источники так же.
func briefInput(technology extractdoc.Technology) application.TechnologyBriefInput {
	in := application.TechnologyBriefInput{TechnologyID: technology.TechnologyID, Name: technology.CanonicalName,
		Description: technology.Description, Aliases: make([]string, 0, len(technology.Aliases)),
		Sources: []application.TechnologyBriefSource{}}
	for _, alias := range technology.Aliases {
		in.Aliases = append(in.Aliases, alias.Value)
	}
	index := map[string]int{}
	for _, mention := range technology.Mentions {
		i, seen := index[mention.SourceRevisionID]
		if !seen {
			i = len(in.Sources)
			index[mention.SourceRevisionID] = i
			in.Sources = append(in.Sources, application.TechnologyBriefSource{
				Title: mention.SourceTitle, Date: mention.SourcePublishedAt})
		}
		source := &in.Sources[i]
		source.Problem = cmp.Or(source.Problem, mention.Problem)
		source.Mechanism = cmp.Or(source.Mechanism, mention.Mechanism)
		source.Inputs = append(source.Inputs, mention.Inputs...)
		source.Outputs = append(source.Outputs, mention.Outputs...)
		for _, quote := range mention.Evidence {
			source.Quotes = append(source.Quotes, quote.Quote)
		}
	}
	return in
}
