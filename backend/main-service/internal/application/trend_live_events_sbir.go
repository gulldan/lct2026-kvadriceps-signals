package application

import (
	"cmp"
	"context"
	"encoding/csv"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"strconv"
	"strings"
	"time"
)

// Грант SBIR/STTR — маркер перехода от идеи к PoC (фаза I) и прототипу (фаза II).

const (
	liveEventSourceSBIR = "sbir"
	liveSBIRDump        = "https://data.www.sbir.gov/awarddatapublic/award_data.csv"
	liveSBIRKey         = "trends/live/grants/sbir-v1.json.gz"
)

func liveSBIRSource() liveGrantSource {
	return liveGrantSource{name: liveEventSourceSBIR, key: liveSBIRKey, url: liveSBIRDump,
		dateBasis: "дата решения о гранте в выгрузке SBIR.gov", parse: liveParseSBIR}
}

func (s *TrendLiveService) sbirEvents(_ context.Context, _ *http.Client, query LiveEventQuery,
) ([]LiveAdoptionEvent, error) {
	return s.grantEvents(&s.eventData.sbir, liveSBIRSource(), query)
}

// Контакты руководителей и исполнителей в выгрузке есть, но в хранилище радара не попадают.
func liveParseSBIR(file *os.File, since int) ([]liveGrant, error) {
	reader := csv.NewReader(file)
	reader.LazyQuotes = true
	reader.FieldsPerRecord = -1
	reader.ReuseRecord = true
	header, err := reader.Read()
	if err != nil {
		return nil, fmt.Errorf("sbir: header: %w", err)
	}
	column := map[string]int{}
	for i, name := range header {
		column[name] = i
	}
	for _, name := range []string{"Company", "Award Title", "Agency", "Phase", "Program", "Award Year", "Abstract"} {
		if _, ok := column[name]; !ok {
			return nil, fmt.Errorf("sbir: column %q is missing", name)
		}
	}
	grants := []liveGrant{}
	for {
		row, err := reader.Read()
		if errors.Is(err, io.EOF) {
			return grants, nil
		}
		if err != nil {
			return nil, fmt.Errorf("sbir: row: %w", err)
		}
		// Копия, а не срез: поле CSV держит в памяти всю строку с контактами, и выгрузка заняла бы втрое больше.
		field := func(name string) string {
			if i, ok := column[name]; ok && i < len(row) {
				return strings.Clone(strings.TrimSpace(row[i]))
			}
			return ""
		}
		if grant, ok := liveSBIRGrant(field, since); ok {
			grants = append(grants, grant)
		}
	}
}

func liveSBIRGrant(field func(string) string, since int) (liveGrant, bool) {
	year, err := strconv.Atoi(field("Award Year"))
	eventType := map[string]string{"Phase I": LiveEventGrantEarly, "Phase II": LiveEventGrantLate}[field("Phase")]
	if err != nil || year < since || eventType == "" || field("Abstract") == "" {
		return liveGrant{}, false
	}
	date := strconv.Itoa(year)
	if day, err := time.Parse("01/02/2006", field("Proposal Award Date")); err == nil {
		date = day.Format(time.DateOnly)
	}
	agency := field("Agency")
	if branch := field("Branch"); branch != "" {
		agency += " / " + branch
	}
	amount := field("Award Amount")
	if amount != "" {
		amount = "$" + amount
	}
	// Страницы sbir.gov нумеруются своим номером, которого в выгрузке нет; поиск по номеру контракта находит этот грант.
	number := cmp.Or(field("Contract"), field("Agency Tracking Number"))
	address := "https://www.sbir.gov/awards?" + url.Values{"keywords": {cmp.Or(number, field("Award Title"))}}.Encode()
	return liveGrant{
		ID: "sbir:" + cmp.Or(number, field("Company")+"|"+field("Award Title")) + "|" + field("Phase"), Type: eventType,
		Title: field("Award Title"), Text: strings.Join(strings.Fields(field("Abstract")), " "),
		Actor: field("Company"), Amount: amount,
		Phase: field("Phase") + " " + field("Program") + ", " + agency, Date: date, URL: address,
	}, true
}
