package application

import (
	"encoding/json/v2"
	"fmt"
	"slices"
	"strings"
	"testing"
	"time"

	commonv1 "github.com/example/main-service/internal/platform/genproto/common/v1"
	"github.com/example/main-service/internal/platform/sourcedoc"
)

func TestLiveInvestorContextDoesNotBecomeAdoptionEvidence(t *testing.T) {
	for _, genre := range []string{GenreInvestmentThesis, GenreInvestorCommentary, GenreAnnouncement} {
		t.Run(genre, func(t *testing.T) {
			investor := genre != GenreAnnouncement
			record := LiveSourceRecord{
				ID: "aquifer", URL: "https://www.ycombinator.com/rfs/aquifer", Source: "Y Combinator",
				Genre: genre, Title: "Aquifer filtration: we want to fund a pilot or Series A",
			}
			if !investor {
				record.URL = "https://www.ycombinator.com/press-release/aquifer"
				record.Title = "Aquifer Labs raises $12M Series A for aquifer filtration"
				record.Date = "2026-10-17"
			}
			records := []LiveSourceRecord{record}
			stampPassports(records)
			record = records[0]
			if record.Genre != genre || record.PublisherType != PublisherVendor || record.Trust != TrustLowered {
				t.Errorf("source identity and genre ceiling: %+v", record)
			}
			if investor && (!strings.Contains(record.TrustBasis, "инвестора") || record.Date != "") {
				t.Errorf("interest must disclose its basis without an invented date: %+v", record)
			}
			papers := []StudyPaper{{ID: record.ID, URL: record.URL, Title: record.Title}}
			metadata := map[string]LivePaperMetadata{record.ID: {Genre: genre}}
			liveStampPaperPassports(papers, metadata)
			if meta := metadata[record.ID]; meta.Genre != genre || meta.Trust != TrustLowered ||
				(investor && !strings.Contains(meta.TrustBasis, "инвестора")) {
				t.Errorf("paper passport lost the explicit genre ceiling: %+v", meta)
			}
			if liveJournalSource(StudyPaper{ID: record.ID, URL: record.URL},
				LivePaperMetadata{Category: "crossref", Genre: record.Genre}) {
				t.Error("a bibliographic provider must not turn investor or promotional content into journal evidence")
			}
			topic := LiveTopic{Web: &LiveChannel{Records: records}, eventSources: []string{liveEventWebSource}}
			// Одинаковая запись может прийти из веб-ленты и отдельного сборщика событий.
			liveAttachEvents(&topic, []LiveAdoptionEvent{{Source: liveEventWebSource, Record: record}},
				LiveEventQuery{Term: "aquifer filtration"})
			if investor {
				if len(topic.Events) != 0 {
					t.Errorf("investment intent became an adoption event: %+v", topic.Events)
				}
				return
			}
			if len(topic.Events) != 1 || topic.Events[0].Type != LiveEventFunding ||
				topic.Events[0].Amount != "$12M" || liveEventDate(topic.Events[0]) != "2026-10-17" {
				t.Errorf("a dated funding announcement must remain funding evidence: %+v", topic.Events)
			}
		})
	}
	unknown := SourcePassport{Publisher: PublisherUnknown, Trust: "unknown", TrustBasis: "издатель не подтверждён"}
	passport, _ := liveApplyRecordGenre(unknown, "https://unknown-fund.example/thesis", "Investment thesis",
		GenreInvestmentThesis)
	if passport.Trust != unknown.Trust || !strings.Contains(passport.TrustBasis, "инвестора") {
		t.Errorf("a genre ceiling must not upgrade unknown trust: %+v", passport)
	}

	// Старые снимки могли уже превратить интересы портфельных компаний в рыночные события.
	topic := LiveTopic{eventSources: []string{liveEventWebSource}}
	for i := range liveMatureActors + 1 {
		topic.Events = append(topic.Events, LiveAdoptionEvent{
			Type: LiveEventFunding, Actor: fmt.Sprintf("Portfolio company %d", i), Verified: true,
			Record: LiveSourceRecord{Genre: GenreInvestorCommentary},
		})
	}
	topic.Events = append(topic.Events, LiveAdoptionEvent{
		Type: LiveEventPilot, Actor: "Google", Verified: true, Quote: "We want a pilot generally available to all",
		Record: LiveSourceRecord{Genre: GenreInvestmentThesis, Title: "Our wishlist for aquifer filtration"},
	})
	if level, facts, _ := liveEventMaturity(topic); level != liveMarketEarly || len(facts) != 0 {
		t.Errorf("stored investor context inflated market maturity: %s %v", level, facts)
	}
	liveAssessTZ(&topic, nil)
	if len(topic.Events) != 0 || topic.TZ.StageCode != 0 {
		t.Errorf("stored investor context inflated the adoption stage: %+v", topic.TZ)
	}

	t.Run("independence", func(t *testing.T) {
		records := []LiveSourceRecord{
			{ID: "sosv-thesis", URL: "https://sosv.com/aquifer", Source: "SOSV", Genre: GenreInvestmentThesis},
			{ID: "hax-comment", URL: "https://hax.co/aquifer", Source: "HAX", Genre: GenreInvestorCommentary},
		}
		metadata := map[string]LivePaperMetadata{}
		for i := range records {
			records[i].Title, records[i].Date = "Aquifer filtration investment thesis", "2026-10-17"
			if _, ok := liveRecordDocument(records[i]); ok {
				t.Errorf("investor context became a supporting document: %s", records[i].ID)
			}
			metadata[records[i].ID] = LivePaperMetadata{Links: []LiveEvidenceLink{{Kind: "independence_review",
				URL: records[i].URL, SourceURL: records[i].URL, ObservedAt: records[i].Date, Independent: new(true)}}}
		}
		candidate := LiveTopic{PapersCount: 1}
		candidate.Groups = 1
		liveAttachMarket(&candidate, &LiveChannel{Records: records}, "aquifer filtration")
		if len(liveChannelRecords(candidate.Web)) != 2 || len(candidate.EvidenceIDs) != 0 ||
			candidate.DocumentsCount != 0 || liveIndependentSources(candidate) != 1 {
			t.Errorf("fund sites must remain context without becoming independent confirmations: %+v", candidate)
		}
		independence := liveIndependence(candidate, nil, metadata, "2026-10-18")
		if independence.DistinctEvents != 0 || independence.IndependentImplementations != nil {
			t.Errorf("source independence became experimental confirmation: %+v", independence)
		}
		if field := liveMarketMaturity(candidate, liveMarketBarOf(candidate)).field(); len(field.Facts) != 0 ||
			!strings.Contains(strings.Join(field.Checks, "; "), "издателей 0 при пороге 2") {
			t.Errorf("investor domains inflated diagnostic market coverage: %+v", field)
		}
		id := liveEvidenceID(records[0])
		candidate.EvidenceIDs = []string{id}
		candidate.Market = &LiveMarket{Status: liveMarketMature, Known: true, Mature: true,
			Evidence: []LiveClaimEvidence{{Field: liveClaimMarket, SourceID: id,
				Quote: "Our thesis assumes an established aquifer filtration market", Verified: true}}}
		if field := liveMarketMaturity(candidate, liveAbsoluteBar()).field(); field.Known || field.Mature {
			t.Errorf("a saved investor quotation became a market assessment: %+v", field)
		}
		documents := map[string]StudyPaper{id: {ID: id, Title: records[0].Title, URL: records[0].URL}}
		if works, _ := liveCardInput(candidate, documents, nil); len(works) != 0 {
			t.Errorf("a saved investor document reached card source context: %v", works)
		}
	})

	t.Run("stored_corpus", func(t *testing.T) {
		observedAt := time.Date(2026, 10, 17, 12, 0, 0, 0, time.UTC)
		observed := observedAt.Format(time.RFC3339)
		metadata, err := json.Marshal(sourcedoc.Source{Version: 1, Kind: "analysis", Provider: "web",
			Genre: GenreInvestmentThesis, NativeID: "fund-thesis", URL: "https://fund.example/thesis",
			CanonicalID: "https://fund.example/thesis", ObservedAt: observedAt,
			Revision: strings.Repeat("0", 64), ContentHash: strings.Repeat("0", 64), HashAlgorithm: "sha256",
			AccessScope: "public", TextStatus: "article_text"})
		if err != nil {
			t.Fatal(err)
		}
		src := newLiveSources(nil)
		src.answerer = &fakeAnswerer{sources: corpusSources("investor", "own")}
		src.documents = &fakeDocuments{byID: map[string]*commonv1.Document{
			"investor": {Id: "investor", Title: "Aquifer filtration investment thesis", CreatedAt: observed,
				SourceMetadata: string(metadata)},
			"own": {Id: "own", Title: "Aquifer filtration laboratory notes", CreatedAt: observed},
		}}
		periods := livePeriods(time.Date(2026, 11, 1, 12, 0, 0, 0, time.UTC))
		channel := liveCorpusSearch(t.Context(), src, "owner", []string{"aquifer filtration"}, periods)
		if channel == nil || len(channel.Records) != 2 {
			t.Fatalf("imported context or ordinary owner document disappeared: %+v", channel)
		}
		for _, record := range channel.Records {
			if record.ID != "investor" {
				continue
			}
			if record.Genre != GenreInvestmentThesis || record.Trust != TrustLowered || record.Date != "" ||
				record.PublishedAt != "" || record.ObservedAt != observed || record.OriginalURL != "https://fund.example/thesis" {
				t.Errorf("import changed investor provenance or invented publication date: %+v", record)
			}
			if _, ok := liveRecordDocument(record); ok {
				t.Error("imported investor context became a supporting document")
			}
		}
		total := 0
		for _, count := range channel.Counts {
			if count != nil {
				total += *count
			}
		}
		if total != 1 {
			t.Errorf("undated investor context entered the publication timeline: %d", total)
		}
	})
}

func TestLiveInvestorSavedClaimsAndMarketQuota(t *testing.T) {
	fund := LiveSourceRecord{ID: "fund", URL: "https://fund.example/wishlist", Genre: GenreInvestmentThesis,
		Title: "Aquifer filtration: we want to fund a pilot", Date: "2026-10-17",
		PublisherType: PublisherVendor, Trust: TrustLowered}
	id := liveEvidenceID(fund)
	t.Run("saved_claims", func(t *testing.T) {
		for _, fromCard := range []bool{false, true} {
			topic := LiveTopic{Web: &LiveChannel{Records: []LiveSourceRecord{fund}}}
			claim := LiveClaimEvidence{Field: liveClaimAdoption, SourceID: id, Verified: true,
				Quote: "We want a pilot deployment"}
			var documents map[string]StudyPaper
			if fromCard {
				claim.Field = liveClaimExample
				topic.Card = &LiveCard{ExampleID: id, ExampleType: LiveEventPilot, Evidence: []LiveClaimEvidence{claim}}
				documents = map[string]StudyPaper{id: {ID: id, Title: fund.Title, URL: fund.URL, Date: fund.Date}}
			} else {
				topic.Adoption = &LiveAdoption{Code: liveAdoptionPilot, Rank: 3, Evidence: []LiveClaimEvidence{claim}}
			}
			// Старая карточка могла уже записать пилот, потеряв жанр при преобразовании документа.
			topic.Events = []LiveAdoptionEvent{{Source: liveEventCardSource, Type: LiveEventPilot, Verified: true,
				Quote: claim.Quote, Record: LiveSourceRecord{ID: id, URL: fund.URL}}}
			liveAssessTZ(&topic, documents)
			if topic.TZ.StageCode != 0 || topic.Adoption.Rank != 0 || len(topic.Events) != 0 ||
				(fromCard && topic.Card.ExampleType != "") {
				t.Errorf("saved investment context became adoption (card=%v): %+v", fromCard, topic)
			}
		}
	})
	t.Run("market_quota", func(t *testing.T) {
		pilot := LiveSourceRecord{ID: "actual-pilot", URL: "https://vendor.example/pilot",
			PublisherType: PublisherVendor, Trust: TrustLowered, Genre: GenreAnnouncement,
			Date: "2026-10-01", Title: "Aquifer filtration pilot deployed"}
		baseline := LiveTopic{}
		liveAttachMarket(&baseline, &LiveChannel{Records: []LiveSourceRecord{pilot}}, "aquifer filtration")
		records := make([]LiveSourceRecord, 1, liveMarketRecords+2)
		records[0] = pilot
		for i := range liveMarketRecords + 1 {
			context := fund
			context.ID = fmt.Sprintf("fund-%d", i)
			records = append(records, context)
		}
		topic := LiveTopic{}
		liveAttachMarket(&topic, &LiveChannel{Records: records}, "aquifer filtration")
		if !slices.Equal(topic.EvidenceIDs, baseline.EvidenceIDs) || topic.RecordsCount != 1 ||
			topic.DocumentsCount != 1 || len(liveChannelRecords(topic.Web)) != liveMarketRecords+1 {
			t.Errorf("investor context displaced the pilot or exceeded its own quota: %+v", topic)
		}
	})
}
