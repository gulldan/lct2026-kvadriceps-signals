package application

import (
	"context"
	_ "embed"
	"encoding/json/v2"
	"errors"
	"github.com/example/main-service/internal/platform/contracts"
	"github.com/example/main-service/internal/platform/dbclient"
	"slices"

	commonv1 "github.com/example/main-service/internal/platform/genproto/common/v1"
	"github.com/example/main-service/internal/platform/indexdoc"
	"github.com/example/main-service/internal/platform/sourcedoc"
)

// Каталог закрепляет перечисленную публичную совокупность без посчитанных трендов: работа учитывается,
// только когда её точный источник готов в базе.
//
//go:embed trend_study_corpus.json
var studyCorpusJSON []byte

// Датированные публичные свидетельства вне корпуса ACL, проверяемые по ссылке; звёзды — на дату
// каталога, история не восстанавливается.
//
//go:embed trend_study_evidence.json
var studyEvidenceJSON []byte

type StudyPaper struct {
	ID          string   `json:"id"`
	Title       string   `json:"title"`
	Date        string   `json:"date"`
	CanonicalID string   `json:"canonical_id"`
	ContentHash string   `json:"content_hash"`
	URL         string   `json:"url"`
	Authors     []string `json:"authors"`
	DocumentID  string   `json:"document_id"`
	SnapshotID  string   `json:"snapshot_id"`
}

type StudyCorpus struct {
	Version          string       `json:"version"`
	Revision         string       `json:"revision"`
	ObservedAt       string       `json:"observed_at"`
	Start            string       `json:"start"`
	End              string       `json:"end"`
	TitleQuery       string       `json:"title_query"`
	EnumeratedPapers int          `json:"enumerated_papers"`
	Papers           []StudyPaper `json:"papers"`
}

func studyCatalog() (StudyCorpus, error) {
	var corpus StudyCorpus
	err := json.Unmarshal(studyCorpusJSON, &corpus, json.RejectUnknownMembers(true))
	if err != nil || corpus.Version != "acl-rag-study-v1" || len(corpus.Papers) == 0 || len(corpus.Papers) > 1000 {
		return corpus, errors.New("invalid research population catalog")
	}
	return corpus, nil
}

type StudyEvidence struct {
	Version    string       `json:"version"`
	ObservedAt string       `json:"observed_at"`
	Events     []StudyEvent `json:"events"`
}

func studyEvidence() (StudyEvidence, error) {
	var evidence StudyEvidence
	err := json.Unmarshal(studyEvidenceJSON, &evidence, json.RejectUnknownMembers(true))
	if err != nil || evidence.Version != "acl-rag-study-external-v1" || len(evidence.Events) == 0 {
		return evidence, errors.New("invalid external evidence catalog")
	}
	return evidence, nil
}

type StudyView struct {
	Version          string       `json:"version"`
	Method           string       `json:"method"`
	Revision         string       `json:"revision"`
	ObservedAt       string       `json:"observed_at"`
	ExternalObserved string       `json:"external_observed_at"`
	TitleQuery       string       `json:"title_query"`
	EnumeratedPapers int          `json:"enumerated_papers"`
	ExpectedPapers   int          `json:"expected_papers"`
	AvailablePapers  int          `json:"available_papers"`
	Coverage         string       `json:"coverage"`
	Topics           []StudyTopic `json:"topics"`
	Papers           []StudyPaper `json:"papers"`
}

func studyMatches(docs []*commonv1.Document, wanted map[string]StudyPaper) (map[string]StudyPaper, []string) {
	byDocument := map[string]StudyPaper{}
	ids := []string{}
	for _, doc := range docs {
		source, decodeErr := sourcedoc.Decode([]byte(doc.GetSourceMetadata()))
		if decodeErr != nil {
			continue
		}
		paper, exists := wanted[source.CanonicalID]
		if !exists || source.ContentHash != paper.ContentHash || source.Kind != "paper" ||
			source.Publication == nil || source.Publication.Value != paper.Date || source.URL != paper.URL {
			continue
		}
		paper.DocumentID = doc.GetId()
		byDocument[doc.GetId()] = paper
		ids = append(ids, doc.GetId())
	}
	return byDocument, ids
}

func (s *IngestionService) studyReady(ctx context.Context, reader string, ids []string,
	byDocument map[string]StudyPaper) (map[string]StudyPaper, error) {
	found := map[string]StudyPaper{}
	for batch := range slices.Chunk(ids, 256) {
		states, readErr := s.sourceIndexes.ReadIndexes(ctx, indexdoc.Read{
			ReaderID: reader, SharedCorpus: s.sharedCorpus, DocumentIDs: batch,
		})
		if readErr != nil {
			return nil, readErr
		}
		for _, state := range states.Documents {
			paper, exists := byDocument[state.DocumentID]
			if !exists || !state.Allowed || state.Active == nil || state.Active.RawHash != paper.ContentHash ||
				state.Active.DocumentID != paper.DocumentID || state.Active.SourceRevision != "sha256:"+paper.ContentHash {
				continue
			}
			paper.SnapshotID = state.Active.SnapshotID
			// Копия той же канонической работы — всё равно одна работа.
			if previous, ok := found[paper.ID]; !ok || paper.DocumentID < previous.DocumentID {
				found[paper.ID] = paper
			}
		}
	}
	return found, nil
}

// TrendStudy строит проекцию по читаемым источникам базы: недостающая или изменённая работа делает
// покрытие частичным и отключает утверждения о доле, росте и стадии.
func (s *IngestionService) TrendStudy(ctx context.Context, reader string) (StudyView, error) {
	corpus, err := studyCatalog()
	evidence, evidenceErr := studyEvidence()
	if err != nil || evidenceErr != nil || s.sourceIndexes == nil {
		return StudyView{}, errors.New("research population unavailable")
	}

	wanted := make(map[string]StudyPaper, len(corpus.Papers))
	for _, paper := range corpus.Papers {
		wanted[paper.CanonicalID] = paper
	}
	found := map[string]StudyPaper{}
	err = dbclient.WalkDocuments(ctx, s.docs, "", contracts.DocumentScan{}, func(docs []*commonv1.Document) (bool, error) {
		byDocument, ids := studyMatches(docs, wanted)
		page, readErr := s.studyReady(ctx, reader, ids, byDocument)
		if readErr != nil {
			return false, readErr
		}
		for id, paper := range page {
			if prior, ok := found[id]; !ok || paper.DocumentID < prior.DocumentID {
				found[id] = paper
			}
		}
		return true, nil
	})
	if err != nil {
		return StudyView{}, err
	}

	papers := make([]StudyPaper, 0, len(found))
	for _, expected := range corpus.Papers {
		if paper, ok := found[expected.ID]; ok {
			papers = append(papers, paper)
		}
	}
	complete := len(papers) == len(corpus.Papers)
	coverage := "partial"
	if complete {
		coverage = "complete_bounded_population"
	}
	return StudyView{Version: corpus.Version, Method: "bibliographic-signals-v3-screened", Revision: corpus.Revision,
		ObservedAt: corpus.ObservedAt, ExternalObserved: evidence.ObservedAt, TitleQuery: corpus.TitleQuery,
		EnumeratedPapers: corpus.EnumeratedPapers, ExpectedPapers: len(corpus.Papers), AvailablePapers: len(papers),
		Coverage: coverage, Topics: studyTopics(papers, complete, evidence.Events), Papers: papers}, nil
}
