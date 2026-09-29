package main

import (
	"encoding/json/v2"
	"errors"
	"fmt"
	"math"
	"os"
	"path/filepath"
	"strings"
	"time"
)

// epFairUseDefault — 10 ГБ в неделю на IP за вычетом запаса: учёт считает тела ответов, а условия — весь трафик.
const epFairUseDefault = 9 << 30

const epFairUseWindow = 7

var errEPFairUse = errors.New("EPO fair-use volume for the last seven days is spent; the sweep continues from the cursor")

// epLedger хранится на диске: квота меряется по IP за семь дней и перезапуском не сбрасывается.
type epLedger struct {
	Days map[string]int64 `json:"days"`
}

func (s *service) epLedgerPath() string { return filepath.Join(s.dir, "ep-fairuse.json") }

func (s *service) epLedgerRead() (epLedger, error) {
	var ledger epLedger
	raw, err := os.ReadFile(s.epLedgerPath())
	if errors.Is(err, os.ErrNotExist) {
		return epLedger{Days: map[string]int64{}}, nil
	}
	if err != nil {
		return ledger, fmt.Errorf("read EPO fair-use ledger: %w", err)
	}
	if err = json.Unmarshal(raw, &ledger); err != nil || ledger.Days == nil {
		return ledger, errors.New("invalid EPO fair-use ledger")
	}
	cutoff := time.Now().UTC().AddDate(0, 0, -epFairUseWindow).Format(time.DateOnly)
	var total int64
	for day, n := range ledger.Days {
		if _, err := time.Parse(time.DateOnly, day); err != nil || n < 0 || n > math.MaxInt64-total {
			return ledger, errors.New("invalid EPO fair-use ledger entry")
		}
		total += n
		if day < cutoff {
			delete(ledger.Days, day)
		}
	}
	return ledger, nil
}

func (l epLedger) total() int64 {
	var sum int64
	for _, n := range l.Days {
		sum += n
	}
	return sum
}

func (s *service) epFairUseBudget() int64 {
	if s.config.EPFairUseBytes > 0 {
		return int64(s.config.EPFairUseBytes)
	}
	return epFairUseDefault
}

// epFairUseCheck закрывает доступ, если прошлый расход не восстановить.
func (s *service) epFairUseCheck() error {
	ledger, err := s.epLedgerRead()
	if err != nil {
		return err
	}
	if ledger.total() >= s.epFairUseBudget() {
		return errEPFairUse
	}
	return nil
}

func (s *service) epFairUseAdd(n int) error {
	ledger, err := s.epLedgerRead()
	if err != nil {
		return err
	}
	ledger.Days[time.Now().UTC().Format(time.DateOnly)] += int64(n)
	if err := atomicSourceJSON(s.epLedgerPath(), ledger); err != nil {
		return fmt.Errorf("record EPO fair-use volume: %w", err)
	}
	return nil
}

func isEPAddress(address string) bool { return strings.HasPrefix(address, epBase) }
