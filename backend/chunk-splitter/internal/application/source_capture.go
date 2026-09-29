package application

import (
	"context"
	"errors"
	"fmt"
	"maps"
	"strconv"
	"strings"

	commonv1 "github.com/example/chunk-splitter/internal/platform/genproto/common/v1"
	"github.com/example/chunk-splitter/internal/platform/indexdoc"
	"github.com/example/chunk-splitter/internal/platform/sourcedoc"
	"google.golang.org/protobuf/proto"
)

// Ни проекция, ни статус ready не меняются, пока полный снапшот текста и векторов не сохранён.
type captureIndex struct {
	pinnedTitle bool
	text        string
	title       string
	sections    []sourcedoc.Section
	points      []VectorPoint
	docs        []SearchDoc
}

func (c *captureIndex) Upsert(_ context.Context, points []VectorPoint) error {
	if len(c.points)+len(points) > indexdoc.MaxChunks {
		return errors.New("source snapshot exceeds chunk limit")
	}
	c.points = append(c.points, points...)
	return nil
}
func (c *captureIndex) Index(_ context.Context, docs []SearchDoc) error {
	if len(c.docs)+len(docs) > indexdoc.MaxChunks {
		return errors.New("source snapshot exceeds chunk limit")
	}
	c.docs = append(c.docs, docs...)
	return nil
}
func (*captureIndex) UpdateDocumentStatus(context.Context, string, string, string, *int32) error {
	return nil
}
func (*captureIndex) PublishProto(context.Context, string, string, proto.Message) error { return nil }

func (c *captureIndex) snapshot(evt *commonv1.DocumentParsed, job indexdoc.Job, model indexdoc.Model) (indexdoc.Snapshot, error) {
	out := indexdoc.Snapshot{
		Filename: evt.GetFilename(), MIMEType: evt.GetMimeType(), Title: c.title,
		Version: 1, DocumentID: job.Lease.DocumentID, OwnerID: job.Lease.OwnerID, SnapshotID: job.Lease.Token,
		RawHash: job.RawHash(), RawRevision: job.Revision(), Source: job.Source, TextHash: indexdoc.Hash(
			[]byte(
				c.text,
			),
		), Model: model, PipelineRevision: "source-index-v4",
		ParserRevision: evt.GetSource() + "/" + evt.GetMetadata()["parser_revision"], ParserMetadata: maps.Clone(evt.GetMetadata()),
	}
	if len(c.points) == 0 || len(c.points) != len(c.docs) || len(c.text) > indexdoc.MaxTextBytes {
		return out, errors.New("source snapshot has incomplete text/chunks")
	}
	if err := c.pinRevision(evt, out.TextHash); err != nil {
		return out, err
	}
	runes := []rune(c.text)
	// Структура сборщика главнее, пока адресует сохранённый текст: он видел настоящие разделы, а наши выведены
	// из заголовков. Устаревшая структура отбрасывается, а не подгоняется.
	out.Sections = c.sections
	if len(job.Source.Sections) > 0 && sourcedoc.ValidateSections(job.Source.Sections, len(runes)) == nil {
		out.Sections = job.Source.Sections
	}
	if err := sourcedoc.ValidateSections(out.Sections, len(runes)); err != nil {
		return out, fmt.Errorf("source sections do not address the stored text: %w", err)
	}
	if sourcedoc.ValidateAnchors(job.Source.Anchors, len(runes)) == nil {
		out.Anchors = job.Source.Anchors
	}
	for i := range c.points {
		p, d := &c.points[i], &c.docs[i]
		if err := indexdoc.ValidateVector(p.Vector, model.Dimension); err != nil {
			return out, err
		}
		if p.ChunkIndex != i || p.ID != d.ID || p.CharStart < 0 || p.CharEnd <= p.CharStart || p.CharEnd > len(runes) {
			return out, fmt.Errorf("source chunk %d has no exact text locator", i)
		}
		rawText := string(runes[p.CharStart:p.CharEnd])
		if strings.Join(strings.Fields(rawText), " ") != strings.Join(strings.Fields(p.Text), " ") {
			return out, fmt.Errorf("source chunk %d differs from its text locator", i)
		}
		p.Text = rawText
		d.SourceText = rawText
		p.ID = chunkPointID(job.Lease.DocumentID+":"+job.Lease.Token, i)
		d.ID = p.ID
		meta := maps.Clone(p.Metadata)
		if meta == nil {
			meta = map[string]string{}
		}
		// Путь элемента участвует в решении ниже, поэтому вычисляется первым.
		meta["element_path"] = anchorPathAt(out.Anchors, p.CharStart)
		kind := sourceLocatorKind(p, meta["element_path"], evt.GetSource())
		for key, value := range map[string]string{
			"source_index_snapshot": job.Lease.Token, "source_revision": job.Revision(),
			"source_access_scope": job.Source.AccessScope, "source_content_hash": job.RawHash(),
			"text_sha256": out.TextHash, "parser_revision": out.ParserRevision, "locator_kind": kind,
			"char_start": strconv.Itoa(p.CharStart), "char_end": strconv.Itoa(p.CharEnd),
			"page_start": strconv.Itoa(p.PageStart), "page_end": strconv.Itoa(p.PageEnd),
		} {
			meta[key] = value
		}
		p.Metadata, d.Metadata = meta, maps.Clone(meta)
		out.Chunks = append(out.Chunks, indexdoc.Chunk{
			ID: p.ID, Index: i, Text: p.Text, LexicalText: d.Text, Vector: p.Vector,
			CharStart: p.CharStart, CharEnd: p.CharEnd, PageStart: p.PageStart, PageEnd: p.PageEnd,
			LocatorKind: kind, Section: p.SectionHeading, Metadata: meta,
		})
	}
	return out, nil
}

func anchorPathAt(anchors []sourcedoc.Anchor, start int) string {
	for _, anchor := range anchors {
		if start >= anchor.Start && start < anchor.End {
			return anchor.Path
		}
	}
	return ""
}

// Текст парсера сверяется с названным им хешем; простой текст передаётся ссылкой,
// и ревизию закрепляет хеш прочитанного.
func (*captureIndex) pinRevision(evt *commonv1.DocumentParsed, textHash string) error {
	declared := evt.GetMetadata()["parsed_text_sha256"]
	byReference := declared == "" && evt.GetTextObjectKey() != "" && evt.GetText() == ""
	if declared != textHash && !byReference {
		return errors.New("parsed text hash differs from the event revision")
	}
	if evt.GetMetadata()["parser_revision"] == "" {
		return errors.New("source parser revision is required")
	}
	return nil
}

func (ix *Indexer) sourceChunking() indexdoc.Chunking {
	out := indexdoc.Chunking{
		Algorithm: "recursive-page-v1", Unit: "rune", OverlapRunes: ix.overlap,
		WindowBytes: ix.splitWindow, BatchChunks: ix.batchSize, ContextualHeaders: ix.headers,
	}
	if sp, ok := ix.splitter.(interface{ Configuration() (int, int) }); ok {
		out.Budget, out.OverlapRunes = sp.Configuration()
	}
	if ix.tokenizer != nil {
		out.Unit, out.Budget = "model-token-with-rune-fallback", ix.maxTokens
	}
	return out
}

func sourceLocatorKind(point *VectorPoint, elementPath, source string) string {
	if point.Metadata["sheet"] != "" {
		return "spreadsheet_cell"
	}
	if elementPath != "" {
		return "html_element"
	}
	if point.PageStart == 0 {
		return "text_span"
	}
	if source == "ocr" {
		return "ocr_page"
	}
	return "pdf_text_page"
}
