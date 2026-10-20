package application

import (
	"cmp"
	"context"
	"crypto/rand"
	"errors"
	"fmt"
	"slices"
	"strings"
	"time"
	"unicode"
	"unicode/utf8"
)

const liveQueryRunes = 200

type LiveQuery struct {
	Arxiv  string   `json:"arxiv"`
	GitHub string   `json:"github"`
	HN     string   `json:"hn"`
	Terms  []string `json:"terms,omitempty"`
}

type LiveTopic struct {
	StudyTopic
	Name    string    `json:"name"`
	Query   LiveQuery `json:"query"`
	Curated bool      `json:"curated"`
	Source  string    `json:"source"`
	Branch  string    `json:"branch,omitempty"`
	// Industry и Money ставятся при выдаче обзора и в снимок не пишутся.
	Industry    string         `json:"industry,omitempty"`
	Money       *LiveMapMoney  `json:"money,omitempty"`
	Label       string         `json:"label,omitempty"`
	Citations   *LiveCitations `json:"citations,omitempty"`
	PapersCount int            `json:"papers_count"`
	// DocumentsCount включает RecordsCount — записи веба, реестров и патентов.
	DocumentsCount int `json:"documents_count,omitempty"`
	RecordsCount   int `json:"records_count,omitempty"`
	// Понижение планки ради полной выдачи обязано быть видно в строке.
	EvidenceLevel string          `json:"evidence_level,omitempty"`
	Shortfall     *LiveShortfall  `json:"shortfall,omitempty"`
	Repos         []int           `json:"repos"`
	Discussions   []int           `json:"discussions"`
	Limits        []LiveLimit     `json:"limits"`
	Uniqueness    *float64        `json:"uniqueness"`
	Relevance     *float64        `json:"relevance"`
	Semantic      *LiveSemantic   `json:"semantic"`
	Comparison    *LiveComparison `json:"comparison"`
	Patents       *LiveChannel    `json:"patents"`
	Web           *LiveChannel    `json:"web"`
	Registry      *LiveChannel    `json:"registry,omitempty"`
	Research      *LiveChannel    `json:"research,omitempty"`
	PatentsCN     *LiveChannel    `json:"patents_cn,omitempty"`
	// Corpus не попадает в общий снимок и сохранённый поиск: он принадлежит спросившему.
	Corpus     *LiveChannel       `json:"corpus,omitempty"`
	Analysis   *LiveTopicAnalysis `json:"analysis,omitempty"`
	Confidence *LiveConfidence    `json:"confidence,omitempty"`
	// Market и Adoption — поля со своим «неизвестно»: без чисел в ответе отказ по зрелости нечем оспорить.
	Market   *LiveMarket   `json:"market,omitempty"`
	Adoption *LiveAdoption `json:"adoption,omitempty"`
	// Релевантный зонтик не делает релевантной каждую технологию внутри него.
	QueryRelevance *float64                `json:"query_relevance,omitempty"`
	Qualification  *LiveQualification      `json:"qualification,omitempty"`
	Card           *LiveCard               `json:"card,omitempty"`
	ModelScore     *LiveModelScore         `json:"model_score,omitempty"`
	ML             *LiveMLExtractCandidate `json:"ml,omitempty"`
	Emergence      *LiveEmergence          `json:"emergence,omitempty"`
	Description    string                  `json:"description,omitempty"`
	Events         []LiveAdoptionEvent     `json:"events,omitempty"`
	TZ             *LiveTZAssessment       `json:"tz,omitempty"`
	// Ответившие рыночные источники: их перечисляет основание «пилотов не найдено».
	eventSources     []string
	marketIDs        []string
	mechanismProfile *liveMechanismProfile
	// Прямые источники извлечения не должны теряться за найденным тематическим контекстом.
	qualificationAnchors []string
}

type LiveSnapshot struct {
	WriterEpoch   string                       `json:"writer_epoch,omitempty"`
	Version       string                       `json:"version"`
	Method        string                       `json:"method"`
	ObservedAt    string                       `json:"observed_at"`
	Categories    string                       `json:"categories"`
	Periods       []LivePeriod                 `json:"periods"`
	Topics        []LiveTopic                  `json:"topics"`
	Discovered    []LiveTopic                  `json:"discovered"`
	Papers        []StudyPaper                 `json:"papers"`
	PaperMetadata map[string]LivePaperMetadata `json:"paper_metadata,omitempty"`
	Limits        []LiveLimit                  `json:"limits"`
	Citations     *LiveCitationSummary         `json:"citations,omitempty"`
	GitHubAuth    bool                         `json:"github_authenticated"`
	Duration      string                       `json:"duration"`
}

type LiveOverview struct {
	Status     string                `json:"status"`
	Error      string                `json:"error"`
	ErrorCode  string                `json:"error_code"`
	Refreshing bool                  `json:"refreshing"`
	Collection *LiveCollectionStatus `json:"collection,omitempty"`
	LiveSnapshot
}

type LiveDetail struct {
	Scope             *LiveQueryScope              `json:"scope,omitempty"`
	RunID             string                       `json:"run_id,omitempty"`
	Version           string                       `json:"version"`
	Method            string                       `json:"method"`
	SearchContract    string                       `json:"search_contract,omitempty"`
	ClassifierVersion string                       `json:"classifier_version,omitempty"`
	ObservedAt        string                       `json:"observed_at"`
	Categories        string                       `json:"categories"`
	Periods           []LivePeriod                 `json:"periods"`
	Limits            []LiveLimit                  `json:"limits"`
	GitHubAuth        bool                         `json:"github_authenticated"`
	Topic             LiveTopic                    `json:"topic"`
	Papers            []StudyPaper                 `json:"papers"`
	Candidates        []LiveTopic                  `json:"candidates,omitempty"`
	Rejected          []LiveRejection              `json:"rejected,omitempty"`
	Passports         map[string]LivePaperPassport `json:"passports,omitempty"`
	Counters          LiveCounters                 `json:"counters"`
	Volume            LiveVolume                   `json:"volume,omitzero"`

	SourceRevisions map[string]LivePublisherRevision `json:"source_revisions,omitempty"`
	// RelevanceFloor приезжает числом: порог в переводе интерфейса соврал бы, когда сдвинется константа.
	RelevanceFloor float64              `json:"relevance_floor"`
	Extraction     *LiveMLExtractionRun `json:"extraction,omitempty"`
	// Rescoring — ответ открыт с оценкой прежней модели, а новая придёт в следующих опросах.
	Rescoring       bool `json:"rescoring,omitempty"`
	DigestsPending  bool `json:"digests_pending,omitempty"`
	lateDigests     <-chan map[string]liveDigest
	storeVersion    string
	storeRunID      string
	storeReadFailed bool
}

type liveCacheEntry struct {
	detail         LiveDetail
	expires        time.Time
	digestAttempts int
}

func (s *TrendLiveService) Overview() LiveOverview {
	s.mu.RLock()
	defer s.mu.RUnlock()
	overview := LiveOverview{Status: s.status, Error: s.lastError, ErrorCode: LiveFailureCode(s.lastError),
		Refreshing: s.refreshing, Collection: s.collection,
		LiveSnapshot: LiveSnapshot{Version: liveVersion, Method: liveMethod, Categories: liveCategories,
			Periods: []LivePeriod{}, Topics: []LiveTopic{}, Papers: []StudyPaper{}, Limits: []LiveLimit{}}}
	if s.snapshot == nil {
		return overview
	}
	overview.LiveSnapshot = *s.snapshot
	overview.Papers = []StudyPaper{}
	overview.PaperMetadata = nil
	overview.WriterEpoch = ""
	if overview.Collection != nil && overview.Collection.Phase != "idle" {
		overview.Refreshing = true
	}
	overview.Topics = make([]LiveTopic, 0, len(s.snapshot.Topics)+len(s.snapshot.Discovered)+len(s.searchTopics))
	overview.Discovered = []LiveTopic{}
	// Урезается и при чтении: сохранённое направление могло прийти из прошлого поколения формата.
	industries, rubrics := liveFamilyIndustries(), liveRubrics()
	for _, topic := range slices.Concat(s.snapshot.Topics, s.snapshot.Discovered, s.searchTopics) {
		row := liveOverviewTopic(topic)
		row.Industry = liveTopicIndustry(row, industries, rubrics)
		overview.Topics = append(overview.Topics, row)
	}
	money := s.topicMoney(overview.Topics)
	for i := range overview.Topics {
		if found, ok := money[overview.Topics[i].ID]; ok {
			overview.Topics[i].Money = &found
		}
	}
	return overview
}

// liveOverviewTopic — форма и обзора, и сохранённого направления запроса.
func liveOverviewTopic(topic LiveTopic) LiveTopic {
	topic.Branch = cmp.Or(liveCuratedBranch(topic), topic.Branch)
	topic.PaperIDs = []string{}
	topic.Analysis = nil
	topic.External = []StudyEvent{}
	// Смысловое сравнение растёт квадратом от числа направлений и весило треть обзора; оно нужно только карточке.
	topic.Semantic, topic.Confidence, topic.Market, topic.Adoption, topic.Emergence = nil, nil, nil, nil, nil
	// Белый список, а не чёрный: новое поле канала попадёт в обзор, только если его допишут явно.
	for _, target := range []**LiveChannel{&topic.Patents, &topic.PatentsCN, &topic.Registry, &topic.Web, &topic.Research} {
		if *target != nil {
			*target = &LiveChannel{Counts: (*target).Counts, Total: (*target).Total,
				Partial: (*target).Partial, Limits: []string{}}
		}
	}
	return topic
}

func (s *TrendLiveService) Topic(id string) (LiveDetail, bool) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	if s.snapshot == nil {
		return LiveDetail{}, false
	}
	for _, topic := range slices.Concat(s.snapshot.Topics, s.snapshot.Discovered) {
		if topic.ID != id {
			continue
		}
		if answer, ok := s.directions[id]; ok && liveDirectionShown(answer, topic) {
			return liveDirectionDetail(s.snapshot, topic, answer), true
		}
		return liveDetail(s.snapshot, topic, s.snapshot.Papers, nil, nil, nil, nil), true
	}
	return LiveDetail{}, false
}

func liveDetail(snapshot *LiveSnapshot, topic LiveTopic, papers, records []StudyPaper,
	candidates []LiveTopic, rejected []LiveRejection, metadata map[string]LivePaperMetadata,
) LiveDetail {
	branch := liveCuratedBranch(topic)
	topic.Branch = cmp.Or(branch, topic.Branch)
	if metadata == nil {
		metadata = snapshot.PaperMetadata
	}
	topic = liveRegistryOnTopic(topic, snapshot.Periods)
	// Пересчёт после отсева записей не по теме: иначе вердикт держится на числе, которого нет в ответе.
	topic.Market = liveMarketMaturity(topic, liveAbsoluteBar()).field()
	own := make([]StudyPaper, 0, len(topic.PaperIDs))
	for _, paper := range papers {
		if slices.Contains(topic.PaperIDs, paper.ID) {
			own = append(own, paper)
		}
	}
	// Счётчик обязан сходиться с тем, что видно на экране, а не с числом направлений снимка.
	considered := len(candidates) + len(rejected)
	detail := LiveDetail{Version: snapshot.Version, Method: snapshot.Method, ObservedAt: snapshot.ObservedAt,
		Categories: snapshot.Categories, Periods: snapshot.Periods, Limits: snapshot.Limits,
		GitHubAuth: snapshot.GitHubAuth, Topic: withoutProvenance(topic), Papers: own,
		Rejected: rejected, Passports: livePaperPassports(own, metadata), SourceRevisions: liveSourceRevisions(own, metadata),
		Counters:       liveCounters(topic, own, metadata, considered),
		RelevanceFloor: float64(liveRelevanceFloor) / 100}
	// Порог считается по найденным технологиям, а не по зонтику запроса: они и есть выдача.
	detail.Counters.AboveThreshold = 0
	detail.Counters.Selected = len(candidates)
	detail.Counters.Rejected = len(rejected)
	if len(candidates) == 0 {
		return detail
	}
	for _, candidate := range candidates {
		// Такие кандидаты наследуют ветвь зонтика при построении, в том числе в прежних сохранённых ответах.
		if branch != "" && candidate.Source == "candidate" {
			candidate.Branch = branch
		}
		// При отдаче, а не при сборе: так чинятся и названия в прежних сохранённых ответах.
		candidate.Label = liveLabelWithoutCJK(candidate.Label, candidate.Name)
		detail.Candidates = append(detail.Candidates, withoutProvenance(candidate))
		if candidate.ModelScore != nil {
			detail.Counters.ModelScored++
		}
		if liveModelAbove(candidate) {
			detail.Counters.ModelAboveThreshold++
		}
		// Добранная технология не входит в прошедшие порог, какой бы ни была её оценка.
		if candidate.Shortfall != nil {
			detail.Counters.TopUp++
			continue
		}
		if candidate.Confidence != nil && candidate.Confidence.AboveMin {
			detail.Counters.AboveThreshold++
		}
	}
	// Без записей каналов кейс-пример на новости, патенте или реестре ссылался бы на документ вне ответа.
	detail.Papers = append(detail.Papers, liveEvidenceDocuments(candidates, records)...)
	// Паспорт — по итоговому списку: иначе источник не из науки остался бы без типа издателя и языка.
	detail.Passports = livePaperPassports(detail.Papers, metadata)
	detail.SourceRevisions = liveSourceRevisions(detail.Papers, metadata)
	return detail
}

// liveEvidenceDocuments берёт и записи событий стадии: иначе цитата стадии ведёт в никуда.
func liveEvidenceDocuments(candidates []LiveTopic, records []StudyPaper) []StudyPaper {
	wanted := map[string]bool{}
	for _, candidate := range candidates {
		for _, id := range candidate.EvidenceIDs {
			wanted[id] = true
		}
		if candidate.Adoption != nil {
			for _, evidence := range candidate.Adoption.Evidence {
				wanted[evidence.SourceID] = true
			}
		}
	}
	out := []StudyPaper{}
	have := map[string]bool{}
	for _, record := range records {
		if wanted[record.ID] && !have[record.ID] {
			have[record.ID] = true
			out = append(out, record)
		}
	}
	for _, candidate := range candidates {
		for _, event := range candidate.Events {
			document, ok := liveRecordDocument(event.Record)
			if ok && wanted[document.ID] && !have[document.ID] {
				have[document.ID] = true
				out = append(out, document)
			}
		}
	}
	return out
}

// withoutProvenance: странице нужны счётчики и курсоры, а список страниц был основной частью ответа.
func withoutProvenance(topic LiveTopic) LiveTopic {
	for _, target := range []**LiveChannel{&topic.Patents, &topic.PatentsCN, &topic.Registry, &topic.Web, &topic.Research} {
		if *target == nil {
			continue
		}
		channel := **target
		channel.Coverage = slices.Clone(channel.Coverage)
		for i := range channel.Coverage {
			channel.Coverage[i].Pages = nil
		}
		*target = &channel
	}
	return topic
}

var ErrLiveNotReady = errors.New("live snapshot not ready")

// ErrLiveSearchBusy — единственный слот занят другим поиском; это не ErrLiveBusy, который про сбор.
var ErrLiveSearchBusy = errors.New("live search is busy")

// normalizeLiveQuery даёт ключ, обязанный совпадать в кэше и в карте работ.
func normalizeLiveQuery(query string) (string, error) {
	query = strings.Join(strings.Fields(strings.ToLower(query)), " ")
	if liveJunkQuery(query) {
		return "", fmt.Errorf("live: query %q is not a search phrase", query)
	}
	if utf8.RuneCountInString(query) > liveQueryRunes {
		return "", fmt.Errorf("live: query must be 1-%d characters", liveQueryRunes)
	}
	return query, nil
}

// liveJunkQuery: иначе String(undefined) с фронта становится сохранённым запросом на карте.
func liveJunkQuery(query string) bool {
	switch strings.ToLower(strings.TrimSpace(query)) {
	case "", "undefined", "null", "[object object]":
		return true
	}
	return false
}

// search — тело фоновой работы StartSearch: сбор не укладывается в жизнь HTTP-запроса.
func (s *TrendLiveService) search(ctx context.Context, query, owner string,
	progress func(step string, wait time.Duration),
) (LiveDetail, error) {
	s.mu.RLock()
	snapshot := s.snapshot
	s.mu.RUnlock()
	if snapshot == nil {
		return LiveDetail{}, ErrLiveNotReady
	}
	// Слот, а не мьютекс: sync.Mutex не знает про контекст, и ждущие копились бы без предела.
	// Слот может держать сворачивающаяся ночная пересборка: человек ждёт секунды, а не получает «занято».
	select {
	case s.searchSlot <- struct{}{}:
		defer func() { <-s.searchSlot }()
	case <-time.After(cmp.Or(s.slotWait, liveSlotWait)):
		return LiveDetail{}, ErrLiveSearchBusy
	case <-ctx.Done():
		return LiveDetail{}, fmt.Errorf("live: search canceled: %w", ctx.Err())
	}
	// Пока захватывали слот, предыдущий запрос мог положить в кэш ровно этот ответ.
	key := liveSearchKey(owner, query)
	s.mu.RLock()
	if entry, ok := s.cache[key]; ok && entry.expires.After(s.now()) && s.reusableSearch(entry.detail, query) {
		s.mu.RUnlock()
		return entry.detail, nil
	}
	s.mu.RUnlock()
	storeVersion, storeRunID, storeRead := s.searchVersion(ctx, query)
	ctx = context.WithValue(ctx, liveLLMRunKey{}, rand.Text())
	ctx = s.liveMLContext(ctx)
	ctx, tally := liveWithTally(ctx)
	family, err := s.searchFamily(ctx, query)
	if err != nil {
		return LiveDetail{}, err
	}
	ctx = liveWithQueryScope(ctx, family.plan.Scope)
	src := s.newSources()
	src.progress, src.patience, src.owner, src.excerpts = progress, liveSearchPatience, owner, true
	// Сборщик держит окно arXiv в памяти: ответ за секунду и без 429; без него — API arXiv с оговоркой.
	topic, papers, err := s.searchWindow(ctx, src, family, snapshot.Periods)
	if errors.Is(err, errLiveCollectorUnavailable) {
		topic, papers, err = liveCollect(ctx, src, family, snapshot.Periods)
		if err == nil {
			topic.Limits = append(topic.Limits,
				liveLimitOf(liveLimitCollectorFallback, "collector: unavailable, arXiv API used", nil))
		}
	}
	if err != nil {
		return LiveDetail{}, err
	}
	topic.Curated = false
	topic.Source = "search"
	others := slices.Concat(snapshot.Topics, snapshot.Discovered)
	if len(papers) > 0 {
		topic.Relevance = new(liveRelevance(slices.Concat([]string{query}, topic.Query.Terms), papers))
		topic.Uniqueness = new(liveUniqueness(topic.PaperIDs, others, ""))
	}
	comparison := LiveSnapshot{Topics: append([]LiveTopic{topic}, others...), Papers: slices.Concat(papers, snapshot.Papers),
		Periods: snapshot.Periods, ObservedAt: snapshot.ObservedAt, Categories: snapshot.Categories,
		Limits: snapshot.Limits, PaperMetadata: liveSearchMetadata(snapshot, src, papers)}
	src.step(liveStepAnalysis, 0)
	// Извлечение идёт параллельно смысловому анализу: шаг ждёт дольшего из двух, а не суммы.
	documents := slices.Concat(papers, liveChannelDocuments(topic))
	livePrepareAnswerMetadata([]LiveTopic{topic}, documents, comparison.PaperMetadata)
	extracting, extraction := s.mlStartExtraction(ctx, query, &topic, documents, comparison.PaperMetadata)
	extracting = s.withMarketExtraction(ctx, query, &topic, comparison.PaperMetadata, extracting)
	// Исходные тексты записей нужны только пакету /extract, а он уже собран.
	ctx = context.WithValue(ctx, liveMLSearchKey{}, nil)
	// Упавший анализ — оговорка в ответе, а не повод выбросить минуту сбора.
	if err := s.enrichAnalysis(ctx, &comparison, query, false); err != nil {
		topic.Limits = append(topic.Limits, liveLimitFail(liveLimitAnalysisOff, "analysis", err))
	} else {
		topic.Semantic = comparison.Topics[0].Semantic
		topic.Analysis = comparison.Topics[0].Analysis
	}
	// Направления берутся из сравнения: там у каждого измерена близость к запросу.
	candidates, rejected, documents := s.shortlist(ctx, query, &topic, papers, comparison.PaperMetadata,
		snapshot.Periods, extracting, comparison.Topics[1:])
	src.step(liveStepCards, 0)
	describedSearch := s.describeSearch(ctx, &topic, papers, comparison.PaperMetadata)
	candidates, rejected, documents = s.finishCandidates(ctx, query, &topic, candidates, documents,
		comparison.PaperMetadata, snapshot.Periods, rejected)
	describedSearch()
	// Пустая выдача из-за чужого запроса уже объяснена своей оговоркой; «корпус мал» было бы неправдой.
	offTopic := slices.ContainsFunc(topic.Limits, func(limit LiveLimit) bool {
		return limit.Code == liveRejectOffTopic
	})
	if len(candidates) == 0 && !offTopic {
		topic.Limits = append(topic.Limits, liveLimitOf(liveLimitNoCandidates,
			"В собранных источниках нет кандидатов, прошедших все проверки. Причины приведены в списке исключённых.", nil))
	}
	topic.Limits = s.liveMarkHistoryBudget(topic.Limits)
	detail := liveDetail(snapshot, topic, papers, documents, candidates, rejected, comparison.PaperMetadata)
	detail.Volume = liveVolume(detail, comparison.PaperMetadata, src.scanned, tally)
	// Дата наблюдения — время этого сбора, а не дата снимка радара: по ней восстанавливается корпус.
	detail.ObservedAt = s.now().UTC().Format(time.RFC3339)
	detail.Method = liveMethod
	detail.Scope = liveCloneQueryScope(family.plan.Scope)
	detail.SearchContract = liveSearchContract
	detail.RunID, _ = ctx.Value(liveLLMRunKey{}).(string)
	detail.ClassifierVersion = s.classifierVersion
	detail.Extraction = extraction
	detail.storeVersion, detail.storeReadFailed = storeVersion, !storeRead
	detail.storeRunID = storeRunID
	s.startLateDigests(ctx, &detail, comparison.PaperMetadata)
	return detail, nil
}

// shortlist общий для ответа на запрос и направления карты, чтобы вкладка «Технологии» не расходилась;
// направления карты кандидатами получает только ответ на запрос.
func (s *TrendLiveService) shortlist(ctx context.Context, query string, topic *LiveTopic, papers []StudyPaper,
	metadata map[string]LivePaperMetadata, periods []LivePeriod, extracting func() []LiveExtractedTechnology,
	directions []LiveTopic,
) ([]LiveTopic, []LiveRejection, []StudyPaper) {
	candidates, rejected := liveSearchCandidates(topic, papers, metadata, periods)
	// Рыночная запись о пилоте даёт кейс-пример там, где научная работа даёт только описание.
	documents := slices.Concat(papers, liveChannelDocuments(*topic))
	livePrepareAnswerMetadata([]LiveTopic{*topic}, documents, metadata)
	if extracting == nil {
		extracting = s.withMarketExtraction(ctx, query, topic, metadata,
			s.startExtraction(ctx, query, documents, metadata))
	}
	documents = liveWithMarketDocuments(documents, *topic)
	extracted, refused := s.extractCandidates(*topic, extracting(), documents, metadata, periods)
	trends, skipped := liveDirectionCandidates(*topic, papers, directions, periods)
	candidates = slices.Concat(trends, liveInterleaveRecords(extracted), candidates)
	rejected = slices.Concat(rejected, refused, skipped)
	// Относимость проверяется у каждого кандидата до карточек: на чужую технологию незачем тратить шлюз.
	candidates, rejected = s.candidatesOnTopic(ctx, query, topic, candidates, documents, rejected)
	return candidates, rejected, documents
}

func liveAnnotate(groups ...[]LiveTopic) {
	all := slices.Concat(groups...)
	for _, group := range groups {
		for i := range group {
			if len(group[i].PaperIDs) == 0 {
				group[i].Uniqueness = nil
				continue
			}
			group[i].Uniqueness = new(liveUniqueness(group[i].PaperIDs, all, group[i].ID))
		}
	}
}

func liveUniqueness(ids []string, others []LiveTopic, self string) float64 {
	if len(ids) == 0 {
		return 0
	}
	own := make(map[string]bool, len(ids))
	for _, id := range ids {
		own[id] = true
	}
	worst := 0.0
	for _, other := range others {
		if other.ID == self {
			continue
		}
		shared := 0
		for _, id := range other.PaperIDs {
			if own[id] {
				shared++
			}
		}
		worst = max(worst, float64(shared)/float64(len(ids)))
	}
	return 1 - worst
}

func liveWords(text string) []string {
	return strings.FieldsFunc(strings.ToLower(text), func(r rune) bool {
		return !unicode.IsLetter(r) && !unicode.IsNumber(r)
	})
}

// liveRelevance — доля заголовков со всеми словами хотя бы одной формулировки, а не смысловая близость.
func liveRelevance(phrases []string, papers []StudyPaper) float64 {
	// Английские термины перевода нужны: русская формулировка с английскими заголовками не совпадает.
	terms := make([][]string, 0, len(phrases))
	for _, phrase := range phrases {
		if words := liveWords(phrase); len(words) > 0 {
			terms = append(terms, words)
		}
	}
	if len(terms) == 0 || len(papers) == 0 {
		return 0
	}
	hits := 0
	for _, paper := range papers {
		title := liveWords(paper.Title)
		if slices.ContainsFunc(terms, func(words []string) bool {
			return !slices.ContainsFunc(words, func(word string) bool { return !slices.Contains(title, word) })
		}) {
			hits++
		}
	}
	return float64(hits) / float64(len(papers))
}
