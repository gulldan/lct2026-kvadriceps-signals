package httpapi

import (
	"net/http"

	"github.com/example/main-service/internal/platform/httpx"
	"github.com/example/main-service/internal/platform/indexdoc"
	"github.com/example/main-service/internal/platform/jwt"
	"github.com/example/main-service/internal/platform/sourcedoc"
)

type sourceEvidenceView struct {
	DocumentID       string               `json:"document_id"`
	SnapshotID       string               `json:"snapshot_id"`
	Title            string               `json:"title"`
	Filename         string               `json:"filename"`
	MIMEType         string               `json:"mime_type"`
	Source           sourcedoc.Source     `json:"source"`
	TextHash         string               `json:"text_hash"`
	ParserRevision   string               `json:"parser_revision"`
	PipelineRevision string               `json:"pipeline_revision"`
	ParserMetadata   map[string]string    `json:"parser_metadata"`
	Sections         []sourcedoc.Section  `json:"sections"`
	Anchors          []sourcedoc.Anchor   `json:"anchors"`
	Chunks           []sourceFragmentView `json:"chunks"`
}

type sourceFragmentView struct {
	ID          string `json:"id"`
	Index       int    `json:"index"`
	Text        string `json:"text"`
	CharStart   int    `json:"char_start"`
	CharEnd     int    `json:"char_end"`
	PageStart   int    `json:"page_start"`
	PageEnd     int    `json:"page_end"`
	LocatorKind string `json:"locator_kind"`
	Section     string `json:"section"`
	ElementPath string `json:"element_path,omitempty"`
}

func (a *API) sourceEvidence(w http.ResponseWriter, r *http.Request) {
	claims, ok := jwt.ClaimsFromContext(r.Context())
	if !ok || claims.UserID == "" {
		httpx.Error(w, http.StatusUnauthorized, "missing claims")
		return
	}
	snapshot, err := a.ingestion.SourceEvidence(r.Context(), claims.UserID, r.PathValue("id"), r.URL.Query().Get("snapshot"))
	if err != nil {
		sourceReadError(w, err)
		return
	}
	w.Header().Set("Cache-Control", "no-store")
	httpx.JSON(w, http.StatusOK, newSourceEvidenceView(snapshot))
}

func newSourceEvidenceView(s *indexdoc.Snapshot) sourceEvidenceView {
	chunks := make([]sourceFragmentView, len(s.Chunks))
	for i, c := range s.Chunks {
		chunks[i] = sourceFragmentView{ID: c.ID, Index: c.Index, Text: c.Text, CharStart: c.CharStart,
			CharEnd: c.CharEnd, PageStart: c.PageStart, PageEnd: c.PageEnd, LocatorKind: c.LocatorKind,
			Section: c.Section, ElementPath: c.Metadata["element_path"]}
	}
	return sourceEvidenceView{
		DocumentID: s.DocumentID, SnapshotID: s.SnapshotID, Title: s.Title, Filename: s.Filename, MIMEType: s.MIMEType,
		Source: s.Source, TextHash: s.TextHash, ParserRevision: s.ParserRevision, PipelineRevision: s.PipelineRevision,
		ParserMetadata: s.ParserMetadata, Sections: s.Sections, Anchors: s.Anchors, Chunks: chunks,
	}
}
