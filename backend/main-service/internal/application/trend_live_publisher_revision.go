package application

import (
	"cmp"
	"context"
	"encoding/json/v2"
	"errors"
	"fmt"
	"io"
	"mime"
	"net"
	"net/http"
	"net/netip"
	"net/url"
	"strconv"
	"strings"
	"sync"
	"syscall"
	"time"
	"unicode/utf8"

	"github.com/example/main-service/internal/platform/openalex"
)

const (
	liveZenodoOrigin       = "https://zenodo.org"
	liveZenodoDOIPrefix    = "10.5281/zenodo."
	livePublisherCoverage  = "publisher_metadata"
	livePublisherDocuments = 32
	livePublisherRequest   = 9 * time.Second
	livePublisherBudget    = 45 * time.Second
	livePublisherMaxBody   = 512 << 10
	livePublisherLimit     = "publisher_revision_unavailable"
)

// LivePublisherRevision сохраняет авторский текст без изменения даты работы или наблюдения аффилиаций.
type LivePublisherRevision struct {
	RequestedDOI    string    `json:"requested_doi"`
	ResolvedDOI     string    `json:"resolved_doi"`
	ConceptDOI      string    `json:"concept_doi"`
	SourceURL       string    `json:"source_url"`
	APIURL          string    `json:"api_url"`
	SourceUpdatedAt time.Time `json:"source_updated_at"`
	FetchedAt       time.Time `json:"fetched_at"`
	RawSHA256       string    `json:"raw_sha256"`
	TextSHA256      string    `json:"text_sha256"`
	Text            string    `json:"text"`
	Coverage        string    `json:"coverage"`
}

type livePublisherLoad struct {
	doi      string
	revision *LivePublisherRevision
	err      error
}

// Обновляется локальная совокупность до её передачи читателям; работники не меняют общие карты.
func liveRefreshPublisherRevisions(ctx context.Context, papers []StudyPaper, metadata map[string]LivePaperMetadata,
	client *http.Client,
) ([]StudyPaper, []LiveLimit) {
	loads := make([]*livePublisherLoad, 0, len(papers))
	byDOI := make(map[string]*livePublisherLoad, len(papers))
	for _, paper := range papers {
		doi := livePublisherDOI(paper, metadata[paper.ID])
		if doi == "" || byDOI[doi] != nil {
			continue
		}
		load := &livePublisherLoad{doi: doi, err: errors.New("publisher metadata budget exhausted")}
		loads = append(loads, load)
		byDOI[doi] = load
	}
	if len(loads) == 0 {
		return papers, nil
	}
	defer client.CloseIdleConnections()
	work, cancel := context.WithTimeout(ctx, livePublisherBudget)
	defer cancel()
	jobs := make(chan *livePublisherLoad)
	var workers sync.WaitGroup
	for range 2 {
		workers.Go(func() {
			for load := range jobs {
				load.revision, load.err = liveFetchPublisherRevision(work, client, load.doi)
				if status, ok := errors.AsType[*livePublisherStatusError](load.err); ok &&
					status.code == http.StatusTooManyRequests {
					cancel()
				}
			}
		})
	}
	for _, load := range loads[:min(len(loads), livePublisherDocuments)] {
		if work.Err() != nil {
			break
		}
		select {
		case jobs <- load:
		case <-work.Done():
		}
	}
	close(jobs)
	workers.Wait()
	return liveApplyPublisherRevisions(papers, metadata, byDOI)
}

// Приватный снимок никогда не подменяется более поздним публичным текстом с похожим идентификатором.
func livePublisherDOI(paper StudyPaper, meta LivePaperMetadata) string {
	if paper.DocumentID != "" || paper.SnapshotID != "" || !strings.HasPrefix(paper.ID, "openalex:") {
		return ""
	}
	doi := openalex.PlainDOI(cmp.Or(paper.CanonicalID, meta.DOI))
	if strings.HasPrefix(doi, liveZenodoDOIPrefix) {
		return doi
	}
	return ""
}

func liveApplyPublisherRevisions(papers []StudyPaper, metadata map[string]LivePaperMetadata,
	loads map[string]*livePublisherLoad,
) ([]StudyPaper, []LiveLimit) {
	kept := make([]StudyPaper, 0, len(papers))
	excluded := 0
	for _, paper := range papers {
		meta := metadata[paper.ID]
		load := loads[livePublisherDOI(paper, meta)]
		if load != nil {
			if load.err != nil || load.revision == nil {
				excluded++
				delete(metadata, paper.ID)
				continue
			}
			revision := *load.revision
			meta.Abstract, meta.SourceURL, meta.SourceRevision = revision.Text, revision.SourceURL, &revision
			meta.ContentHash = revision.TextSHA256
			meta.Digest, meta.DigestModel = "", ""
			paper.URL, paper.ContentHash = revision.SourceURL, revision.TextSHA256
			metadata[paper.ID] = meta
		}
		kept = append(kept, paper)
	}
	if excluded == 0 {
		return kept, nil
	}
	return kept, []LiveLimit{liveLimitOf(livePublisherLimit,
		fmt.Sprintf("Zenodo: %d источников исключены — актуальные авторские метаданные не подтверждены; "+
			"старая аннотация не использована", excluded), map[string]int{"excluded": excluded, "unique": len(loads)})}
}

func livePublisherClient() *http.Client {
	transport := &http.Transport{DialContext: (&net.Dialer{Timeout: livePublisherRequest,
		Control: livePublisherPublicAddress}).DialContext, TLSHandshakeTimeout: livePublisherRequest,
		ResponseHeaderTimeout: livePublisherRequest, IdleConnTimeout: livePublisherRequest}
	return &http.Client{Transport: transport, Timeout: livePublisherRequest,
		CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}
}

// Даже фиксированный хост не даёт разрешения соединяться с внутренним адресом после разрешения имени.
func livePublisherPublicAddress(_, address string, _ syscall.RawConn) error {
	endpoint, err := netip.ParseAddrPort(address)
	if err != nil {
		return fmt.Errorf("publisher address: %w", err)
	}
	ip := endpoint.Addr().Unmap()
	if !ip.IsGlobalUnicast() || ip.IsPrivate() || ip.IsLoopback() ||
		netip.MustParsePrefix("100.64.0.0/10").Contains(ip) {
		return errors.New("publisher address is not public")
	}
	return nil
}

func liveZenodoRecordID(doi string) string {
	id, ok := strings.CutPrefix(doi, liveZenodoDOIPrefix)
	if !ok || len(id) == 0 || len(id) > 18 || id[0] == '0' || strings.ContainsFunc(id, func(r rune) bool {
		return r < '0' || r > '9'
	}) {
		return ""
	}
	return id
}

func liveFetchPublisherRevision(ctx context.Context, client *http.Client, doi string) (*LivePublisherRevision, error) {
	id := liveZenodoRecordID(doi)
	if id == "" {
		return nil, errors.New("invalid Zenodo record DOI")
	}
	work, cancel := context.WithTimeout(ctx, livePublisherRequest)
	defer cancel()
	deadline, _ := work.Deadline()
	address := liveZenodoOrigin + "/api/records/" + id
	for attempt := range 2 {
		if err := work.Err(); err != nil {
			return nil, fmt.Errorf("publisher metadata context: %w", err)
		}
		// Таймер отмены может ещё ждать выполнения, хотя абсолютный срок уже наступил.
		if !time.Now().Before(deadline) {
			return nil, fmt.Errorf("publisher metadata deadline: %w", context.DeadlineExceeded)
		}
		body, redirect, err := livePublisherJSON(work, client, address)
		if err != nil {
			return nil, err
		}
		if redirect == "" {
			return liveDecodePublisherRevision(body, doi, address)
		}
		if attempt == 1 {
			return nil, errors.New("publisher metadata redirect limit exceeded")
		}
		address = redirect
	}
	return nil, errors.New("publisher metadata unavailable")
}

func livePublisherJSON(ctx context.Context, client *http.Client, address string) ([]byte, string, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, address, http.NoBody)
	if err != nil {
		return nil, "", fmt.Errorf("publisher metadata request: %w", err)
	}
	req.Header.Set("Accept", "application/json")
	req.Header.Set("User-Agent", "rag-test-signalgraph/1.0")
	response, err := client.Do(req)
	if err != nil {
		return nil, "", fmt.Errorf("publisher metadata fetch: %w", err)
	}
	defer func() { _ = response.Body.Close() }()
	if response.StatusCode >= 300 && response.StatusCode < 400 {
		redirect, locationErr := response.Location()
		if locationErr != nil || !livePublisherAddress(redirect) {
			return nil, "", errors.New("publisher metadata redirect leaves the record API")
		}
		return nil, redirect.String(), nil
	}
	if response.StatusCode != http.StatusOK {
		return nil, "", &livePublisherStatusError{code: response.StatusCode}
	}
	kind, _, err := mime.ParseMediaType(response.Header.Get("Content-Type"))
	if err != nil || kind != "application/json" {
		return nil, "", errors.New("publisher metadata is not JSON")
	}
	body, err := io.ReadAll(io.LimitReader(response.Body, livePublisherMaxBody+1))
	if err != nil {
		return nil, "", fmt.Errorf("publisher metadata body: %w", err)
	}
	if len(body) > livePublisherMaxBody {
		return nil, "", errors.New("publisher metadata exceeds body limit")
	}
	return body, "", nil
}

type livePublisherStatusError struct{ code int }

func (e *livePublisherStatusError) Error() string {
	return fmt.Sprintf("publisher metadata HTTP %d", e.code)
}

func livePublisherAddress(address *url.URL) bool {
	if address == nil || address.Scheme != liveCardSecureScheme || address.Host != "zenodo.org" || address.User != nil ||
		address.RawPath != "" || address.RawQuery != "" || address.Fragment != "" || address.ForceQuery {
		return false
	}
	id, ok := strings.CutPrefix(address.Path, "/api/records/")
	return ok && liveZenodoRecordID(liveZenodoDOIPrefix+id) != ""
}

type liveZenodoRecord struct {
	ID         int64     `json:"id"`
	DOI        string    `json:"doi"`
	ConceptDOI string    `json:"conceptdoi"`
	Updated    time.Time `json:"updated"`
	Metadata   struct {
		DOI         string `json:"doi"`
		Description string `json:"description"`
	} `json:"metadata"`
}

func liveDecodePublisherRevision(body []byte, requested, address string) (*LivePublisherRevision, error) {
	var record liveZenodoRecord
	if err := json.Unmarshal(body, &record); err != nil {
		return nil, fmt.Errorf("decode publisher metadata: %w", err)
	}
	resolved := liveZenodoDOIPrefix + strconv.FormatInt(record.ID, 10)
	if record.ID <= 0 || record.DOI != resolved || record.Metadata.DOI != resolved ||
		liveZenodoRecordID(record.ConceptDOI) == "" || (requested != record.ConceptDOI && requested != resolved) ||
		record.Updated.IsZero() {
		return nil, errors.New("publisher metadata DOI or version mismatch")
	}
	// После ручного перехода тело обязано принадлежать именно адресу полученной версии.
	api := liveZenodoOrigin + "/api/records/" + strconv.FormatInt(record.ID, 10)
	if address != api && address != liveZenodoOrigin+"/api/records/"+liveZenodoRecordID(record.ConceptDOI) {
		return nil, errors.New("publisher metadata response changed record identity")
	}
	text, err := SourceHTMLText(strings.NewReader(record.Metadata.Description))
	if err != nil {
		return nil, err
	}
	if utf8.RuneCountInString(text) > liveCardAbstract {
		return nil, errors.New("whole publisher description exceeds source context")
	}
	return &LivePublisherRevision{RequestedDOI: requested, ResolvedDOI: resolved, ConceptDOI: record.ConceptDOI,
		SourceURL: liveZenodoOrigin + "/records/" + strconv.FormatInt(record.ID, 10), APIURL: address,
		SourceUpdatedAt: record.Updated, FetchedAt: time.Now().UTC(), RawSHA256: liveCardTextHash(string(body)),
		TextSHA256: liveCardTextHash(text), Text: text, Coverage: livePublisherCoverage}, nil
}

func liveSourceRevisions(papers []StudyPaper, metadata map[string]LivePaperMetadata) map[string]LivePublisherRevision {
	var revisions map[string]LivePublisherRevision
	for _, paper := range papers {
		meta := metadata[paper.ID]
		if livePublisherDOI(paper, meta) == "" || meta.SourceRevision == nil {
			continue
		}
		if revisions == nil {
			revisions = map[string]LivePublisherRevision{}
		}
		revisions[paper.ID] = *meta.SourceRevision
	}
	return revisions
}
