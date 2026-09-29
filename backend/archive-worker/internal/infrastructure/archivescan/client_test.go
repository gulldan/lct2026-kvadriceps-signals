package archivescan

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"testing/synctest"
	"time"
)

const maxUploadFormMemory = 1 << 20

type stubStore struct{ body string }

func (s stubStore) Get(_ context.Context, _ string) (io.ReadCloser, error) {
	return io.NopCloser(strings.NewReader(s.body)), nil
}

func newServer(t *testing.T, respBody string) *httptest.Server {
	t.Helper()
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = io.WriteString(w, respBody)
	}))
}

func testLimits() Limits {
	return Limits{
		MaxFileBytes:   256 << 20,
		MaxTotalBytes:  2048 << 20,
		MaxRatio:       200,
		ExtractTimeout: 5 * time.Second,
	}
}

func TestExtract_SkipsBadURI(t *testing.T) {
	const body = `{
      "total_files": 2, "stored_files": 2, "stored_bytes": 20,
      "entries": [
        {"sanitized_path":"good.txt","row":{"mime":"text/plain","full_b3":"h1"},
         "stored_object":{"uri":"s3://bucket/extracted/good.txt","size_bytes":10}},
        {"sanitized_path":"bad.txt","row":{"mime":"text/plain","full_b3":"h2"},
         "stored_object":{"uri":"not-a-valid-uri","size_bytes":10}}
      ]
    }`
	srv := newServer(t, body)
	defer srv.Close()

	c := New(srv.URL, stubStore{body: "ZIPDATA"}, 5*time.Second, testLimits())
	res, err := c.Extract(context.Background(), "k", "a.zip", "ex-1")
	if err != nil {
		t.Fatalf("Extract returned error, want nil (one bad uri must be skipped): %v", err)
	}
	if len(res.Entries) != 1 {
		t.Fatalf("entries = %d, want 1", len(res.Entries))
	}
	if res.Entries[0].Path != "good.txt" || res.Entries[0].ObjectKey != "extracted/good.txt" {
		t.Errorf("unexpected entry: %+v", res.Entries[0])
	}
	if res.Skipped != 1 {
		t.Errorf("skipped = %d, want 1", res.Skipped)
	}
}

func TestExtract_AllBadURIsFail(t *testing.T) {
	const body = `{
      "total_files": 2, "stored_files": 2, "stored_bytes": 20,
      "entries": [
        {"sanitized_path":"a.txt","row":{"mime":"text/plain"},
         "stored_object":{"uri":"bogus-1","size_bytes":10}},
        {"sanitized_path":"b.txt","row":{"mime":"text/plain"},
         "stored_object":{"uri":"bogus-2","size_bytes":10}}
      ]
    }`
	srv := newServer(t, body)
	defer srv.Close()

	c := New(srv.URL, stubStore{body: "ZIPDATA"}, 5*time.Second, testLimits())
	if _, err := c.Extract(context.Background(), "k", "a.zip", "ex-1"); err == nil {
		t.Fatal("Extract returned nil, want error (all stored URIs unparseable)")
	}
}

func TestExtract_DirectoriesIgnored(t *testing.T) {
	const body = `{
      "total_files": 2, "stored_files": 1, "stored_bytes": 10,
      "entries": [
        {"sanitized_path":"dir/","row":{"mime":"inode/directory"},"stored_object":null},
        {"sanitized_path":"dir/f.txt","row":{"mime":"text/plain","full_b3":"h"},
         "stored_object":{"uri":"s3://bucket/extracted/dir/f.txt","size_bytes":10}}
      ]
    }`
	srv := newServer(t, body)
	defer srv.Close()

	c := New(srv.URL, stubStore{body: "ZIPDATA"}, 5*time.Second, testLimits())
	res, err := c.Extract(context.Background(), "k", "a.zip", "ex-1")
	if err != nil {
		t.Fatalf("Extract returned error: %v", err)
	}
	if len(res.Entries) != 1 {
		t.Fatalf("entries = %d, want 1", len(res.Entries))
	}
	if res.Skipped != 0 {
		t.Errorf("skipped = %d, want 0 (directory is not a skipped entry)", res.Skipped)
	}
}

func TestExtract_ForwardsLimitFields(t *testing.T) {
	got := make(chan map[string]string, 1)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if err := r.ParseMultipartForm(maxUploadFormMemory); err != nil {
			t.Errorf("parse multipart form: %v", err)
		}
		fields := map[string]string{}
		for k, v := range r.MultipartForm.Value {
			fields[k] = v[0]
		}
		got <- fields
		w.Header().Set("Content-Type", "application/json")
		_, _ = io.WriteString(w, `{"total_files":0,"stored_files":0,"stored_bytes":0,"entries":[]}`)
	}))
	defer srv.Close()

	limits := Limits{
		MaxFileBytes:   256 << 20,
		MaxTotalBytes:  2048 << 20,
		MaxRatio:       200,
		ExtractTimeout: 90 * time.Second,
	}
	c := New(srv.URL, stubStore{body: "ZIPDATA"}, 5*time.Second, limits)
	if _, err := c.Extract(context.Background(), "k", "a.zip", "ex-1"); err != nil {
		t.Fatalf("Extract returned error: %v", err)
	}

	fields := <-got
	want := map[string]string{
		"max_file_bytes":       "268435456",
		"max_total_bytes":      "2147483648",
		"max_ratio":            "200",
		"extract_timeout_secs": "90",
	}
	for k, v := range want {
		if fields[k] != v {
			t.Errorf("form field %q = %q, want %q", k, fields[k], v)
		}
	}
}

func TestExtract_TimeoutAbortsSlowScan(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		time.Sleep(200 * time.Millisecond)
		w.Header().Set("Content-Type", "application/json")
		_, _ = io.WriteString(w, `{"total_files":0,"stored_files":0,"stored_bytes":0,"entries":[]}`)
	}))
	defer srv.Close()

	limits := Limits{
		MaxFileBytes:   0,
		MaxTotalBytes:  0,
		MaxRatio:       0,
		ExtractTimeout: 20 * time.Millisecond,
	}
	// Таймаут HTTP с запасом: первым должен сработать дедлайн распаковки.
	c := New(srv.URL, stubStore{body: "ZIPDATA"}, 5*time.Second, limits)
	if _, err := c.Extract(context.Background(), "k", "a.zip", "ex-1"); err == nil {
		t.Fatal("Extract returned nil, want error (extract timeout must abort the slow scan)")
	}
}

func TestExtract_ReleasesUploadOnRequestFailure(t *testing.T) {
	for _, baseURL := range []string{"%", "unsupported://archive.test"} {
		t.Run(baseURL, func(t *testing.T) {
			// Проверка ждёт завершения всех горутин: зависшая запись в поток завершит её ошибкой.
			synctest.Test(t, func(t *testing.T) {
				client := New(baseURL, stubStore{body: "ZIPDATA"}, time.Second, testLimits())
				if _, err := client.Extract(t.Context(), "k", "a.zip", "ex-1"); err == nil {
					t.Fatal("invalid archive endpoint must fail")
				}
			})
		})
	}
}
