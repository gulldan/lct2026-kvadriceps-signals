package delimited

import (
	"path/filepath"
	"strings"
)

func IsCandidate(filename, mimeType string) bool {
	switch strings.ToLower(filepath.Ext(filename)) {
	case ".csv", ".tsv":
		return true
	}
	switch mimeBase(mimeType) {
	case "text/csv", "text/tab-separated-values":
		return true
	default:
		return false
	}
}

func mimeBase(mimeType string) string {
	return strings.TrimSpace(strings.ToLower(strings.SplitN(mimeType, ";", 2)[0]))
}
