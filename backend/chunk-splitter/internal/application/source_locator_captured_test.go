package application

import (
	"encoding/json/v2"
	"os"
	"strings"
	"testing"

	"github.com/example/chunk-splitter/internal/infrastructure/splitter"
)

// Повтор явно захваченных событий парсера по запросу, без моделей и записи.
func TestCapturedSourceLocators(t *testing.T) {
	path := os.Getenv("SIGNALGRAPH_LOCATOR_INPUTS")
	if path == "" {
		t.Skip("set SIGNALGRAPH_LOCATOR_INPUTS to the captured parser event manifest")
	}
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	var inputs []struct {
		ID       string            `json:"id"`
		Text     string            `json:"text"`
		Metadata map[string]string `json:"metadata"`
	}
	if err = json.Unmarshal(raw, &inputs); err != nil {
		t.Fatal(err)
	}
	for _, input := range inputs {
		t.Run(input.ID, func(t *testing.T) {
			structure := parseDocStructure(input.Metadata, input.Text)
			for start := 0; start < len(input.Text); {
				end := structure.nextPageByteAfter(start, len(input.Text))
				if end <= start {
					end = len(input.Text)
				}
				segment := input.Text[start:end]
				chunks := splitter.NewRecursive(1000, 150).Split(segment)
				offsets := splitter.LocateOffsets(segment, chunkTexts(chunks))
				for i, span := range offsets {
					if span == [2]int{} {
						needle := strings.Join(strings.Fields(chunks[i].Text), " ")
						found := strings.Index(strings.Join(strings.Fields(segment), " "), needle)
						if i > 0 {
							t.Logf("previous span %v, previous suffix %q", offsets[i-1], chunks[i-1].Text[max(0, len(chunks[i-1].Text)-100):])
						}
						t.Errorf("page byte %d chunk %d missing; global match %d; start %q", start, i, found, needle[:min(90, len(needle))])
					}
				}
				start = end
			}
		})
	}
}
