package provider

import (
	"encoding/json/jsontext"
	"encoding/json/v2"
	"fmt"
	"time"

	"github.com/example/source-harvester/internal/platform/sourcedoc"
)

// collectorRevision версионирует исправленные факты источника независимо от байтов файла: время, провенанс
// ответа и обновление одного разборщика ключ не меняют; свидетельство хранит неизменный манифест страницы.
func collectorRevision(source sourcedoc.Source, raw jsontext.Value) (string, error) {
	var facts map[string]jsontext.Value
	if err := json.Unmarshal(raw, &facts); err != nil {
		return "", fmt.Errorf("decode collector revision facts: %w", err)
	}
	for _, key := range []string{"observed_at", "available_at", "parser_version", "provenance"} {
		delete(facts, key)
	}
	source.ObservedAt = time.Time{}
	source.AvailableAt = nil
	source.AvailableBasis = ""
	source.Revision = ""
	identity := struct {
		Source sourcedoc.Source          `json:"source"`
		Facts  map[string]jsontext.Value `json:"facts"`
	}{Source: source, Facts: facts}
	encoded, err := json.Marshal(identity, json.Deterministic(true), jsontext.ReorderRawObjects(true))
	if err != nil {
		return "", fmt.Errorf("encode collector revision facts: %w", err)
	}
	return "collector-v2:" + digest(encoded), nil
}
