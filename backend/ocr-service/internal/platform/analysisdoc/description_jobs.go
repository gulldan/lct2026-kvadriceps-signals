package analysisdoc

import (
	"errors"
	"strings"
	"time"
	"uuid"
)

var ErrCapacity = errors.New("analysis queue capacity reached")

type DescriptionRead struct {
	JobRead
	CandidateID string `json:"candidate_id"`
}

// DescriptionJob: читателям доходят только безопасные результаты; аренды и полные пакеты фактов
// остаются в приватном RPC воркера.
type DescriptionJob struct {
	JobID          string             `json:"job_id"`
	CandidateID    string             `json:"candidate_id"`
	Version        string             `json:"version"`
	BundleSHA256   string             `json:"bundle_sha256"`
	ResultSHA256   string             `json:"result_sha256"`
	EvidenceSHA256 string             `json:"evidence_sha256"`
	Status         string             `json:"status"`
	Attempt        int                `json:"attempt"`
	Result         *DescriptionResult `json:"result"`
	UpdatedAt      time.Time          `json:"updated_at"`
}

// DescriptionLease не может адресовать другой запуск, кандидата или версию промпта.
type DescriptionLease struct {
	JobID       string `json:"job_id"`
	CandidateID string `json:"candidate_id"`
	Version     string `json:"version"`
	Token       string `json:"token"`
}

type DescriptionClaim struct {
	Version   string `json:"version"`
	TestRunID string `json:"test_run_id,omitempty"`
}

func (c DescriptionClaim) Validate() error {
	if c.Version != DescriptionVersion {
		return ErrInvalid
	}
	if c.TestRunID == "" {
		return nil
	}
	id, ok := strings.CutPrefix(c.TestRunID, "tr-test-")
	if _, err := uuid.Parse(id); err != nil || !ok {
		return ErrInvalid
	}
	return nil
}

type DescriptionClaimed struct {
	Lease  *DescriptionLease `json:"lease"`
	Bundle *FactBundle       `json:"bundle"`
}

type DescriptionComplete struct {
	DescriptionLease
	Result DescriptionResult `json:"result"`
}
