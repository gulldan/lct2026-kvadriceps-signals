package application

import (
	"context"
	"crypto/sha256"
	"errors"
	"fmt"
	"os"
	"sync"
	"time"

	"github.com/example/main-service/internal/domain"
	"github.com/example/main-service/internal/platform/openalex"
)

const (
	liveGraphRAG    = "graphrag"
	liveSnapshotKey = "trends/live/monthly-v2.json"
	liveVersion     = "arxiv-live-v2"
	liveMethod      = "live-signals-v8"
	livePriorMethod = "live-signals-v7"
	liveCategories  = "cat:cs.* OR cat:stat.* OR cat:math.* OR cat:physics.* OR cat:cond-mat.* OR cat:quant-ph OR " +
		"cat:astro-ph.* OR cat:hep-th OR cat:hep-ph OR cat:hep-ex OR cat:hep-lat OR cat:nucl-th OR cat:nucl-ex OR " +
		"cat:gr-qc OR cat:math-ph OR cat:nlin.* OR cat:q-bio.* OR cat:q-fin.* OR cat:eess.* OR cat:econ.*"
	liveMonths       = 14
	liveRefreshEvery = 24 * time.Hour
	liveSearchTTL    = 24 * time.Hour
	// liveSearchBudget — потолок разового поиска: работа идёт в фоне, и её не держит время жизни HTTP-запроса.
	liveSearchBudget = 12 * time.Minute
	// liveSearchPatience — потолок ожидания ОДНОГО источника в разовом поиске.
	liveSearchPatience = 45 * time.Second
	liveTopRepos       = 8
	liveTopStories     = 6
	liveCoherenceQuery = "query"
)

// TrendLiveService держит последний снимок радара в памяти и в хранилище, обновляет его раз в сутки и
// отвечает на разовые поиски по тому же окну.
type TrendLiveService struct {
	store            liveStore
	semantic         liveSemanticEncoder
	analyzer         LiveAnalyzer
	token            func(context.Context) string
	now              func() time.Time
	newSources       func() *liveSources
	scholarCache     *liveScholarCache
	mu               sync.RWMutex
	snapshot         *LiveSnapshot
	loadedHash       [sha256.Size]byte
	status           string
	lastError        string
	refreshing       bool
	writerGuard      func(context.Context) (context.Context, func(), error)
	collectionReader func(context.Context) (*LiveCollectionStatus, error)
	collection       *LiveCollectionStatus
	// searchSlot допускает один поиск за раз. Канал, а не мьютекс: ждущий
	// должен уметь отмениться по контексту, иначе очередь копится молча.
	searchSlot chan struct{}
	// searchJobs — выполняющиеся и только что отказавшие поиски по запросу.
	searchJobs map[string]*liveSearchJob
	// typicalSearch — длительность прошлого успешного поиска, чтобы человеку
	// было с чем сравнить ожидание. Ноль означает «замера нет».
	typicalSearch int
	cache         map[string]liveCacheEntry
	// searchStore сохраняет поиск и его сырые записи в базе: перезапуск не
	// теряет собранное, а близкий запрос переиспользует его.
	searchStore LiveSearchStore
	// searchTopics — описания сохранённых запросов операторов для обзора: на
	// карте они стоят рядом с направлениями снимка.
	searchTopics []LiveTopic
	// refreshingSearches держит одну пересборку сохранённых запросов за раз,
	// searchesRefreshedFor — сутки UTC, за которые она уже прошла.
	refreshingSearches   bool
	searchesRefreshedFor string
	// opened — когда запрос открывали в последний раз: ночная пересборка тратит OpenAlex только на них.
	// storedWarmed — дооценка сохранённых ответов новой моделью уже прошла.
	opened       map[string]time.Time
	openedLoaded bool
	storedWarmed bool
	// reopenPatience — ноль означает liveReopenPatience; тесты его укорачивают.
	reopenPatience time.Duration
	// slotWait — срок ожидания слота поиска; ноль — liveSlotWait.
	slotWait time.Duration
	// directions — технологии направлений снимка; directionTried помнит снимок последней попытки, чтобы отказ
	// шлюза не повторялся каждую минуту.
	directions           map[string]liveDirectionAnswer
	directionTried       map[string]string
	refreshingDirections bool
	// directionDigests — по какому ответу направления уже пишутся пересказы: отказ шлюза не повторяется на
	// каждом опросе страницы.
	directionDigests map[string]liveDigestRun
	// background — отмены идущих фоновых работ: поиск или обращение человека прерывает их все, а друг друга
	// они не ждут.
	background    map[uint64]context.CancelFunc
	backgroundSeq uint64
	// people — сколько обращений человека к шлюзу (справка, черновик прогноза) идёт сейчас: фоновые работы
	// ждут, пока их нет.
	people int
	// window — последнее перечисленное окно arXiv с аннотациями: по нему
	// отвечает разовый поиск в сборщике. Держится между циклами намеренно.
	window      []StudyPaper
	windowMeta  map[string]LivePaperMetadata
	windowIndex liveWindowIndex
	plan        LiveQueryPlanner
	name        func(ctx context.Context, titles []string) (string, error)
	qualify     CandidateQualifier
	extract     CandidateExtractor
	// card пишет обязательные поля выдачи, digest — русский пересказ источника,
	// describe — абзац о сути направления под его названием на карте.
	card     CandidateCard
	digest   SourceDigest
	describe TopicDescriber
	// executive пишет справку для руководителя, executives — её кэш по run_id
	// ответа. ponytail: кэш в памяти реплики; в Valkey, когда реплик станет больше.
	executive  LiveExecutiveWriter
	executives map[string]LiveExecutive
	// collectorURL — адрес сборщика для разового поиска из API-реплики; пусто — поиск идёт в API arXiv.
	collectorURL string
	// answerer и documents ищут по загруженным документам: тот же запрос, что
	// уходит в интернет, задаётся и своей базе.
	answerer  domain.Answerer
	documents liveDocumentReader
	// recordStage выносит наружу числа поиска: иначе деградация радара тихая — контейнеры живы, а выдача
	// перестала наполняться.
	recordStage func(stage string, seconds float64)
	// history — OpenAlex для истории термина во встроенной модели; nil — общий
	// клиент источников радара. Тесты подставляют свой сервер.
	history      *openalex.Client
	historyOnce  sync.Once
	historyCache *liveHistoryCache
	// oa — один клиент OpenAlex на сервис: остаток суточного бюджета из заголовков ответа должен видеть
	// каждый поиск.
	oa *openalex.Client
	// hnCache — обсуждения фраз на Hacker News, rater — оценка зрелости
	// моделью языка: входы встроенной модели. Тесты подменяют адрес Algolia.
	hnCache *liveHNCache
	rater   *LiveMaturityRater
	// events — ответы источников событий раздела B по имени технологии,
	// checkEvents — проверка их привязки и типа моделью в волне карточек.
	events      *liveEventCache
	checkEvents LiveEventCheck
	// phrase подбирает фразы технологии для проверки модели на файле; fileCheck держит одну проверку за раз,
	// fileResult — последняя.
	phrase              TechnologyPhraser
	fileCheck           sync.Mutex
	fileResult          WeakSignalFileCheck
	ml                  *liveMLSource
	classifier          CandidateClassifier
	classifierVersion   string
	classifierOwnBudget bool
	workers             func(ctx context.Context, key string, def int) int
	// eventData — выгрузки грантов и темп EDGAR для источников событий раздела B.
	eventData liveMarketState
}

// NewTrendLiveService: token читает GITHUB_TOKEN на каждый вызов, а не один раз при старте.
func NewTrendLiveService(store liveStore, token func(context.Context) string) *TrendLiveService {
	s := &TrendLiveService{store: store, token: token, now: time.Now, status: "collecting",
		cache: map[string]liveCacheEntry{}, searchSlot: make(chan struct{}, 1),
		searchJobs: map[string]*liveSearchJob{}, directions: map[string]liveDirectionAnswer{},
		directionTried: map[string]string{}, directionDigests: map[string]liveDigestRun{},
		collectorURL: os.Getenv("RADAR_COLLECTOR_URL"),
		scholarCache: newLiveScholarCache(store), hnCache: newLiveHNCache(""), events: newLiveEventCache(),
		oa: liveOpenAlex(), opened: map[string]time.Time{}, background: map[uint64]context.CancelFunc{}}
	s.newSources = func() *liveSources {
		src := newLiveSources(token)
		src.answerer, src.documents, src.scholarCache, src.oa = s.answerer, s.documents, s.scholarCache, s.oa
		return src
	}
	return s
}

func (s *TrendLiveService) SetStageRecorder(record func(stage string, seconds float64)) {
	s.recordStage = record
}

// SetWorkers подключает чтение настройки RADAR_LLM_WORKERS поверх окружения.
func (s *TrendLiveService) SetWorkers(getInt func(ctx context.Context, key string, def int) int) {
	s.workers = getInt
}

// llmWorkers не опускается ниже единицы: семафор нулевой ёмкости не замедлил
// бы стадию, а остановил её навсегда.
func (s *TrendLiveService) llmWorkers(ctx context.Context) int {
	if s.workers == nil {
		return LiveDefaultWorkers
	}
	return max(1, s.workers(ctx, liveWorkersKey, LiveDefaultWorkers))
}

func (s *TrendLiveService) Start(ctx context.Context) {
	s.keepTermHistory()
	go func() {
		s.load(ctx)
		tick := time.Tick(time.Hour)
		for {
			// Пересборка запросов операторов сама решает, пора ли: раз в сутки ночью.
			go s.refreshStoredSearches(ctx)
			go s.keepGrantDumps(ctx)
			if s.stale() {
				err := s.Refresh(ctx)
				switch {
				case err == nil:
				case ctx.Err() == nil:
					s.mu.Lock()
					s.lastError = err.Error()
					if s.snapshot == nil {
						s.status = "failed"
					}
					s.mu.Unlock()
				}
			}
			s.loadSearchTopics(ctx)
			select {
			case <-ctx.Done():
				return
			case <-tick:
			}
		}
	}()
}

func (s *TrendLiveService) stale() bool {
	s.mu.RLock()
	defer s.mu.RUnlock()
	if s.snapshot == nil {
		return true
	}
	observed, err := time.Parse(time.RFC3339, s.snapshot.ObservedAt)
	return err != nil || s.now().Sub(observed) >= liveRefreshEvery
}

// Refresh собирает все курируемые семейства; отказ одного семейства — оговорка, а не провал.
func (s *TrendLiveService) Refresh(ctx context.Context) error {
	ctx, finish, err := s.beginWrite(ctx)
	if err != nil {
		return err
	}
	defer finish()
	started := s.now()
	src := s.newSources()
	s.mu.RLock()
	src.priorRepos = livePriorRepos(s.snapshot)
	s.mu.RUnlock()
	periods := livePeriods(started)
	snapshot := LiveSnapshot{Version: liveVersion, Method: liveMethod, ObservedAt: started.UTC().Format(time.RFC3339),
		Categories: liveCategories, Periods: periods, Topics: []LiveTopic{}, Discovered: []LiveTopic{}, Papers: []StudyPaper{},
		Limits: []LiveLimit{}, GitHubAuth: s.token(ctx) != ""}
	window, population, err := s.liveWindow(ctx, src, periods)
	if err != nil {
		return err
	}
	for i := range snapshot.Periods {
		snapshot.Periods[i].Population = population[i]
	}
	if err := s.collectAll(ctx, src, &snapshot, window); err != nil {
		return err
	}
	if err := ctx.Err(); err != nil {
		return fmt.Errorf("live: refresh canceled: %w", err)
	}
	liveAnnotate(snapshot.Topics, snapshot.Discovered)
	if err := s.enrichAnalysis(ctx, &snapshot, "", true); err != nil {
		return err
	}
	s.describeTopics(ctx, &snapshot)
	snapshot.Limits = append(snapshot.Limits, src.limits...)
	snapshot.Duration = s.now().Sub(started).Round(time.Second).String()
	if len(snapshot.Topics) == 0 {
		return errors.New("live: no family collected")
	}
	return s.publish(ctx, &snapshot)
}

// RefreshDelay восстанавливает ритм сбора после перезапуска: последний успешный проход сохраняется, а
// --once по-прежнему просит немедленного обновления.
func (s *TrendLiveService) RefreshDelay(ctx context.Context, interval time.Duration) time.Duration {
	s.load(ctx)
	s.mu.RLock()
	defer s.mu.RUnlock()
	if s.snapshot == nil {
		return 0
	}
	observed, err := time.Parse(time.RFC3339, s.snapshot.ObservedAt)
	if err != nil {
		return 0
	}
	duration, _ := time.ParseDuration(s.snapshot.Duration)
	return min(interval, max(0, observed.Add(duration).Add(interval).Sub(s.now())))
}

// StartReader следит за снимком, который пишет отдельный сборщик: API-реплики не запускают конкурирующий
// сбор и не повторяют обогащение на GPU.
func (s *TrendLiveService) StartReader(ctx context.Context) {
	s.keepTermHistory()
	go func() {
		for {
			s.load(ctx)
			// Пересборка запросов операторов сама решает, пора ли: раз в сутки и только ночью.
			go s.refreshStoredSearches(ctx)
			// Проверка раз в минуту ничего не стоит, когда всё посчитано, и
			// подхватывает направления, которые не успели до перезапуска.
			go s.refreshDirections(ctx)
			go s.warmStoredSearches(ctx)
			go s.keepGrantDumps(ctx)
			s.loadSearchTopics(ctx)
			s.readCollection(ctx)
			select {
			case <-ctx.Done():
				return
			case <-time.After(time.Minute):
			}
		}
	}()
}
