package application

import (
	"context"

	"github.com/example/main-service/internal/platform/analysisdoc"
)

// Description всегда привязано к опубликованному результату: недоступный текст не стирает его факты.
func (s *AnalysisService) Description(
	ctx context.Context, in analysisdoc.DescriptionRead, start bool,
) (analysisdoc.DescriptionJob, error) {
	job, err := s.Read(ctx, in.JobRead)
	if err != nil {
		return analysisdoc.DescriptionJob{}, err
	}
	bundle, err := analysisdoc.BuildFactBundle(job, in.CandidateID)
	if err != nil {
		return analysisdoc.DescriptionJob{}, err
	}
	var d analysisdoc.DescriptionJob
	if start {
		d, err = s.db.StartDescription(ctx, in)
	} else {
		d, err = s.db.ReadDescription(ctx, in)
	}
	if err != nil {
		return d, err
	}
	if d.JobID != job.ID || d.CandidateID != in.CandidateID || d.BundleSHA256 != bundle.SHA256() ||
		(d.Result != nil && d.Result.Check(bundle) != nil) {
		return analysisdoc.DescriptionJob{}, analysisdoc.ErrInvalid
	}
	return d, nil
}
