package application

import (
	"context"
	"errors"
	"log/slog"

	"github.com/example/main-service/internal/domain"
	"github.com/example/main-service/internal/platform/analysisdoc"
)

type AnalysisJobs interface {
	AnalysisInputs
	SubmitAnalysis(context.Context, analysisdoc.Submit) (analysisdoc.Job, error)
	ReadAnalysisJob(context.Context, analysisdoc.JobRead) (analysisdoc.Job, error)
	StartDescription(context.Context, analysisdoc.DescriptionRead) (analysisdoc.DescriptionJob, error)
	ReadDescription(context.Context, analysisdoc.DescriptionRead) (analysisdoc.DescriptionJob, error)
}

type AnalysisService struct {
	db        AnalysisJobs
	objects   domain.ObjectStore
	method    string
	translate QueryTranslator
}

// SetTranslator задаёт перевод запроса: большой корпус читается по английским поисковым терминам.
func (s *AnalysisService) SetTranslator(translate QueryTranslator) { s.translate = translate }

func NewAnalysisService(db AnalysisJobs, objects domain.ObjectStore, method string) (*AnalysisService, error) {
	if _, err := analysisdoc.ParseMethod(method); err != nil {
		return nil, err
	}
	return &AnalysisService{db: db, objects: objects, method: method}, nil
}

// Submit отвечает accepted/prepared, пока проекция не опубликована целиком.
func (s *AnalysisService) Submit(ctx context.Context, in analysisdoc.Request) (analysisdoc.Job, error) {
	if s.translate != nil && len(in.SearchTerms) == 0 {
		terms, err := s.translate(ctx, in.Query)
		if err != nil {
			slog.WarnContext(ctx, "analysis query terms unavailable; corpus read by recency", "error", err)
		}
		in.SearchTerms = analysisdoc.NormalizeTerms(terms)
	}
	frozen, err := FreezeAnalysis(ctx, s.db, s.objects, in)
	if err != nil {
		return analysisdoc.Job{}, err
	}
	job, err := s.db.SubmitAnalysis(ctx, analysisdoc.Submit{Read: analysisdoc.Read{RunID: frozen.RunID,
		ReaderID: in.OwnerID, SharedCorpus: in.SharedCorpus}, MethodJSON: s.method})
	if err != nil {
		return job, err
	}
	return s.Read(ctx, analysisdoc.JobRead{JobID: job.ID, ReaderID: in.OwnerID, SharedCorpus: in.SharedCorpus})
}

// Read сверяет хэши сохранённой проекции и метода, затем заново проверяет права.
// Отсутствие кэша не меняет порядок и не подменяет прогон более новым.
func (s *AnalysisService) Read(ctx context.Context, in analysisdoc.JobRead) (analysisdoc.Job, error) {
	job, err := s.db.ReadAnalysisJob(ctx, in)
	if err != nil || job.Status != analysisdoc.Published {
		return job, err
	}
	if job.ResultArtifact != analysisdoc.ResultKey(job.ID, job.ResultSHA256) ||
		analysisdoc.ResultHash(job.ResultJSON) != job.ResultSHA256 ||
		analysisdoc.ResultHash(job.MethodJSON) != job.MethodSHA256 {
		return analysisdoc.Job{}, errors.New("stored analysis projection is inconsistent")
	}
	for key, raw := range map[string]string{job.ResultArtifact: job.ResultJSON,
		analysisdoc.MethodKey(job.MethodSHA256): job.MethodJSON} {
		if err = analysisdoc.VerifyObject(ctx, s.objects, key, raw); err != nil {
			return analysisdoc.Job{}, err
		}
	}
	current, err := s.db.ReadAnalysisJob(ctx, in)
	if err != nil {
		return analysisdoc.Job{}, err
	}
	if current.ResultSHA256 != job.ResultSHA256 || current.MethodSHA256 != job.MethodSHA256 {
		return analysisdoc.Job{}, errors.New("saved analysis changed while reading")
	}
	return current, nil
}
