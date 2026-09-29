package main

import (
	"os"
	"slices"
	"strconv"
	"strings"
	"testing"
)

func TestCNRecordFromRealPublication(t *testing.T) {
	raw, err := os.ReadFile("testdata/cn-122065940-a.html")
	if err != nil {
		t.Fatal(err)
	}
	row, err := cnRecord(raw, "CN122065940A")
	if err != nil {
		t.Fatal(err)
	}
	if row.Date != "2026-05-19" || row.Filing != "2026-01-07" || row.KindCode != "A" || row.Language != "zh" {
		t.Fatalf("wrong publication metadata: %s %s %s %s", row.Date, row.Filing, row.KindCode, row.Language)
	}
	if !strings.Contains(row.Title, "大语言模型") || !strings.Contains(row.Text, "知识抽取") || row.TextStatus != patentFullText {
		t.Fatalf("Chinese full text not recovered: %q %s", row.Title, row.TextStatus)
	}
	if row.FullText == nil || len(row.FullText.Claims) != 7 || len(row.FullText.Sections) != 3 {
		t.Fatalf("sections or nested claims duplicated/missing: %+v", row.FullText)
	}
	runes := []rune(row.Text)
	for i, claim := range row.FullText.Claims {
		if claim.Number != i+1 || claim.End > len(runes) || claim.Start >= claim.End ||
			!strings.HasPrefix(string(runes[claim.Start:claim.End]), string(rune('1'+i))+".") {
			t.Fatalf("claim cannot be reproduced exactly: %+v", claim)
		}
	}
	if row.Priority != "" || !strings.HasPrefix(row.FamilyID, "application:CN") ||
		!strings.Contains(strings.Join(row.Limitations, " "), "patent_images_not_transcribed") {
		t.Fatal("unverified priority, worldwide family or image-only equations not disclosed")
	}
	if _, err := cnRecord(raw, "CN119961390A"); err == nil {
		t.Fatal("wrong publication accepted")
	}
	withoutDate := strings.ReplaceAll(string(raw), `itemprop="publicationDate"`, `itemprop="unavailableDate"`)
	if _, err := cnRecord([]byte(withoutDate), row.ID); err == nil {
		t.Fatal("missing publication date filled from unrelated metadata")
	}
}

func TestCNSitemapSelectsBoundedValidatedPublicationPaths(t *testing.T) {
	raw := []byte(`<ul><li><a href='2026-W21.html'>week</a></li>
<li><a href='2026-W21-p1.html'>part</a></li><li><a href='2024-W21.html'>old</a></li>
<li><a href='https://example.org/2026-W21.html'>external</a></li>
<li>CN122065940A - Large language model knowledge extraction :
<a href='https://patents.google.com/patent/CN122065940A/zh'>zh</a></li>
<li>CN122065940A - Large language model knowledge extraction : duplicate</li>
<li>US12345678B2 - Large language model : en</li><li>CN122065941A - Chair : zh</li></ul>`)
	files, rows := cnSitemap(raw, "2026-05-01", "2026-09-13", []string{"large language model"})
	if !slices.Equal(files, []string{"2026-W21.html", "2026-W21-p1.html"}) ||
		len(rows) != 1 || rows[0].ID != "CN122065940A" {
		t.Fatalf("unsafe or incorrect discovery: %v %+v", files, rows)
	}
	if !exportMatches(record{ID: "CN122065940A", Source: cnSource, Title: "中文"}, "country:CN") ||
		exportMatches(record{ID: "US12345678", Source: patentSource}, "country:CN") {
		t.Fatal("jurisdiction scope depends on English text or includes US")
	}
}

func TestCNPartialSectionsStayReadable(t *testing.T) {
	raw, err := os.ReadFile("testdata/cn-122065940-a.html")
	if err != nil {
		t.Fatal(err)
	}
	for _, tc := range []struct {
		name, status string
		missing      []string
	}{
		{"without abstract", patentFullText, []string{cnAbstract}},
		{"without claims", patentFullText, []string{sectionClaims}},
		{"abstract only", "abstract_only", []string{cnDescription, sectionClaims}},
		{"metadata only", metadataOnlyText, []string{cnAbstract, cnDescription, sectionClaims}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			input := string(raw)
			for _, name := range tc.missing {
				input = strings.ReplaceAll(input, `itemprop="`+name+`"`, `itemprop="unavailable"`)
			}
			row, parseErr := cnRecord([]byte(input), "CN122065940A")
			if parseErr != nil {
				t.Fatal(parseErr)
			}
			if row.TextStatus != tc.status || row.FullText == nil || !slices.Equal(row.FullText.Missing, tc.missing) {
				t.Fatalf("partial original lost its text status or missing sections: %s, %+v", row.TextStatus, row.FullText)
			}
			if tc.status == metadataOnlyText {
				if row.Text != "" || row.Language != "" {
					t.Fatal("missing text invented a body or a verified language")
				}
			} else if row.Text == "" || row.Language != "zh" ||
				!strings.Contains(strings.Join(row.Limitations, " "), "patent_sections_partial") {
				t.Fatal("readable Chinese sections or their limitations were lost")
			}
		})
	}
}

func TestCNLanguageComesFromPatentSections(t *testing.T) {
	raw, err := os.ReadFile("testdata/cn-122065940-a.html")
	if err != nil {
		t.Fatal(err)
	}
	for _, substitute := range []string{`lang="EN"`, ""} {
		input := strings.ReplaceAll(string(raw), `lang="ZH"`, substitute)
		if _, err := cnRecord([]byte(input), "CN122065940A"); err == nil {
			t.Fatalf("/zh URL was trusted over unverified patent content: %s", substitute)
		}
	}
	input := strings.ReplaceAll(string(raw), `lang="ZH"`, `lang="zh-Hans"`)
	if row, err := cnRecord([]byte(input), "CN122065940A"); err != nil || row.Language != "zh" {
		t.Fatalf("Chinese language variant rejected: %s, %v", row.Language, err)
	}
}

func TestCNGrantedPublicationMetadata(t *testing.T) {
	for _, tc := range []struct {
		file, id, kind, day, filing string
		claims                      int
	}{
		{"testdata/cn-117489642-b.html", "CN117489642B", "B", "2026-05-19", "2023-10-31", 8},
		{"testdata/cn-224265727-u.html", "CN224265727U", "U", "2026-05-22", "2025-03-24", 11},
	} {
		t.Run(tc.id, func(t *testing.T) {
			raw, err := os.ReadFile(tc.file)
			if err != nil {
				t.Fatal(err)
			}
			row, err := cnRecord(raw, tc.id)
			if err != nil {
				t.Fatal(err)
			}
			if row.ID != tc.id || row.KindCode != tc.kind || row.Date != tc.day || row.Filing != tc.filing ||
				row.Language != "zh" || row.TextStatus != patentFullText || row.FullText == nil ||
				len(row.FullText.Sections) != 3 || len(row.FullText.Claims) != tc.claims {
				t.Fatalf("grant identity/date/text was lost: %s %s %s %s %s", row.ID, row.KindCode,
					row.Date, row.Filing, row.Language)
			}
			runes := []rune(row.Text)
			for i, claim := range row.FullText.Claims {
				if claim.Number != i+1 || claim.Start >= claim.End || claim.End > len(runes) ||
					!strings.HasPrefix(string(runes[claim.Start:claim.End]), strconv.Itoa(i+1)+".") {
					t.Fatalf("grant claim cannot be reproduced: %+v", claim)
				}
			}
			for name, input := range map[string]string{
				"wrong number": strings.Replace(string(raw), `name="citation_patent_number" content="CN:`,
					`name="citation_patent_number" content="CN:0`, 1),
				"missing number": strings.Replace(string(raw), `name="citation_patent_number"`,
					`name="missing_patent_number"`, 1),
				"missing issue date": strings.Replace(string(raw), `scheme="issue"`, `scheme="dateSubmitted"`, 1),
				"disagreeing date": strings.Replace(string(raw), `content="`+tc.day+`" scheme="issue"`,
					`content="1999-01-01" scheme="issue"`, 1),
			} {
				if _, err := cnRecord([]byte(input), tc.id); err == nil {
					t.Fatalf("invalid grant metadata accepted: %s", name)
				}
			}
		})
	}
}

func TestCNApplicationDoesNotUseGrantIssueDate(t *testing.T) {
	raw, err := os.ReadFile("testdata/cn-122065940-a.html")
	if err != nil {
		t.Fatal(err)
	}
	input := strings.Replace(string(raw), `<meta name="DC.date" content="2026-05-19">`,
		`<meta name="DC.date" content="2026-05-19" scheme="issue">`, 1)
	if _, err := cnRecord([]byte(input), "CN122065940A"); err == nil {
		t.Fatal("an application used a grant issue date instead of its publication metadata")
	}
}
