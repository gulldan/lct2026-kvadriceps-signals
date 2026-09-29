package provider

import (
	"errors"
	"strconv"
	"testing"
)

func TestCrossrefCursorOverlapAndStall(t *testing.T) {
	var progress PageProgress
	var last Page
	for i, id := range []string{"a", "a", "b", "b", "b", "b", "c"} {
		raw := []byte(crPage(strconv.Itoa(i+1), crItem(id, "[[2025]]")))
		page, err := decodeCrossref(raw, testObserved(), testScope(), strconv.Itoa(i))
		if err != nil {
			t.Fatal(err)
		}
		page.Exhausted, page.Next = false, strconv.Itoa(i+1)
		fresh, duplicates, err := progress.Observe(&page)
		if err != nil {
			t.Fatal("valid advancing overlap stopped collection", err)
		}
		if i == 0 || i == 2 || i == 6 {
			if fresh != 1 || duplicates != 0 {
				t.Fatal("new work was hidden")
			}
		} else if fresh != 0 || duplicates != 1 || len(page.Limitations) == 0 {
			t.Fatal("overlap was counted as new or complete")
		}
		last = page
	}
	if _, _, err := progress.Observe(&last); !errors.Is(err, ErrCursorExpired) {
		t.Fatal("identical transport replay was accepted", err)
	}
	last.RawHash, last.Next = digest([]byte("stalled transport page")), last.Cursor
	if _, _, err := progress.Observe(&last); !errors.Is(err, ErrCursorExpired) {
		t.Fatal("stalled cursor was accepted", err)
	}
}
