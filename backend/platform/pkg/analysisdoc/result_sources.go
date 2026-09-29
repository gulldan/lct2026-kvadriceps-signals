package analysisdoc

import (
	"context"
	"encoding/json/v2"
	"fmt"
	"io"
	"slices"
	"time"
	"unicode/utf8"

	"github.com/example/rag-mvp/pkg/indexdoc"
)

type RelationEvidence struct {
	DocumentID       string `json:"document_id"`
	SourceRevision   string `json:"source_revision"`
	SnapshotID       string `json:"snapshot_id"`
	TextSHA256       string `json:"text_sha256"`
	Start            int    `json:"char_start"`
	End              int    `json:"char_end"`
	Quote            string `json:"quote"`
	Direction        string `json:"direction"`
	TargetIdentifier string `json:"target_identifier"`
	AvailableAt      string `json:"available_at"`
}

type Relation struct {
	ID             string             `json:"id"`
	PaperID        string             `json:"paper_id"`
	RepositoryID   string             `json:"repository_id"`
	RepositoryRoot *string            `json:"repository_root"`
	Evidence       []RelationEvidence `json:"evidence"`
	Relation       string             `json:"relation"`
	Official       bool               `json:"official"`
	Company        *string            `json:"company"`
	ProductionUse  *bool              `json:"production_use"`
}

type sourceSpan struct {
	start, end int
	quote      string
}

// MaxAnalysisArtifactBytes совпадает с лимитом воркера на максимум в 2000 документов.
const MaxAnalysisArtifactBytes int64 = 4 << 30

// VerifyResultSources сам сверяет выбранные воркером цитаты с закреплённым текстом в S3, загружая
// по одному документу.
func VerifyResultSources(ctx context.Context, store ObjectReader, input Input, raw string) error {
	result, err := Decode[Result](raw)
	if err != nil {
		return err
	}
	spans, err := resultSpans(result, input)
	if err != nil {
		return err
	}
	var total int64
	for _, doc := range input.Records {
		if len(spans[doc.ID]) == 0 {
			continue
		}
		n, readErr := verifySpans(ctx, store, doc.Index.Text, spans[doc.ID])
		if readErr != nil {
			return readErr
		}
		total += n
		if total > MaxAnalysisArtifactBytes {
			return ErrInvalid
		}
	}
	return verifyDiversity(ctx, store, input, result, total)
}

func resultSpans(result Result, input Input) (map[string][]sourceSpan, error) {
	spans := map[string][]sourceSpan{}
	works := map[string]Work{}
	docs := map[string]Record{}
	for _, w := range result.Works {
		works[w.ID] = w
	}
	for _, d := range input.Records {
		docs[d.ID] = d
	}
	for _, c := range result.Candidates {
		for _, e := range c.Evidence {
			if !slices.Contains(works[e.WorkID].DocumentIDs, e.DocumentID) {
				return nil, ErrInvalid
			}
			spans[e.DocumentID] = append(spans[e.DocumentID], sourceSpan{e.Start, e.End, e.Quote})
		}
	}
	for _, technology := range result.Technologies {
		for _, quote := range technology.Evidence {
			spans[quote.DocumentID] = append(spans[quote.DocumentID], sourceSpan{quote.Start, quote.End, quote.Quote})
		}
	}
	if len(result.Relations) > MaxDocuments*4 {
		return nil, ErrInvalid
	}
	seen := map[string]bool{}
	for _, rel := range result.Relations {
		if rel.ID == "" || seen[rel.ID] || works[rel.PaperID].Kind != "paper" ||
			works[rel.RepositoryID].Kind != "software_repository" || rel.Relation != "explicit_reference" ||
			rel.Official || rel.Company != nil || rel.ProductionUse != nil || len(rel.Evidence) == 0 || len(rel.Evidence) > 20 {
			return nil, ErrInvalid
		}
		seen[rel.ID] = true
		if err := relationSpans(rel, docs, works, input.Request.Cutoff, spans); err != nil {
			return nil, err
		}
	}
	return spans, nil
}

func relationSpans(rel Relation, docs map[string]Record, works map[string]Work, cutoff string,
	spans map[string][]sourceSpan) error {
	end, err := time.Parse(time.RFC3339, cutoff)
	if err != nil {
		return ErrInvalid
	}
	for _, e := range rel.Evidence {
		d, ok := docs[e.DocumentID]
		available, err := time.Parse(time.RFC3339Nano, e.AvailableAt)
		if !ok || err != nil || available.After(end) || e.SnapshotID != d.Index.SnapshotID ||
			e.SourceRevision != d.Index.SourceRevision || e.TextSHA256 != d.Index.Text.SHA256 ||
			(!slices.Contains(works[rel.PaperID].DocumentIDs, e.DocumentID) &&
				!slices.Contains(works[rel.RepositoryID].DocumentIDs, e.DocumentID)) {
			return ErrInvalid
		}
		spans[e.DocumentID] = append(spans[e.DocumentID], sourceSpan{e.Start, e.End, e.Quote})
	}
	return nil
}

func verifySpans(ctx context.Context, store ObjectReader, obj indexdoc.Object, spans []sourceSpan) (int64, error) {
	if store == nil {
		return 0, ErrInvalid
	}
	r, err := store.Get(ctx, obj.Key)
	if err != nil {
		return 0, fmt.Errorf("read evidence text: %w", err)
	}
	defer func() { _ = r.Close() }()
	b, err := io.ReadAll(io.LimitReader(r, MaxBytes+1))
	if err != nil || len(b) > MaxBytes || indexdoc.Hash(b) != obj.SHA256 || !utf8.Valid(b) {
		return 0, ErrInvalid
	}
	text := []rune(string(b))
	for _, s := range spans {
		if s.start < 0 || s.end <= s.start || s.end > len(text) || s.end-s.start > 3000 || string(text[s.start:s.end]) != s.quote {
			return 0, ErrInvalid
		}
	}
	return int64(len(b)), nil
}

func MarshalResult(result Result) (string, error) {
	b, err := json.Marshal(result, json.Deterministic(true))
	if err != nil {
		return "", fmt.Errorf("encode analysis result: %w", err)
	}
	return string(b), nil
}
