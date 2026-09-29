package storage

import (
	"strings"
	"testing"
)

func TestObjectReadEnforcesDeclaredAndStreamingBudgets(t *testing.T) {
	for _, tc := range []struct {
		name, body string
		size       int64
		valid      bool
	}{
		{"exact", "1234", 4, true}, {"empty", "", 0, true},
		{"declared oversize", "12345", 5, false}, {"truncated", "123", 4, false},
		{"false size", "1234", 2, false}, {"unknown exact", "1234", -1, true},
		{"unknown oversize", "12345", -1, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			r := strings.NewReader(tc.body)
			raw, err := readObjectBytes(r, tc.size, 4)
			if (err == nil) != tc.valid || (tc.valid && string(raw) != tc.body) {
				t.Fatalf("bounded read: %q %v", raw, err)
			}
			if tc.size > 4 && r.Len() != len(tc.body) {
				t.Fatal("oversized declared object was read")
			}
		})
	}
}
