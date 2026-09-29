package analysisdoc

import (
	"encoding/json/jsontext"
	"slices"
	"strings"
)

type ResultTechnology struct {
	TechnologyID string            `json:"technology_id"`
	Name         string            `json:"name"`
	Role         string            `json:"role"`
	Evidence     []ResultQuote     `json:"evidence"`
	Support      TechnologySupport `json:"support"`
	Limitations  []string          `json:"limitations,omitempty"`
}

type ResultQuote struct {
	DocumentID string `json:"document_id"`
	Start      int    `json:"char_start"`
	End        int    `json:"char_end"`
	Quote      string `json:"quote"`
}

// TechnologySupport — n/E: сколько просмотренных ревизий называют технологию из всех просмотренных.
// Оба числа пересчитываются по манифесту, а не берутся на веру.
type TechnologySupport struct {
	Numerator   int `json:"numerator"`
	Denominator int `json:"denominator"`
}

// ResultDirection состоит из технологий, а не работ: подсчёт через работу учёл бы её дважды.
type ResultDirection struct {
	DirectionID  string         `json:"direction_id"`
	Title        string         `json:"title"`
	Technologies []string       `json:"technologies"`
	Stage        string         `json:"stage"`
	Measurements jsontext.Value `json:"measurements,omitempty"`
	Limitations  []string       `json:"limitations,omitempty"`
}

type ResultTechnologyOrder struct {
	Rule       string   `json:"rule"`
	OrderedIDs []string `json:"ordered_ids"`
}

const (
	OrderBySupport = "support_desc_then_name"
)

func validateTechnologyContent(r Result, input Input) error {
	if len(r.Technologies) > MaxDocuments || len(r.Directions) > MaxDocuments {
		return ErrInvalid
	}
	if len(r.Technologies) == 0 {
		if len(r.Directions) != 0 || r.TechnologyOrder.Rule != "" || len(r.TechnologyOrder.OrderedIDs) != 0 {
			return ErrInvalid
		}
		return nil
	}
	examined := map[string]bool{}
	for _, record := range input.Records {
		examined[record.ID] = true
	}
	byID, err := validateResultTechnologies(r, examined)
	if err != nil {
		return err
	}
	if err = validateDirections(r.Directions, byID); err != nil {
		return err
	}
	return validateTechnologyOrder(r, byID)
}

func validateResultTechnologies(r Result, examined map[string]bool,
) (map[string]ResultTechnology, error) {
	byID := make(map[string]ResultTechnology, len(r.Technologies))
	for _, technology := range r.Technologies {
		if technology.TechnologyID == "" || strings.TrimSpace(technology.Name) == "" ||
			technology.Role == "" || len(technology.Evidence) == 0 {
			return nil, ErrInvalid
		}
		if _, seen := byID[technology.TechnologyID]; seen {
			return nil, ErrInvalid
		}
		revisions := map[string]bool{}
		for _, quote := range technology.Evidence {
			// Цитату ревизии вне манифеста нельзя воспроизвести из прочитанного запуском, такой результат не публикуется.
			if !examined[quote.DocumentID] || quote.Start < 0 || quote.End <= quote.Start ||
				strings.TrimSpace(quote.Quote) == "" {
				return nil, ErrInvalid
			}
			revisions[quote.DocumentID] = true
		}
		if technology.Support.Numerator != len(revisions) || technology.Support.Denominator != len(examined) {
			return nil, ErrInvalid
		}
		byID[technology.TechnologyID] = technology
	}
	return byID, nil
}

func validateDirections(directions []ResultDirection, byID map[string]ResultTechnology) error {
	seen := map[string]bool{}
	for _, direction := range directions {
		if direction.DirectionID == "" || direction.Title == "" || seen[direction.DirectionID] ||
			len(direction.Technologies) == 0 {
			return ErrInvalid
		}
		seen[direction.DirectionID] = true
		members := map[string]bool{}
		for _, id := range direction.Technologies {
			if _, ok := byID[id]; !ok || members[id] {
				return ErrInvalid
			}
			members[id] = true
		}
	}
	return nil
}

func validateTechnologyOrder(r Result, byID map[string]ResultTechnology) error {
	if r.TechnologyOrder.Rule != OrderBySupport || len(r.TechnologyOrder.OrderedIDs) != len(r.Technologies) {
		return ErrInvalid
	}
	shown := make([]ResultTechnology, 0, len(r.TechnologyOrder.OrderedIDs))
	seen := map[string]bool{}
	for _, id := range r.TechnologyOrder.OrderedIDs {
		technology, ok := byID[id]
		if !ok || seen[id] {
			return ErrInvalid
		}
		seen[id] = true
		shown = append(shown, technology)
	}
	want := slices.Clone(shown)
	slices.SortStableFunc(want, func(a, b ResultTechnology) int {
		if a.Support.Numerator != b.Support.Numerator {
			return b.Support.Numerator - a.Support.Numerator
		}
		return strings.Compare(a.Name, b.Name)
	})
	for i := range want {
		if want[i].TechnologyID != shown[i].TechnologyID {
			return ErrInvalid
		}
	}
	return nil
}
