package application_test

import (
	"context"
	"errors"
	"testing"

	"github.com/example/db-service/internal/application"
	"github.com/example/db-service/internal/domain"
)

type stubHypotheses struct {
	created bool
	updated bool
}

func (s *stubHypotheses) Create(_ context.Context, _ *domain.Hypothesis, _ *domain.Revision) error {
	s.created = true
	return nil
}
func (s *stubHypotheses) Get(_ context.Context, _ string) (*domain.Hypothesis, error) {
	return &domain.Hypothesis{}, nil
}
func (s *stubHypotheses) List(_ context.Context, _ domain.HypothesisFilter) ([]*domain.Hypothesis, error) {
	return nil, nil
}
func (s *stubHypotheses) Update(_ context.Context, _ *domain.Hypothesis, _ *domain.Revision) error {
	s.updated = true
	return nil
}
func (s *stubHypotheses) ListEvidence(_ context.Context, _ string) ([]*domain.Evidence, error) {
	return nil, nil
}
func (s *stubHypotheses) AddRevision(_ context.Context, _ *domain.Revision) error { return nil }
func (s *stubHypotheses) ListRevisions(_ context.Context, _ string) ([]*domain.Revision, error) {
	return nil, nil
}

func intPtr(v int) *int { return &v }

func newSvcWithHypotheses(h domain.HypothesisRepository) *application.Service {
	return application.New(nil, nil, nil, nil, nil, nil, h, nil, nil)
}

func TestCreateHypothesisRejectsOutOfRangeTRL(t *testing.T) {
	svc := newSvcWithHypotheses(&stubHypotheses{})
	base := func(trl int) *domain.Hypothesis {
		return &domain.Hypothesis{OwnerID: "o", Title: "t", Statement: "s", TRL: intPtr(trl)}
	}
	for _, trl := range []int{0, -1, 10, 100} {
		if _, err := svc.CreateHypothesis(context.Background(), base(trl), nil); !errors.Is(err, domain.ErrInvalidArgument) {
			t.Fatalf("CreateHypothesis trl=%d: want ErrInvalidArgument, got %v", trl, err)
		}
	}
}

func TestCreateHypothesisAcceptsBoundaryAndNilTRL(t *testing.T) {
	for _, trl := range []*int{nil, intPtr(1), intPtr(9)} {
		stub := &stubHypotheses{}
		svc := newSvcWithHypotheses(stub)
		h := &domain.Hypothesis{OwnerID: "o", Title: "t", Statement: "s", TRL: trl}
		if _, err := svc.CreateHypothesis(context.Background(), h, nil); err != nil {
			t.Fatalf("CreateHypothesis trl=%v: unexpected error %v", trl, err)
		}
		if !stub.created {
			t.Fatalf("CreateHypothesis trl=%v: expected repo Create to be called", trl)
		}
	}
}

func TestUpdateHypothesisRejectsOutOfRangeTRL(t *testing.T) {
	svc := newSvcWithHypotheses(&stubHypotheses{})
	h := &domain.Hypothesis{ID: "id", TRL: intPtr(12)}
	if err := svc.UpdateHypothesis(context.Background(), h, nil); !errors.Is(err, domain.ErrInvalidArgument) {
		t.Fatalf("UpdateHypothesis: want ErrInvalidArgument, got %v", err)
	}
}

func TestUpdateHypothesisAcceptsBoundaryTRL(t *testing.T) {
	stub := &stubHypotheses{}
	svc := newSvcWithHypotheses(stub)
	h := &domain.Hypothesis{ID: "id", TRL: intPtr(9)}
	if err := svc.UpdateHypothesis(context.Background(), h, nil); err != nil {
		t.Fatalf("UpdateHypothesis trl=9: unexpected error %v", err)
	}
	if !stub.updated {
		t.Fatal("UpdateHypothesis trl=9: expected repo Update to be called")
	}
}

func TestCreateHypothesisMissingRequiredFieldsIsInvalidArgument(t *testing.T) {
	svc := newSvcWithHypotheses(&stubHypotheses{})
	cases := []*domain.Hypothesis{
		{Title: "t", Statement: "s"},
		{OwnerID: "o", Statement: "s"},
		{OwnerID: "o", Title: "t"},
	}
	for i, h := range cases {
		if _, err := svc.CreateHypothesis(context.Background(), h, nil); !errors.Is(err, domain.ErrInvalidArgument) {
			t.Fatalf("case %d: want ErrInvalidArgument, got %v", i, err)
		}
	}
}

func TestUpdateHypothesisMissingIDIsInvalidArgument(t *testing.T) {
	svc := newSvcWithHypotheses(&stubHypotheses{})
	if err := svc.UpdateHypothesis(context.Background(), &domain.Hypothesis{}, nil); !errors.Is(err, domain.ErrInvalidArgument) {
		t.Fatalf("UpdateHypothesis: want ErrInvalidArgument, got %v", err)
	}
}

func (s *stubHypotheses) Board(ctx context.Context, f domain.HypothesisFilter) ([]*domain.Hypothesis, []byte, error) {
	hs, err := s.List(ctx, f)
	return hs, nil, err
}
