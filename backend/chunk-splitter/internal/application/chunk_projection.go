package application

import (
	"github.com/example/chunk-splitter/internal/domain"
	commonv1 "github.com/example/chunk-splitter/internal/platform/genproto/common/v1"
	"github.com/google/uuid"
	"strconv"
	"strings"
)

// buildBatch даёт точке вектора и лексическому документу один id, чтобы гибридный поиск их сливал.
func buildBatch(
	evt *commonv1.DocumentParsed,
	title string,
	headers bool,
	batch []domain.Chunk,
	base int,
	offsets [][2]int,
	baseRune int,
	ds docStructure,
	workbookAnchors workbookAnchorIndex,
	vectors [][]float32,
) (points []VectorPoint, docs []SearchDoc) {
	points = make([]VectorPoint, len(batch))
	docs = make([]SearchDoc, len(batch))
	for i, c := range batch {
		chunkIndex := base + c.Index
		id := chunkPointID(evt.GetDocumentId(), chunkIndex)
		charStart, charEnd := absOffset(offsets, c.Index, baseRune)
		pageStart, pageEnd := 0, 0
		if charStart != 0 || charEnd != 0 {
			pageStart = ds.pageOf(charStart)
			pageEnd = ds.pageOf(charEnd - 1)
		}
		heading := sectionOf(ds, charStart, c.Section)
		metadata := workbookAnchors.metadataAt(charStart, charEnd)
		if len(metadata) == 0 {
			metadata = workbookAnchorMetadata(c.Text)
		}
		points[i] = VectorPoint{
			ID:             id,
			Vector:         vectors[i],
			DocumentID:     evt.GetDocumentId(),
			OwnerID:        evt.GetOwnerId(),
			Filename:       evt.GetFilename(),
			Text:           c.Text,
			ChunkIndex:     chunkIndex,
			CharStart:      charStart,
			CharEnd:        charEnd,
			PageStart:      pageStart,
			PageEnd:        pageEnd,
			SectionHeading: heading,
			Metadata:       metadata,
			NodeType:       nodeTypeChunk,
			NodeLevel:      0,
		}
		docs[i] = SearchDoc{
			ID:         id,
			Text:       withHeader(headers, title, heading, c.Text),
			DocumentID: evt.GetDocumentId(),
			OwnerID:    evt.GetOwnerId(),
			Filename:   evt.GetFilename(),
			Section:    heading,
			Metadata:   metadata,
		}
	}
	return points, docs
}

func sectionOf(ds docStructure, charStart int, fallback string) string {
	if h := ds.sectionAt(charStart); h != "" {
		return h
	}
	return fallback
}

// Шапка «{title} — {section}» идёт только в эмбеддер и лексический индекс: payload Qdrant хранит сырой чанк,
// и шапка не протекает ни в интерфейс, ни в цитаты llm-service.
func withHeader(enabled bool, title, section, chunk string) string {
	if !enabled {
		return chunk
	}
	crumb := breadcrumb(title, section)
	if crumb == "" {
		return chunk
	}
	return crumb + "\n\n" + chunk
}

func breadcrumb(title, section string) string {
	title, section = strings.TrimSpace(title), strings.TrimSpace(section)
	switch {
	case title != "" && section != "":
		return title + " — " + section
	case title != "":
		return title
	default:
		return section
	}
}

// chunkPointID — чистая функция (documentID, chunkIndex): на ней держится идемпотентность при повторной доставке.
func chunkPointID(documentID string, chunkIndex int) string {
	ns := uuid.MustParse(chunkIDNamespace)
	return uuid.NewSHA1(ns, []byte(documentID+":"+strconv.Itoa(chunkIndex))).String()
}

// Ненайденный промежуток [0, 0] не сдвигается, чтобы отсутствующее смещение не стало ложным.
func absOffset(offsets [][2]int, idx, baseRune int) (start, end int) {
	if idx < 0 || idx >= len(offsets) {
		return 0, 0
	}
	o := offsets[idx]
	if o == [2]int{0, 0} {
		return 0, 0
	}
	return o[0] + baseRune, o[1] + baseRune
}
