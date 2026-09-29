package provider

import (
	"errors"
	"fmt"
)

// PageProgress отличает перекрывающийся живой поиск от застрявшего курсора: курсор Crossref может повторять работы.
type PageProgress struct {
	pages   map[string]bool
	records map[string]bool
}

// Observe принимает продвигающиеся страницы перекрытия: сброс рабочего курсора запер бы следующий запуск
// на тех же работах. Одинаковая страница или вставший курсор окно всё же перезапускают.
func (p *PageProgress) Observe(page *Page) (fresh, duplicates int, err error) {
	if p.pages == nil {
		p.pages, p.records = map[string]bool{}, map[string]bool{}
	}
	if p.pages[page.RawHash] {
		return 0, 0, progressError(page.Provider, "provider replayed an identical page")
	}
	fresh, duplicates = countNew(page.Records, p.records)
	if duplicates > 0 {
		page.Limitations = append(page.Limitations, "overlapping_provider_pages; duplicate works retained as observations only")
	}
	if len(page.Records) > 0 && fresh == 0 && len(page.Rejected) == 0 &&
		(page.Provider != providerCrossref || !page.Exhausted && (page.Next == "" || page.Next == page.Cursor)) {
		return fresh, duplicates, progressError(page.Provider, "provider cursor stalled on repeated revisions")
	}
	p.pages[page.RawHash] = true
	return fresh, duplicates, nil
}

func progressError(provider, message string) error {
	if provider == providerCrossref {
		return fmt.Errorf("%w: %s", ErrCursorExpired, message)
	}
	return errors.New(message)
}
