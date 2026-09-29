package httpapi

import (
	"encoding/csv"
	"encoding/json/jsontext"
	"encoding/json/v2"
	"fmt"
	"maps"
	"net/http"
	"slices"
	"strconv"
	"strings"

	"github.com/example/main-service/internal/platform/analysisdoc"
	"github.com/example/main-service/internal/platform/httpx"
)

const (
	formatJSON     = "json"
	formatCSV      = "csv"
	csvLimitations = "limitations"
)

// exportAnalysis оставляет неизмеренную метрику пустой с причиной рядом: 0 превратил бы отсутствие
// в измерение. Размер ограничен analysisdoc.MaxBytes, поэтому лимит строк не нужен.
func (a *API) exportAnalysis(w http.ResponseWriter, r *http.Request) {
	job, ok := a.loadAnalysis(w, r)
	if !ok {
		return
	}
	if job.Status != analysisdoc.Published {
		// Черновик положил бы непроверенное число в файл, который его переживёт.
		httpx.Error(w, http.StatusConflict, "analysis not published")
		return
	}
	switch r.URL.Query().Get("format") {
	case "", formatJSON:
		w.Header().Set("Cache-Control", "no-store")
		w.Header().Set("Content-Disposition", `attachment; filename="analysis-`+job.ID+`.json"`)
		analysisJSON(w, http.StatusOK, map[string]any{
			fieldAnalysisID: job.ID, "result_sha256": job.ResultSHA256,
			fieldMethodHash: job.MethodSHA256, "result": jsontext.Value(job.ResultJSON),
		})
	case formatCSV:
		a.exportAnalysisCSV(w, job)
	default:
		httpx.Error(w, http.StatusBadRequest, "format must be json or csv")
	}
}

func (a *API) exportAnalysisCSV(w http.ResponseWriter, job analysisdoc.Job) {
	profiles, limitations, err := exportProfiles(job)
	if err != nil {
		a.analysisError(w, err)
		return
	}
	rows, err := profileCSVRows(profiles)
	if err != nil {
		httpx.Error(w, http.StatusInternalServerError, "analysis could not be written as a table")
		return
	}
	w.Header().Set("Content-Type", "text/csv; charset=utf-8")
	w.Header().Set("Cache-Control", "no-store")
	w.Header().Set("Content-Disposition", `attachment; filename="analysis-`+job.ID+`.csv"`)
	writer := csv.NewWriter(w)
	for _, row := range rows {
		if err = writer.Write(row); err != nil {
			return
		}
	}
	// Ограничения прогона едут вместе с таблицей: без них каждое число выглядит безусловным.
	if len(limitations) > 0 {
		_ = writer.Write(nil)
		_ = writer.Write([]string{csvLimitations})
		for _, limitation := range limitations {
			_ = writer.Write([]string{limitation})
		}
	}
	writer.Flush()
}

type exportProfile struct {
	id, title, complete, stage string
	measurements               jsontext.Value
}

func briefProfiles(result analysisdoc.Result) []exportProfile {
	profiles := make([]exportProfile, 0, len(result.Candidates))
	for _, candidate := range result.Candidates {
		profiles = append(profiles, exportProfile{id: candidate.ID, title: candidate.Title,
			complete: strconv.FormatBool(candidate.CompleteForBrief), measurements: candidate.Measurements})
	}
	return profiles
}

func exportProfiles(job analysisdoc.Job) ([]exportProfile, []string, error) {
	result, err := analysisdoc.Decode[analysisdoc.Result](job.ResultJSON)
	if err != nil {
		return nil, nil, err
	}
	profiles := briefProfiles(result)
	limitations := slices.Clone(result.Limitations)
	for _, direction := range result.Directions {
		profiles = append(profiles, exportProfile{id: direction.DirectionID, title: direction.Title,
			stage: direction.Stage, measurements: direction.Measurements})
		limitations = append(limitations, direction.Limitations...)
	}
	return profiles, limitations, nil
}

func profileCSVRows(profiles []exportProfile) ([][]string, error) {
	type record struct {
		profile exportProfile
		csvMeasurements
	}
	var records []record
	seen := map[string]bool{}
	temporal := false
	for _, profile := range profiles {
		measured, err := profileMetrics(profile)
		if err != nil {
			return nil, err
		}
		for _, measurement := range measured {
			records = append(records, record{profile, measurement})
			temporal = temporal || measurement.context != ""
			for name := range measurement.values {
				seen[name] = true
			}
		}
	}
	names := slices.Sorted(maps.Keys(seen))
	header := []string{"direction_id", "title", "complete_for_brief"}
	for _, name := range names {
		header = append(header, name, name+"_reason")
	}
	header = append(header, "stage")
	if temporal {
		header = append(header, "period", "measurement_context")
	}
	rows := [][]string{header}
	for _, record := range records {
		profile := record.profile
		row := []string{profile.id, profile.title, profile.complete}
		for _, name := range names {
			cell, present := record.values[name]
			value, reason := cell.value, cell.reason
			if !present {
				reason = "not_reported"
			}
			row = append(row, value, reason)
		}
		row = append(row, profile.stage)
		if temporal {
			row = append(row, record.period, record.context)
		}
		rows = append(rows, row)
	}
	return rows, nil
}

type metricCell struct{ value, reason string }

type csvMeasurements struct {
	values          map[string]metricCell
	period, context string
}

func profileMetrics(profile exportProfile) ([]csvMeasurements, error) {
	out := map[string]metricCell{}
	rows := []csvMeasurements{{values: out}}
	if len(profile.measurements) == 0 {
		return rows, nil
	}
	var measurements map[string]jsontext.Value
	if err := json.Unmarshal(profile.measurements, &measurements); err != nil {
		// Нечитаемый блок не пишется пустыми ячейками: они выглядели бы как измеренное отсутствие.
		return nil, fmt.Errorf("read measurements of %s: %w", profile.id, err)
	}
	if _, ok := measurements["series"]; ok {
		return temporalCSVMeasurements(measurements)
	}
	for name, raw := range measurements {
		var metric struct {
			Value       *float64 `json:"value"`
			Status      string   `json:"status"`
			ReasonCodes []string `json:"reason_codes"`
		}
		if err := json.Unmarshal(raw, &metric); err != nil {
			return nil, fmt.Errorf("read metric %s of %s: %w", name, profile.id, err)
		}
		cell := metricCell{reason: strings.Join(metric.ReasonCodes, " ")}
		if metric.Value != nil {
			cell.value = strconv.FormatFloat(*metric.Value, 'g', -1, 64)
		} else if cell.reason == "" {
			// Отсутствие без причины называется явно, чтобы не выглядеть чистым нулём.
			cell.reason = "unavailable_without_reason"
		}
		if metric.Status != "" {
			cell.reason = strings.TrimSpace(metric.Status + " " + cell.reason)
		}
		out[name] = cell
	}
	return rows, nil
}
