package application

import (
	"github.com/example/main-service/internal/platform/jsonx"
	"strings"
)

type clusterRepresentative struct {
	DocumentID string `json:"document_id"`
	Filename   string `json:"filename"`
	Snippet    string `json:"snippet"`
}

func parseClusterRepresentatives(raw string) []clusterRepresentative {
	if strings.TrimSpace(raw) == "" {
		return nil
	}
	var reps []clusterRepresentative
	if err := jsonx.Unmarshal([]byte(raw), &reps); err != nil {
		return nil
	}
	return reps
}
