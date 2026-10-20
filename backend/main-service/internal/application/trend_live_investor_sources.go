package application

import (
	"cmp"
	"strings"
)

const (
	GenreInvestmentThesis   = "investment_thesis"
	GenreInvestorCommentary = "investor_commentary"
)

func liveInvestorContext(genre string) bool {
	switch strings.TrimSpace(genre) {
	case GenreInvestmentThesis, GenreInvestorCommentary:
		return true
	}
	return false
}

// Старый кейс карточки мог сохранить событие без жанра; исходный источник всё равно определяет его роль.
func liveInvestorEvent(topic LiveTopic, event LiveAdoptionEvent) bool {
	return liveInvestorContext(event.Record.Genre) || liveInvestorSource(topic, event.Record.ID) ||
		liveInvestorSource(topic, event.Record.URL)
}

// Интерес фонда ограничивает доверие сверху, но не повышает его для неизвестного издателя.
func liveApplyRecordGenre(passport SourcePassport, rawURL, title, explicitGenre string) (SourcePassport, string) {
	if !liveInvestorContext(explicitGenre) {
		return liveApplyGenre(passport, rawURL, title)
	}
	if liveTrustOrder(passport.Trust) > liveTrustOrder(TrustLowered) {
		passport.Trust = TrustLowered
	}
	passport.TrustBasis = "материал инвестора: отражает инвестиционный интерес и возможную финансовую " +
		"заинтересованность; не подтверждает научный результат или внедрение; издатель: " + passport.TrustBasis
	return passport, strings.TrimSpace(explicitGenre)
}

// Общий паспорт сохраняет происхождение и язык, а явный инвестиционный жанр ограничивает доверие радара.
func liveStampPaperPassports(papers []StudyPaper, metadata map[string]LivePaperMetadata) {
	stampPaperPassports(papers, metadata)
	for _, paper := range papers {
		meta := metadata[paper.ID]
		if !liveInvestorContext(meta.Genre) {
			continue
		}
		passport := SourcePassport{Publisher: meta.PublisherType, Trust: meta.Trust, TrustBasis: meta.TrustBasis}
		passport, _ = liveApplyRecordGenre(passport, cmp.Or(paper.URL, meta.SourceURL), paper.Title, meta.Genre)
		meta.Trust, meta.TrustBasis = passport.Trust, passport.TrustBasis
		metadata[paper.ID] = meta
	}
}
