package httpapi_test

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/example/main-service/internal/application"
	"github.com/example/main-service/internal/interfaces/httpapi"
	"github.com/example/main-service/internal/platform/jwt"
)

func jsonMarshal(v any) ([]byte, error)   { return json.Marshal(v) }
func jsonUnmarshal(b []byte, v any) error { return json.Unmarshal(b, v) }

const jwtSecret = "test-secret-32-bytes-long-padding!"

func newManager() *jwt.Manager { return jwt.NewManager(jwtSecret, "rag-platform", time.Hour) }

type harness struct {
	api          *httpapi.API
	server       *httptest.Server
	mgr          *jwt.Manager
	cat          *fakeCatalog
	docs         *fakeDocs
	chats        *fakeChats
	users        *fakeUsers
	store        *fakeObjectStore
	pub          *fakePublisher
	answerer     *scriptedAnswerer
	metrics      *fakeMetrics
	weights      *fakeWeights
	settings     *fakeRuntimeSettings
	graph        *fakeGraph
	jobStore     *fakeJobStore
	sharedCorpus bool
	nilMetrics   bool
	nilJobs      bool
}

type option func(*harness)

func withSharedCorpus(on bool) option { return func(h *harness) { h.sharedCorpus = on } }

func withoutMetrics() option { return func(h *harness) { h.nilMetrics = true } }

func withoutJobs() option { return func(h *harness) { h.nilJobs = true } }

func newHarness(t *testing.T, opts ...option) *harness {
	t.Helper()
	h := &harness{
		cat:      newCatalog(),
		docs:     newDocs(),
		chats:    newChats(),
		users:    &fakeUsers{},
		store:    newObjectStore(),
		pub:      &fakePublisher{},
		answerer: newAnswerer(),
		metrics:  newMetrics(),
		weights:  newWeights(),
		settings: newRuntimeSettings(),
		graph:    newGraph(),
		jobStore: newJobStore(),
	}
	for _, opt := range opts {
		opt(h)
	}
	ingestion := application.NewIngestionService(h.store, h.docs, &fakeChunks{})
	ingestion.EnableSourceReads(nil, h.sharedCorpus, false)
	chat := application.NewChatService(h.chats, h.answerer)
	admin := application.NewAdminService(h.users)
	uploads := application.NewUploadSessionService(&fakeMultipart{}, h.store, newSessions(), ingestion, 0)
	hyp := application.NewHypothesisService(h.cat, h.answerer, h.weights, h.settings, nil, h.graph, &fakeChunks{})
	jobs := application.NewHypothesisJobService(hyp, h.jobStore)
	if h.nilJobs {
		jobs = nil
	}
	var metrics *fakeMetrics
	if !h.nilMetrics {
		metrics = h.metrics
	}

	h.mgr = newManager()
	// Пустое хранилище метрик передаётся настоящим nil-интерфейсом, а не типизированным nil.
	if metrics == nil {
		h.api = httpapi.New(ingestion, chat, admin, uploads, hyp, jobs, nil, nil, nil, 50, h.sharedCorpus)
	} else {
		h.api = httpapi.New(ingestion, chat, admin, uploads, hyp, jobs, metrics, nil, nil, 50, h.sharedCorpus)
	}

	mux := http.NewServeMux()
	h.api.Routes(mux)
	h.server = httptest.NewServer(h.mgr.Middleware(mux))
	t.Cleanup(h.server.Close)
	return h
}

func (h *harness) token(t *testing.T, userID string, roles ...string) string {
	t.Helper()
	tok, _, err := h.mgr.Issue(userID, userID, roles)
	if err != nil {
		t.Fatalf("issue token: %v", err)
	}
	return tok
}

type resp struct {
	code   int
	body   string
	header http.Header
}

func (h *harness) do(t *testing.T, method, path, token, body string) resp {
	t.Helper()
	var rdr io.Reader
	if body != "" {
		rdr = strings.NewReader(body)
	}
	req, err := http.NewRequestWithContext(context.Background(), method, h.server.URL+path, rdr)
	if err != nil {
		t.Fatalf("new request: %v", err)
	}
	if token != "" {
		req.Header.Set("Authorization", "Bearer "+token)
	}
	if body != "" {
		req.Header.Set("Content-Type", "application/json")
	}
	return h.run(t, req)
}

func (h *harness) run(t *testing.T, req *http.Request) resp {
	t.Helper()
	out, err := h.server.Client().Do(req)
	if err != nil {
		t.Fatalf("do request: %v", err)
	}
	defer func() { _ = out.Body.Close() }()
	b, rerr := io.ReadAll(out.Body)
	if rerr != nil {
		t.Fatalf("read body: %v", rerr)
	}
	return resp{code: out.StatusCode, body: string(b), header: out.Header}
}

// doMultipart с пустым field не добавляет часть с файлом.
func (h *harness) doMultipart(t *testing.T, token, field, filename, content string, headers ...map[string]string) resp {
	t.Helper()
	var buf bytes.Buffer
	mw := multipart.NewWriter(&buf)
	if field != "" {
		fw, err := mw.CreateFormFile(field, filename)
		if err != nil {
			t.Fatalf("create form file: %v", err)
		}
		if _, werr := fw.Write([]byte(content)); werr != nil {
			t.Fatalf("write form file: %v", werr)
		}
	} else {
		_ = mw.WriteField("other", "x")
	}
	_ = mw.Close()

	req, err := http.NewRequestWithContext(context.Background(), http.MethodPost, h.server.URL+"/documents", &buf)
	if err != nil {
		t.Fatalf("new request: %v", err)
	}
	req.Header.Set("Authorization", "Bearer "+token)
	req.Header.Set("Content-Type", mw.FormDataContentType())
	for _, values := range headers {
		for key, value := range values {
			req.Header.Set(key, value)
		}
	}
	return h.run(t, req)
}

func extractJSONString(t *testing.T, body, key string) string {
	t.Helper()
	var m map[string]any
	if err := json.Unmarshal([]byte(body), &m); err != nil {
		t.Fatalf("decode body %q: %v", body, err)
	}
	s, ok := m[key].(string)
	if !ok {
		t.Fatalf("field %q missing or not a string in %s", key, body)
	}
	return s
}

type fakeMetrics struct {
	mu   sync.Mutex
	data map[string]string
	err  error
}

func newMetrics() *fakeMetrics { return &fakeMetrics{data: map[string]string{}} }

func (f *fakeMetrics) Set(_ context.Context, key, val string, _ time.Duration) error {
	if f.err != nil {
		return f.err
	}
	f.mu.Lock()
	f.data[key] = val
	f.mu.Unlock()
	return nil
}

func (f *fakeMetrics) Get(_ context.Context, key string) (string, bool, error) {
	if f.err != nil {
		return "", false, f.err
	}
	f.mu.Lock()
	v, ok := f.data[key]
	f.mu.Unlock()
	return v, ok, nil
}

func (f *fakeMetrics) EnqueueWork(ctx context.Context, key, request string) error {
	return f.Set(ctx, key, request, 0)
}
