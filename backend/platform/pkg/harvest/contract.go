package harvest

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json/v2"
	"errors"
	"fmt"
	"net/url"
	"path"
	"regexp"
	"slices"
	"strings"
	"time"
	"uuid"

	"github.com/example/rag-mvp/pkg/sourcedoc"
)

const MaxPageBytes = 4 << 20

var (
	ErrInvalid  = errors.New("invalid collection request")
	ErrConflict = errors.New("collection lease or checkpoint conflict")
	// ErrNotFound получает и чужой сбор: пустой ответ подтвердил бы, что он существует.
	ErrNotFound = errors.New("collection not found")
)

const providerGitHub = "github"

// Scope: интервал From..Through включительно; после каждого полного прохода окно перекрытия
// проходится заново целиком.
type Scope struct {
	Query    string `json:"query"`
	From     string `json:"from"`
	Through  string `json:"through"`
	PageSize int    `json:"page_size"`
	WorkType string `json:"work_type"`
	Sort     string `json:"sort,omitempty"`
	Category string `json:"category,omitempty"`
}

type Start struct {
	OwnerID   string `json:"owner_id"`
	Provider  string `json:"provider"`
	Scope     Scope  `json:"scope"`
	RunID     string `json:"run_id"`
	Token     string `json:"lease_token"`
	Limit     int    `json:"limit"`
	TestRunID string `json:"test_run_id,omitempty"`
}

// Request — сбор по запросу пользователя, а не из конфигурации сборщика; URLs — явный список
// страниц для провайдера web.
type Request struct {
	OwnerID   string   `json:"owner_id"`
	RequestID string   `json:"request_id"`
	Provider  string   `json:"provider"`
	Scope     Scope    `json:"scope"`
	URLs      []string `json:"urls,omitempty"`
	Limit     int      `json:"limit"`
	TestRunID string   `json:"test_run_id,omitempty"`
}

type Requested struct {
	Provider string `json:"provider"`
	Limit    int    `json:"limit"`
}

type Progress struct {
	OwnerID string `json:"owner_id"`
	ScopeID string `json:"scope_id"`
}

type Collection struct {
	ScopeID     string     `json:"scope_id"`
	OwnerID     string     `json:"owner_id"`
	RequestID   string     `json:"request_id"`
	Provider    string     `json:"provider"`
	Scope       Scope      `json:"scope"`
	URLs        []string   `json:"urls,omitempty"`
	Limit       int        `json:"run_limit"`
	State       string     `json:"state"`
	Cursor      string     `json:"next_cursor,omitempty"`
	Received    int        `json:"received"`
	LastError   string     `json:"last_error,omitempty"`
	RequestedAt time.Time  `json:"requested_at"`
	UpdatedAt   time.Time  `json:"updated_at"`
	Manifests   []Manifest `json:"manifests"`
}

type Manifest struct {
	RunID        string    `json:"run_id"`
	Pages        int       `json:"pages"`
	Listed       int       `json:"listed"`
	Received     int       `json:"received"`
	Rejected     int       `json:"rejected"`
	Processed    int       `json:"processed"`
	Cursor       string    `json:"next_cursor,omitempty"`
	Exhausted    bool      `json:"exhausted"`
	Incomplete   bool      `json:"incomplete"`
	Completeness string    `json:"completeness"`
	Limitations  []string  `json:"limitations"`
	StartedAt    time.Time `json:"started_at"`
	UpdatedAt    time.Time `json:"updated_at"`
}

const (
	StateRequested = "requested"
	StateRunning   = "running"
	StatePartial   = "partial"
	StateComplete  = "complete"
	StateFailed    = "failed"
)

// Fingerprint делает повтор с тем же request_id либо тем же сбором, либо конфликтом.
func (r Request) Fingerprint() string {
	raw, _ := json.Marshal(struct {
		Provider  string   `json:"provider"`
		Scope     Scope    `json:"scope"`
		URLs      []string `json:"urls"`
		Limit     int      `json:"limit"`
		TestRunID string   `json:"test_run_id"`
	}{r.Provider, r.Scope, r.URLs, r.Limit, r.TestRunID})
	sum := sha256.Sum256(raw)
	return hex.EncodeToString(sum[:])
}

// ScopeID общий для запросов одной области, чтобы они продолжали один сбор, а не гонялись двумя.
func (r Request) ScopeID() string {
	return Start{OwnerID: r.OwnerID, Provider: r.Provider, Scope: r.Scope, TestRunID: r.TestRunID}.ScopeID()
}

func (r Request) Validate(allowed []string) error {
	if _, err := uuid.Parse(r.OwnerID); err != nil {
		return ErrInvalid
	}
	if _, err := uuid.Parse(r.RequestID); err != nil {
		return ErrInvalid
	}
	if (len(r.URLs) > 0) != (r.Provider == "web") || len(r.URLs) > 500 {
		return ErrInvalid
	}
	for _, address := range r.URLs {
		u, err := url.Parse(address)
		if err != nil || u.Scheme != "https" || u.Hostname() == "" || u.User != nil {
			return ErrInvalid
		}
		if !hostInPanel(u.Hostname(), allowed) {
			return fmt.Errorf("%w: address %s is outside the configured domains", ErrInvalid, u.Hostname())
		}
	}
	start := Start{OwnerID: r.OwnerID, Provider: r.Provider, Scope: r.Scope,
		RunID: uuid.NewV4().String(), Token: uuid.NewV4().String(), Limit: r.Limit, TestRunID: r.TestRunID}
	return start.Validate()
}

// hostInPanel принимает и поддомены — так же, как периметр самого сборщика.
func hostInPanel(host string, allowed []string) bool {
	host = strings.ToLower(host)
	for _, domain := range allowed {
		domain = strings.ToLower(strings.TrimPrefix(domain, "."))
		if domain != "" && (host == domain || strings.HasSuffix(host, "."+domain)) {
			return true
		}
	}
	return false
}

// Lease фенсит попытку от устаревших планировщиков.
type Lease struct {
	ScopeID string `json:"scope_id"`
	RunID   string `json:"run_id"`
	Token   string `json:"lease_token"`
}

type Job struct {
	Lease      Lease  `json:"lease"`
	OwnerID    string `json:"owner_id"`
	Provider   string `json:"provider"`
	Scope      Scope  `json:"scope"`
	Cursor     string `json:"cursor"`
	Received   int    `json:"received"`
	PageNumber int    `json:"page_number"`
	Exhausted  bool   `json:"exhausted"`
	Incomplete bool   `json:"incomplete"`
	Limit      int    `json:"limit"`
	Status     string `json:"status"`
	TestRunID  string `json:"test_run_id,omitempty"`
}

type Quota struct {
	Lease     Lease      `json:"lease"`
	Reserve   bool       `json:"reserve"`
	NotBefore *time.Time `json:"not_before"`
}

type Grant struct {
	Allowed   bool      `json:"allowed"`
	NotBefore time.Time `json:"not_before"`
}

// Prepared: Text заполняется только для пути text/plain -> чанки; PDF уходят в очередь разборщика.
type Prepared struct {
	DocumentID  string           `json:"document_id"`
	TextStatus  string           `json:"text_status"`
	Limitations []string         `json:"limitations"`
	Source      sourcedoc.Source `json:"source"`
	Title       string           `json:"title"`
	ObjectKey   string           `json:"object_key"`
	Size        int64            `json:"size"`
	BLAKE3      string           `json:"blake3"`
	MIME        string           `json:"mime_type"`
	Text        string           `json:"text,omitempty"`
}

type CommitPage struct {
	Lease        Lease      `json:"lease"`
	Cursor       string     `json:"cursor"`
	PageNumber   int        `json:"page_number"`
	Next         string     `json:"next_cursor"`
	Exhausted    bool       `json:"exhausted"`
	Received     int        `json:"received"`
	Rejected     int        `json:"rejected"`
	Limitations  []string   `json:"limitations"`
	RawKey       string     `json:"raw_key"`
	RawHash      string     `json:"raw_hash"`
	ManifestKey  string     `json:"manifest_key"`
	ManifestHash string     `json:"manifest_hash"`
	Records      []Prepared `json:"records"`
}

type Dispatch struct {
	DocumentID string `json:"document_id"`
	OwnerID    string `json:"owner_id"`
	Filename   string `json:"filename"`
	ObjectKey  string `json:"object_key"`
	MIME       string `json:"mime_type"`
	Size       int64  `json:"size"`
	Text       string `json:"text,omitempty"`
}

type Pending struct {
	Lease Lease `json:"lease"`
	Limit int   `json:"limit"`
}

type PendingResult struct {
	Records []Dispatch `json:"records"`
}

type Ack struct {
	Lease      Lease  `json:"lease"`
	DocumentID string `json:"document_id"`
}

type Finish struct {
	Lease       Lease  `json:"lease"`
	Status      string `json:"status"`
	Error       string `json:"error"`
	ResetCursor bool   `json:"reset_cursor"`
}

var hexHash = regexp.MustCompile(`^[0-9a-f]{64}$`)
var testNamespace = regexp.MustCompile(`^tr-test-[a-zA-Z0-9][a-zA-Z0-9_-]{0,100}$`)
var arxivCategory = regexp.MustCompile(`^[a-z][a-z0-9-]{0,31}(\.[A-Za-z][A-Za-z0-9-]{0,31})?$`)

func (s Scope) ValidateProvider(provider string) error {
	if s.Sort != "" && (provider != providerGitHub || !slices.Contains([]string{"updated", "stars"}, s.Sort)) {
		return errors.New("collection sort is supported only for GitHub: updated or stars")
	}
	if s.Category != "" && (provider != "arxiv" || len(s.Category) > 64 || !arxivCategory.MatchString(s.Category)) {
		return errors.New("collection category requires one arXiv category, such as cs.IR or hep-th")
	}
	return nil
}

func Decode[T any](raw string) (T, error) {
	var value T
	if len(raw) == 0 || len(raw) > MaxPageBytes {
		return value, errors.New("collection envelope must contain 1..4 MiB")
	}
	if err := json.Unmarshal([]byte(raw), &value, json.RejectUnknownMembers(true)); err != nil {
		return value, fmt.Errorf("decode collection: %w", err)
	}
	return value, nil
}

// Validate оборачивает отказы в ErrInvalid, чтобы вызывающий ответил 400 с причиной, а не 500.
func (s Start) Validate() error {
	if s.OwnerID == "" || len(s.OwnerID) > 128 ||
		!slices.Contains([]string{"crossref", "arxiv", providerGitHub, "patent", "web"}, s.Provider) ||
		s.Limit < 1 || s.Limit > 200 || s.Scope.PageSize < 1 || s.Scope.PageSize > 100 ||
		strings.TrimSpace(s.Scope.Query) == "" || len(s.Scope.Query) > 512 {
		return fmt.Errorf("%w: invalid collection identity, query or budget", ErrInvalid)
	}
	from, err := time.Parse(time.DateOnly, s.Scope.From)
	through, terr := time.Parse(time.DateOnly, s.Scope.Through)
	if err != nil || terr != nil || from.Year() < 1900 || through.Before(from) {
		return fmt.Errorf("%w: invalid collection interval: from and through are required as YYYY-MM-DD", ErrInvalid)
	}
	if err := s.Scope.ValidateProvider(s.Provider); err != nil {
		return err
	}
	if _, err := uuid.Parse(s.RunID); err != nil {
		return fmt.Errorf("%w: invalid collection run ID", ErrInvalid)
	}
	if _, err := uuid.Parse(s.Token); err != nil {
		return fmt.Errorf("%w: invalid collection lease token", ErrInvalid)
	}
	if s.TestRunID != "" && !testNamespace.MatchString(s.TestRunID) {
		return fmt.Errorf("%w: invalid test namespace", ErrInvalid)
	}
	if s.TestRunID != "" && s.Limit > 50 {
		return fmt.Errorf("%w: test runs are limited to 50 records", ErrInvalid)
	}
	if s.Limit%s.Scope.PageSize != 0 || (s.Provider == providerGitHub && s.Scope.PageSize != 1) {
		return fmt.Errorf("%w: budget must contain whole pages; GitHub enrichment uses one repository per page", ErrInvalid)
	}
	return nil
}

func (s Start) ScopeID() string {
	raw, _ := json.Marshal(
		struct {
			Owner    string
			Provider string
			Scope    Scope
			TestRun  string
		}{
			s.OwnerID,
			s.Provider,
			s.Scope,
			s.TestRunID,
		},
	)
	h := sha256.Sum256(raw)
	return hex.EncodeToString(h[:])
}

func (l Lease) Validate() error {
	if !hexHash.MatchString(l.ScopeID) {
		return errors.New("invalid collection scope ID")
	}
	if _, err := uuid.Parse(l.RunID); err != nil {
		return errors.New("invalid collection run ID")
	}
	if _, err := uuid.Parse(l.Token); err != nil {
		return errors.New("invalid collection lease token")
	}
	return nil
}

func (p CommitPage) Validate(job Job) error {
	if err := p.Lease.Validate(); err != nil {
		return err
	}
	if p.PageNumber < 0 || p.PageNumber > 200 || p.Received < 0 || p.Received > job.Scope.PageSize ||
		p.Rejected < 0 || p.Received != len(p.Records)+p.Rejected ||
		job.Received+p.Received > job.Limit || len(p.Cursor) > 16384 || len(p.Next) > 16384 || !hexHash.MatchString(p.RawHash) ||
		(!p.Exhausted && (p.Next == "" || p.Received == 0)) || (p.Exhausted && p.Next != "") {
		return errors.New("invalid collection page or run budget")
	}
	prefix := "sources/" + job.Provider + "/" + job.Lease.RunID + "/"
	if !safeObject(p.RawKey, prefix) || !safeObject(p.ManifestKey, prefix) || !hexHash.MatchString(p.ManifestHash) {
		return errors.New("raw page must belong to this run")
	}
	for _, r := range p.Records {
		if err := r.validate(job); err != nil {
			return err
		}
	}

	return nil
}

func safeObject(key, prefix string) bool {
	return strings.HasPrefix(key, prefix) && path.Clean(key) == key && len(key) < 2048 && !strings.Contains(key, "\\")
}

// DocumentKey переиспользует неизменное содержимое между областями и опросами; для GitHub берётся
// коммит, чтобы наблюдения звёзд не плодили файлы.
func DocumentKey(owner string, r Prepared) string {
	revision := r.Source.Revision
	if r.Source.GitHub != nil {
		revision = r.Source.GitHub.CommitSHA
	}
	h := sha256.Sum256(
		[]byte(
			strings.Join(
				[]string{
					owner,
					r.Source.Provider,
					r.Source.NativeID,
					revision,
					r.MIME,
					r.Source.ContentHash,
					r.Source.TestRunID,
				},
				"\x00",
			),
		),
	)
	return hex.EncodeToString(h[:])
}

func (r Prepared) validate(job Job) error {
	if _, err := uuid.Parse(r.DocumentID); err != nil {
		return errors.New("prepared document requires a manifest UUID")
	}
	sourceJSON, err := json.Marshal(r.Source)
	if err != nil {
		return fmt.Errorf("encode prepared source: %w", err)
	}
	if _, err := sourcedoc.Decode(sourceJSON); err != nil {
		return err
	}
	if r.Source.Provider != job.Provider || r.Source.AccessScope != "public" || r.Source.TestRunID != job.TestRunID ||
		!safeObject(r.ObjectKey, "sources/"+job.Provider+"/") || !hexHash.MatchString(r.BLAKE3) ||
		r.Title == "" || len(r.Title) > 4096 ||
		r.Size < 1 || r.Size > 8<<20 || !slices.Contains([]string{"text/plain", "application/pdf"}, r.MIME) {
		return errors.New("invalid prepared source identity, object or file size")
	}
	if r.MIME == "text/plain" {
		h := sha256.Sum256([]byte(r.Text))
		if int64(len(r.Text)) != r.Size || r.Source.HashAlgorithm != "sha256" || hex.EncodeToString(h[:]) != r.Source.ContentHash {
			return errors.New("prepared text does not match its source hash/size")
		}
	} else if r.Text != "" {
		return errors.New("PDF must go through the parser")
	}
	return nil
}
