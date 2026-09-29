package application

import (
	"encoding/json/jsontext"
	"encoding/json/v2"
	"fmt"
)

// Прежний промпт разрешал пустой массив для неизвестной строки. Принимаем только эту
// пустую форму; непустые массивы и другие ошибочные типы не превращаем в факты паспорта.
type enrichText string

func (text *enrichText) UnmarshalJSON(raw []byte) error {
	var value string
	if err := json.Unmarshal(raw, &value); err != nil {
		var empty []jsontext.Value
		if arrayErr := json.Unmarshal(raw, &empty); arrayErr != nil || len(empty) != 0 {
			return fmt.Errorf("enrichment text must be a string: %w", err)
		}
	}
	*text = enrichText(value)
	return nil
}
