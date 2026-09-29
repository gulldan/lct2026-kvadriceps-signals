package httpapi

import (
	"strings"
	"testing"
)

func TestHTMLSourceTextIgnoresActiveAndHiddenContent(t *testing.T) {
	input := `<html><head><title>Chrome</title></head><body><h1>Method</h1><p>Works &amp; measured.</p>` +
		`<script>invented score</script><style>secret</style><div hidden>hidden claim</div>` +
		`<div aria-hidden="true">hidden label</div><iframe src="http://localhost">untrusted</iframe></body></html>`
	got, err := htmlSourceText(strings.NewReader(input))
	if err != nil || got != "Method\n\nWorks & measured." {
		t.Fatalf("wrong inert text: %q %v", got, err)
	}
}
