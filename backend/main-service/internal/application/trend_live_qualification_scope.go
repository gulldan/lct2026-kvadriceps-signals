package application

import "errors"

// Только оставшиеся прямые источники учитываются при проверке ветвей и общих условий.
func liveQualificationScopeSatisfied(qualification LiveQualification) bool {
	if qualification.Scope == nil {
		return false
	}
	var criteria []string
	for _, evidence := range qualification.Evidence {
		criteria = append(criteria, evidence.ScopeCriteria...)
	}
	return liveScopeSatisfied(qualification.Scope, criteria)
}

func liveValidateQualificationScope(qualification LiveQualification) error {
	if qualification.Scope == nil {
		return errors.New("qualification: missing query scope")
	}
	if err := liveValidateQueryScope(qualification.Scope, qualification.Scope.Query); err != nil {
		return err
	}
	for _, evidence := range qualification.Evidence {
		if err := liveValidateScopeCriteria(qualification.Scope, evidence.ScopeCriteria); err != nil {
			return err
		}
	}
	if !liveQualificationScopeSatisfied(qualification) {
		return errors.New("qualification: required query criteria are not grounded")
	}
	return nil
}
