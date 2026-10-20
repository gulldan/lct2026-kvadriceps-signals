package application

import (
	"archive/zip"
	"cmp"
	"context"
	"encoding/csv"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"slices"
	"strconv"
	"strings"
)

// Схемы Horizon: IA и EIC Accelerator — пилот (УГТ 5–7), RIA и Pathfinder — проверка идеи, стипендии и ERC — наука.
// Проекты прочих схем выгрузка держит только ради компаний-участников: деньги ЕС получают и они.

const (
	liveEventSourceCORDIS = "cordis"
	liveCORDISDump        = "https://cordis.europa.eu/data/cordis-HORIZONprojects-csv.zip"
	liveCORDISKey         = "trends/live/grants/cordis-horizon-v3.json.gz"
)

func liveCORDISSchemes() map[string]string {
	return map[string]string{
		"HORIZON-IA": LiveEventGrantLate, "HORIZON-JU-IA": LiveEventGrantLate, "EURATOM-IA": LiveEventGrantLate,
		"HORIZON-EIC-ACC": LiveEventGrantLate, "HORIZON-EIC-ACC-BF": LiveEventGrantLate, "HORIZON-PCP": LiveEventGrantLate,
		"HORIZON-RIA": LiveEventGrantEarly, "HORIZON-JU-RIA": LiveEventGrantEarly, "EURATOM-RIA": LiveEventGrantEarly,
		"HORIZON-EIC": LiveEventGrantEarly, "HORIZON-ERC-POC": LiveEventGrantEarly,
	}
}

func liveCORDISSource() liveGrantSource {
	return liveGrantSource{name: liveEventSourceCORDIS, key: liveCORDISKey, url: liveCORDISDump,
		dateBasis: "дата подписания грантового соглашения в выгрузке CORDIS", parse: liveParseCORDIS}
}

func (s *TrendLiveService) cordisEvents(_ context.Context, _ *http.Client, query LiveEventQuery,
) ([]LiveAdoptionEvent, error) {
	return s.grantEvents(&s.eventData.cordis, liveCORDISSource(), query)
}

func liveParseCORDIS(file *os.File, since int) ([]liveGrant, error) {
	info, err := file.Stat()
	if err != nil {
		return nil, fmt.Errorf("cordis: stat: %w", err)
	}
	archive, err := zip.NewReader(file, info.Size())
	if err != nil {
		return nil, fmt.Errorf("cordis: zip: %w", err)
	}
	grants := []liveGrant{}
	byID := map[string]int{}
	schemes := liveCORDISSchemes()
	err = liveCORDISRows(archive, "project.csv", func(field func(string) string) {
		date := cmp.Or(field("ecSignatureDate"), field("startDate"))
		year, _ := strconv.Atoi(date[:min(len(date), 4)])
		if year < since || field("objective") == "" {
			return
		}
		amount, _, _ := strings.Cut(field("ecMaxContribution"), ",")
		if amount != "" {
			amount = "€" + amount
		}
		byID[field("id")] = len(grants)
		grants = append(grants, liveGrant{
			ID: "cordis:" + field("id"), Type: schemes[field("fundingScheme")], Title: field("title"),
			Text: strings.Join(strings.Fields(field("objective")), " "), Amount: amount,
			Phase: field("fundingScheme"), Date: date, URL: "https://cordis.europa.eu/project/id/" + field("id"),
		})
	})
	if err != nil {
		return nil, err
	}
	// Исполнитель — компания, если она есть: инвестору нужен тот, кто выводит технологию на рынок.
	rank := map[string]int{}
	err = liveCORDISRows(archive, "organization.csv", func(field func(string) string) {
		i, ok := byID[field("projectID")]
		if !ok || field("name") == "" {
			return
		}
		if participant, funded := liveCORDISCompany(field); funded {
			grants[i].Participants = append(grants[i].Participants, participant)
		}
		order, _ := strconv.Atoi(field("order"))
		score := order
		if field("activityType") != "PRC" {
			score += 1 << 20
		}
		if best, seen := rank[field("projectID")]; !seen || score < best {
			rank[field("projectID")] = score
			grants[i].Actor, grants[i].Country = field("name"), field("country")
		}
	})
	return slices.DeleteFunc(grants, func(grant liveGrant) bool { return grant.Type == "" && len(grant.Participants) == 0 }), err
}

// liveCORDISCompany отбрасывает ассоциированного партнёра: денег ЕС он не получает и был бы ложным получателем гранта.
func liveCORDISCompany(field func(string) string) (liveGrantParticipant, bool) {
	sme, _ := strconv.ParseBool(field("SME"))
	if field("activityType") != "PRC" && !sme || field("role") == "associatedPartner" {
		return liveGrantParticipant{}, false
	}
	amount, _ := strconv.ParseFloat(strings.Replace(field("ecContribution"), ",", ".", 1), 64)
	return liveGrantParticipant{Name: field("name"), Country: field("country"), Amount: amount}, true
}

func liveCORDISRows(archive *zip.Reader, name string, visit func(field func(string) string)) error {
	entry, err := archive.Open(name)
	if err != nil {
		return fmt.Errorf("cordis: %s: %w", name, err)
	}
	defer func() { _ = entry.Close() }()
	reader := csv.NewReader(entry)
	reader.Comma = ';'
	reader.LazyQuotes = true
	reader.FieldsPerRecord = -1
	reader.ReuseRecord = true
	header, err := reader.Read()
	if err != nil {
		return fmt.Errorf("cordis: %s header: %w", name, err)
	}
	column := make(map[string]int, len(header))
	for i, title := range header {
		column[strings.TrimPrefix(title, "\ufeff")] = i
	}
	for {
		row, err := reader.Read()
		if errors.Is(err, io.EOF) {
			return nil
		}
		if err != nil {
			return fmt.Errorf("cordis: %s row: %w", name, err)
		}
		// Копия поля: срез держал бы в памяти всю строку выгрузки.
		visit(func(key string) string {
			if i, ok := column[key]; ok && i < len(row) {
				return strings.Clone(strings.TrimSpace(row[i]))
			}
			return ""
		})
	}
}
