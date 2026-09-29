package analysisdoc

import (
	"cmp"
	"context"
	"encoding/hex"
	"encoding/json/jsontext"
	"encoding/json/v2"
	"errors"
	"fmt"
	"io"
	"slices"
	"strconv"
	"strings"
	"time"
	"uuid"

	"github.com/example/main-service/internal/platform/indexdoc"
	"github.com/example/main-service/internal/platform/sourcedoc"
)

var ErrHistoryFull = errors.New("analysis history capacity reached")

const (
	MaxMethodBytes  = 64 << 10
	ResultVersion   = "analysis-result-v1"
	LeaseDuration   = 2 * time.Minute
	MaxAttempts     = 3
	Prepared        = "prepared"
	Validated       = "validated"
	Published       = "published"
	Failed          = "failed"
	RankingComplete = "complete"
)

// ObjectReader позволяет проверить артефакт, не давая права менять бакет.
type ObjectReader interface {
	Get(context.Context, string) (io.ReadCloser, error)
}

type ObjectStore interface {
	ObjectReader
	Put(context.Context, string, io.Reader, int64, string) error
}

type Method struct {
	Version       string         `json:"version"`
	QualityStatus string         `json:"quality_status"`
	CodeSHA256    string         `json:"code_sha256"`
	Parameters    jsontext.Value `json:"parameters"`
	Explanation   jsontext.Value `json:"explanation"`
}

type Submit struct {
	Read
	MethodJSON string `json:"method_json"`
}

// Job: токены аренды отдаёт только приватный RPC Claim.
type Job struct {
	ID             string      `json:"id"`
	InputRunID     string      `json:"input_run_id"`
	InputSHA256    string      `json:"input_sha256"`
	MethodSHA256   string      `json:"method_sha256"`
	MethodJSON     string      `json:"method_json"`
	Status         string      `json:"status"`
	Attempt        int         `json:"attempt"`
	FailureCode    string      `json:"failure_code"`
	CreatedAt      time.Time   `json:"created_at"`
	UpdatedAt      time.Time   `json:"updated_at"`
	Active         bool        `json:"active"`
	ResultSHA256   string      `json:"result_sha256"`
	ResultArtifact string      `json:"result_artifact"`
	ResultJSON     string      `json:"result_json,omitempty"`
	Sources        []SourceRef `json:"sources,omitempty"`
	Request        *Request    `json:"request,omitempty"`
}

// SourceRef берётся из того же замороженного входа, без приватных ключей хранилища и векторов.
type SourceRef struct {
	DocumentID string           `json:"document_id"`
	SnapshotID string           `json:"snapshot_id"`
	Title      string           `json:"title"`
	Source     sourcedoc.Source `json:"source"`
	TextSHA256 string           `json:"text_sha256"`
}

// JobRead привязывает к аутентифицированному читателю даже агрегированный статус.
type JobRead struct {
	JobID        string `json:"job_id"`
	ReaderID     string `json:"reader_id"`
	SharedCorpus bool   `json:"shared_corpus"`
}

type Claim struct {
	WorkerID     string `json:"worker_id"`
	MethodSHA256 string `json:"method_sha256"`
}

// Lease фенсит каждую запись воркера непредсказуемым токеном попытки.
type Lease struct {
	JobID string `json:"job_id"`
	Token string `json:"token"`
}

type Claimed struct {
	Job   *Job   `json:"job"`
	Lease *Lease `json:"lease"`
	Input Frozen `json:"input"`
}

type Validate struct {
	Lease
	ResultJSON string `json:"result_json"`
}

// Failure хранит ограниченную публичную причину — без текста исключений и секретов.
type Failure struct {
	Lease
	Code      string `json:"code"`
	Retryable bool   `json:"retryable"`
}

func (l Lease) Validate() error {
	if _, err := uuid.Parse(l.JobID); err != nil {
		return ErrInvalid
	}
	if _, err := uuid.Parse(l.Token); err != nil {
		return ErrInvalid
	}
	return nil
}

func ValidHash(value string) bool {
	b, err := hex.DecodeString(value)
	return err == nil && len(b) == 32 && hex.EncodeToString(b) == value
}

func ParseMethod(raw string) (Method, error) {
	m, err := Decode[Method](raw)
	if err != nil || len(raw) > MaxMethodBytes || len(m.Version) < 3 || len(m.Version) > 100 ||
		!ValidHash(m.CodeSHA256) || (m.QualityStatus != "not_frozen" && m.QualityStatus != "evaluated") ||
		len(m.Parameters) < 2 || m.Parameters[0] != '{' || len(m.Explanation) < 2 || m.Explanation[0] != '{' {
		return Method{}, ErrInvalid
	}
	return m, nil
}

// RunVersions: два периода сравнимы, только если совпадают все версии.
type RunVersions struct {
	Extraction string `json:"extraction"`
	Embedding  string `json:"embedding"`
	Identity   string `json:"identity"`
	Clustering string `json:"clustering"`
	Metrics    string `json:"metrics"`
	Parser     string `json:"parser"`
}

// Versions: Parser перечисляет ревизии разборщика корпуса, чтобы перепарсенный период
// не сравнивался со старым.
func Versions(method Method, input Input) (RunVersions, error) {
	var p struct {
		BriefVersion      string `json:"brief_version"`
		CandidatesVersion string `json:"candidates_version"`
		EmergenceVersion  string `json:"emergence_version"`
		Versions          struct {
			Identity     string `json:"identity"`
			Measurements string `json:"measurements"`
		} `json:"versions"`
	}
	if json.Unmarshal(method.Parameters, &p) != nil {
		return RunVersions{}, ErrInvalid
	}
	if p.BriefVersion == "" || p.CandidatesVersion == "" || p.EmergenceVersion == "" ||
		p.Versions.Identity == "" || p.Versions.Measurements == "" {
		return RunVersions{}, ErrInvalid
	}
	parsers := []string{}
	for _, record := range input.Records {
		if revision := record.Index.ParserRevision; revision != "" && !slices.Contains(parsers, revision) {
			parsers = append(parsers, revision)
		}
	}
	slices.Sort(parsers)
	versions := RunVersions{
		Extraction: p.BriefVersion,
		Embedding:  "no_embedding_model",
		Identity:   p.Versions.Identity,
		Clustering: p.CandidatesVersion,
		Metrics:    p.Versions.Measurements + "/" + p.EmergenceVersion,
		Parser:     cmp.Or(strings.Join(parsers, "+"), "no_parsed_records"),
	}
	if input.Model != nil {
		versions.Embedding = input.Model.Name + "@" + input.Model.Revision + "/" + strconv.Itoa(input.Model.Dimension)
	}
	return versions, nil
}

func MethodKey(hash string) string { return "signalgraph/v1/methods/" + hash + ".json" }

func ResultKey(jobID, hash string) string {
	return "signalgraph/v1/" + jobID + "/results/" + hash + ".json"
}

// VerifyObject сверяет сами байты, а не только непроверенный хэш из метаданных S3.
func VerifyObject(ctx context.Context, store ObjectReader, key, raw string) error {
	if store == nil || len(raw) == 0 || len(raw) > MaxBytes {
		return ErrInvalid
	}
	r, err := store.Get(ctx, key)
	if err != nil {
		return fmt.Errorf("read analysis artifact: %w", err)
	}
	defer func() { _ = r.Close() }()
	b, err := io.ReadAll(io.LimitReader(r, int64(len(raw))+1))
	if err != nil || len(b) != len(raw) || indexdoc.Hash(b) != indexdoc.Hash([]byte(raw)) {
		return fmt.Errorf("analysis artifact bytes mismatch: %w", ErrInvalid)
	}
	return nil
}
