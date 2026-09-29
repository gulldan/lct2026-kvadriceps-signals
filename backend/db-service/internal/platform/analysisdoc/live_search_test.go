package analysisdoc

import (
	"encoding/json/jsontext"
	"encoding/json/v2"
	"testing"
)

// Пустое условие означает первую вставку: его исчезновение на проводе включило бы безусловную запись.
func TestLiveSearchWireCompatibility(t *testing.T) {
	for _, tc := range []struct {
		name  string
		match *string
	}{
		{name: "legacy"}, {name: "absent", match: new("")}, {name: "version", match: new("v1")},
	} {
		t.Run("save-"+tc.name, func(t *testing.T) {
			raw, err := json.Marshal(LiveSearchSave{Query: "query", IfMatch: tc.match})
			if err != nil {
				t.Fatal(err)
			}
			decoded, err := Decode[LiveSearchSave](string(raw))
			if err != nil || (decoded.IfMatch == nil) != (tc.match == nil) {
				t.Fatalf("save precondition changed in transport: %s %v", raw, err)
			}
			if tc.match != nil && *decoded.IfMatch != *tc.match {
				t.Fatalf("save precondition lost its value: %s", raw)
			}
			var fields map[string]jsontext.Value
			if err = json.Unmarshal(raw, &fields); err != nil {
				t.Fatal(err)
			}
			if _, present := fields["if_match"]; present != (tc.match != nil) {
				t.Fatalf("legacy request acquired a new field, or conditional request lost it: %s", raw)
			}
		})
	}
	for _, tc := range []struct {
		name        string
		withVersion bool
		version     string
	}{
		{name: "legacy"}, {name: "conditional", withVersion: true, version: "v1"},
	} {
		t.Run("read-"+tc.name, func(t *testing.T) {
			raw, err := json.Marshal(LiveSearchRead{Query: "query", WithVersion: tc.withVersion})
			if err != nil {
				t.Fatal(err)
			}
			request, err := Decode[LiveSearchRead](string(raw))
			if err != nil || request.WithVersion != tc.withVersion {
				t.Fatalf("read opt-in changed in transport: %s %v", raw, err)
			}
			_, legacyErr := Decode[struct {
				Query string `json:"query"`
			}](string(raw))
			if (legacyErr != nil) != tc.withVersion {
				t.Fatalf("legacy read compatibility changed: %s %v", raw, legacyErr)
			}
			raw, err = json.Marshal(LiveSearchStored{Found: true, Version: tc.version})
			if err != nil {
				t.Fatal(err)
			}
			response, err := Decode[LiveSearchStored](string(raw))
			if err != nil || response.Version != tc.version {
				t.Fatalf("response version changed in transport: %s %v", raw, err)
			}
			// Старый строгий декодер знает обязательные поля прежнего ответа, но не версию.
			_, legacyErr = Decode[struct {
				Found   bool `json:"found"`
				Fresh   bool `json:"fresh"`
				Papers  int  `json:"papers"`
				Sources int  `json:"sources"`
			}](string(raw))
			if (legacyErr != nil) != tc.withVersion {
				t.Fatalf("legacy response compatibility changed: %s %v", raw, legacyErr)
			}
		})
	}
}
