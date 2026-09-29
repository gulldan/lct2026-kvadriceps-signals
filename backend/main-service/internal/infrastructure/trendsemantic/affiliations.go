package trendsemantic

import (
	"cmp"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"maps"
	"slices"
	"strings"
	"time"

	"github.com/example/main-service/internal/application"
	"github.com/example/main-service/internal/platform/openalex"
)

// OpenAlex ищет только по точному DOI: ни название, ни авторы не позволяют привязать организацию
// к статье. Результат датирован, и старая отсечка не видит нового наблюдения аффилиации.
type OpenAlex struct {
	Client *openalex.Client
	Now    func() time.Time
}

func NewOpenAlex(client *openalex.Client) *OpenAlex {
	return &OpenAlex{Client: client, Now: time.Now}
}

func (c *OpenAlex) Enrich(
	ctx context.Context, input application.LiveAnalysisRequest, budget int,
) (map[string]application.LivePaperMetadata, []application.LiveLimit) {
	metadata := maps.Clone(input.Metadata)
	if metadata == nil {
		metadata = map[string]application.LivePaperMetadata{}
	}
	limits := []application.LiveLimit{}
	if len(input.Snapshot.Periods) == 0 {
		return metadata, limits
	}
	cutoff := input.Snapshot.Periods[len(input.Snapshot.Periods)-1].EndExclusive
	now := c.Now().UTC()
	if (!input.EnrichMetadata || input.StrictAvailability) && now.Format(time.DateOnly) >= cutoff {
		return metadata, []application.LiveLimit{{Code: application.LiveLimitAffiliationsOff,
			Text: "affiliations: new provider observations excluded from this historical cutoff"}}
	}
	papers := slices.Clone(input.Snapshot.Papers)
	// Сначала давно не проверенные, чтобы следующий цикл продолжил ограниченный проход.
	slices.SortFunc(papers, func(a, b application.StudyPaper) int {
		return cmp.Or(cmp.Compare(metadata[a.ID].AffiliationCheckedAt, metadata[b.ID].AffiliationCheckedAt), cmp.Compare(a.ID, b.ID))
	})
	attempted, pending := 0, 0
	for _, paper := range papers {
		meta := metadata[paper.ID]
		if hasCurrentAffiliations(meta, now) {
			continue
		}
		if attempted >= max(0, min(128, budget)) {
			pending++
			continue
		}
		doi := affiliationDOI(paper.ID, meta.DOI)
		if doi == "" {
			continue
		}
		if err := ctx.Err(); err != nil {
			limits = append(limits, application.LiveLimit{
				Code: application.LiveLimitAffiliationsOff, Text: "affiliations: collection interrupted"})
			break
		}
		attempted++
		updated, available := c.enrichPaper(ctx, doi, meta)
		if !available {
			limits = append(limits, application.LiveLimit{Code: application.LiveLimitAffiliationsOff,
				Text: "affiliations: provider unavailable; prior observations retained"})
			break
		}
		metadata[paper.ID] = updated
		// Бесплатный бюджет публичного API мал: запроса в секунду хватает обогащению и не душит основной сбор.
		select {
		case <-ctx.Done():
			return metadata, append(limits, application.LiveLimit{
				Code: application.LiveLimitAffiliationsOff, Text: "affiliations: collection interrupted"})
		case <-time.After(time.Second):
		}
	}
	if pending > 0 {
		limits = append(limits, application.LiveLimit{Code: application.LiveLimitAffiliationsBudget,
			Values: map[string]int{"pending": pending, "checked": attempted},
			Text: fmt.Sprintf("affiliations: %d unchecked papers remain after the %d-record budget",
				pending, attempted)})
	}
	return metadata, limits
}

func hasCurrentAffiliations(meta application.LivePaperMetadata, now time.Time) bool {
	checked, _ := time.Parse(time.RFC3339, meta.AffiliationCheckedAt)
	return len(meta.Authorships) > 0 || (!checked.IsZero() && now.Sub(checked) < 7*24*time.Hour)
}

func affiliationDOI(paperID, raw string) string {
	doi := strings.TrimPrefix(strings.ToLower(strings.TrimSpace(raw)), "https://doi.org/")
	if doi == "" && strings.HasPrefix(paperID, "arxiv:") {
		doi = "10.48550/arxiv." + strings.TrimPrefix(paperID, "arxiv:")
	}
	if !strings.HasPrefix(doi, "10.") || !strings.Contains(doi, "/") {
		return ""
	}
	return doi
}

func (c *OpenAlex) enrichPaper(
	ctx context.Context, doi string, meta application.LivePaperMetadata,
) (application.LivePaperMetadata, bool) {
	record, hash, status := c.lookup(ctx, doi)
	if status == providerUnavailable {
		return meta, false
	}
	observed := c.Now().UTC().Format(time.RFC3339Nano)
	meta.AffiliationCheckedAt, meta.AffiliationStatus, meta.AffiliationHash = observed, status, hash
	if record != nil {
		meta.Authorships = authorships(record, observed)
		if len(meta.Authorships) == 0 {
			meta.AffiliationStatus = "no_affiliations"
		}
	}
	return meta, true
}

const providerUnavailable = "unavailable"

func (c *OpenAlex) lookup(ctx context.Context, doi string) (*openalex.Work, string, string) {
	work, raw, err := c.Client.WorkByDOI(ctx, doi, "id", "doi", "authorships")
	switch {
	case errors.Is(err, openalex.ErrNotFound):
		return nil, "", "not_found"
	case err != nil:
		return nil, "", providerUnavailable
	case !strings.HasPrefix(work.ID, "https://openalex.org/W") || work.PlainDOI() != doi:
		return nil, "", "identity_mismatch"
	}
	hash := sha256.Sum256(raw)
	return &work, hex.EncodeToString(hash[:]), "provider_attributed"
}

func authorships(w *openalex.Work, observed string) []application.LiveAuthorship {
	result := []application.LiveAuthorship{}
	for _, a := range w.Authorships {
		if !strings.HasPrefix(a.Author.ID, "https://openalex.org/A") {
			continue
		}
		for _, organization := range a.Institutions {
			if !strings.HasPrefix(organization.ID, "https://openalex.org/I") {
				continue
			}
			result = append(result, application.LiveAuthorship{
				AuthorID: a.Author.ID, AuthorName: a.Author.DisplayName,
				OrganizationID: organization.ID, OrganizationName: organization.DisplayName,
				SourceURL:  "https://api.openalex.org/works/" + strings.TrimPrefix(w.ID, "https://openalex.org/"),
				ObservedAt: observed,
			})
		}
	}
	return result
}
