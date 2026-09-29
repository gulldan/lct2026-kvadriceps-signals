package provider

import (
	"bytes"
	"encoding/xml"
	"errors"
	"fmt"
	"strings"
	"unicode/utf8"

	"github.com/example/source-harvester/internal/platform/sourcedoc"
)

type jatsBlock struct {
	XMLName xml.Name
	Title   string `xml:"title"`
	Inner   string `xml:",innerxml"`
}

type jatsPaper struct {
	Text     string
	Language string
	Sections []sourcedoc.Section
	Anchors  []sourcedoc.Anchor
}

// Разделы верхнего уровня JATS включают подразделы, поэтому смещения не пересекаются; исходный XML — в манифесте.
func parseJATSPaper(raw []byte, doi, title string) (jatsPaper, error) {
	var article struct {
		XMLName  xml.Name `xml:"article"`
		Language string   `xml:"http://www.w3.org/XML/1998/namespace lang,attr"`
		Meta     struct {
			IDs []struct {
				Type  string `xml:"pub-id-type,attr"`
				Value string `xml:",chardata"`
			} `xml:"article-id"`
			Abstracts []jatsBlock `xml:"abstract"`
		} `xml:"front>article-meta"`
		Body struct {
			Blocks []jatsBlock `xml:",any"`
		} `xml:"body"`
		Back struct {
			Blocks []jatsBlock `xml:",any"`
		} `xml:"back"`
	}
	if len(raw) > maxResponseBytes {
		return jatsPaper{}, ErrResponseTooLarge
	}
	if bytes.Contains(raw, []byte("<!DOCTYPE")) || bytes.Contains(raw, []byte("<!ENTITY")) {
		return jatsPaper{}, errors.New("JATS DTD/entity declarations are not accepted")
	}
	// #nosec G709 -- bounded XML, DTD/entities refused, fixed data-only struct; Go resolves no external entities.
	if err := xml.Unmarshal(raw, &article); err != nil {
		return jatsPaper{}, errors.New("invalid JATS article XML")
	}
	matched := false
	for _, id := range article.Meta.IDs {
		if id.Type == "doi" && strings.EqualFold(strings.TrimSpace(id.Value), doi) {
			matched = true
		}
	}
	if !matched {
		return jatsPaper{}, errors.New("JATS article DOI does not match the collected work")
	}
	if len(article.Body.Blocks) == 0 {
		return jatsPaper{}, errors.New("JATS body unavailable")
	}
	result := jatsPaper{Text: title, Language: article.Language}
	result.appendBlocks(article.Meta.Abstracts, "/article/front/article-meta")
	before := len(result.Text)
	result.appendBlocks(article.Body.Blocks, "/article/body")
	if len(result.Text) == before {
		return jatsPaper{}, errors.New("JATS body has no readable text")
	}
	result.appendBlocks(article.Back.Blocks, "/article/back")
	if len(result.Text) > 1<<20 {
		return jatsPaper{}, errors.New("JATS text exceeds 1 MiB ingestion envelope; original XML retained")
	}
	if err := sourcedoc.ValidateSections(result.Sections, utf8.RuneCountInString(result.Text)); err != nil {
		return jatsPaper{}, err
	}
	return result, sourcedoc.ValidateAnchors(result.Anchors, utf8.RuneCountInString(result.Text))
}

func (p *jatsPaper) appendBlocks(blocks []jatsBlock, base string) {
	indices := map[string]int{}
	for _, block := range blocks {
		name := block.XMLName.Local
		indices[name]++
		text := stripJATS(block.Inner)
		if text == "" {
			continue
		}
		start := utf8.RuneCountInString(p.Text) + 2
		p.Text += "\n\n" + text
		end := utf8.RuneCountInString(p.Text)
		p.Sections = append(p.Sections, sourcedoc.Section{
			Name: jatsSection(name, block.Title), Title: strings.TrimSpace(block.Title), Start: start, End: end,
		})
		p.Anchors = append(p.Anchors, sourcedoc.Anchor{Path: fmt.Sprintf("%s/%s[%d]", base, name, indices[name]),
			Start: start, End: end})
	}
}

func jatsSection(element, title string) string {
	switch element {
	case "abstract":
		return sourcedoc.SectionAbstract
	case "ref-list":
		return sourcedoc.SectionReferences
	case "ack":
		return sourcedoc.SectionAcknowledgements
	}
	// Именованный раздел — только по явному заголовку; незнакомый остаётся с названием в description, роль не угадывается.
	lower := strings.ToLower(title)
	for _, section := range []struct{ term, name string }{
		{"introduction", sourcedoc.SectionIntroduction}, {"method", sourcedoc.SectionMethod},
		{"experiment", sourcedoc.SectionExperiments}, {"result", sourcedoc.SectionResults},
		{"discussion", sourcedoc.SectionDiscussion}, {"limitation", sourcedoc.SectionLimitations},
		{"conclusion", sourcedoc.SectionConclusion}, {"background", sourcedoc.SectionBackground},
	} {
		if strings.Contains(lower, section.term) {
			return section.name
		}
	}
	return sourcedoc.SectionDescription
}
