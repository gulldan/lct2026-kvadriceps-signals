package application

import (
	"cmp"
	"maps"
	"net/url"
	"regexp"
	"slices"
	"strings"
	"time"
)

type LiveEvidenceGroup struct {
	PaperIDs        []string `json:"paper_ids"`
	AuthorIDs       []string `json:"author_ids"`
	OrganizationIDs []string `json:"organization_ids"`
	Sources         []string `json:"sources"`
}

type LiveIndependence struct {
	Method                     string              `json:"method"`
	Status                     string              `json:"status"`
	ObservedAt                 string              `json:"observed_at,omitempty"`
	RecordsExamined            int                 `json:"records_examined"`
	DistinctEvents             int                 `json:"distinct_events"`
	DependentRecords           int                 `json:"dependent_records"`
	AttributedPapers           int                 `json:"attributed_papers"`
	UnattributedPapers         int                 `json:"unattributed_papers"`
	DisjointOrganizationGroups int                 `json:"disjoint_organization_groups"`
	IndependentImplementations *int                `json:"independent_implementations"`
	UnreviewedEvents           int                 `json:"unreviewed_events"`
	Groups                     []LiveEvidenceGroup `json:"groups"`
	Links                      []LiveEvidenceLink  `json:"links"`
}

var liveRepositoryURL = regexp.MustCompile(`https://github\.com/[A-Za-z0-9_.-]+/[A-Za-z0-9_.-]+`)

func liveAnalysisDate(value string) (time.Time, bool) {
	if len(value) < 10 {
		return time.Time{}, false
	}
	date, err := time.Parse(time.DateOnly, value[:10])
	return date, err == nil
}

func liveBeforeCutoff(value, cutoff string) bool {
	date, ok := liveAnalysisDate(value)
	end, endOK := liveAnalysisDate(cutoff)
	return ok && endOK && date.Before(end)
}

// liveObservedBy: срез без времени исключает свой день, явная метка наблюдения включается; свидетельство с
// одной датой не известно раньше конца этого дня.
func liveObservedBy(value, observation string) bool {
	if len(observation) == len(time.DateOnly) {
		return liveBeforeCutoff(value, observation)
	}
	end, err := time.Parse(time.RFC3339Nano, observation)
	if err != nil {
		return false
	}
	at, err := time.Parse(time.RFC3339Nano, value)
	if err != nil {
		if len(value) != len(time.DateOnly) {
			return false
		}
		date, ok := liveAnalysisDate(value)
		if !ok {
			return false
		}
		at = date.AddDate(0, 0, 1).Add(-time.Nanosecond)
	}
	return !at.After(end)
}

func liveEvidenceURL(raw string) string {
	u, err := url.Parse(raw)
	if err != nil || u.Scheme != "https" || u.Hostname() == "" || u.User != nil {
		return ""
	}
	u.Host = strings.ToLower(u.Host)
	u.Fragment = ""
	if u.Hostname() == "github.com" {
		parts := strings.Split(strings.Trim(u.Path, "/"), "/")
		if len(parts) < 2 || parts[0] == "" || parts[1] == "" {
			return ""
		}
		u.Path = "/" + strings.ToLower(parts[0]) + "/" + strings.TrimSuffix(strings.ToLower(parts[1]), ".git")
		u.RawQuery = ""
	} else {
		q := u.Query()
		for key := range q {
			if strings.HasPrefix(key, "utm_") {
				q.Del(key)
			}
		}
		u.RawQuery = q.Encode()
		u.Path = strings.TrimRight(u.Path, "/")
	}
	return u.String()
}

func liveVerifiedAuthorship(a LiveAuthorship, cutoff string) bool {
	return liveEvidenceURL(a.AuthorID) != "" && liveEvidenceURL(a.OrganizationID) != "" &&
		liveEvidenceURL(a.SourceURL) != "" && liveObservedBy(a.ObservedAt, cutoff)
}

func liveIndependence(
	topic LiveTopic, papers []StudyPaper, metadata map[string]LivePaperMetadata, cutoff string, observation ...string,
) LiveIndependence {
	result := LiveIndependence{Method: "attributed-organization-components-v1", Status: "unknown",
		Groups: []LiveEvidenceGroup{}, Links: []LiveEvidenceLink{}}
	evidenceCutoff := cutoff
	if len(observation) > 0 {
		evidenceCutoff = observation[0]
	}
	result.ObservedAt = evidenceCutoff
	if len(evidenceCutoff) == len(time.DateOnly) {
		if end, ok := liveAnalysisDate(evidenceCutoff); ok {
			result.ObservedAt = end.Add(-time.Nanosecond).Format(time.RFC3339Nano)
		}
	}
	selected := slices.DeleteFunc(liveDatedTopicPapers(topic, papers, cutoff), func(paper StudyPaper) bool {
		return liveInvestorContext(metadata[paper.ID].Genre)
	})
	parent := make([]int, len(selected))
	for i := range parent {
		parent[i] = i
	}
	valid := liveAuthorshipComponents(selected, metadata, evidenceCutoff, parent)
	for _, authorships := range valid {
		if len(authorships) == 0 {
			result.UnattributedPapers++
		} else {
			result.AttributedPapers++
		}
	}
	result.Groups = liveAuthorshipGroups(selected, valid, parent)
	result.DisjointOrganizationGroups = len(result.Groups)
	if result.AttributedPapers > 0 {
		result.Status = "provider_attributed_independence_unreviewed"
	}
	liveEvidenceEvents(&result, topic, selected, metadata, cutoff, evidenceCutoff)
	return result
}

func liveDatedTopicPapers(topic LiveTopic, papers []StudyPaper, cutoff string) []StudyPaper {
	wanted := map[string]bool{}
	for _, id := range topic.PaperIDs {
		wanted[id] = true
	}
	selected := []StudyPaper{}
	for _, paper := range liveUniquePapers(papers) {
		if wanted[paper.ID] && liveBeforeCutoff(paper.Date, cutoff) {
			selected = append(selected, paper)
		}
	}
	return selected
}

func livePaperRoot(parent []int, i int) int {
	for parent[i] != i {
		parent[i] = parent[parent[i]]
		i = parent[i]
	}
	return i
}

func liveAuthorshipComponents(
	papers []StudyPaper, metadata map[string]LivePaperMetadata, cutoff string, parent []int,
) [][]LiveAuthorship {
	identity := map[string]int{}
	valid := make([][]LiveAuthorship, len(papers))
	for i, paper := range papers {
		for _, authorship := range metadata[paper.ID].Authorships {
			if !liveVerifiedAuthorship(authorship, cutoff) {
				continue
			}
			valid[i] = append(valid[i], authorship)
			for _, key := range []string{"author:" + authorship.AuthorID, "org:" + authorship.OrganizationID} {
				if previous, ok := identity[key]; ok {
					parent[livePaperRoot(parent, i)] = livePaperRoot(parent, previous)
				} else {
					identity[key] = i
				}
			}
		}
	}
	return valid
}

func liveAuthorshipGroups(papers []StudyPaper, valid [][]LiveAuthorship, parent []int) []LiveEvidenceGroup {
	groups := map[int]*LiveEvidenceGroup{}
	for i, paper := range papers {
		if len(valid[i]) == 0 {
			continue
		}
		key := livePaperRoot(parent, i)
		if groups[key] == nil {
			groups[key] = &LiveEvidenceGroup{}
		}
		group := groups[key]
		group.PaperIDs = append(group.PaperIDs, paper.ID)
		for _, a := range valid[i] {
			group.AuthorIDs = append(group.AuthorIDs, a.AuthorID)
			group.OrganizationIDs = append(group.OrganizationIDs, a.OrganizationID)
			group.Sources = append(group.Sources, a.SourceURL)
		}
	}
	result := make([]LiveEvidenceGroup, 0, len(groups))
	for _, group := range groups {
		group.PaperIDs = liveSortedUnique(group.PaperIDs)
		group.AuthorIDs = liveSortedUnique(group.AuthorIDs)
		group.OrganizationIDs = liveSortedUnique(group.OrganizationIDs)
		group.Sources = liveSortedUnique(group.Sources)
		result = append(result, *group)
	}
	slices.SortFunc(result, func(a, b LiveEvidenceGroup) int { return cmp.Compare(a.PaperIDs[0], b.PaperIDs[0]) })
	return result
}

func liveSortedUnique(values []string) []string {
	return slices.Compact(slices.Sorted(slices.Values(values)))
}

func liveEvidenceEvents(
	result *LiveIndependence, topic LiveTopic, papers []StudyPaper,
	metadata map[string]LivePaperMetadata, cutoff, observation string,
) {
	metadata = liveChannelEvidenceMetadata(topic, metadata)
	urls, links := liveEvidenceRecords(topic, papers, metadata, cutoff, observation)
	parent := map[string]string{}
	for _, u := range urls {
		liveEvidenceRoot(parent, u)
	}
	decisions := map[string]bool{}
	seen := map[string]bool{}
	for _, link := range links {
		u, p := liveEvidenceURL(link.URL), liveEvidenceURL(link.ParentURL)
		if u == "" || liveEvidenceURL(link.SourceURL) == "" || !liveObservedBy(link.ObservedAt, observation) {
			continue
		}
		key := link.Kind + ":" + u + ":" + p
		if seen[key] {
			continue
		}
		seen[key] = true
		link.URL, link.ParentURL = u, p
		result.Links = append(result.Links, link)
		if link.Kind == "independence_review" && link.Independent != nil {
			decisions[u] = *link.Independent
		}
		if p != "" && liveDependentLink(link.Kind) {
			parent[liveEvidenceRoot(parent, u)] = liveEvidenceRoot(parent, p)
		}
	}
	liveSummarizeEvidence(result, urls, parent, decisions)
}

func liveChannelEvidenceMetadata(topic LiveTopic, metadata map[string]LivePaperMetadata) map[string]LivePaperMetadata {
	metadata = maps.Clone(metadata)
	if metadata == nil {
		metadata = map[string]LivePaperMetadata{}
	}
	families := map[string]string{}
	for _, channel := range []*LiveChannel{topic.Patents, topic.Web} {
		if channel == nil {
			continue
		}
		for _, record := range channel.Records {
			metadata[record.ID] = liveRecordEvidenceMetadata(record, channel.ObservedAt, metadata[record.ID], families)
		}
	}
	return metadata
}

func liveRecordEvidenceMetadata(
	record LiveSourceRecord, observedAt string, meta LivePaperMetadata, families map[string]string,
) LivePaperMetadata {
	meta.Genre = cmp.Or(record.Genre, meta.Genre)
	meta.SourceURL = record.URL
	if len(record.Provenance) > 0 {
		meta.SourceURL = record.Provenance[0].URL
	}
	meta.ContentHash, meta.ObservedAt = record.ContentHash, cmp.Or(record.ObservedAt, observedAt)
	add := func(kind, parent string) {
		meta.Links = append(meta.Links, LiveEvidenceLink{Kind: kind, URL: record.URL, ParentURL: parent,
			SourceURL: meta.SourceURL, ObservedAt: meta.ObservedAt})
	}
	if record.CanonicalURL != "" && record.CanonicalURL != record.URL {
		add("primary_source", record.CanonicalURL)
	}
	if record.OriginalURL != "" && record.OriginalBasis != "" {
		add("reprint", record.OriginalURL)
	}
	if record.FamilyID != "" && record.FamilyBasis != "" {
		if first, ok := families[record.FamilyID]; ok {
			add("patent_family", first)
		} else {
			families[record.FamilyID] = record.URL
		}
	}
	return meta
}

func liveEvidenceRecords(
	topic LiveTopic, papers []StudyPaper, metadata map[string]LivePaperMetadata, cutoff, observation string,
) ([]string, []LiveEvidenceLink) {
	urls, links := []string{}, []LiveEvidenceLink{}
	content := map[string]string{}
	add := func(id, raw string) {
		if liveInvestorContext(metadata[id].Genre) {
			return
		}
		u, records := liveRecordLinks(raw, metadata[id], observation, content)
		if u != "" {
			urls = append(urls, u)
			links = append(links, records...)
		}
	}
	for _, paper := range papers {
		add(paper.ID, paper.URL)
		meta := metadata[paper.ID]
		if liveBeforeCutoff(meta.AvailableAt, cutoff) && liveEvidenceURL(meta.SourceURL) != "" {
			for _, repository := range liveRepositoryURL.FindAllString(meta.Abstract, -1) {
				links = append(links, LiveEvidenceLink{Kind: "paper_repository", URL: repository,
					ParentURL: paper.URL, SourceURL: meta.SourceURL, ObservedAt: meta.AvailableAt})
			}
		}
	}
	for _, event := range topic.External {
		if liveBeforeCutoff(event.Date, cutoff) {
			add(event.URL, event.URL)
		}
	}
	for _, channel := range []*LiveChannel{topic.Patents, topic.Web} {
		for _, record := range liveChannelRecords(channel) {
			if liveBeforeCutoff(record.Date, cutoff) {
				add(record.ID, record.URL)
			}
		}
	}
	return urls, links
}

func liveRecordLinks(
	raw string, meta LivePaperMetadata, observation string, content map[string]string,
) (string, []LiveEvidenceLink) {
	u := liveEvidenceURL(raw)
	if u == "" {
		return "", nil
	}
	links := []LiveEvidenceLink{}
	if meta.ContentHash != "" && liveObservedBy(meta.ObservedAt, observation) {
		if original, ok := content[meta.ContentHash]; ok {
			links = append(links, LiveEvidenceLink{Kind: "identical_content", URL: u, ParentURL: original,
				SourceURL: meta.SourceURL, ObservedAt: meta.ObservedAt})
		} else {
			content[meta.ContentHash] = u
		}
	}
	return u, append(links, meta.Links...)
}

func liveEvidenceRoot(parent map[string]string, value string) string {
	if root, ok := parent[value]; ok && root != value {
		parent[value] = liveEvidenceRoot(parent, root)
		return parent[value]
	}
	parent[value] = value
	return value
}

func liveDependentLink(kind string) bool {
	switch kind {
	case "paper_repository", "fork", "release", "reprint", "primary_source", "identical_content", "patent_family":
		return true
	default:
		return false
	}
}

func liveSummarizeEvidence(result *LiveIndependence, urls []string, parent map[string]string, decisions map[string]bool) {
	roots := map[string]bool{}
	for _, u := range urls {
		roots[liveEvidenceRoot(parent, u)] = true
	}
	result.RecordsExamined, result.DistinctEvents = len(urls), len(roots)
	result.DependentRecords = result.RecordsExamined - result.DistinctEvents
	reviewed, independent := map[string]bool{}, map[string]bool{}
	for u, decision := range decisions {
		root := liveEvidenceRoot(parent, u)
		if roots[root] {
			reviewed[root] = true
			if decision {
				independent[root] = true
			}
		}
	}
	result.UnreviewedEvents = len(roots) - len(reviewed)
	if len(reviewed) > 0 {
		result.IndependentImplementations = new(len(independent))
	}
}
