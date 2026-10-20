package application

import (
	"encoding/json/v2"
	"net/http"
	"net/http/httptest"
	"slices"
	"testing"
)

func TestLiveScientificWebRecordsReachSourceCoverage(t *testing.T) {
	periods := scholarPeriods()
	records := []LiveSourceRecord{
		{ID: "https://doi.org/10.1234/delivery", DOI: "10.1234/delivery", Source: "Crossref: drug delivery",
			Title: "Lipid nanoparticles delivery study", URL: "https://doi.org/10.1234/delivery", Genre: "article"},
		{ID: "https://doi.org/10.1234/targeting", DOI: "10.1234/targeting", Source: "Europe PMC: oncology",
			Title: "Lipid nanoparticles targeting study", URL: "https://pubmed.ncbi.nlm.nih.gov/123/", Genre: "article"},
		{ID: "https://doi.org/10.1234/preprint", DOI: "10.1234/preprint", Source: "Crossref: preprints",
			Title: "Lipid nanoparticles preliminary experiment", URL: "https://doi.org/10.1234/preprint", Genre: "preprint"},
		{ID: "https://doi.org/10.1234/data", DOI: "10.1234/data", Source: "DataCite: drug delivery",
			Title: "Lipid nanoparticles measurements", URL: "https://doi.org/10.1234/data", Genre: "dataset"},
		{ID: "https://zenodo.org/records/1", Source: "Zenodo: междисциплинарные данные",
			Title: "Lipid nanoparticles simulation code", URL: "https://zenodo.org/records/1", Genre: "software"},
		{ID: "https://clinicaltrials.gov/study/NCT1", Source: "ClinicalTrials.gov: клинические исследования",
			Title: "Lipid nanoparticles trial registry", URL: "https://clinicaltrials.gov/study/NCT1", Genre: "clinical_trial"},
		{ID: "https://doi.org/10.1234/biorxiv", DOI: "10.1234/biorxiv", Source: "bioRxiv: архив препринтов",
			Title: "Lipid nanoparticles early experiment", URL: "https://doi.org/10.1234/biorxiv", Genre: "preprint"},
		{ID: "https://europepmc.org/article/PPR/1", Source: "Europe PMC: preprints",
			Title: "Lipid nanoparticles preprint", URL: "https://europepmc.org/article/PPR/1", Genre: "preprint"},
		{ID: "https://news.example/news/1", Source: "Example News",
			Title: "Lipid nanoparticles company news", URL: "https://news.example/news/1"},
		{ID: "https://unverified.example/article/1", Source: "Unverified source",
			Title: "Lipid nanoparticles unsourced claim", URL: "https://unverified.example/article/1", Genre: "article"},
	}
	for i := range records {
		records[i].Date = "2026-08-14"
	}
	body, err := json.Marshal(LiveChannel{Source: "web", Counts: make([]*int, len(periods)), Records: records})
	if err != nil {
		t.Fatal(err)
	}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) { _, _ = w.Write(body) }))
	t.Cleanup(server.Close)
	src := &liveSources{http: server.Client()}
	channel, err := src.queryChannelAt(t.Context(), server.URL, 1<<20, []byte(`{}`), len(periods))
	if err != nil {
		t.Fatal(err)
	}
	for i, record := range channel.Records {
		want := records[i].Genre
		if want == "" {
			want = GenreNews
		}
		if record.Genre != want {
			t.Fatalf("явный жанр потерян или эвристика не сработала: %s: %s != %s", record.ID, record.Genre, want)
		}
	}
	topic := LiveTopic{ID: "ngram:lipid-nanoparticles", Name: "Lipid nanoparticles", Web: channel,
		Query: LiveQuery{Terms: []string{"lipid nanoparticles"}}}
	documents := liveChannelDocuments(topic)
	if len(documents) != len(records) {
		t.Fatalf("научные записи потеряны после веб-канала: %d", len(documents))
	}
	for _, document := range documents {
		topic.EvidenceIDs = append(topic.EvidenceIDs, document.ID)
	}
	if !slices.Equal(documents[0].Authors, documents[1].Authors) || documents[0].Authors[0] != "research:unknown-authors" {
		t.Fatal("названия каталогов стали независимыми авторскими коллективами")
	}
	// Одна работа приходит через нативный научный поиск и веб-канал, с разным оформлением идентификатора.
	duplicate := StudyPaper{ID: "pubmed:duplicate", CanonicalID: "https://doi.org/10.1234/DELIVERY",
		Title: "Lipid nanoparticles alternate catalogue title", Date: "2026-08-14", URL: "https://pubmed.ncbi.nlm.nih.gov/456/"}
	documents = append(documents, duplicate)
	topic.PaperIDs = []string{duplicate.ID}
	topic.Qualification = liveCoverageQualification(append([]string{duplicate.ID}, topic.EvidenceIDs...))
	metadata := map[string]LivePaperMetadata{duplicate.ID: {Genre: "article"}}
	livePrepareAnswerMetadata([]LiveTopic{topic}, documents, metadata)
	passports := livePaperPassports(documents, metadata)
	if passports[documents[0].ID].SourceName != "Crossref" || passports[documents[1].ID].SourceName != "Europe PMC" {
		t.Fatal("научное происхождение потеряно в видимом паспорте публикации")
	}
	// Происхождение должно пережить сохранение снимка; идентификатор веб-записи содержит только цифровой адрес.
	saved, err := json.Marshal(metadata)
	if err != nil {
		t.Fatal(err)
	}
	metadata = nil
	if err := json.Unmarshal(saved, &metadata); err != nil {
		t.Fatal(err)
	}
	input := liveClassifierInput("medicine", "test", []LiveTopic{topic}, documents, metadata)
	if got := input.Candidates[0].journals; len(got) != 6 || got[1].source != "Crossref" || got[2].source != "Europe PMC" {
		t.Fatalf("научные публикации потеряны или подменены данными, реестрами, новостями: %+v", got)
	}
	journals := liveJournalCandidateEvidence(input.Candidates[0], periods)
	if journals == nil || journals.Total != 5 ||
		!(&WeakSignalEvidence{Window: &LivePhraseWindow{}, Journals: journals}).journalCoverage() {
		t.Fatalf("одна работа посчитана дважды или журнальное покрытие потеряно: %+v", journals)
	}
}
