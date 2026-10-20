package provider

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json/jsontext"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/example/source-harvester/internal/platform/harvest"
	"github.com/example/source-harvester/internal/platform/sourcedoc"
)

const providerArXiv = "arxiv"
const providerPatent = "patent"
const schemeHTTPS = "https"
const precisionDay = "day"

const scopePublic = "public"
const hashSHA256 = "sha256"

type Scope struct {
	Query    string `json:"query"`
	From     string `json:"from"`
	Through  string `json:"through"`
	PageSize int    `json:"page_size"`
	WorkType string `json:"work_type"`
	Sort     string `json:"sort,omitempty"`
	Category string `json:"category,omitempty"`
}

func (s Scope) Validate() error {
	from, ferr := time.Parse(time.DateOnly, s.From)
	through, terr := time.Parse(time.DateOnly, s.Through)
	if ferr != nil || terr != nil || from.Year() < 1900 || through.Before(from) ||
		strings.TrimSpace(s.Query) == "" || len(s.Query) > 512 || s.PageSize < 1 || s.PageSize > 100 {
		return errors.New("scope requires a query, ordered day interval since 1900 and page_size 1..100")
	}
	return nil
}

func (s Scope) ValidateFor(provider string) error {
	if err := s.Validate(); err != nil {
		return err
	}
	if s.WorkType == sourcedoc.GenreInvestmentThesis && provider != providerWeb {
		return errors.New("investment_thesis work_type is available only for web collector scopes")
	}
	return harvest.Scope(s).ValidateProvider(provider)
}

type Record struct {
	Source      sourcedoc.Source `json:"source"`
	Title       string           `json:"title"`
	Text        string           `json:"text"`
	TextStatus  string           `json:"text_status"`
	Raw         jsontext.Value   `json:"raw"`
	Limitations []string         `json:"limitations"`
	ContentURL  string           `json:"content_url,omitempty"`
	Releases    []Release        `json:"releases,omitempty"`
}

type Release struct {
	ID        int64     `json:"id"`
	URL       string    `json:"url"`
	Tag       string    `json:"tag"`
	Published time.Time `json:"published_at"`
	Body      string    `json:"body"`
}

type Rejected struct {
	NativeID string `json:"native_id"`
	Reason   string `json:"reason"`
}

// Page сохраняется вместе с точкой возврата до запроса следующей страницы.
type Page struct {
	Provider    string     `json:"provider"`
	Scope       Scope      `json:"scope"`
	Cursor      string     `json:"cursor"`
	Next        string     `json:"next_cursor"`
	Total       int        `json:"provider_total"`
	Received    int        `json:"received"`
	Exhausted   bool       `json:"exhausted"`
	Observed    time.Time  `json:"observed_at"`
	Raw         []byte     `json:"raw_response"`
	RawHash     string     `json:"raw_sha256"`
	Records     []Record   `json:"records"`
	Rejected    []Rejected `json:"rejected"`
	Limitations []string   `json:"limitations"`
}

type Adapter interface {
	Fetch(context.Context, Scope, string) (Page, error)
}

func digest(b []byte) string {
	h := sha256.Sum256(b)
	return hex.EncodeToString(h[:])
}

func scopeDate(d *sourcedoc.Date, scope Scope) error {
	if d == nil {
		return nil // Явно неизвестна; дата сбора не подставляется.
	}
	if err := d.Validate(); err != nil {
		return err
	}
	// Неполная дата обязана целиком попасть в интервал: год на границе не становится точным днём внутри.
	start, end := d.Range()
	lower, upper := start.Format(time.DateOnly), end.AddDate(0, 0, -1).Format(time.DateOnly)
	if lower < scope.From || upper > scope.Through {
		return fmt.Errorf("publication %s/%s is outside or straddles the requested interval", d.Value, d.Precision)
	}
	return nil
}
