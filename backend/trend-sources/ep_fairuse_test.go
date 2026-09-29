package main

import (
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

type ledgerTransport func(*http.Request) (*http.Response, error)

// Снятый с сервера документ (404) — оговорка о нём, а не остановка недели: следующий документ читается.
func TestEPMissingDocumentDoesNotStopTheWeek(t *testing.T) {
	t.Parallel()
	document, err := os.ReadFile("testdata/ep-1004359-b1.xml")
	if err != nil {
		t.Fatal(err)
	}
	s := &service{kind: kindPatents, dir: t.TempDir(), config: defaultConfig()}
	s.http = &http.Client{Transport: ledgerTransport(func(req *http.Request) (*http.Response, error) {
		status, body := http.StatusOK, string(document)
		if strings.Contains(req.URL.Path, "EP0000001") {
			status, body = http.StatusNotFound, ""
		}
		return &http.Response{StatusCode: status, Header: http.Header{}, Body: io.NopCloser(strings.NewReader(body))}, nil
	})}
	data := &snapshot{ObservedAt: "2026-09-13T00:00:00Z"}
	c := &coverage{Source: epSource}
	if !s.epDocument(t.Context(), data, c, "EP0000001NWB1") || !s.epDocument(t.Context(), data, c, "EP1004359NWB1") ||
		len(data.Records) != 1 || c.Cursor != "" || len(c.Limitations) != 1 {
		t.Fatalf("a missing EP document stopped the sweep: records=%d coverage=%+v", len(data.Records), c)
	}
}

func (f ledgerTransport) RoundTrip(r *http.Request) (*http.Response, error) { return f(r) }

func TestEPInvalidLedgerRefusesBeforeRequest(t *testing.T) {
	for _, raw := range []string{`{`, `{}`, `{"days":{"2026-09-13":-1}}`, `{"days":{"not-a-date":5}}`} {
		s := &service{dir: t.TempDir(), config: defaultConfig()}
		if err := os.WriteFile(s.epLedgerPath(), []byte(raw), 0600); err != nil {
			t.Fatal(err)
		}
		s.http = &http.Client{Transport: ledgerTransport(func(*http.Request) (*http.Response, error) {
			t.Fatal("unreadable consumption history allowed a provider request")
			return nil, os.ErrPermission
		})}
		if _, err := s.fetch(t.Context(), epBase+"/publication-dates", &snapshot{}); err == nil {
			t.Fatal("invalid ledger was treated as zero consumption")
		}
	}
}

func TestEPCountsBytesBeforeStorageAndHTTPFailures(t *testing.T) {
	t.Parallel()
	for _, status := range []int{http.StatusOK, http.StatusServiceUnavailable} {
		s := &service{dir: t.TempDir(), config: defaultConfig()}
		// Сырое хранилище сломано, а учёт расхода по-прежнему пишется.
		if err := os.WriteFile(filepath.Join(s.dir, "raw"), []byte("blocked"), 0600); err != nil {
			t.Fatal(err)
		}
		s.http = &http.Client{Transport: ledgerTransport(func(*http.Request) (*http.Response, error) {
			return &http.Response{StatusCode: status, Body: io.NopCloser(strings.NewReader("received bytes")), Header: http.Header{}}, nil
		})}
		if _, err := s.fetch(t.Context(), epBase+"/publication-dates", &snapshot{}); err == nil {
			t.Fatal("fixture must fail after receiving the response")
		}
		ledger, err := s.epLedgerRead()
		if err != nil || ledger.total() != int64(len("received bytes")) {
			t.Fatalf("failed response was not charged: %+v %v", ledger, err)
		}
	}
}
