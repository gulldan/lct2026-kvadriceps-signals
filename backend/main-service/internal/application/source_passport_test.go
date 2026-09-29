package application

import (
	"encoding/json/v2"
	"testing"
)

func TestClassifySource(t *testing.T) {
	for _, tc := range []struct {
		url       string
		publisher string
		trust     string
	}{
		{"https://clinicaltrials.gov/study/NCT01", PublisherRegistry, TrustHigh},
		{"https://www.ema.europa.eu/en/medicines/human/EPAR/zumrad", PublisherRegulator, TrustHigh},
		{"https://news.mit.edu/2026/energy", PublisherUniversity, TrustHigh},
		{"https://journals.plos.org/plosone/article", PublisherJournal, TrustHigh},
		{"https://arxiv.org/abs/2601.00001", PublisherPreprint, TrustMedium},
		{"https://developer.nvidia.com/blog/x", PublisherVendor, TrustMedium},
		{"https://news.ycombinator.com/item?id=1", PublisherAggregator, TrustLowered},
		{"https://github.com/org/repo", PublisherCodeHost, TrustLowered},
		{"https://habr.com/ru/articles/1/", PublisherBlog, TrustLowered},
		{"https://www.prnewswire.com/news-releases/x", PublisherMedia, TrustLowered},
		{"https://ppo.cnipa.gov.cn/x", PublisherPatentOffice, TrustHigh},
		{"https://example.unknown-host.test/a", PublisherUnknown, TrustLowered},
		{"", PublisherUnknown, TrustLowered},
	} {
		got := ClassifySource(tc.url)
		if got.Publisher != tc.publisher || got.Trust != tc.trust {
			t.Errorf("%s: получили %s/%s, ожидали %s/%s",
				tc.url, got.Publisher, got.Trust, tc.publisher, tc.trust)
		}
		if got.TrustBasis == "" {
			t.Errorf("%s: пустое основание доверия", tc.url)
		}
	}
}

// Субдомен обязан наследовать классификацию родительского домена, иначе
// каждый новый поддомен молча попадёт в непроверенные.
func TestClassifySourceInheritsParentDomain(t *testing.T) {
	parent := ClassifySource("https://frontiersin.org/journals/materials/rss")
	child := ClassifySource("https://www.frontiersin.org/journals/materials/articles/10.3389")
	if parent.Publisher != child.Publisher || parent.Trust != child.Trust {
		t.Fatalf("субдомен классифицирован иначе: %+v против %+v", child, parent)
	}
}

func TestClassifySourceDefaultsToLowered(t *testing.T) {
	if got := ClassifySource("https://some-blog.example/post"); got.Trust != TrustLowered {
		t.Fatalf("неизвестный домен получил доверие %q", got.Trust)
	}
}

// Паспорт обязан проставляться каждой записи канала: иначе в выдаче окажутся
// ссылки без типа и уровня доверия, а это прямое требование ТЗ.
func TestStampPassportsFillsEveryRecord(t *testing.T) {
	records := []LiveSourceRecord{
		{URL: "https://news.mit.edu/2026/a"},
		{URL: "", CanonicalURL: "https://github.com/org/repo"},
		{URL: "https://unlisted.example/x"},
	}
	stampPassports(records)
	for i, r := range records {
		if r.PublisherType == "" || r.Trust == "" || r.TrustBasis == "" {
			t.Fatalf("запись %d без паспорта: %+v", i, r)
		}
	}
	if records[0].Trust != TrustHigh {
		t.Errorf("университет получил доверие %q", records[0].Trust)
	}
	if records[1].PublisherType != PublisherCodeHost {
		t.Errorf("канонический адрес не разобран: %q", records[1].PublisherType)
	}
}

func TestStampEventPassportsLowersSelfPublished(t *testing.T) {
	events := []StudyEvent{
		{URL: "https://github.com/org/repo"},
		{URL: "https://news.ycombinator.com/item?id=1"},
	}
	stampEventPassports(events)
	for i, e := range events {
		if e.Trust != TrustLowered || e.TrustBasis == "" {
			t.Fatalf("событие %d: доверие %q, основание %q", i, e.Trust, e.TrustBasis)
		}
	}
}

func TestStampPaperPassportsSeparatesPreprintFromJournal(t *testing.T) {
	papers := []StudyPaper{
		{ID: "arxiv:1", URL: "https://arxiv.org/abs/2601.1"},
		{ID: "openalex:1", URL: "https://doi.org/10.1000/x"},
		{ID: "meta-only", URL: ""},
	}
	metadata := map[string]LivePaperMetadata{"meta-only": {SourceURL: "https://journals.plos.org/plosone/article"}}
	stampPaperPassports(papers, metadata)
	if metadata["arxiv:1"].PublisherType != PublisherPreprint {
		t.Errorf("препринт: %q", metadata["arxiv:1"].PublisherType)
	}
	if metadata["openalex:1"].Trust != TrustMedium {
		t.Errorf("издательский DOI: %q", metadata["openalex:1"].Trust)
	}
	if metadata["meta-only"].PublisherType != PublisherJournal {
		t.Errorf("запасной адрес из метаданных не использован: %q", metadata["meta-only"].PublisherType)
	}
}

// Жанр не теряется при объединении рыночных записей с научными работами;
// наличие издательского адреса само по себе не доказывает рецензирование.
func TestLiveAnswerPassportsPreserveSourceGenre(t *testing.T) {
	const recordID = liveRecordPrefix + "release"
	papers := []StudyPaper{
		{ID: "openalex:doi", URL: "https://doi.org/10.1000/x", Title: "Speech detection study"},
		{ID: "release", URL: "https://news.mit.edu/press-release/speech", Title: "Speech pilot"},
		{ID: "arxiv:1", URL: "https://arxiv.org/abs/2601.1", Title: "Speech detection study"},
		{ID: recordID, URL: "https://example.org/material", Title: "Speech pilot"},
		{ID: "fallback", URL: "https://example.org/news/speech", Title: "Speech pilot"},
	}
	metadata := map[string]LivePaperMetadata{
		"openalex:doi": {Category: "Example Research Journal"},
		"arxiv:1":      {Category: "cs.AI"},
		"fallback":     {PublisherType: PublisherMedia, Trust: TrustMedium},
	}
	stampPaperPassports(papers[:3], metadata)
	candidates := []LiveTopic{{Web: &LiveChannel{Records: []LiveSourceRecord{{
		ID: "release", URL: "https://example.org/material", Genre: GenreAnnouncement,
		PublisherType: PublisherVendor, Trust: TrustLowered, TrustBasis: "пресс-релиз поставщика",
	}}}}}
	livePrepareAnswerMetadata(candidates, papers, metadata)
	passports := livePaperPassports(papers, metadata)
	for _, id := range []string{"release", recordID} {
		if got := passports[id]; got.Genre != GenreAnnouncement || got.Trust != TrustLowered {
			t.Fatalf("жанр или доверие пресс-релиза потеряны: %s: %+v", id, got)
		}
	}
	if got := passports["openalex:doi"]; got.Genre != GenreUnknown || got.Trust != TrustMedium ||
		got.PublisherType != PublisherUnknown || got.SourceName != "Example Research Journal" ||
		got.DiscoverySource != "openalex" || got.Language != "unknown" {
		t.Fatalf("издательский адрес принят за рецензирование: %+v", got)
	}
	if got := passports["arxiv:1"]; got.SourceName != "arXiv" || got.PublisherType != PublisherPreprint ||
		got.Language != "en" || got.DiscoverySource != "arxiv" {
		t.Fatalf("площадка препринта спутана с тематикой: %+v", got)
	}
	if got := passports["fallback"].Genre; got != GenreNews {
		t.Fatalf("жанр по адресу не восстановлен: %s", got)
	}
}

// Паспорт обязан сообщать о неизвестном языке даже при наличии русского
// пересказа: интерпретация модели не определяет язык оригинала или издание.
func TestLivePaperPassportsSerializeUnknownLanguage(t *testing.T) {
	paper := StudyPaper{
		ID: "openalex:10.1109/example", URL: "https://doi.org/10.1109/example",
		Title: "User Centric Explainable Machine Learning for Financial Fraud Detection",
	}
	metadata := map[string]LivePaperMetadata{paper.ID: {
		Abstract: "An explainable system for financial fraud detection.",
		Digest:   "Система обнаружения финансового мошенничества.", DigestModel: "example-model",
	}}
	papers := []StudyPaper{paper}
	livePrepareAnswerMetadata(nil, papers, metadata)
	encoded, err := json.Marshal(livePaperPassports(papers, metadata))
	if err != nil {
		t.Fatal(err)
	}
	var response map[string]map[string]string
	if err := json.Unmarshal(encoded, &response); err != nil {
		t.Fatal(err)
	}
	passport := response[paper.ID]
	if passport["language"] != "unknown" || passport["language_basis"] == "" ||
		passport["publisher_type"] != PublisherUnknown || passport["trust"] != TrustMedium ||
		passport["genre"] != GenreUnknown || passport["discovery_source"] != "openalex" {
		t.Fatalf("неполный паспорт либо неизвестные признаки выданы за измеренные: %s", encoded)
	}
	if _, invented := passport["source_name"]; invented || passport["digest"] != metadata[paper.ID].Digest {
		t.Fatalf("неизвестное издание выдумано либо потерян пересказ: %s", encoded)
	}
	if metadata[paper.ID].Language != "" {
		t.Fatal("неизвестность в ответе подменила исходные метаданные")
	}
}

// TestLivePaperLanguageNamesOnlyWhatItMeasured: кириллица, иероглифы и площадка с английскими метаданными
// решают однозначно, а одна латиница оставляет поле пустым вместо неизмеренного утверждения.
func TestLivePaperLanguageNamesOnlyWhatItMeasured(t *testing.T) {
	for _, row := range []struct {
		name  string
		paper StudyPaper
		meta  LivePaperMetadata
		want  string
	}{
		{"русский заголовок", StudyPaper{Title: "Обнаружение слабых сигналов в патентных данных"},
			LivePaperMetadata{}, "ru"},
		{"китайский заголовок", StudyPaper{Title: "边缘设备上的大模型压缩方法"}, LivePaperMetadata{}, "zh"},
		{"arXiv без подсказок", StudyPaper{Title: "Sparse Attention", URL: "https://arxiv.org/abs/2601.01"},
			LivePaperMetadata{}, "en"},
		{"латиница на неизвестной площадке", StudyPaper{Title: "Sparse Attention", URL: "https://example.org/a"},
			LivePaperMetadata{}, ""},
		{"латиница у адреса идентификатора", StudyPaper{Title: "Sparse Attention for Long Context",
			URL: "https://doi.org/10.1016/j.aej.2026.01.016"}, LivePaperMetadata{}, ""},
		{"язык пришёл от источника", StudyPaper{Title: "Sparse Attention"},
			LivePaperMetadata{Language: "de", LanguageBasis: "заявлен лентой"}, "de"},
	} {
		code, basis := livePaperLanguage(row.paper, row.meta)
		if code != row.want {
			t.Fatalf("%s: язык %q, ожидался %q", row.name, code, row.want)
		}
		if code != "" && basis == "" {
			t.Fatalf("%s: язык назван без основания", row.name)
		}
	}
}

// TestIndustryMediaAreNotUnknownPublishers: эталон организаторов на 87 % из отраслевых медиа, но жанр всё
// равно опускает доверие — пресс-релиз в отраслевом издании остаётся пресс-релизом.
func TestIndustryMediaAreNotUnknownPublishers(t *testing.T) {
	for _, host := range []string{
		"https://techcrunch.com/2026/09/01/startup-raises",
		"https://www.datacenterdynamics.com/en/news/item",
		"https://www.pymnts.com/news/item",
		"https://spectrum.ieee.org/robot-arm",
		"https://www.tomshardware.com/news/chip",
		"https://thenewstack.io/article",
	} {
		passport := ClassifySource(host)
		if passport.Publisher != PublisherMedia || passport.Trust != TrustMedium {
			t.Fatalf("отраслевое медиа классифицировано как %q/%q: %s",
				passport.Publisher, passport.Trust, host)
		}
	}
	release, _ := liveApplyGenre(ClassifySource("https://techcrunch.com/press-release/x"),
		"https://techcrunch.com/press-release/x", "Company announces a new chip")
	if release.Trust != TrustLowered {
		t.Fatalf("пресс-релиз в отраслевом издании сохранил среднее доверие: %+v", release)
	}
}
