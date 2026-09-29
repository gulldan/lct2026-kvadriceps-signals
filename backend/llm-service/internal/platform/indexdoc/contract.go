package indexdoc

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json/v2"
	"errors"
	"fmt"
	"math"
	"strings"
	"time"
	"unicode"
	"uuid"

	"github.com/example/llm-service/internal/platform/sourcedoc"
)

const (
	MaxChunks        = 8192
	MaxTextBytes     = 16 << 20
	MaxSnapshotBytes = 128 << 20
	MaxManifestBytes = 16 << 10
	LeaseDuration    = 2 * time.Minute
	MaxRequestBytes  = 4 << 20
	MaxResponseBytes = 16 << 20
)

var (
	ErrInvalid  = errors.New("invalid source index")
	ErrConflict = errors.New("source index lease conflict")
	ErrNotFound = errors.New("source index unavailable")
)

// Lease: Token одновременно служит id неизменяемого снимка.
type Lease struct {
	Generation int64  `json:"generation,omitzero"`
	DocumentID string `json:"document_id"`
	OwnerID    string `json:"owner_id"`
	Token      string `json:"token"`
}

func (l Lease) Validate() error {
	_, docErr := uuid.Parse(l.DocumentID)
	_, tokenErr := uuid.Parse(l.Token)
	if docErr != nil || tokenErr != nil || strings.TrimSpace(l.OwnerID) == "" || len(l.OwnerID) > 256 {
		return ErrInvalid
	}
	return nil
}

type Object struct {
	Key    string `json:"key"`
	SHA256 string `json:"sha256"`
}

type Model struct {
	Name      string `json:"name"`
	Revision  string `json:"revision"`
	Dimension int    `json:"dimension"`
}

// Manifest ссылается только на проверенные объекты, никогда на изменяемые алиасы индексов.
type Manifest struct {
	Version          int    `json:"version"`
	DocumentID       string `json:"document_id"`
	OwnerID          string `json:"owner_id"`
	SnapshotID       string `json:"snapshot_id"`
	SourceRevision   string `json:"source_revision"`
	RawHash          string `json:"raw_hash"`
	RawKey           string `json:"raw_key"`
	Text             Object `json:"text"`
	Data             Object `json:"data"`
	ChunkCount       int    `json:"chunk_count"`
	Model            Model  `json:"model"`
	PipelineRevision string `json:"pipeline_revision"`
	ParserRevision   string `json:"parser_revision"`
}

func (m Manifest) Validate() error {
	raw, err := json.Marshal(m)
	if err != nil || len(raw) > MaxManifestBytes {
		return ErrInvalid
	}
	if err := (Lease{DocumentID: m.DocumentID, OwnerID: m.OwnerID, Token: m.SnapshotID}).Validate(); err != nil {
		return err
	}
	if m.Version != 1 || m.ChunkCount < 1 || m.ChunkCount > MaxChunks || m.SourceRevision == "" ||
		!IsHash(m.RawHash) || m.RawKey == "" || m.Model.Name == "" || m.Model.Revision == "" ||
		m.Model.Dimension < 1 || m.Model.Dimension > 8192 || m.PipelineRevision == "" || m.ParserRevision == "" {
		return ErrInvalid
	}
	prefix := Prefix(m.DocumentID, m.SnapshotID)
	for _, ref := range []Object{m.Text, m.Data} {
		if !IsHash(ref.SHA256) || (ref.Key != prefix+ref.SHA256+".json" && ref.Key != prefix+ref.SHA256+".txt") {
			return ErrInvalid
		}
	}
	return nil
}

// Prefix изолирует каждую попытку и не берёт путь объекта у вызывающего.
func Prefix(docID, snapshotID string) string {
	return "source-index/v1/" + docID + "/" + snapshotID + "/"
}

func Hash(raw []byte) string {
	sum := sha256.Sum256(raw)
	return hex.EncodeToString(sum[:])
}

func IsHash(value string) bool {
	raw, err := hex.DecodeString(value)
	return err == nil && len(raw) == sha256.Size && value == strings.ToLower(value)
}

// Previous — устаревшая проекция; её доказательства в S3 не удаляются.
type Previous struct {
	SnapshotID string `json:"snapshot_id"`
	ChunkCount int    `json:"chunk_count"`
}

type Job struct {
	Lease       Lease            `json:"lease"`
	ObjectKey   string           `json:"object_key"`
	ContentHash string           `json:"content_hash"`
	Source      sourcedoc.Source `json:"source"`
	Previous    []Previous       `json:"previous"`
	Active      *Manifest        `json:"active"`
	LeasedUntil time.Time        `json:"leased_until"`
}

// Revision сохраняет происхождение обычного файла без вымышленного Source.
func (j Job) Revision() string {
	if j.Source.Version != 0 {
		return j.Source.Revision
	}
	return j.ContentHash
}

// RawHash использует хеш издателя, когда он задан, иначе BLAKE3 загрузки.
func (j Job) RawHash() string {
	if j.Source.Version != 0 {
		return j.Source.ContentHash
	}
	return j.ContentHash
}

type Activation struct {
	Lease    Lease    `json:"lease"`
	Manifest Manifest `json:"manifest"`
}

type Finish struct {
	Pruned []Previous `json:"pruned,omitempty"`
	Lease  Lease      `json:"lease"`
	Error  string     `json:"error,omitempty"`
}

type Empty struct{}

// AccessChange: владелец берётся из аутентификации, а RequestID делает безопасным повтор после сетевого сбоя.
type AccessChange struct {
	DocumentID string `json:"document_id"`
	OwnerID    string `json:"owner_id"`
	RequestID  string `json:"request_id"`
	Revoked    bool   `json:"revoked"`
}

type Read struct {
	ReaderID     string   `json:"reader_id"`
	SharedCorpus bool     `json:"shared_corpus"`
	DocumentIDs  []string `json:"document_ids"`
	SnapshotID   string   `json:"snapshot_id,omitempty"`
	AccessOnly   bool     `json:"access_only,omitzero"`
}

// State отказанной записи раскрывает только запрошенный ID.
type State struct {
	DocumentID string            `json:"document_id"`
	OwnerID    string            `json:"owner_id,omitempty"`
	Allowed    bool              `json:"allowed"`
	Source     *sourcedoc.Source `json:"source,omitempty"`
	Active     *Manifest         `json:"active,omitempty"`
}

// States привязывает результат доступа к эпохе корпуса, чтобы изолировать кэш.
type States struct {
	Epoch     int64   `json:"epoch"`
	Documents []State `json:"documents"`
}

// Chunk: смещения — кодовые точки в точном сохранённом тексте. Локатор страниц отличает OCR
// от извлечения текста PDF; неизвестный остаётся текстовым.
type Chunk struct {
	ID          string            `json:"id"`
	Index       int               `json:"index"`
	Text        string            `json:"text"`
	LexicalText string            `json:"lexical_text"`
	Vector      []float32         `json:"vector"`
	CharStart   int               `json:"char_start"`
	CharEnd     int               `json:"char_end"`
	PageStart   int               `json:"page_start"`
	PageEnd     int               `json:"page_end"`
	LocatorKind string            `json:"locator_kind"`
	Section     string            `json:"section"`
	Metadata    map[string]string `json:"metadata"`
}

// Locator: кодовые точки, конец не включается, в том тексте, который закрепляет хэш снимка.
type Locator struct {
	Section        string `json:"section"`
	Page           int    `json:"page,omitzero"`
	StartChar      int    `json:"start_char"`
	EndChar        int    `json:"end_char"`
	ElementPath    string `json:"element_path,omitempty"`
	SnapshotSHA256 string `json:"snapshot_sha256,omitempty"`
}

func (l Locator) Verify(text []rune, quote string) error {
	if l.StartChar < 0 || l.EndChar <= l.StartChar || l.EndChar > len(text) {
		return fmt.Errorf("locator span [%d,%d) is outside the stored text: %w", l.StartChar, l.EndChar, ErrInvalid)
	}
	if got := string(text[l.StartChar:l.EndChar]); got != quote {
		return fmt.Errorf("locator does not reproduce its quote: %w", ErrInvalid)
	}
	return nil
}

func CanonicalSection(heading string) string {
	word := strings.ToLower(TrimHeadingNumber(strings.TrimSpace(heading)))
	word = strings.Trim(word, " .:—-：。、")
	if i := strings.IndexFunc(word, unicode.IsSpace); i > 0 {
		word = word[:i]
	}
	return sectionVocabulary()[word]
}

// TrimHeadingNumber требует разделителя после номера, чтобы «I» из «Introduction» не съедалась
// как римская цифра.
func TrimHeadingNumber(heading string) string {
	runes := []rune(heading)
	number := 0
	for number < len(runes) && strings.ContainsRune("0123456789IVXivx一二三四五六七八九十", runes[number]) {
		number++
	}
	if number == 0 || number == len(runes) {
		return heading
	}
	after := number
	for after < len(runes) && strings.ContainsRune(".)、 \t", runes[after]) {
		after++
	}
	if after == number {
		return heading
	}
	return string(runes[after:])
}

func sectionVocabulary() map[string]string {
	return map[string]string{
		"abstract": sourcedoc.SectionAbstract, "аннотация": sourcedoc.SectionAbstract,
		"реферат": sourcedoc.SectionAbstract, "摘要": sourcedoc.SectionAbstract,
		"introduction": sourcedoc.SectionIntroduction, "введение": sourcedoc.SectionIntroduction,
		"引言": sourcedoc.SectionIntroduction, "绪论": sourcedoc.SectionIntroduction,
		"method": sourcedoc.SectionMethod, "methods": sourcedoc.SectionMethod,
		"methodology": sourcedoc.SectionMethod, "методы": sourcedoc.SectionMethod,
		"метод": sourcedoc.SectionMethod, "методика": sourcedoc.SectionMethod,
		"методология": sourcedoc.SectionMethod, "方法": sourcedoc.SectionMethod,
		"experiments": sourcedoc.SectionExperiments, "experiment": sourcedoc.SectionExperiments,
		"experimental": sourcedoc.SectionExperiments, "evaluation": sourcedoc.SectionExperiments,
		"эксперимент": sourcedoc.SectionExperiments, "эксперименты": sourcedoc.SectionExperiments,
		"实验": sourcedoc.SectionExperiments, "实验方法": sourcedoc.SectionExperiments,
		"results": sourcedoc.SectionResults, "результаты": sourcedoc.SectionResults,
		"结果":         sourcedoc.SectionResults,
		"discussion": sourcedoc.SectionDiscussion, "обсуждение": sourcedoc.SectionDiscussion,
		"讨论":          sourcedoc.SectionDiscussion,
		"limitations": sourcedoc.SectionLimitations, "limitation": sourcedoc.SectionLimitations,
		"ограничения": sourcedoc.SectionLimitations, "局限性": sourcedoc.SectionLimitations,
		"conclusion": sourcedoc.SectionConclusion, "conclusions": sourcedoc.SectionConclusion,
		"выводы": sourcedoc.SectionConclusion, "заключение": sourcedoc.SectionConclusion,
		"结论":         sourcedoc.SectionConclusion,
		"references": sourcedoc.SectionReferences, "bibliography": sourcedoc.SectionReferences,
		"литература": sourcedoc.SectionReferences, "библиография": sourcedoc.SectionReferences,
		"参考文献":             sourcedoc.SectionReferences,
		"acknowledgements": sourcedoc.SectionAcknowledgements, "acknowledgments": sourcedoc.SectionAcknowledgements,
		"благодарности": sourcedoc.SectionAcknowledgements, "致谢": sourcedoc.SectionAcknowledgements,
		"background": sourcedoc.SectionBackground, "summary": sourcedoc.SectionSummary,
		"description": sourcedoc.SectionDescription, "claims": sourcedoc.SectionClaims,
	}
}

// Chunking фиксирует нарезку сохранённых векторов; LexicalText чанка — ровно вход эмбеддера, с заголовками.
type Chunking struct {
	Algorithm         string `json:"algorithm"`
	Unit              string `json:"unit"`
	Budget            int    `json:"budget"`
	OverlapRunes      int    `json:"overlap_runes"`
	WindowBytes       int    `json:"window_bytes"`
	BatchChunks       int    `json:"batch_chunks"`
	ContextualHeaders bool   `json:"contextual_headers"`
}

type Snapshot struct {
	RawHash          string            `json:"raw_hash,omitempty"`
	RawRevision      string            `json:"raw_revision,omitempty"`
	Filename         string            `json:"filename"`
	MIMEType         string            `json:"mime_type"`
	Title            string            `json:"title"`
	Chunking         Chunking          `json:"chunking"`
	Version          int               `json:"version"`
	DocumentID       string            `json:"document_id"`
	OwnerID          string            `json:"owner_id"`
	SnapshotID       string            `json:"snapshot_id"`
	Source           sourcedoc.Source  `json:"source"`
	TextHash         string            `json:"text_hash"`
	Model            Model             `json:"model"`
	PipelineRevision string            `json:"pipeline_revision"`
	ParserRevision   string            `json:"parser_revision"`
	ParserMetadata   map[string]string `json:"parser_metadata"`
	// Разделы хранятся отдельно от чанков: раздел переживает любую нарезку.
	Sections []sourcedoc.Section `json:"sections,omitempty"`
	Anchors  []sourcedoc.Anchor  `json:"anchors,omitempty"`
	Chunks   []Chunk             `json:"chunks"`
}

func ValidateVector(vector []float32, dimension int) error {
	if len(vector) != dimension || dimension < 1 {
		return fmt.Errorf("embedding dimension mismatch: %w", ErrInvalid)
	}
	norm := 0.0
	for _, value := range vector {
		n := float64(value)
		if math.IsNaN(n) || math.IsInf(n, 0) {
			return fmt.Errorf("nonfinite embedding: %w", ErrInvalid)
		}
		norm += n * n
	}
	if norm == 0 {
		return fmt.Errorf("zero embedding: %w", ErrInvalid)
	}
	return nil
}

func Decode[T any](raw string) (T, error) {
	return decodeBounded[T](raw, MaxRequestBytes)
}

// DecodeResponse учитывает суммарный размер метаданных разрешённой пачки.
func DecodeResponse[T any](raw string) (T, error) {
	return decodeBounded[T](raw, MaxResponseBytes)
}

func decodeBounded[T any](raw string, maximum int) (T, error) {
	var value T
	if len(raw) == 0 || len(raw) > maximum {
		return value, ErrInvalid
	}
	if err := json.Unmarshal([]byte(raw), &value, json.RejectUnknownMembers(true)); err != nil {
		return value, fmt.Errorf("decode index request: %w: %w", ErrInvalid, err)
	}
	return value, nil
}

// CanRead: документ без Source подчиняется старому общему режиму.
func CanRead(reader, owner string, source *sourcedoc.Source, shared bool, revoked bool) bool {
	if reader == "" || revoked {
		return false
	}
	if reader == owner {
		return true
	}
	if source == nil {
		return shared
	}
	return source.AccessScope == "public" || (source.AccessScope == "shared" && shared)
}
