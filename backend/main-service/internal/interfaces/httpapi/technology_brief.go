package httpapi

import (
	"cmp"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"log/slog"
	"net/http"
	"slices"
	"strings"

	"github.com/example/main-service/internal/application"
	"github.com/example/main-service/internal/platform/extractdoc"
	"github.com/example/main-service/internal/platform/httpx"
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

func (a *API) SetTechnologyBrief(write application.TechnologyBriefWriter) {
	a.brief, a.briefs = write, map[string]application.TechnologyBrief{}
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
	// Ключ — технология плюс отпечаток её упоминаний: две проекции одной
	// технологии не делят бриф, а новое упоминание делает старый бриф чужим.
	key := briefKey(technology)
	w.Header().Set("Cache-Control", "no-store")
	if r.Method == http.MethodGet {
		a.briefMu.Lock()
		brief, cached := a.briefs[key]
		a.briefMu.Unlock()
		if !cached {
			httpx.Error(w, http.StatusNotFound, "brief not written yet")
			return
		}
		httpx.JSON(w, http.StatusOK, brief)
		return
	}
	// Карточка просит бриф сама при открытии, поэтому уход со страницы не отменяет вызов модели;
	// срок ограничивает писатель.
	// ponytail: одновременные открытия несчитанной технологии дают по вызову модели на каждое — общей
	// записи «уже считаю» нет; если станет заметно, хватит канала ожидания в briefs.
	brief, err := a.brief(context.WithoutCancel(r.Context()), briefInput(technology))
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
	httpx.JSON(w, http.StatusOK, brief)
}

func briefKey(technology extractdoc.Technology) string {
	ids := make([]string, 0, len(technology.Mentions))
	for _, mention := range technology.Mentions {
		ids = append(ids, mention.MentionID)
	}
	slices.Sort(ids)
	sum := sha256.Sum256([]byte(strings.Join(ids, "\n")))
	return technology.TechnologyID + ":" + hex.EncodeToString(sum[:])
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
