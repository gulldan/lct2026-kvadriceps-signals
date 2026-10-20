package application

import (
	"fmt"
	"slices"
	"strings"
)

type liveQualificationIdentity struct {
	SourceID       string                     `json:"source_id"`
	SameTechnology bool                       `json:"same_technology"`
	Role           string                     `json:"role"`
	Candidate      liveQualificationCriterion `json:"candidate"`
	Mechanism      liveQualificationCriterion `json:"mechanism"`
	Relevance      liveQualificationCriterion `json:"relevance"`
}

// Прямые свидетельства считает сервер: общий вердикт модели не делает обзор или соседний подход подтверждением.
func liveApplyQualificationIdentity(qualification LiveQualification,
	assessments []liveQualificationIdentity, documents []LiveQualificationDocument,
) (LiveQualification, error) {
	byID, err := liveQualificationAssessments(qualification.Evidence, assessments)
	if err != nil {
		return qualification, err
	}
	kept := make([]LiveQualificationEvidence, 0, len(qualification.Evidence))
	rejection, mechanismFound := "", false
	for _, evidence := range qualification.Evidence {
		assessment := byID[evidence.SourceID]
		if err := liveValidateScopeCriteria(qualification.Scope, assessment.Relevance.Criteria); err != nil {
			return qualification, err
		}
		if !assessment.SameTechnology || !liveQualificationDirectRole(assessment.Role) {
			continue
		}
		grounded, err := liveGroundQualificationEvidence(evidence, assessment, documents)
		if err != nil {
			return qualification, err
		}
		if !*assessment.Candidate.Supported {
			rejection = assessment.Candidate.Reason
			continue
		}
		if !*assessment.Mechanism.Supported {
			if rejection == "" {
				rejection = assessment.Mechanism.Reason
			}
			continue
		}
		mechanismFound = true
		if !*assessment.Relevance.Supported || len(assessment.Relevance.Criteria) == 0 {
			rejection = assessment.Relevance.Reason
			continue
		}
		evidence = grounded
		evidence.Role, evidence.SameTechnology = assessment.Role, true
		kept = append(kept, evidence)
	}
	return liveFinalizeQualificationIdentity(qualification, kept, rejection, mechanismFound), nil
}

func liveFinalizeQualificationIdentity(qualification LiveQualification, kept []LiveQualificationEvidence,
	rejection string, mechanismFound bool,
) LiveQualification {
	// Модель сама ставит точку в конце механизма и иногда начинает его с «Механизм:»; без обрезки
	// выходит «падение.. Оба» и «Механизм: Механизм:».
	mechanism := strings.TrimSpace(strings.TrimPrefix(strings.TrimSpace(qualification.Mechanism), "Механизм:"))
	qualification.Reason = "Механизм: " + strings.TrimRight(mechanism, ".") + ". " + qualification.Reason
	qualification.Coherent = len(kept) >= liveCandidateMinDocuments
	if !qualification.Coherent {
		qualification.Reason = fmt.Sprintf("Прямое подтверждение одного механизма найдено в %d из %d документов; "+
			"обзоры, общий контекст и другие способы не считаются подтверждениями. Проверялся механизм: %s",
			len(kept), len(qualification.Evidence), qualification.Mechanism)
		if rejection != "" {
			qualification.Reason = rejection
			qualification.Relevant = !mechanismFound
		}
	}
	qualification.Evidence = kept
	if mechanismFound {
		qualification.Relevant = liveQualificationScopeSatisfied(qualification)
		if qualification.Coherent && !qualification.Relevant {
			qualification.Reason = "Источники не подтверждают все обязательные критерии области запроса."
		}
	}
	return qualification
}

func liveQualificationAssessments(evidence []LiveQualificationEvidence,
	assessments []liveQualificationIdentity,
) (map[string]liveQualificationIdentity, error) {
	byID := make(map[string]liveQualificationIdentity, len(assessments))
	for _, assessment := range assessments {
		if !slices.ContainsFunc(evidence, func(e LiveQualificationEvidence) bool { return e.SourceID == assessment.SourceID }) {
			return nil, fmt.Errorf("qualification review: unexpected source %q", assessment.SourceID)
		}
		if _, found := byID[assessment.SourceID]; found {
			return nil, fmt.Errorf("qualification review: duplicate assessment for %q", assessment.SourceID)
		}
		if !liveQualificationRoleKnown(assessment.Role) {
			return nil, fmt.Errorf("qualification review: missing source role for %q", assessment.SourceID)
		}
		byID[assessment.SourceID] = assessment
	}
	if len(byID) != len(evidence) {
		return nil, fmt.Errorf("qualification review: assessed %d of %d sources", len(byID), len(evidence))
	}
	return byID, nil
}

func liveQualificationDirectRole(role string) bool {
	switch role {
	case "introduces", "implements", "evaluates", "deployment":
		return true
	default:
		return false
	}
}

func liveQualificationRoleKnown(role string) bool {
	return liveQualificationDirectRole(role) || role == "review" || role == "context" || role == "different"
}
