package worker

import (
	"strings"
	"testing"

	"github.com/example/source-harvester/internal/platform/harvest"
	"github.com/example/source-harvester/internal/platform/sourcedoc"
)

const germanPage = "Die Forschungsgruppe hat einen neuen Elektrolyseur mit einer Protonenaustauschmembran " +
	"entwickelt. Der Versuchsaufbau lief tausend Stunden lang ohne messbare Degradation der Membran. " +
	"Die Ergebnisse wurden in einem unabhängigen Labor bestätigt und sollen weiter untersucht werden."

const chinesePage = "研究小组开发了一种采用质子交换膜的新型电解槽。实验装置在恒定电流密度下连续运行了一千小时，" +
	"没有出现可测量的膜降解。该结果已由独立实验室验证，并将继续研究其在工业规模上的适用性和长期稳定性表现。"

const russianPage = "Исследовательская группа разработала электролизёр на протонообменной мембране нового состава. " +
	"Установка отработала тысячу часов при постоянной плотности тока без измеримой деградации мембраны. " +
	"Результат подтверждён независимой лабораторией и будет исследоваться дальше."

const englishPage = "The research group has built an electrolyser on a proton exchange membrane of a new " +
	"composition. The rig ran for a thousand hours at a constant current density with no measurable membrane " +
	"degradation. The result was confirmed by an independent laboratory and will be studied further."

func preparedFor(text string, sections []sourcedoc.Section) harvest.Prepared {
	prepared := harvest.Prepared{Text: text}
	prepared.Source.Sections = sections
	applyLanguage(&prepared)
	return prepared
}

func TestOriginalLanguagesArePreserved(t *testing.T) {
	for code, page := range map[string]string{"de": germanPage, "zh": chinesePage, "ru": russianPage, "en": englishPage} {
		prepared := preparedFor(page, nil)
		if prepared.Source.Language != code || prepared.Source.LanguageBasis == "" {
			t.Fatalf("%s original language was lost: %+v", code, prepared.Source)
		}
		reasons := strings.Join(prepared.Limitations, " | ")
		if strings.Contains(reasons, "unsupported_language") {
			t.Fatalf("%s original was excluded by language: %s", code, reasons)
		}
		if strings.Contains(reasons, "no_technology") || strings.Contains(reasons, "failed") {
			t.Fatalf("%s material was answered as an empty or broken extraction: %s", code, reasons)
		}
		if prepared.Text != page {
			t.Fatalf("%s material was altered", code)
		}
	}
}

func TestBilingualDocumentKeepsPredominantLanguage(t *testing.T) {
	body := strings.Repeat(germanPage+"\n", 4)
	text := body + "\n\n" + russianPage
	german := len([]rune(body))
	sections := []sourcedoc.Section{
		{Name: sourcedoc.SectionAbstract, Start: 0, End: german},
		{Name: sourcedoc.SectionIntroduction, Start: german + 2, End: len([]rune(text))},
	}
	prepared := preparedFor(text, sections)
	if prepared.Source.Language != "de" || prepared.Source.LanguageBasis != detectorVersion {
		t.Fatalf("an original was relabelled by its translated section: %+v", prepared.Source)
	}
	if prepared.Text != text || strings.Contains(strings.Join(prepared.Limitations, " "), "unsupported_language") {
		t.Fatal("a bilingual original was changed or excluded")
	}
}

func TestShortTextIsNotGuessed(t *testing.T) {
	prepared := preparedFor("Elektrolyseur", nil)
	if prepared.Source.Language != "" || prepared.Source.LanguageBasis != "" {
		t.Fatalf("a language was guessed from a title: %+v", prepared.Source)
	}
	if !strings.Contains(strings.Join(prepared.Limitations, " "), "language_undetermined") {
		t.Fatalf("an undetermined language was not recorded: %+v", prepared.Limitations)
	}
}

func TestProviderLanguageIsBelieved(t *testing.T) {
	for _, code := range []string{"zh", "ja", "ko", "ar", "hi", "es", "fr", "en"} {
		prepared := harvest.Prepared{Text: germanPage}
		prepared.Source.Language, prepared.Source.LanguageBasis = code, "provider language field"
		applyLanguage(&prepared)
		if prepared.Source.Language != code || prepared.Source.LanguageBasis != "provider language field" {
			t.Fatalf("the provider statement was overwritten: %+v", prepared.Source)
		}
		if len(prepared.Limitations) != 0 {
			t.Fatalf("provider language %s created an intake limitation: %v", code, prepared.Limitations)
		}
	}
}
