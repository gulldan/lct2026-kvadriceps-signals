package httpapi

import (
	"encoding/json/jsontext"
	"encoding/json/v2"
	"errors"
	"fmt"
	"strconv"
)

// temporalCSVMeasurements сплющивает счётчики temporal.measure(), сохраняя весь контекст JSON-ом, чтобы
// даты, точность, база покрытия и нераспределённые работы пережили CSV.
func temporalCSVMeasurements(measurements map[string]jsontext.Value) ([]csvMeasurements, error) {
	var series []jsontext.Value
	if err := json.Unmarshal(measurements["series"], &series); err != nil {
		return nil, fmt.Errorf("read temporal series: %w", err)
	}
	delete(measurements, "series")
	metadata, err := json.Marshal(measurements)
	if err != nil {
		return nil, fmt.Errorf("encode temporal context: %w", err)
	}
	if len(series) == 0 {
		return []csvMeasurements{{values: map[string]metricCell{}, context: string(metadata)}}, nil
	}
	rows := make([]csvMeasurements, 0, len(series))
	for _, raw := range series {
		var period struct {
			Period             string              `json:"period"`
			Coverage           string              `json:"coverage"`
			Complete           bool                `json:"complete_period"`
			Found              map[string]*float64 `json:"found"`
			FoundExposure      *float64            `json:"found_exposure_scientific_works"`
			ComparableWorks    *float64            `json:"comparable_scientific_works"`
			ComparableExposure *float64            `json:"comparable_exposure"`
			Activity           *float64            `json:"normalized_activity"`
		}
		if err = json.Unmarshal(raw, &period); err != nil {
			return nil, fmt.Errorf("read temporal period: %w", err)
		}
		if period.Period == "" || period.Coverage == "" {
			return nil, errors.New("temporal period has no name or coverage")
		}
		values := map[string]*float64{
			"found_exposure_scientific_works": period.FoundExposure,
			"comparable_scientific_works":     period.ComparableWorks,
			"comparable_exposure":             period.ComparableExposure,
			"normalized_activity":             period.Activity,
		}
		for unit, count := range period.Found {
			values["found."+unit] = count
		}
		cells := make(map[string]metricCell, len(values))
		for name, value := range values {
			if value != nil {
				cells[name] = metricCell{value: strconv.FormatFloat(*value, 'g', -1, 64)}
				continue
			}
			cell := metricCell{}
			switch {
			case !period.Complete:
				cell.reason = "incomplete_period"
			case period.Coverage != "examined_bounded_population":
				cell.reason = period.Coverage
			case name == "normalized_activity" && period.ComparableExposure != nil && *period.ComparableExposure == 0:
				cell.reason = "zero_exposure"
			default:
				cell.reason = "unavailable_without_reason"
			}
			cells[name] = cell
		}
		rows = append(rows, csvMeasurements{values: cells, period: period.Period,
			context: `{"profile":` + string(metadata) + `,"period":` + string(raw) + `}`})
	}
	return rows, nil
}
