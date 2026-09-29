package worker

import (
	"bytes"
	"context"
	"encoding/hex"
	"encoding/json/v2"
	"io"
	"net/http"
	"os"
	"path"
	"slices"
	"strings"
	"testing"
	"time"
	"uuid"

	"github.com/aws/aws-sdk-go-v2/aws"
	awsconfig "github.com/aws/aws-sdk-go-v2/config"
	"github.com/aws/aws-sdk-go-v2/credentials"
	"github.com/aws/aws-sdk-go-v2/service/s3"
	"github.com/example/source-harvester/internal/platform/config"
	dbv1 "github.com/example/source-harvester/internal/platform/genproto/db/v1"
	"github.com/example/source-harvester/internal/platform/grpcx"
	"github.com/example/source-harvester/internal/platform/harvest"
	"github.com/example/source-harvester/internal/platform/sourcedoc"
	"github.com/example/source-harvester/internal/platform/storage"
	"github.com/example/source-harvester/internal/provider"
	"lukechampine.com/blake3"
)

// Очистка удаляет только проверенные ID манифеста и объекты с префиксом теста; строки PostgreSQL оператор удаляет сам.
func TestSourcePipelineLive(t *testing.T) {
	filename := os.Getenv("TR_SOURCE_PIPELINE_MANIFEST")
	if filename == "" {
		t.Skip("explicit pre-commit manifest required")
	}
	raw, readErr := os.ReadFile(filename)
	if readErr != nil {
		t.Fatal(readErr)
	}
	var m struct {
		Version int      `json:"version"`
		TestRun string   `json:"test_run_id"`
		Owner   string   `json:"owner_id"`
		Scope   string   `json:"scope_id"`
		Run     string   `json:"run_id"`
		IDs     []string `json:"ids"`
		Objects []string `json:"object_keys"`
	}
	if err := json.Unmarshal(raw, &m, json.RejectUnknownMembers(true)); err != nil {
		t.Fatal(err)
	}
	if m.Version != 1 || !strings.HasPrefix(m.TestRun, "tr-test-") || len(m.IDs) < 1 || len(m.IDs) > 50 || m.Owner == "" {
		t.Fatal("invalid verification manifest")
	}
	for _, id := range m.IDs {
		if _, err := uuid.Parse(id); err != nil {
			t.Fatal(err)
		}
	}
	ctx, cancel := context.WithTimeout(t.Context(), 2*time.Minute)
	defer cancel()
	conn, dialErr := grpcx.Dial(config.Get("DB_SERVICE_ADDR", "db-service:9091"))
	if dialErr != nil {
		t.Fatal(dialErr)
	}
	defer func() { _ = conn.Close() }()
	db := dbv1.NewDbServiceClient(conn)
	cfg := storage.ConfigFromEnv()
	store, storeErr := storage.New(ctx, cfg)
	if storeErr != nil {
		t.Fatal("open existing object store failed")
	}
	providers := map[string]bool{}
	allowedObjects := map[string]bool{}
	sourceHashes := map[string]string{}
	for _, id := range m.IDs {
		d, err := db.GetDocument(ctx, &dbv1.GetDocumentRequest{Id: id})
		if err != nil {
			t.Fatal(err)
		}
		s, sourceErr := sourcedoc.Decode([]byte(d.GetSourceMetadata()))
		if sourceErr != nil {
			t.Fatal(sourceErr)
		}
		if d.GetOwnerId() != m.Owner || s.TestRunID != m.TestRun || d.GetStatus() != "indexed" || d.GetChunkCount() < 1 ||
			!slices.Contains(m.Objects, d.GetObjectKey()) {
			t.Fatal("foreign, incomplete or unmanifested document")
		}
		providers[s.Provider] = true
		allowedObjects[d.GetObjectKey()] = true
		sourceHashes[id] = s.ContentHash
		body := readSourceObject(ctx, t, store, d.GetObjectKey())
		b3 := blake3.Sum256(body)
		if int64(len(body)) != d.GetSize() || sha(body) != s.ContentHash || hex.EncodeToString(b3[:]) != d.GetContentHash() {
			t.Fatal("original hash/size drift")
		}
		q, o := liveCounts(ctx, t, id, m.Owner)
		if q != int(d.GetChunkCount()) || o != q {
			t.Fatalf("partial dual write: db=%d qdrant=%d opensearch=%d", d.GetChunkCount(), q, o)
		}
		t.Logf(
			"document=%s provider=%s mime=%s bytes=%d chunks=%d sha256=%s revision=%s",
			id,
			s.Provider,
			d.GetMimeType(),
			len(body),
			q,
			s.ContentHash,
			s.Revision,
		)
	}

	// Сырой объект удаляется, только если неизменная сохранённая страница доказывает его принадлежность
	// проверенным тестовым документам: одного UUID прогона мало, чтобы удалить чужие сырые ответы.
	for _, key := range m.Objects {
		if !strings.HasSuffix(key, ".json") {
			continue
		}
		body := readSourceObject(ctx, t, store, key)
		var saved struct {
			Page     provider.Page      `json:"page"`
			Prepared []harvest.Prepared `json:"prepared"`
		}
		if err := json.Unmarshal(body, &saved, json.RejectUnknownMembers(true)); err != nil {
			t.Fatal(err)
		}
		prefix := "sources/" + saved.Page.Provider + "/" + m.Run + "/"
		if len(saved.Prepared) == 0 || !providers[saved.Page.Provider] || key != prefix+sha(body)+".json" {
			t.Fatal("unowned page manifest")
		}
		for _, r := range saved.Prepared {
			if r.Source.TestRunID != m.TestRun || sourceHashes[r.DocumentID] != r.Source.ContentHash || !allowedObjects[r.ObjectKey] {
				t.Fatal("raw page contains a foreign source")
			}
		}
		if sha(saved.Page.Raw) != saved.Page.RawHash {
			t.Fatal("raw response drift")
		}
		allowedObjects[key] = true
		allowedObjects[prefix+saved.Page.RawHash+".response"] = true
	}
	for _, key := range m.Objects {
		if !allowedObjects[key] || path.Clean(key) != key {
			t.Fatal("foreign or unsafe object in cleanup manifest")
		}
		body := readSourceObject(ctx, t, store, key)
		hash := strings.TrimSuffix(strings.TrimSuffix(path.Base(key), ".response"), ".json")
		if sha(body) != hash {
			t.Fatal("raw page/manifest hash mismatch")
		}
	}

	if os.Getenv("TR_SOURCE_CLEANUP") != "1" {
		return
	}
	for _, id := range m.IDs {
		filter := sourceFilter(id, m.Owner)
		var qresponse struct {
			Status string `json:"status"`
		}
		liveJSON(ctx, t,
			config.Get("QDRANT_URL", "http://qdrant:6333")+"/collections/documents/points/delete?wait=true",
			map[string]any{"filter": filter},
			&qresponse,
		)
		if qresponse.Status != "ok" {
			t.Fatal("Qdrant delete was not confirmed")
		}
		var osresponse struct {
			Failures []any `json:"failures"`
			TimedOut bool  `json:"timed_out"`
		}
		liveJSON(ctx, t,
			config.Get("OPENSEARCH_URL", "http://opensearch:9200")+"/chunks/_delete_by_query?refresh=true",
			sourceQuery(id, m.Owner),
			&osresponse,
		)
		if len(osresponse.Failures) > 0 || osresponse.TimedOut {
			t.Fatal("OpenSearch delete incomplete")
		}
		q, o := liveCounts(ctx, t, id, m.Owner)
		if q != 0 || o != 0 {
			t.Fatal("manifest IDs remain in index")
		}
	}
	awsCfg, awsErr := awsconfig.LoadDefaultConfig(
		ctx,
		awsconfig.WithRegion(cfg.Region),
		awsconfig.WithCredentialsProvider(
			credentials.NewStaticCredentialsProvider(cfg.AccessKey, cfg.SecretKey, ""),
		),
	)
	if awsErr != nil {
		t.Fatal("load object credentials failed")
	}
	api := s3.NewFromConfig(
		awsCfg,
		func(o *s3.Options) { o.BaseEndpoint = aws.String(cfg.Endpoint); o.UsePathStyle = true },
	)
	for _, key := range m.Objects {
		if _, err := api.DeleteObject(ctx, &s3.DeleteObjectInput{Bucket: aws.String(cfg.Bucket), Key: aws.String(key)}); err != nil {
			t.Fatal("manifest object cleanup failed")
		}
	}
	t.Log(
		"manifest objects removed; both indexes contain zero matching points; database cleanup remains explicit",
	)
}

func readSourceObject(ctx context.Context, t *testing.T, store *storage.Client, key string) []byte {
	t.Helper()
	r, err := store.Get(ctx, key)
	if err != nil {
		t.Fatal("saved source is missing")
	}
	defer func() { _ = r.Close() }()
	b, err := io.ReadAll(io.LimitReader(r, 20<<20+1))
	if err != nil || len(b) > 20<<20 {
		t.Fatal("source read failed or exceeded bound")
	}
	return b
}

func sourceFilter(id, owner string) map[string]any {
	return map[string]any{
		"must": []any{
			map[string]any{"key": "document_id", "match": map[string]any{"value": id}},
			map[string]any{"key": "owner_id", "match": map[string]any{"value": owner}},
		},
	}
}
func sourceQuery(id, owner string) map[string]any {
	return map[string]any{
		"query": map[string]any{
			"bool": map[string]any{
				"filter": []any{
					map[string]any{"term": map[string]any{"document_id": id}},
					map[string]any{"term": map[string]any{"owner_id": owner}},
				},
			},
		},
	}
}

func liveCounts(ctx context.Context, t *testing.T, id, owner string) (int, int) {
	t.Helper()
	var q struct {
		Result struct {
			Count int `json:"count"`
		} `json:"result"`
	}
	var o struct {
		Count int `json:"count"`
	}
	liveJSON(ctx, t,
		config.Get("QDRANT_URL", "http://qdrant:6333")+"/collections/documents/points/count",
		map[string]any{"filter": sourceFilter(id, owner), "exact": true},
		&q,
	)
	liveJSON(ctx, t,
		config.Get("OPENSEARCH_URL", "http://opensearch:9200")+"/chunks/_count",
		sourceQuery(id, owner),
		&o,
	)
	return q.Result.Count, o.Count
}

func liveJSON(ctx context.Context, t *testing.T, endpoint string, in, out any) {
	t.Helper()
	raw, err := json.Marshal(in)
	if err != nil {
		t.Fatal(err)
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, endpoint, bytes.NewReader(raw))
	if err != nil {
		t.Fatal(err)
	}
	req.Header.Set("Content-Type", "application/json")
	c := &http.Client{
		Timeout:       20 * time.Second,
		CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse },
	}
	res, err := c.Do(req)
	if err != nil {
		t.Fatal("index request failed")
	}
	defer func() { _ = res.Body.Close() }()
	if res.StatusCode != http.StatusOK {
		t.Fatalf("index HTTP %d", res.StatusCode)
	}
	b, err := io.ReadAll(io.LimitReader(res.Body, 1<<20))
	if err != nil {
		t.Fatal(err)
	}
	if err := json.Unmarshal(b, out); err != nil {
		t.Fatal(err)
	}
}
