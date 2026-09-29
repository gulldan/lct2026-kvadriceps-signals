package provider

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json/v2"
	"encoding/xml"
	"errors"
	"fmt"
	"io"
	"net/url"
	"strconv"
	"strings"
	"time"
)

const arxivOAIPrefix = "oai-v1:"

// arXiv сам выбирает размер страницы OAI (сейчас до 1300 записей): ответ режется на ограниченные страницы
// сборщика без потери записей, а окно datestamp не выдаётся за поиск по дате публикации.
type arxivOAICursor struct {
	Token  string `json:"token,omitempty"`
	Offset int    `json:"offset"`
	Hash   string `json:"records_sha256,omitempty"`
}

type arxivOAICache struct {
	Endpoint string
	Raw      []byte
	Observed time.Time
	Response arxivOAIResponse
}

type arxivOAIResponse struct {
	XMLName xml.Name `xml:"http://www.openarchives.org/OAI/2.0/ OAI-PMH"`
	Error   struct {
		Code string `xml:"code,attr"`
	} `xml:"error"`
	List *struct {
		Records []arxivOAIRecord `xml:"record"`
		Token   string           `xml:"resumptionToken"`
	} `xml:"ListRecords"`
}

func (a *ArXiv) fetchOAI(ctx context.Context, scope Scope, cursor string) (Page, error) {
	a.oaiMu.Lock()
	defer a.oaiMu.Unlock()
	position, err := arxivOAIPosition(cursor)
	if err != nil {
		return Page{}, err
	}
	set, err := arxivOAIQuerySet(scope)
	if err != nil {
		return Page{}, err
	}
	q := url.Values{"verb": {"ListRecords"}, "metadataPrefix": {"arXivRaw"}, "from": {scope.From}, "until": {scope.Through}}
	if set != "" {
		q.Set("set", set)
	}
	if position.Token != "" {
		// OAI запрещает добавлять исходные параметры к запросу с токеном.
		q = url.Values{"verb": {"ListRecords"}, "resumptionToken": {position.Token}}
	}
	endpoint := a.base + "?" + q.Encode()
	if a.oaiCache.Endpoint != endpoint || position.Offset == 0 {
		raw, observed, fetchErr := a.fetcher.get(ctx, endpoint)
		if fetchErr != nil {
			return Page{}, fetchErr
		}
		response, decodeErr := decodeArxivOAI(raw)
		if decodeErr != nil {
			return Page{}, decodeErr
		}
		a.oaiCache = arxivOAICache{Endpoint: endpoint, Raw: raw, Observed: observed, Response: response}
	}
	return a.oaiCache.page(scope, cursor, position)
}

func arxivOAIPosition(cursor string) (arxivOAICursor, error) {
	var position arxivOAICursor
	if cursor == "" || cursor == "0" {
		return position, nil
	}
	encoded, ok := strings.CutPrefix(cursor, arxivOAIPrefix)
	if !ok {
		return position, fmt.Errorf("%w: saved Atom offset cannot resume an OAI window", ErrCursorExpired)
	}
	raw, err := base64.RawURLEncoding.DecodeString(encoded)
	if err != nil || len(cursor) > 12000 || json.Unmarshal(raw, &position, json.RejectUnknownMembers(true)) != nil ||
		position.Offset < 0 || position.Offset > 10000 || len(position.Token) > 6000 ||
		(position.Offset > 0 && len(position.Hash) != 64) || (position.Offset == 0 && position.Hash != "") {
		return position, errors.New("invalid arxiv OAI cursor")
	}
	return position, nil
}

func arxivOAISet(category string) string {
	archive, subject, _ := strings.Cut(category, ".")
	group := archive
	switch archive {
	case "astro-ph", "cond-mat", "gr-qc", "hep-ex", "hep-lat", "hep-ph", "hep-th", "math-ph", "nlin",
		"nucl-ex", "nucl-th", "physics", "quant-ph":
		group = "physics"
	}
	if subject != "" {
		return group + ":" + archive + ":" + subject
	}
	return group + ":" + archive
}

func decodeArxivOAI(raw []byte) (arxivOAIResponse, error) {
	var response arxivOAIResponse
	decoder := xml.NewDecoder(bytes.NewReader(raw))
	for {
		token, err := decoder.Token()
		if errors.Is(err, io.EOF) {
			break
		}
		if err != nil {
			return response, fmt.Errorf("invalid arxiv OAI XML: %w", err)
		}
		if _, ok := token.(xml.Directive); ok {
			return response, errors.New("arxiv OAI XML directives are forbidden")
		}
	}
	// #nosec G709 -- the response is bounded by fetcher and all XML directives
	// were rejected above; Go's XML decoder never fetches external entities.
	if err := xml.Unmarshal(raw, &response); err != nil {
		return response, fmt.Errorf("decode arxiv OAI response: %w", err)
	}
	switch response.Error.Code {
	case "badResumptionToken":
		return response, fmt.Errorf("%w: OAI resumption token expired", ErrCursorExpired)
	case "noRecordsMatch":
		if response.List == nil {
			return response, nil
		}
	case "":
		if response.List != nil && len(response.List.Records) <= 10000 &&
			(len(response.List.Records) > 0 || response.List.Token == "") {
			return response, nil
		}
	}
	return response, errors.New("invalid arxiv OAI list envelope or provider error")
}

func (cache arxivOAICache) page(scope Scope, cursor string, position arxivOAICursor) (Page, error) {
	page := Page{Provider: providerArXiv, Scope: scope, Cursor: cursor, Observed: cache.Observed, Total: -1,
		Limitations: []string{
			"oai_datestamp_window_not_submission_search; later metadata changes can hide historical publications",
			"local_all_query_terms_in_title_abstract_categories; budget counts scanned OAI records",
			"oai_latest_metadata_only; publication and revision dates use arXivRaw version history",
		}}
	var records []arxivOAIRecord
	nextToken := ""
	if cache.Response.List != nil {
		records, nextToken = cache.Response.List.Records, strings.TrimSpace(cache.Response.List.Token)
	}
	metadata, err := json.Marshal(records)
	if err != nil {
		return page, fmt.Errorf("encode arxiv OAI record evidence: %w", err)
	}
	hash := digest(metadata)
	if position.Offset > len(records) || (position.Hash != "" && position.Hash != hash) {
		return page, fmt.Errorf("%w: OAI page changed before the saved offset", ErrCursorExpired)
	}
	end := min(position.Offset+scope.PageSize, len(records))
	page.Received = end - position.Offset
	page.Exhausted = end == len(records) && nextToken == ""
	if !page.Exhausted {
		next := arxivOAICursor{Token: position.Token, Offset: end, Hash: hash}
		if end == len(records) {
			next = arxivOAICursor{Token: nextToken}
		}
		if next == position || len(next.Token) > 6000 {
			return page, fmt.Errorf("%w: arxiv OAI cursor did not advance", ErrCursorExpired)
		}
		encoded, marshalErr := json.Marshal(next)
		if marshalErr != nil {
			return page, fmt.Errorf("encode arxiv OAI cursor: %w", marshalErr)
		}
		page.Next = arxivOAIPrefix + base64.RawURLEncoding.EncodeToString(encoded)
	}
	for _, item := range records[position.Offset:end] {
		record, recordErr := item.record(cache.Observed, scope)
		if recordErr != nil {
			page.Rejected = append(page.Rejected, Rejected{NativeID: item.Header.Identifier, Reason: recordErr.Error()})
			continue
		}
		page.Records = append(page.Records, record)
	}
	// Сохраняется точный XML ответа с выбранным диапазоном: у каждого среза свой хеш сырья.
	page.Raw, err = json.Marshal(struct {
		XML    string `json:"oai_response_xml"`
		Offset int    `json:"offset"`
		Count  int    `json:"count"`
	}{string(cache.Raw), position.Offset, page.Received})
	if err != nil {
		return page, fmt.Errorf("encode arxiv OAI page evidence: %w", err)
	}
	page.RawHash = digest(page.Raw)
	return page, page.Validate(scope.PageSize)
}

func arxivOAIVersion(version string, index int) bool {
	return version == "v"+strconv.Itoa(index+1)
}
