package provider

import (
	"context"
	"errors"
	"fmt"
)

// Collection: упёршийся в бюджет или сбойный запрос никогда не выдаётся за пустые данные.
type Collection struct {
	Status      string   `json:"status"`
	Limit       int      `json:"limit"`
	Pages       int      `json:"pages"`
	Received    int      `json:"received"`
	Unique      int      `json:"unique_revisions"`
	Duplicates  int      `json:"duplicate_revisions"`
	Rejected    int      `json:"rejected"`
	Cursor      string   `json:"next_cursor"`
	Error       string   `json:"error,omitempty"`
	Limitations []string `json:"limitations"`
}

// Collect сохраняет страницу до перехода дальше; потолок в 200 записей — замеренный бюджет пилота, не предел API.
func Collect(ctx context.Context, adapter Adapter, scope Scope, limit int, persist func(Page) error) (Collection, error) {
	result := Collection{Status: "running", Limit: limit}
	if limit < 1 || limit > 200 || persist == nil {
		return failed(result, "invalid_request", errors.New("collection requires limit 1..200 and a page sink"))
	}
	if err := scope.Validate(); err != nil {
		return failed(result, "invalid_request", err)
	}
	if limit%scope.PageSize != 0 {
		return failed(result, "invalid_request", errors.New("collection budget must contain whole pages"))
	}
	var progress PageProgress
	for result.Received < limit {
		page, err := adapter.Fetch(ctx, scope, result.Cursor)
		if err != nil {
			return failed(result, "provider_failed", err)
		}
		if err = page.Validate(scope.PageSize); err != nil {
			return failed(result, "provider_failed", err)
		}
		newRecords, duplicates, err := progress.Observe(&page)
		if err != nil {
			return failed(result, "provider_failed", err)
		}
		if err := persist(page); err != nil {
			return failed(result, "checkpoint_failed", fmt.Errorf("persist page: %w", err))
		}
		result.Pages++
		result.Received += page.Received
		result.Unique += newRecords
		result.Duplicates += duplicates
		result.Rejected += len(page.Rejected)
		result.Cursor = page.Next
		result.Limitations = append(result.Limitations, page.Limitations...)
		if page.Exhausted {
			result.Status = completedStatus(result)
			return result, nil
		}
	}
	result.Status = "budget_exhausted"
	return result, nil
}

func (page Page) Validate(pageSize int) error {
	if page.Received < 0 || page.Received > pageSize || page.Received != len(page.Records)+len(page.Rejected) ||
		page.RawHash != digest(page.Raw) || (!page.Exhausted && (page.Next == "" || page.Received == 0)) {
		return errors.New("invalid page size, checkpoint or raw hash")
	}
	return nil
}

func countNew(records []Record, seen map[string]bool) (int, int) {
	fresh, duplicates := 0, 0
	for _, record := range records {
		key := record.Source.Provider + ":" + record.Source.NativeID + ":" + record.Source.Revision
		if seen[key] {
			duplicates++
		} else {
			fresh++
			seen[key] = true
		}
	}
	return fresh, duplicates
}

func failed(result Collection, status string, err error) (Collection, error) {
	result.Status, result.Error = status, err.Error()
	return result, err
}

func completedStatus(result Collection) string {
	if len(result.Limitations) > 0 {
		return "provider_incomplete"
	}
	if result.Rejected > 0 {
		return "search_exhausted_with_rejections"
	}
	return "search_exhausted"
}
