package contracts_test

import (
	"strconv"
	"testing"

	"github.com/example/db-parser/internal/platform/contracts"
	technologyv1 "github.com/example/db-parser/internal/platform/genproto/technology/v1"
	"google.golang.org/protobuf/proto"
)

func TestParserQueues(t *testing.T) {
	want := map[string]string{
		contracts.RouteParseEmail:  contracts.RouteParseEmail,
		contracts.RouteParseOffice: contracts.RouteParseOffice,
		contracts.RouteParsePDF:    contracts.RouteParsePDF,
		contracts.RouteParseOCR:    contracts.RouteParseOCR,
		contracts.RouteParseVLM:    contracts.RouteParseVLM,
		contracts.RouteParseArch:   contracts.RouteParseArch,
		contracts.RouteParseDB:     contracts.RouteParseDB,
		contracts.RouteChunk:       contracts.RouteChunk,
	}
	got := contracts.ParserQueues()
	if len(got) != len(want) {
		t.Fatalf("ParserQueues size = %d, want %d", len(got), len(want))
	}
	for k, v := range want {
		if got[k] != v {
			t.Errorf("ParserQueues[%q] = %q, want %q", k, got[k], v)
		}
	}
}

func TestParserQueuesIsolated(t *testing.T) {
	a := contracts.ParserQueues()
	a[contracts.RouteChunk] = "mutated"
	b := contracts.ParserQueues()
	if b[contracts.RouteChunk] != contracts.RouteChunk {
		t.Fatalf("ParserQueues is not isolated: got %q", b[contracts.RouteChunk])
	}
}

func TestRouteForMIME(t *testing.T) {
	cases := []struct {
		name, mime, file, want string
	}{
		{"zip mime", "application/zip", "bundle.bin", contracts.RouteParseArch},
		{"rar mime", "application/x-rar-compressed", "a.dat", contracts.RouteParseArch},
		{"7z mime", "application/x-7z-compressed", "a.dat", contracts.RouteParseArch},
		{"tar mime", "application/x-tar", "a.dat", contracts.RouteParseArch},
		{"gzip mime", "application/gzip", "a.dat", contracts.RouteParseArch},
		{"octet zip ext", "application/octet-stream", "a.zip", contracts.RouteParseArch},
		{"empty mime tgz ext", "", "a.tgz", contracts.RouteParseArch},
		{"octet tar ext", "application/octet-stream", "a.tar", contracts.RouteParseArch},
		{"pdf mime", "application/pdf", "doc.pdf", contracts.RouteParsePDF},
		{"pdf substring", "application/x-pdf", "doc.bin", contracts.RouteParsePDF},
		{"rfc822 mime", "message/rfc822", "mail.bin", contracts.RouteParseEmail},
		{"eml ext", "application/octet-stream", "mail.eml", contracts.RouteParseEmail},
		{"msg ext", "application/octet-stream", "mail.msg", contracts.RouteParseEmail},
		{"image png", "image/png", "scan.png", contracts.RouteParseOCR},
		{"image jpeg", "image/jpeg", "scan.jpg", contracts.RouteParseOCR},
		{"docx office", "application/vnd.openxmlformats-officedocument", "doc.docx", contracts.RouteParseOffice},
		{"sqlite mime", "application/vnd.sqlite3", "data.bin", contracts.RouteParseDB},
		{"sqlite x- mime", "application/x-sqlite3", "data", contracts.RouteParseDB},
		{"octet sqlite ext", "application/octet-stream", "corpus.sqlite", contracts.RouteParseDB},
		{"octet db ext", "application/octet-stream", "corpus.db", contracts.RouteParseDB},
		{"db ext but known mime not sqlite", "application/pdf", "weird.db", contracts.RouteParsePDF},
		{"unknown binary fallback", "application/octet-stream", "data.bin", contracts.RouteParseOffice},
		{"empty everything fallback", "", "", contracts.RouteParseOffice},
		{"uppercase mime normalised", "APPLICATION/PDF", "DOC.PDF", contracts.RouteParsePDF},
		{"zip ext but known mime not archive", "application/pdf", "weird.zip", contracts.RouteParsePDF},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := contracts.RouteForMIME(tc.mime, tc.file); got != tc.want {
				t.Fatalf("RouteForMIME(%q, %q) = %q, want %q", tc.mime, tc.file, got, tc.want)
			}
		})
	}
}

func TestContractConstants(t *testing.T) {
	pairs := []struct {
		got, want string
	}{
		{contracts.ExchangeIngestion, "ingestion"},
		{contracts.ExchangeEvents, "events"},
		{contracts.ExchangeDLX, "ingestion.dlx"},
		{contracts.QueueDead, "dead"},
		{contracts.StatusUploaded, "uploaded"},
		{contracts.StatusQueued, "queued"},
		{contracts.StatusParsing, "parsing"},
		{contracts.StatusParsed, "parsed"},
		{contracts.StatusChunking, "chunking"},
		{contracts.StatusIndexed, "indexed"},
		{contracts.StatusFailed, "failed"},
	}
	for _, p := range pairs {
		if p.got != p.want {
			t.Errorf("constant = %q, want %q", p.got, p.want)
		}
	}
}

func TestTechnologyContractRoundTrip(t *testing.T) {
	mention := &technologyv1.TechnologyMention{
		MentionId:        "mention-1",
		Technology:       &technologyv1.TechnologyRef{TechnologyId: "tech-1"},
		SourceRevisionId: "revision-1",
		ExtractorVersion: "extractor-v1",
		SchemaVersion:    "technology-extraction-v1",
		Name:             "Адаптивная маршрутизация поиска",
		NameKind:         "descriptive",
		Role:             "proposed_contribution",
		Problem:          "Выбор способа поиска",
		Mechanism:        "Маршрутизация по характеристикам запроса",
		Inputs:           []string{"Запрос"},
		Outputs:          []string{"Выбранный способ поиска"},
		ExtractionStatus: "complete",
	}
	for i := range 3 {
		id := "evidence-" + strconv.Itoa(i)
		mention.Evidence = append(mention.Evidence, &technologyv1.Evidence{
			EvidenceId: id, MentionId: mention.GetMentionId(), SourceRevisionId: mention.GetSourceRevisionId(),
			Locator: &technologyv1.Locator{Section: "3", Page: proto.Int32(int32(i + 1)),
				StartChar: int32(1000 + i), EndChar: int32(1100 + i)},
			Quote: "Точная подстрока сохранённого источника " + id,
		})
		mention.FieldEvidence = append(mention.FieldEvidence,
			&technologyv1.FieldEvidence{Field: "mechanism", EvidenceIds: []string{id}})
	}
	result := &technologyv1.TechnologyExtractionResult{
		SchemaVersion: "technology-extraction-v1", SourceRevisionId: "revision-1",
		ExtractionVersion: "extractor-v1",
		Coverage:          &technologyv1.ExtractionCoverage{Status: "complete", ProcessedSections: 12, TotalSections: 12},
		Technologies:      []*technologyv1.TechnologyMention{mention},
	}
	raw, err := proto.Marshal(result)
	if err != nil {
		t.Fatal(err)
	}
	var back technologyv1.TechnologyExtractionResult
	if err = proto.Unmarshal(raw, &back); err != nil {
		t.Fatal(err)
	}
	restored := back.GetTechnologies()[0]
	if len(restored.GetEvidence()) != 3 || restored.GetTechnology().GetTechnologyId() != "tech-1" ||
		restored.GetEvidence()[2].GetQuote() != "Точная подстрока сохранённого источника evidence-2" ||
		restored.GetEvidence()[0].GetLocator().GetPage() != 1 || back.GetCoverage().GetStatus() != "complete" {
		t.Fatalf("technology did not survive the wire: %+v", restored)
	}

	// Направление держит технологии и поддерживающие их работы в разных полях разных типов;
	// ни один список не принимает ссылки другого.
	direction := &technologyv1.TrendDirection{
		DirectionId:     "direction-1",
		Technologies:    []*technologyv1.TechnologyRef{{TechnologyId: "tech-1"}},
		SupportingWorks: []*technologyv1.WorkRef{{CanonicalWorkId: "work-1"}},
		Metrics: []*technologyv1.Metric{{Name: "share", Status: "unavailable",
			ReasonCodes: []string{"denominator_unknown"}}},
	}
	if direction.GetMetrics()[0].Value != nil {
		t.Fatal("an unavailable metric must stay absent, not zero")
	}
}
