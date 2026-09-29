package httpapi

import (
	"cmp"
	"encoding/json/jsontext"
	"encoding/json/v2"
	"errors"
	"fmt"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
	"io"
	"log/slog"
	"net/http"
	"strings"
	"sync"
	"time"

	"github.com/example/main-service/internal/application"
	"github.com/example/main-service/internal/platform/analysisdoc"
	"github.com/example/main-service/internal/platform/httpx"
)

func (a *API) SetAnalyses(s *application.AnalysisService) { a.analyses = s }

type analysisJobView struct {
	ID           string                  `json:"id"`
	InputRunID   string                  `json:"input_run_id"`
	InputSHA256  string                  `json:"input_sha256"`
	MethodSHA256 string                  `json:"method_sha256"`
	Status       string                  `json:"status"`
	Attempt      int                     `json:"attempt"`
	FailureCode  string                  `json:"failure_code"`
	CreatedAt    time.Time               `json:"created_at"`
	UpdatedAt    time.Time               `json:"updated_at"`
	Active       bool                    `json:"active"`
	ResultSHA256 string                  `json:"result_sha256"`
	Result       jsontext.Value          `json:"result"`
	Method       analysisdoc.Method      `json:"method"`
	Sources      []analysisdoc.SourceRef `json:"sources"`
	Request      *analysisRequestView    `json:"request"`
}

const (
	fieldAnalysisID = "analysis_id"
	fieldMethodHash = "method_sha256"
)

type analysisRequestView struct {
	RequestID string `json:"request_id"`
	Query     string `json:"query"`
	Start     string `json:"start"`
	Cutoff    string `json:"cutoff"`
	// Mode принимает и прежние имена режимов истории, сопоставляя их с текущими.
	Mode string `json:"mode"`
	// AnalysisKind выбирает контракт результата; без него клиент получает прежний бриф.
	AnalysisKind       string   `json:"analysis_kind,omitempty"`
	SearchTerms        []string `json:"search_terms,omitempty"`
	CollectionScopeIDs []string `json:"collection_scope_ids,omitempty"`
}

// Страница не читает списки id по периодам: это провенанс выгрузки, занимавший бо́льшую часть
// ответа в 8 МБ. Обрезанная копия кэшируется на опубликованный результат — он неизменен.
const trimmedResultsKept = 8

type resultTrimCache struct {
	mu    sync.Mutex
	order []string
	byKey map[string]jsontext.Value
}

func (c *resultTrimCache) get(key string) (jsontext.Value, bool) {
	c.mu.Lock()
	defer c.mu.Unlock()
	cached, ok := c.byKey[key]
	return cached, ok
}

func (c *resultTrimCache) put(key string, value jsontext.Value) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.byKey == nil {
		c.byKey = map[string]jsontext.Value{}
	}
	if len(c.order) >= trimmedResultsKept {
		delete(c.byKey, c.order[0])
		c.order = c.order[1:]
	}
	c.order = append(c.order, key)
	c.byKey[key] = value
}

func trimmedResult(job analysisdoc.Job, trimmed *resultTrimCache) (jsontext.Value, error) {
	key := job.ID + ":" + job.ResultSHA256
	if cached, ok := trimmed.get(key); ok {
		return cached, nil
	}
	var doc map[string]any
	if err := json.Unmarshal([]byte(job.ResultJSON), &doc); err != nil {
		return nil, fmt.Errorf("decode analysis result: %w", err)
	}
	candidates, _ := doc["candidates"].([]any)
	for _, candidate := range candidates {
		measurements, _ := candidate.(map[string]any)["measurements"].(map[string]any)
		series, _ := measurements["series"].([]any)
		for _, period := range series {
			if fields, ok := period.(map[string]any); ok {
				delete(fields, "missing_scientific_scope_ids")
				delete(fields, "unscoped_scientific_work_ids")
			}
		}
	}
	raw, err := json.Marshal(doc)
	if err != nil {
		return nil, fmt.Errorf("encode analysis result: %w", err)
	}
	trimmed.put(key, jsontext.Value(raw))
	return jsontext.Value(raw), nil
}

func jobView(job analysisdoc.Job, trimmed *resultTrimCache) (analysisJobView, error) {
	method, err := analysisdoc.ParseMethod(job.MethodJSON)
	if err != nil {
		return analysisJobView{}, err
	}
	var result jsontext.Value
	if job.Status == analysisdoc.Published {
		result, err = trimmedResult(job, trimmed)
		if err != nil {
			return analysisJobView{}, err
		}
	}
	var request *analysisRequestView
	if job.Request != nil {
		request = &analysisRequestView{RequestID: job.Request.RequestID, Query: job.Request.Query,
			Start: job.Request.Start, Cutoff: job.Request.Cutoff,
			Mode:               analysisdoc.NormalizeMode(job.Request.Mode),
			AnalysisKind:       cmp.Or(job.Request.AnalysisKind, analysisdoc.KindBriefV1),
			CollectionScopeIDs: job.Request.CollectionScopeIDs, SearchTerms: job.Request.SearchTerms}
	}
	return analysisJobView{ID: job.ID, InputRunID: job.InputRunID, InputSHA256: job.InputSHA256,
		MethodSHA256: job.MethodSHA256, Status: job.Status, Attempt: job.Attempt, FailureCode: job.FailureCode,
		CreatedAt: job.CreatedAt, UpdatedAt: job.UpdatedAt, Active: job.Active, ResultSHA256: job.ResultSHA256,
		Result: result, Method: method, Sources: job.Sources, Request: request}, nil
}

func (a *API) submitAnalysis(w http.ResponseWriter, r *http.Request) {
	owner, ok := ownerFromContext(r)
	if !ok {
		httpx.Error(w, http.StatusUnauthorized, "missing claims")
		return
	}
	if a.analyses == nil {
		httpx.Error(w, http.StatusServiceUnavailable, "analysis unavailable")
		return
	}
	var body analysisRequestView
	raw, err := io.ReadAll(http.MaxBytesReader(w, r.Body, 4096))
	if err != nil || json.Unmarshal(raw, &body, json.RejectUnknownMembers(true)) != nil {
		httpx.Error(w, http.StatusBadRequest, "invalid analysis request")
		return
	}
	request, err := (analysisdoc.Request{OwnerID: owner, RequestID: body.RequestID, Query: body.Query,
		Start: body.Start, Cutoff: body.Cutoff, Mode: body.Mode, AnalysisKind: body.AnalysisKind,
		SharedCorpus: a.sharedCorpus, CollectionScopeIDs: body.CollectionScopeIDs}).Normalize(time.Now())
	if err != nil {
		httpx.Error(w, http.StatusBadRequest, "invalid query, period, history mode, analysis kind or request_id")
		return
	}
	job, err := a.analyses.Submit(r.Context(), request)
	if err != nil {
		a.analysisError(w, err)
		return
	}
	view, err := jobView(job, &a.trimmed)
	if err != nil {
		a.analysisError(w, err)
		return
	}
	w.Header().Set("Cache-Control", "no-store")
	w.Header().Set("Location", "/api/v1/trends/analyses/"+job.ID)
	code := http.StatusAccepted
	if job.Status == analysisdoc.Published {
		code = http.StatusOK
	}
	analysisJSON(w, code, view)
}

func (a *API) readAnalysis(w http.ResponseWriter, r *http.Request) {
	job, ok := a.loadAnalysis(w, r)
	if !ok {
		return
	}
	view, err := jobView(job, &a.trimmed)
	if err != nil {
		a.analysisError(w, err)
		return
	}
	analysisJSON(w, http.StatusOK, view)
}

func (a *API) readAnalysisMethod(w http.ResponseWriter, r *http.Request) {
	job, ok := a.loadAnalysis(w, r)
	if !ok {
		return
	}
	view, err := jobView(job, &a.trimmed)
	if err != nil {
		a.analysisError(w, err)
		return
	}
	analysisJSON(w, http.StatusOK, map[string]any{fieldAnalysisID: job.ID, "sha256": job.MethodSHA256, "method": view.Method})
}

func (a *API) readAnalysisCandidate(w http.ResponseWriter, r *http.Request) {
	job, ok := a.loadAnalysis(w, r)
	if !ok {
		return
	}
	if job.Status != analysisdoc.Published {
		httpx.Error(w, http.StatusConflict, "analysis not published")
		return
	}
	result, err := analysisdoc.Decode[analysisdoc.Result](job.ResultJSON)
	if err != nil {
		a.analysisError(w, err)
		return
	}
	for _, candidate := range result.Candidates {
		if candidate.ID == r.PathValue("candidate") {
			analysisJSON(w, http.StatusOK, map[string]any{fieldAnalysisID: job.ID, "input_run_id": job.InputRunID,
				fieldMethodHash: job.MethodSHA256, "candidate": candidate})
			return
		}
	}
	httpx.Error(w, http.StatusNotFound, "candidate unavailable")
}

func (a *API) loadAnalysis(w http.ResponseWriter, r *http.Request) (analysisdoc.Job, bool) {
	owner, ok := ownerFromContext(r)
	if !ok {
		httpx.Error(w, http.StatusUnauthorized, "missing claims")
		return analysisdoc.Job{}, false
	}
	if a.analyses == nil {
		httpx.Error(w, http.StatusServiceUnavailable, "analysis unavailable")
		return analysisdoc.Job{}, false
	}
	job, err := a.analyses.Read(r.Context(), analysisdoc.JobRead{JobID: r.PathValue("id"),
		ReaderID: owner, SharedCorpus: a.sharedCorpus})
	if err != nil {
		a.analysisError(w, err)
		return analysisdoc.Job{}, false
	}
	w.Header().Set("Cache-Control", "no-store")
	return job, true
}

func analysisJSON(w http.ResponseWriter, code int, value any) {
	// Битый символ в строке заменяется, а не роняет ответ целиком: 503 на каждый опрос
	// заставлял интерфейс запускать тот же пятиминутный поиск заново.
	raw, err := json.Marshal(value, jsontext.EscapeForHTML(true), jsontext.AllowInvalidUTF8(true))
	if err != nil {
		slog.Warn("analysis response encode", "error", err)
		httpx.Error(w, http.StatusServiceUnavailable, "analysis response unavailable")
		return
	}
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.Header().Set("X-Content-Type-Options", "nosniff")
	w.WriteHeader(code)
	_, _ = w.Write(raw) //nolint:gosec // G705: JSON-v2 encoded bytes, HTML escaped, application/json with nosniff.
}

// cachedAnalysisJSON отдаёт то же тело, что analysisJSON, но с валидатором и 304. Deterministic
// обязателен: без него порядок ключей карт плавает и валидатор не совпадёт сам с собой.
func cachedAnalysisJSON(w http.ResponseWriter, r *http.Request, value any) {
	raw, err := json.Marshal(value, jsontext.EscapeForHTML(true), json.Deterministic(true),
		jsontext.AllowInvalidUTF8(true))
	if err != nil {
		slog.Warn("analysis response encode", "error", err)
		httpx.Error(w, http.StatusServiceUnavailable, "analysis response unavailable")
		return
	}
	serveCachedJSON(w, r, raw)
}

func (a *API) analysisError(w http.ResponseWriter, err error) {
	w.Header().Set("Cache-Control", "no-store")
	switch {
	case errors.Is(err, analysisdoc.ErrCapacity), status.Code(err) == codes.ResourceExhausted:
		w.Header().Set("Retry-After", "10")
		httpx.Error(w, http.StatusTooManyRequests, "analysis queue full; retry the same request")
	case errors.Is(err, analysisdoc.ErrHistoryFull), status.Code(err) == codes.FailedPrecondition:
		httpx.Error(w, http.StatusInsufficientStorage, "analysis history full; operator action required")
	case errors.Is(err, analysisdoc.ErrInvalid), status.Code(err) == codes.InvalidArgument:
		httpx.Error(w, http.StatusBadRequest, analysisInvalidMessage(err))
	case errors.Is(err, analysisdoc.ErrAccessChanged), status.Code(err) == codes.PermissionDenied:
		// Источники анализа больше не доступны: отдать старый результат значило бы опубликовать то,
		// что владелец источника отозвал.
		httpx.Error(w, http.StatusConflict, "access_changed")
	case errors.Is(err, analysisdoc.ErrNotFound), status.Code(err) == codes.NotFound:
		httpx.Error(w, http.StatusNotFound, "analysis unavailable")
	case errors.Is(err, analysisdoc.ErrConflict), status.Code(err) == codes.Aborted:
		httpx.Error(w, http.StatusConflict, "request_id already binds a different input")
	default:
		httpx.Error(w, http.StatusServiceUnavailable, "analysis data temporarily unavailable")
	}
}

// analysisInvalidMessage пропускает клиенту только понятные ему причины; остальное — «invalid analysis»,
// чтобы не утекали детали бэкенда.
func analysisInvalidMessage(err error) string {
	msg := status.Convert(err).Message()
	switch {
	case strings.Contains(msg, "coverage limit"):
		return "analysis coverage limit exceeded; narrow the scope"
	case strings.Contains(msg, "exceeds"), strings.Contains(msg, "oversized"):
		return "analysis input too large; narrow the scope"
	case strings.Contains(msg, "embedding models"):
		return "sources indexed with different embedding models; narrow the scope"
	}
	return "invalid analysis"
}
