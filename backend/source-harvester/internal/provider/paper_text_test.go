package provider

import (
	"strings"
	"testing"
	"unicode/utf8"

	"github.com/example/source-harvester/internal/platform/sourcedoc"
)

func TestPaperAbstractEvidence(t *testing.T) {
	item := strings.Replace(crItem("jats", "[[2025,3]]"), `"type":`,
		`"abstract":"<jats:p>Нейро<jats:italic>сеть</jats:italic> выбирает H<jats:sub>2</jats:sub>.</jats:p>`+
			`<jats:p>A &amp; B.</jats:p>","type":`, 1)
	crossref, err := decodeCrossref([]byte(crPage("", item)), testObserved(), testScope(), "*")
	if err != nil || len(crossref.Records) != 1 {
		t.Fatalf("Crossref: %v", err)
	}
	if !strings.HasSuffix(crossref.Records[0].Text, "Нейросеть выбирает H2. A & B.") {
		t.Fatal("JATS inline markup changed the scientific text")
	}
	atom := atomFeed(0, 1, atomWork("2501.00001v1", "2025-01-01", "2025-01-01"))
	arxiv, err := decodeArxiv([]byte(atom), testObserved(), testScope(), 0)
	if err != nil || len(arxiv.Records) != 1 {
		t.Fatalf("arXiv: %v", err)
	}
	for _, row := range []Record{crossref.Records[0], arxiv.Records[0]} {
		if row.TextStatus != "abstract_only" || len(row.Source.Sections) != 1 {
			t.Fatalf("abstract presented as a full paper: %+v", row.Source.Sections)
		}
		section := row.Source.Sections[0]
		if err := sourcedoc.ValidateSections(row.Source.Sections, utf8.RuneCountInString(row.Text)); err != nil {
			t.Fatal(err)
		}
		_, abstract, _ := strings.Cut(row.Text, "\n\n")
		if section.Name != sourcedoc.SectionAbstract || string([]rune(row.Text)[section.Start:section.End]) != abstract {
			t.Fatal("abstract evidence does not address the saved text")
		}
	}
	if _, status, sections := paperText("Название", ""); status != "title_only" || len(sections) != 0 {
		t.Fatal("missing abstract was fabricated")
	}
}
