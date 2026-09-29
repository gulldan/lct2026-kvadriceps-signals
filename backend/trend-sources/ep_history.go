package main

import (
	"context"
	"errors"
	"net/http"
)

// epHistory держит строки EP отдельно от US, иначе неполнота одной области спряталась бы.
// Исчерпанный бюджет — `partial` с курсором, а не утверждение, что в неделе больше ничего нет.
func (s *service) epHistory(ctx context.Context, data *snapshot) {
	if s.config.EPDocuments <= 0 {
		return
	}
	c := coverage{Source: epSource, From: data.From, Through: data.Through, ObservedAt: data.ObservedAt,
		StopReason: "EP publication weeks are read in bounded passes; a cursor continues the sweep"}
	root, err := s.cached(ctx, epBase+"/publication-dates", "ep-dates:"+data.Through, data)
	if err != nil {
		c.StopReason = err.Error()
		data.Coverage = append(data.Coverage, c)
		return
	}
	c.Pages = append(c.Pages, provenance{URL: epBase + "/publication-dates", SHA256: hashBytes(root)})
	weeks := epPublicationDates(root, data.From, data.Through)
	if len(weeks) == 0 {
		c.StopReason = "no EP publication week inside the configured history"
		data.Coverage = append(data.Coverage, c)
		return
	}
	read := 0
	for _, week := range weeks {
		if done := s.epWeek(ctx, data, &c, week, &read); done {
			break
		}
	}
	c.Exhausted = c.Cursor == "" && read < s.config.EPDocuments
	if c.Exhausted {
		c.StopReason = "every EP publication week of the configured history was read"
	}
	data.Coverage = append(data.Coverage, c)
	data.Limits = append(data.Limits,
		"EP publications come from the European Publication Server, which needs no account. Granted publications "+
			"(B1/B2) carry claims and description; A1 and A2 carry bibliographic data only and A3 adds an abstract, "+
			"so the EP scope reads full text from granted publications and says so for the rest.",
		"EP families are not collected: DOCDB and INPADOC families need an EPO OPS account, which is not configured. "+
			"Until it is, an EP publication is grouped by its earliest claimed priority application, which identifies "+
			"the same filing and never a worldwide family. A missing field stays empty and is reported here; it is "+
			"never filled from the USPTO record of a related document.")
}

// epWeek сообщает, закончен ли проход.
func (s *service) epWeek(ctx context.Context, data *snapshot, c *coverage, week string, read *int) bool {
	address := epBase + "/publication-dates/" + week + "/patents"
	// Завершённая неделя публикаций неизменна, поэтому её список кэшируется навсегда.
	list, err := s.cached(ctx, address, "ep-week:"+week, data)
	if err != nil {
		c.Cursor, c.StopReason = address, err.Error()
		return true
	}
	c.Pages = append(c.Pages, provenance{URL: address, SHA256: hashBytes(list)})
	known := s.previousRecords()
	for _, id := range epWeekPatents(list) {
		if *read >= s.config.EPDocuments {
			c.Cursor = id
			c.StopReason = "EP document budget reached; the sweep continues from this publication"
			return true
		}
		if previous, ok := known[epRecordID(id)]; ok && previous.TextStatus == patentFullText {
			data.Records = append(data.Records, previous)
			continue
		}
		*read++
		if !s.epDocument(ctx, data, c, id) {
			return true
		}
	}
	return false
}

// epDocument сообщает, может ли проход продолжаться.
func (s *service) epDocument(ctx context.Context, data *snapshot, c *coverage, id string) bool {
	address := epBase + "/patents/" + id + "/document.xml"
	raw, err := s.cached(ctx, address, "ep-doc:"+id, data)
	if failure, ok := errors.AsType[*sourceHTTPError](err); ok &&
		(failure.Status == http.StatusNotFound || failure.Status == http.StatusGone) {
		// Снятый с сервера документ — оговорка о нём: иначе каждый проход останавливался на нём же,
		// и более ранние недели EP не читались никогда.
		c.Limitations = append(c.Limitations, "EP document unavailable: "+id+": "+err.Error())
		return true
	}
	if err != nil {
		c.Cursor, c.StopReason = id, err.Error()
		return false
	}
	row, err := epRecord(raw, id, address)
	if err != nil {
		// Нечитаемый документ — названный отказ по нему, а не повод остановиться или записать его пустым.
		c.Limitations = append(c.Limitations, "EP document not readable: "+id+": "+err.Error())
		return true
	}
	row.ObservedAt, row.AvailableAt = data.ObservedAt, data.ObservedAt
	row.Provenance = []provenance{{URL: address, SHA256: hashBytes(raw)}}
	data.Records = append(data.Records, row)
	return true
}

// epRecordID: элемент списка EP1004359NWB1 — публикация EP1004359B1.
func epRecordID(listed string) string {
	if len(listed) < 5 {
		return listed
	}
	return listed[:len(listed)-4] + listed[len(listed)-2:]
}
