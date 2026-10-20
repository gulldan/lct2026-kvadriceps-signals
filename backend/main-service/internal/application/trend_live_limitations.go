package application

import "regexp"

var liveSinglePatientClaim = regexp.MustCompile(`(?i)(?:^|[^\pL\pN])` +
	`(?:одного|один|одном|единственного|единственный|единственном|1)\s+пациент(?:а|е|у|ом)?(?:$|[^\pL\pN])`)
var liveSinglePatientSource = regexp.MustCompile(`(?i)(?:^|[^\pL\pN])(?:one|single|1)\s+patient(?:$|[^\pL\pN])`)
var liveUncertainNegative = regexp.MustCompile(`(?i)\b(?:may|might)\s+not\b`)
var liveQualifiedPrediction = regexp.MustCompile(`(?i)\b(?:rather than|not)\s+(?:as\s+)?(?:a\s+)?definitive\s+prediction\b`)
var liveCategoricalNegative = regexp.MustCompile(`(?i)(?:^|[^\pL\pN])не(?:\s+|пригод|применим|возмож|подход|предсказыв)`)
var livePossibleClaim = regexp.MustCompile(`(?i)(?:^|[^\pL\pN])(?:может|могут|могло|могли|возможн|вероятн)`)
var liveQualifiedClaim = regexp.MustCompile(`(?i)(?:однозначн|окончательн|определённ|определенн)`)

// Явные усиления нельзя пропускать даже при ошибочном согласии проверяющего.
// Эти проверки дополняют смысловой вердикт, но сами не доказывают утверждение.
func liveLimitationQualifiersFit(claim, source string) bool {
	if !livePatientCountExplicit(claim, source) {
		return false
	}
	if !liveCategoricalNegative.MatchString(claim) {
		return true
	}
	if liveUncertainNegative.MatchString(source) && !livePossibleClaim.MatchString(claim) {
		return false
	}
	return !liveQualifiedPrediction.MatchString(source) || liveQualifiedClaim.MatchString(claim)
}

// Описание одной опухоли не устанавливает размер когорты.
func livePatientCountExplicit(claim, source string) bool {
	return !liveSinglePatientClaim.MatchString(claim) ||
		liveSinglePatientClaim.MatchString(source) || liveSinglePatientSource.MatchString(source)
}
