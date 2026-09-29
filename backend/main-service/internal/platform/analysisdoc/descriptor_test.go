package analysisdoc_test

import (
	"encoding/json/v2"
	"strings"
	"testing"

	"github.com/example/main-service/internal/platform/analysisdoc"
	"github.com/example/main-service/internal/platform/indexdoc"
)

func TestFrozenWireSizeDoesNotDependOnInputSize(t *testing.T) {
	frozen := analysisdoc.Frozen{RunID: "71c3d28e-1a3b-41ca-9bc2-03c39f3c3bed", ReaderID: "reader",
		JSON: strings.Repeat("x", analysisdoc.MaxBytes), Bytes: analysisdoc.MaxBytes}
	frozen.SHA256 = indexdoc.Hash([]byte(frozen.JSON))
	frozen.Artifact = analysisdoc.ArtifactKey(frozen.RunID, frozen.SHA256)
	if err := frozen.Validate(); err != nil {
		t.Fatal(err)
	}
	raw, err := json.Marshal(frozen)
	if err != nil || len(raw) > 1024 {
		t.Fatalf("oversized control-plane descriptor: %d %v", len(raw), err)
	}
	decoded, err := analysisdoc.Decode[analysisdoc.Frozen](string(raw))
	if err != nil || decoded.Validate() != nil || decoded.JSON != "" || decoded.Bytes != frozen.Bytes {
		t.Fatalf("invalid descriptor round trip: %+v %v", decoded, err)
	}
	frozen.Bytes++
	if frozen.Validate() == nil {
		t.Fatal("oversized input accepted")
	}
}
