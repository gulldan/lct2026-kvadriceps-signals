package trendsemantic

import (
	"encoding/json/v2"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestEncoderBatchesAndOrdersModelRows(t *testing.T) {
	sizes := []int{}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var body struct {
			Input []string `json:"input"`
		}
		if err := json.UnmarshalRead(r.Body, &body); err != nil {
			t.Error(err)
			return
		}
		sizes = append(sizes, len(body.Input))
		rows := []map[string]any{}
		for i := len(body.Input) - 1; i >= 0; i-- {
			rows = append(rows, map[string]any{"index": i, "embedding": []float64{float64(i + 1), 1}})
		}
		_ = json.MarshalWrite(w, map[string]any{"model": "local", "revision": "v1", "data": rows})
	}))
	defer server.Close()
	result, err := New(server.URL, "local").Encode(t.Context(), []string{"a", "b", "c", "d", "e"})
	if err != nil || len(result.Vectors) != 5 || result.Vectors[0][0] != 1 || result.Vectors[3][0] != 4 ||
		len(sizes) != 2 || sizes[0] != 4 || sizes[1] != 1 {
		t.Fatalf("bad batches: %+v %v %v", result, sizes, err)
	}
}

func TestEncoderRejectsUnversionedOrDuplicateRows(t *testing.T) {
	for _, body := range []string{
		`{"model":"other","revision":"v1","data":[{"index":0,"embedding":[1]}]}`,
		`{"model":"local","revision":"","data":[{"index":0,"embedding":[1]}]}`,
		`{"model":"local","revision":"v1","data":[{"index":1,"embedding":[1]}]}`,
	} {
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) { fmt.Fprint(w, body) }))
		_, err := New(server.URL, "local").Encode(t.Context(), []string{"a"})
		server.Close()
		if err == nil {
			t.Fatalf("accepted invalid response: %s", body)
		}
	}
}
