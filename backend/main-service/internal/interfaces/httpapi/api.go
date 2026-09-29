package httpapi

import (
	"context"
	"errors"
	"log/slog"
	"net/http"
	"strconv"
	"strings"
	"sync"
	"time"

	"google.golang.org/genproto/googleapis/rpc/errdetails"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"

	"github.com/example/main-service/internal/application"
	"github.com/example/main-service/internal/platform/dbclient"
	"github.com/example/main-service/internal/platform/httpx"
	"github.com/example/main-service/internal/platform/jwt"
	"github.com/example/main-service/internal/platform/runtimecfg"
)

type metricsStore interface {
	EnqueueWork(ctx context.Context, key, request string) error
	Set(ctx context.Context, key, val string, ttl time.Duration) error
	Get(ctx context.Context, key string) (string, bool, error)
}

type stageRecorder interface {
	RecordStage(stage string, seconds float64)
}

const evalScorecardKey = "rag:eval:scorecard"

// Ключи-триггеры слушают фоновые воркеры: API только заказывает прогон, долгие задания моделей
// в пути запроса не выполняются.
const (
	evalTriggerKey = "rag:eval:trigger"
	itcTriggerKey  = "rag:itc:trigger"
)

type API struct {
	analyses    *application.AnalysisService
	trimmed     resultTrimCache
	ingestion   *application.IngestionService
	live        *application.TrendLiveService
	covers      *application.LiveCovers
	watches     *application.LiveWatches
	chat        *application.ChatService
	admin       *application.AdminService
	uploads     *application.UploadSessionService
	hypotheses  *application.HypothesisService
	hypJobs     *application.HypothesisJobService
	metrics     metricsStore
	usage       *application.LLMUsageService
	stages      stageRecorder
	maxUploadMB int64
	// sharedCorpus повторяет RAG_SHARED_CORPUS llm-service: база знаний общая на компанию,
	// поэтому documentChunks не проверяет владельца.
	sharedCorpus bool
	appSettings  *AppSettingsStore
	ovr          *runtimecfg.Overrides
	collections  application.Collections
	technologies application.Technologies
	review       ReviewQueue
	// ponytail: кэш в памяти одной реплики; вынести в Valkey, когда реплик станет больше.
	brief   application.TechnologyBriefWriter
	briefMu sync.Mutex
	briefs  map[string]application.TechnologyBrief

	signalBrief application.LiveSignalBriefWriter
}

func (a *API) SetAppSettingsStore(s *AppSettingsStore) { a.appSettings = s }

func (a *API) SetRuntimeOverrides(ovr *runtimecfg.Overrides) { a.ovr = ovr }

func New(
	ingestion *application.IngestionService,
	chat *application.ChatService,
	admin *application.AdminService,
	uploads *application.UploadSessionService,
	hypotheses *application.HypothesisService,
	hypJobs *application.HypothesisJobService,
	metrics metricsStore,
	usage *application.LLMUsageService,
	stages stageRecorder,
	maxUploadMB int64,
	sharedCorpus bool,
) *API {
	if maxUploadMB <= 0 {
		maxUploadMB = 50
	}
	return &API{
		ingestion:    ingestion,
		chat:         chat,
		admin:        admin,
		uploads:      uploads,
		hypotheses:   hypotheses,
		hypJobs:      hypJobs,
		metrics:      metrics,
		usage:        usage,
		stages:       stages,
		maxUploadMB:  maxUploadMB,
		sharedCorpus: sharedCorpus,
	}
}

// Routes регистрирует бизнес-ручки; health живёт на ops-порту, /ws монтируется отдельно со своей авторизацией.
func (a *API) Routes(mux *http.ServeMux) {
	a.trendRoutes(mux)
	mux.HandleFunc("POST /trends/analyses/{id}/candidates/{candidate}/description", a.analysisDescription)
	mux.HandleFunc("POST /documents", a.uploadDocument)
	mux.HandleFunc("GET /documents", a.listDocuments)
	mux.HandleFunc("GET /documents/{id}", a.getDocument)

	mux.HandleFunc("POST /chats", a.createChat)
	mux.HandleFunc("GET /chats", a.listChats)
	mux.HandleFunc("GET /chats/{id}", a.getChat)
	mux.HandleFunc("GET /chats/{id}/messages", a.listMessages)
	mux.HandleFunc("POST /chats/{id}/messages", a.postMessage)

	mux.HandleFunc("GET /documents/{id}/chunks", a.documentChunks)
	mux.HandleFunc("GET /documents/{id}/source", a.sourceEvidence)
	a.technologyRoutes(mux)
	mux.HandleFunc("POST /documents/{id}/source-access", a.sourceAccess)
	mux.HandleFunc("GET /documents/{id}/content", a.documentContent)
	mux.HandleFunc("GET /documents/{id}/source/content", a.sourceOriginal)

	// Части крупных файлов грузятся в S3 напрямую, минуя edge.
	mux.HandleFunc("POST /uploads", a.beginUpload)
	mux.HandleFunc("GET /uploads/{id}/parts", a.uploadPartURLs)
	mux.HandleFunc("POST /uploads/{id}/complete", a.completeUpload)
	mux.HandleFunc("DELETE /uploads/{id}", a.abortUpload)

	mux.HandleFunc("GET /models", a.listModels)

	mux.HandleFunc("GET /system/activity", a.systemActivity)
	mux.HandleFunc("POST /client-errors", a.clientError)

	// Роли проверяются внутри обработчиков (403 при несовпадении).
	mux.HandleFunc("GET /admin/users", a.adminUsers)
	mux.HandleFunc("GET /admin/documents", a.adminDocuments)
	mux.HandleFunc("GET /admin/documents/stats", a.adminDocumentsStats)
	mux.HandleFunc("POST /admin/documents/requeue", a.requeueDocuments)
	mux.HandleFunc("GET /admin/review-queue", a.readReviewQueue)

	mux.HandleFunc("POST /admin/metrics", a.postMetrics)
	mux.HandleFunc("GET /admin/metrics", a.getMetrics)
	mux.HandleFunc("GET /admin/llm-usage", a.getLLMUsage)
	mux.HandleFunc("GET /admin/perf", a.getPerf)
	mux.HandleFunc("GET /admin/openalex", a.getOpenAlexBudget)
	mux.HandleFunc("POST /admin/metrics/run", a.triggerMetricsRun)
	mux.HandleFunc("POST /admin/itc/run", a.triggerITCRun)

	mux.HandleFunc("GET /admin/settings", a.getAppSettings)
	mux.HandleFunc("PUT /admin/settings", a.putAppSettings)

	mux.HandleFunc("GET /hypotheses/board", a.hypothesisBoard)
	mux.HandleFunc("GET /hypotheses", a.listHypotheses)
	mux.HandleFunc("POST /hypotheses", a.createHypothesis)
	mux.HandleFunc("POST /hypotheses/generate", a.generateHypotheses)
	mux.HandleFunc("GET /hypotheses/graph", a.hypothesisGraph)
	mux.HandleFunc("GET /hypotheses/{id}", a.getHypothesis)
	mux.HandleFunc("POST /hypotheses/{id}/verify", a.verifyHypothesis)
	mux.HandleFunc("POST /hypotheses/{id}/enrich", a.enrichHypothesis)
	mux.HandleFunc("POST /hypotheses/{id}/assess-trl", a.assessHypothesisTRL)
	mux.HandleFunc("POST /hypotheses/{id}/competitors", a.analyzeCompetitors)
	mux.HandleFunc("POST /hypotheses/{id}/refine", a.refineHypothesis)
	mux.HandleFunc("POST /hypotheses/{id}/experiment", a.planExperiment)
	mux.HandleFunc("POST /hypotheses/{id}/tag", a.tagHypothesis)
	mux.HandleFunc("POST /hypotheses/{id}/itc", a.storeHypothesisITC)
	mux.HandleFunc("GET /trl/rubric", a.trlRubric)
	mux.HandleFunc("GET /itc/rubric", a.itcRubric)

	mux.HandleFunc("GET /hypotheses/scoring-weights", a.getScoringWeights)
	mux.HandleFunc("PUT /hypotheses/scoring-weights", a.updateScoringWeights)
	mux.HandleFunc("GET /hypotheses/runtime-settings", a.getHypothesisRuntimeSettings)
	mux.HandleFunc("PUT /hypotheses/runtime-settings", a.updateHypothesisRuntimeSettings)
	mux.HandleFunc("PUT /hypotheses/{id}", a.updateHypothesis)
	mux.HandleFunc("GET /hypotheses/{id}/revisions", a.listHypothesisRevisions)
	mux.HandleFunc("POST /hypotheses/{id}/revisions", a.addHypothesisRevision)
	mux.HandleFunc("GET /hypotheses/{id}/evidence", a.listHypothesisEvidence)

	mux.HandleFunc("POST /hypothesis-jobs", a.createHypothesisJob)
	mux.HandleFunc("GET /hypothesis-jobs", a.listHypothesisJobs)
	mux.HandleFunc("GET /hypothesis-jobs/{id}", a.getHypothesisJob)

	mux.HandleFunc("GET /kpis", a.listKPIs)
	mux.HandleFunc("POST /kpis", a.createKPI)
	mux.HandleFunc("POST /kpis/suggest", a.suggestKPIs)
	mux.HandleFunc("POST /kpis/parse", a.parseKPIPrompt)
	mux.HandleFunc("POST /kpis/{id}/graph-hypotheses", a.graphHypotheses)
	mux.HandleFunc("GET /kpis/{id}", a.getKPI)
	mux.HandleFunc("PUT /kpis/{id}", a.updateKPI)
	mux.HandleFunc("DELETE /kpis/{id}", a.deleteKPI)
	mux.HandleFunc("POST /kpis/{id}/documents", a.attachKPIDocuments)
	mux.HandleFunc("GET /kpis/{id}/documents", a.listKPIDocuments)
	mux.HandleFunc("DELETE /kpis/{id}/documents/{docId}", a.detachKPIDocument)

	mux.HandleFunc("GET /clusters", a.listClusters)
	mux.HandleFunc("GET /clusters/head", a.clusterHead)
	mux.HandleFunc("POST /clusters/{id}/itc", a.storeClusterITC)
	mux.HandleFunc("POST /clusters/replace", a.replaceClusters)
	mux.HandleFunc("POST /clusters", a.createCluster)
	mux.HandleFunc("DELETE /clusters", a.deleteClusters)
	mux.HandleFunc("GET /clusters/{id}", a.getCluster)
	mux.HandleFunc("PUT /clusters/{id}", a.updateCluster)
}

// fail отдаёт 404 и на ErrForbidden, чтобы не раскрывать существование чужих ресурсов; ResourceExhausted
// из очереди llm-service — 503, чтобы клиент повторил запрос, а не счёл его провалом.
func (a *API) fail(w http.ResponseWriter, err error) {
	if s, ok := status.FromError(err); ok && s.Code() == codes.ResourceExhausted {
		for _, detail := range s.Details() {
			if _, ok := detail.(*errdetails.RetryInfo); ok {
				w.Header().Set("Retry-After", "1")
				httpx.Error(w, http.StatusServiceUnavailable, "processing capacity reached; retry later")
				return
			}
		}
		httpx.Error(w, http.StatusTooManyRequests, "request exceeds available resources")
		return
	}
	switch {
	case status.Code(err) == codes.AlreadyExists, status.Code(err) == codes.Aborted:
		httpx.Error(w, http.StatusConflict, "resource changed; reload before retrying")
	case status.Code(err) == codes.Unavailable:
		httpx.Error(w, http.StatusServiceUnavailable, "service temporarily unavailable")
	case status.Code(err) == codes.DeadlineExceeded, errors.Is(err, context.DeadlineExceeded):
		httpx.Error(w, http.StatusGatewayTimeout, "upstream request timed out")
	case status.Code(err) == codes.Canceled, errors.Is(err, context.Canceled):
		httpx.Error(w, http.StatusRequestTimeout, "request canceled")
	case status.Code(err) == codes.Unauthenticated:
		httpx.Error(w, http.StatusUnauthorized, "authentication required")
	case status.Code(err) == codes.PermissionDenied, status.Code(err) == codes.NotFound:
		httpx.Error(w, http.StatusNotFound, "not found")
	case status.Code(err) == codes.FailedPrecondition:
		httpx.Error(w, http.StatusConflict, "request precondition failed")
	case status.Code(err) == codes.InvalidArgument:
		httpx.Error(w, http.StatusBadRequest, "invalid request")
	case errors.Is(err, application.ErrSessionNotFound):
		httpx.Error(w, http.StatusNotFound, "сеанс загрузки не найден или истёк")
	case errors.Is(err, application.ErrInvalidArgument):
		httpx.Error(w, http.StatusBadRequest, err.Error())
	case errors.Is(err, application.ErrInsufficientContext):
		httpx.Error(w, http.StatusUnprocessableEntity, "В источниках недостаточно данных для обогащения гипотезы")
	case errors.Is(err, application.ErrForbidden), errors.Is(err, application.ErrNotFound), errors.Is(err, dbclient.ErrNotFound):
		httpx.Error(w, http.StatusNotFound, "not found")
	default:
		slog.Error("API operation failed", "error", err)
		httpx.Error(w, http.StatusInternalServerError, "internal server error")
	}
}

func splitIDList(raw string) []string {
	var out []string
	for _, id := range strings.Split(raw, ",") {
		if id = strings.TrimSpace(id); id != "" {
			out = append(out, id)
		}
	}
	return out
}

func queryInt(r *http.Request, name string, def int) int {
	v := r.URL.Query().Get(name)
	if v == "" {
		return def
	}
	n, err := strconv.Atoi(v)
	if err != nil {
		return def
	}
	return n
}

func (a *API) requireRole(w http.ResponseWriter, r *http.Request, roles ...string) bool {
	claims, ok := jwt.ClaimsFromContext(r.Context())
	if !ok {
		httpx.Error(w, http.StatusUnauthorized, "missing claims")
		return false
	}
	if !hasAnyRole(claims, roles...) {
		httpx.Error(w, http.StatusForbidden, "insufficient role")
		return false
	}
	return true
}

// hypothesesPrivileged разрешает читать гипотезы всех владельцев (надзор админа); запись остаётся за владельцем.
func (a *API) hypothesesPrivileged(r *http.Request) bool {
	claims, ok := jwt.ClaimsFromContext(r.Context())
	return ok && hasAnyRole(claims, "admin")
}

func hasAnyRole(claims *jwt.Claims, roles ...string) bool {
	for _, have := range claims.Roles {
		for _, want := range roles {
			if have == want {
				return true
			}
		}
	}
	return false
}

func (a *API) trendRoutes(mux *http.ServeMux) {
	mux.HandleFunc("POST /trends/collections", a.requestCollection)
	mux.HandleFunc("GET /trends/collections/{id}", a.readCollection)
	mux.HandleFunc("POST /trends/analyses", a.submitAnalysis)
	mux.HandleFunc("GET /trends/study", a.trendStudy)
	mux.HandleFunc("GET /trends/live", a.trendLive)
	mux.HandleFunc("GET /trends/live/model", a.trendLiveModel)
	mux.HandleFunc("POST /trends/live/model/check", a.trendLiveModelCheck)
	mux.HandleFunc("GET /trends/live/model/check/{id}", a.trendLiveModelCheckResult)
	mux.HandleFunc("GET /trends/live/topics/{id}", a.trendLiveTopic)
	mux.HandleFunc("GET /trends/live/topics/{id}/cover", a.trendLiveCover)
	mux.HandleFunc("GET /trends/live/topics/{id}/cover/image", a.trendLiveCoverImage)
	mux.HandleFunc("GET /trends/live/papers/{id}/cover", a.trendLivePaperCover)
	mux.HandleFunc("GET /trends/live/papers/{id}/cover/image", a.trendLivePaperCoverImage)
	mux.HandleFunc("GET /trends/live/search", a.trendLiveSearch)
	mux.HandleFunc("GET /trends/live/search/top", a.trendLiveSearchTop)
	mux.HandleFunc("GET /trends/live/searches", a.trendLiveSearches)
	mux.HandleFunc("POST /trends/live/executive", a.trendLiveExecutive)
	mux.HandleFunc("GET /trends/live/watches", a.liveWatchList)
	mux.HandleFunc("POST /trends/live/watches", a.liveWatchCreate)
	mux.HandleFunc("DELETE /trends/live/watches", a.liveWatchDelete)
	mux.HandleFunc("POST /trends/live/watches/ack", a.liveWatchAck)
	mux.HandleFunc("POST /trends/live/signal-brief", a.trendLiveSignalBrief)
	mux.HandleFunc("POST /trends/live/affinity", a.trendLiveAffinity)
	mux.HandleFunc("POST /trends/live/bets/check", a.trendLiveBetsCheck)
	mux.HandleFunc("GET /trends/analyses/{id}", a.readAnalysis)
	mux.HandleFunc("GET /trends/analyses/{id}/method", a.readAnalysisMethod)
	mux.HandleFunc("GET /trends/analyses/{id}/candidates/{candidate}", a.readAnalysisCandidate)
	mux.HandleFunc("GET /trends/analyses/{id}/topics/{topic}/metrics", a.topicMetrics)
	mux.HandleFunc("GET /trends/analyses/{id}/export", a.exportAnalysis)
	mux.HandleFunc("GET /trends/analyses/{id}/candidates/{candidate}/description", a.analysisDescription)
}

func ownerFromContext(r *http.Request) (string, bool) {
	claims, ok := jwt.ClaimsFromContext(r.Context())
	if !ok || claims.UserID == "" {
		return "", false
	}
	return claims.UserID, true
}
