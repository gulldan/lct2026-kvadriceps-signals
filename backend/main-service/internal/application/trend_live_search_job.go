package application

import (
	"cmp"
	"context"
	"errors"
	"log/slog"
	"maps"
	"math"
	"slices"
	"strings"
	"time"
)

const (
	liveStepArxiv    = "arxiv"
	liveStepGitHub   = "github"
	liveStepHN       = "hn"
	liveStepPatents  = "patents"
	liveStepWeb      = "web"
	liveStepAnalysis = "analysis"
	liveStepCards    = "cards"
	// liveStepRescore нет в плане сбора: он бывает только при открытии сохранённого ответа.
	liveStepRescore = "rescore"
	// liveSearchFailWindow: опрос раз в две секунды должен увидеть отказ, а повтор человеком — уже запустить новый поиск.
	liveSearchFailWindow = 10 * time.Second
	// liveSlotWait — ожидание слота после вытеснения ночной пересборки: она сворачивается за секунды.
	liveSlotWait = time.Minute
	// liveReopenPatience — семь, а не десять секунд: к сроку открытия добавляется шаг опроса страницы.
	liveReopenPatience = 7 * time.Second
	// liveReopenHold длиннее окна отказа: скрытую вкладку опрашивают реже, и каждый опрос заново запускал бы дооценку.
	liveReopenHold = 5 * time.Minute

	// Метрики считают число записей, а не длительность: отдельного счётчика в стеке нет.
	liveStageSearch       = "live_search"
	liveStageCardWritten  = "live_card"
	liveStageCardMissing  = "live_card_missing"
	liveStageSearchFailed = "live_search_failed"
)

func liveSearchSteps() []string {
	return []string{liveStepArxiv, liveStepScholar, liveStepGitHub, liveStepHN,
		liveStepPatents, liveStepWeb, liveStepRegistry, liveStepCorpus, liveStepAnalysis, liveStepCards}
}

// liveSearchKey разделяет владельцев: общий ключ отдал бы чужие загруженные документы тому, кто задал тот же вопрос.
func liveSearchKey(owner, query string) string { return owner + "\n" + query }

const (
	LiveSearchRunning = "running"
	LiveSearchReady   = "ready"
	LiveSearchFailed  = "failed"
)

type LiveSearchState struct {
	Query     string `json:"query"`
	Status    string `json:"status"`
	Reopening bool   `json:"reopening,omitempty"`
	Step      string `json:"step,omitempty"`
	Index     int    `json:"step_index,omitempty"`
	Total     int    `json:"step_total,omitempty"`
	// Посещённый этап не означает успешный сбор: источники могут пропускаться и запрашиваться повторно.
	VisitedSteps []string `json:"visited_steps,omitempty"`
	// WaitSeconds объявляет сам источник; ноль — ожидание не объявлено, а не «ждать нечего».
	WaitSeconds    int `json:"wait_seconds,omitempty"`
	ElapsedSeconds int `json:"elapsed_seconds"`
	TypicalSeconds int `json:"typical_seconds,omitempty"`
	// Found — промежуточная выдача: окончательная будет после карточек, и часть ещё может выпасть.
	Found  []string    `json:"found,omitempty"`
	Error  string      `json:"error,omitempty"`
	Detail *LiveDetail `json:"detail,omitempty"`
}

type liveFoundKey struct{}

func liveNotifyFound(ctx context.Context, label string) {
	if notify, ok := ctx.Value(liveFoundKey{}).(func(string)); ok && label != "" {
		notify(label)
	}
}

// liveSearchJob живёт в памяти, как и кэш поиска: та же долговечность и тот же потолок в одну реплику.
type liveSearchJob struct {
	started    time.Time
	waitUntil  time.Time
	expires    time.Time
	owner      string
	stored     *LiveDetail
	previous   *LiveDetail
	background bool
	cancel     context.CancelFunc
	state      LiveSearchState
	done       chan struct{}
}

// StartSearch отвечает сразу, а работа продолжается в фоне и переживает разрыв HTTP-запроса.
func (s *TrendLiveService) StartSearch(ctx context.Context, query, owner string, refresh bool) (LiveSearchState, error) {
	query, err := normalizeLiveQuery(query)
	if err != nil {
		return LiveSearchState{}, err
	}
	s.noteOpened(ctx, query)
	key := liveSearchKey(owner, query)
	s.mu.Lock()
	// Пересборка забывает ответ у всех владельцев, иначе опрос без флага получил бы прежний из кэша.
	if refresh {
		maps.DeleteFunc(s.cache, func(cached string, _ liveCacheEntry) bool {
			return strings.HasSuffix(cached, "\n"+query)
		})
		// Открытый сохранённый ответ держится опросу минутами (liveReopenHold) и перехватил бы пересборку.
		if job, ok := s.searchJobs[key]; ok && job.state.Status != LiveSearchRunning {
			delete(s.searchJobs, key)
		}
	}
	state, known := s.currentSearch(ctx, key, query)
	ready := s.snapshot != nil
	s.mu.Unlock()
	if known {
		return state, nil
	}
	if !ready {
		return LiveSearchState{}, ErrLiveNotReady
	}
	// Сохранённый ответ читается до допуска: вчерашний запрос не ждёт чужого сбора и не тратит лимиты источников.
	var stored *LiveDetail
	if !refresh {
		if detail, ok := s.storedSearch(ctx, query, nil); ok {
			stored = &detail
		}
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	// Пока читали базу, тот же запрос мог поставить другой вызов.
	if state, known := s.currentSearch(ctx, key, query); known {
		return state, nil
	}
	if stored == nil {
		if err := s.admitSearch(false); err != nil {
			return LiveSearchState{}, err
		}
	} else if stored.ClassifierVersion != s.classifierVersion {
		// Дооценка ответа, который ждёт человек, не должна стоять в очереди шлюза за фоновыми пересчётами.
		s.yieldBackground()
	}
	job := s.newSearchJob(query, owner, stored)
	s.searchJobs[key] = job
	go s.runSearch(context.WithoutCancel(ctx), query, key, job)
	return job.state, nil
}

// currentSearch вызывается под блокировкой.
func (s *TrendLiveService) currentSearch(ctx context.Context, key, query string) (LiveSearchState, bool) {
	now := s.now()
	if entry, ok := s.cache[key]; ok && entry.expires.After(now) && s.reusableSearch(entry.detail, query) {
		entry = s.repairCachedDigests(ctx, query, key, entry)
		s.cache[key] = entry
		detail := entry.detail
		return LiveSearchState{Query: query, Status: LiveSearchReady, Detail: &detail}, true
	}
	job, ok := s.searchJobs[key]
	if !ok || (job.state.Status != LiveSearchRunning && !job.expires.After(now)) {
		return LiveSearchState{}, false
	}
	return job.progressState(now), true
}

// admitSearch пускает один сбор за раз, поиск человека вытесняет ночную пересборку. Вызывается под блокировкой.
func (s *TrendLiveService) admitSearch(background bool) error {
	now := s.now()
	for running, job := range s.searchJobs {
		if job.state.Status == LiveSearchRunning && job.stored == nil {
			if !job.background || background {
				return ErrLiveSearchBusy
			}
			job.cancel()
		}
		if !job.expires.After(now) {
			delete(s.searchJobs, running)
		}
	}
	// Вместе с фоновым пересчётом направлений поиск делил шлюз пополам, и проверки обоих упирались в срок.
	if !background {
		s.yieldBackground()
	}
	return nil
}

func (s *TrendLiveService) newSearchJob(query, owner string, stored *LiveDetail) *liveSearchJob {
	return &liveSearchJob{started: s.now(), done: make(chan struct{}), owner: owner, stored: stored,
		state: LiveSearchState{Query: query, Status: LiveSearchRunning, Reopening: stored != nil,
			Total: len(liveSearchSteps()), TypicalSeconds: s.typicalSearch}}
}

func (s *TrendLiveService) refreshSearch(ctx context.Context, query string) error {
	key := liveSearchKey("", query)
	s.mu.Lock()
	if s.snapshot == nil {
		s.mu.Unlock()
		return ErrLiveNotReady
	}
	if err := s.admitSearch(true); err != nil {
		s.mu.Unlock()
		return err
	}
	jobCtx, cancel := context.WithCancel(liveInBackground(ctx))
	defer cancel()
	job := s.newSearchJob(query, "", nil)
	job.background, job.cancel = true, cancel
	s.searchJobs[key] = job
	s.mu.Unlock()
	if previous, ok := s.storedSearch(ctx, query, nil); ok {
		job.previous = &previous
	}
	s.runSearch(jobCtx, query, key, job)
	// Вытесненная поиском человека пересборка — не отказ: запрос повторится позже.
	if jobCtx.Err() != nil && ctx.Err() == nil {
		return ErrLiveSearchBusy
	}
	s.mu.RLock()
	defer s.mu.RUnlock()
	if job.state.Status == LiveSearchFailed {
		return errors.New(job.state.Error)
	}
	return nil
}

func (s *TrendLiveService) SearchState(query, owner string) (LiveSearchState, bool) {
	query, err := normalizeLiveQuery(query)
	if err != nil {
		return LiveSearchState{}, false
	}
	key := liveSearchKey(owner, query)
	s.mu.RLock()
	defer s.mu.RUnlock()
	if entry, ok := s.cache[key]; ok && entry.expires.After(s.now()) && s.reusableSearch(entry.detail, query) {
		detail := entry.detail
		return LiveSearchState{Query: query, Status: LiveSearchReady, Detail: &detail}, true
	}
	job, ok := s.searchJobs[key]
	if !ok {
		return LiveSearchState{}, false
	}
	return job.progressState(s.now()), true
}

func (job *liveSearchJob) progressState(now time.Time) LiveSearchState {
	state := job.state
	if state.Status == LiveSearchRunning {
		state.ElapsedSeconds = max(0, int(now.Sub(job.started).Seconds()))
		// Повторный опрос не должен заново запускать паузу, объявленную источником.
		state.WaitSeconds = max(0, int(math.Ceil(job.waitUntil.Sub(now).Seconds())))
	}
	return state
}

// Вызывается под блокировкой; прошлые ответы сохраняют свою неизменяемую историю.
func (job *liveSearchJob) noteProgress(step string, wait time.Duration, now time.Time) {
	job.state.Step = step
	job.state.Index = liveStepIndex(step)
	job.waitUntil = now.Add(wait)
	if job.state.Index > 0 && !slices.Contains(job.state.VisitedSteps, step) {
		job.state.VisitedSteps = append(slices.Clone(job.state.VisitedSteps), step)
	}
}

func (s *TrendLiveService) runSearch(ctx context.Context, query, key string, job *liveSearchJob) {
	defer close(job.done)
	ctx, cancel := liveRunBudget(ctx)
	defer cancel()
	progress := func(step string, wait time.Duration) {
		s.mu.Lock()
		changed := job.state.Step != step
		job.noteProgress(step, wait, s.now())
		s.mu.Unlock()
		if changed {
			slog.Info("live search step", "query", query, "step", step,
				"seconds", int(s.now().Sub(job.started).Seconds()))
		}
	}
	ctx = context.WithValue(ctx, liveFoundKey{}, func(label string) {
		s.mu.Lock()
		if !slices.Contains(job.state.Found, label) {
			job.state.Found = append(slices.Clone(job.state.Found), label)
		}
		s.mu.Unlock()
	})
	var detail LiveDetail
	var err error
	if job.stored != nil {
		detail, err = s.completeStored(ctx, query, *job.stored, job.owner, progress, func(scored LiveDetail) {
			// Поздняя оценка ждёт, пока работа отдаст ответ со старой, иначе её перекроет состояние «оценка обновляется».
			<-job.done
			s.finishRescore(query, key, job, scored)
		})
	} else {
		detail, err = s.search(ctx, query, job.owner, progress)
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if err != nil {
		// Отказ виден опросу лишь liveSearchFailWindow: без записи его причина терялась (19.10, отказ разбора запроса).
		slog.Warn("live search failed", "query", query, "error", err)
		job.expires = s.now().Add(liveSearchFailWindow)
		job.state = LiveSearchState{Query: query, Status: LiveSearchFailed, Error: err.Error()}
		// Отказ считается наравне с успехом: счётчик одних успехов молчит, когда поиск перестал получаться у всех.
		if s.recordStage != nil {
			s.recordStage(liveStageSearchFailed, s.now().Sub(job.started).Seconds())
		}
		return
	}
	// Ответ из базы не измеряется и не пишется обратно: длительность и дата принадлежат сбору, а не чтению.
	if job.stored == nil {
		s.typicalSearch = int(s.now().Sub(job.started).Seconds())
		s.measure(detail, s.now().Sub(job.started).Seconds())
	}
	if s.holdAnswer(job, query, key, detail) {
		if job.stored == nil && detail.lateDigests != nil {
			go s.finishHeldDigests(query, key, job, detail)
		}
		return
	}
	// Пересборка не обедняет ответ: пустая выдача поверх прежней с технологиями — отказ прохода.
	if job.previous != nil && len(detail.Candidates) == 0 && len(job.previous.Candidates) > 0 {
		slog.Warn("live search refresh kept the previous answer", "query", query,
			"previous_candidates", len(job.previous.Candidates))
		job.expires = s.now().Add(liveSearchFailWindow)
		job.state = LiveSearchState{Query: query, Status: LiveSearchReady, Detail: &detail}
		return
	}
	delete(s.searchJobs, key)
	expires := s.now().Add(liveSearchTTL)
	// Открытые источники одни на всех: новый сбор вытесняет из кэша и чужие копии ответа.
	maps.DeleteFunc(s.cache, func(cached string, _ liveCacheEntry) bool {
		return strings.HasSuffix(cached, "\n"+query)
	})
	s.cache[key] = liveCacheEntry{detail: detail, expires: expires, digestAttempts: 1}
	if job.stored != nil {
		return
	}
	// Запись идёт вне блокировки и бюджета поиска: ответ уже отдан, а в базу ляжет с опоздавшими событиями и пересказами.
	go s.persistSearch(context.WithoutCancel(ctx), query, key, detail, expires)
}

func (s *TrendLiveService) persistSearch(ctx context.Context, query, key string, detail LiveDetail,
	expires time.Time,
) {
	original := detail
	late, changed := s.awaitEvents(ctx, detail)
	if late.lateDigests != nil {
		late, changed = liveWithDigests(late, <-late.lateDigests), true
	}
	if changed {
		detail = late
	}
	written, ok := s.saveSearch(ctx, query, detail, expires)
	if !ok {
		written, _ = liveDigestFailureLimits(original)
		written.DigestsPending = false
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	// Следующий проход наследует подтверждённую версию; отказ записи завершает ожидание с прежними данными.
	if entry, found := s.cache[key]; found && entry.detail.RunID == original.RunID &&
		entry.detail.storeVersion == original.storeVersion {
		if written.RunID != original.RunID {
			entry.digestAttempts = 1
		}
		entry.detail = written
		s.cache[key] = entry
	}
}

// Диагностический ответ не пишется в суточный кэш, но уже начатые пересказы должны завершить его опрос.
func (s *TrendLiveService) finishHeldDigests(query, key string, job *liveSearchJob, detail LiveDetail) {
	// Срок уже ограничен в startLateDigests: второй таймер потерял бы частичный результат при отмене последнего вызова.
	filled := liveWithDigests(detail, <-detail.lateDigests)
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.searchJobs[key] != job || job.state.Detail == nil || job.state.Detail.RunID != detail.RunID {
		return
	}
	job.expires = s.now().Add(liveReopenHold)
	job.state = LiveSearchState{Query: query, Status: LiveSearchReady, Detail: &filled}
}

// completeStored ждёт не дольше liveReopenPatience; досчитанная позже оценка приходит в late.
func (s *TrendLiveService) completeStored(ctx context.Context, query string, detail LiveDetail, owner string,
	progress func(step string, wait time.Duration), late func(LiveDetail),
) (LiveDetail, error) {
	s.mu.RLock()
	snapshot := s.snapshot
	s.mu.RUnlock()
	if snapshot == nil {
		return LiveDetail{}, ErrLiveNotReady
	}
	patience := cmp.Or(s.reopenPatience, liveReopenPatience)
	deadline := time.NewTimer(patience)
	defer deadline.Stop()
	var scored chan LiveDetail
	if detail.ClassifierVersion != s.classifierVersion && s.classifier != nil {
		// Дооценка переживает открытие: её сохранённый результат нужен следующему, даже если этот ответ ушёл со старой.
		scored = make(chan LiveDetail, 1)
		stored := detail
		go func() {
			rescored, _ := s.rescoreStored(context.WithoutCancel(ctx), query, stored)
			scored <- rescored
		}()
		progress(liveStepRescore, 0)
	}
	src := s.newSources()
	// Шаг показывает то, что дольше: дооценку, если она идёт, иначе свою базу.
	if scored == nil {
		src.progress = progress
	}
	// База ждётся не дольше того же срока: её отказ — лишь оговорка канала.
	corpusCtx, cancel := context.WithTimeout(ctx, patience)
	defer cancel()
	channel := liveCorpusSearch(corpusCtx, src, owner, liveTopicTerms(&detail.Topic), snapshot.Periods)
	if scored != nil {
		// Готовая оценка берётся и тогда, когда срок истёк, пока ждали базу.
		select {
		case detail = <-scored:
		default:
			select {
			case detail = <-scored:
			case <-deadline.C:
				detail.Rescoring = true
				go func() {
					rescored := <-scored
					rescored.Topic.Corpus = channel
					late(rescored)
				}()
			}
		}
	}
	detail.Topic.Corpus = channel
	// Пересказы — только к ответу с новой оценкой, иначе поздние оценка и пересказы перезаписали бы друг друга.
	if !detail.Rescoring {
		s.startStoredDigests(ctx, query, &detail, late)
	}
	return detail, nil
}

func (s *TrendLiveService) finishRescore(query, key string, job *liveSearchJob, detail LiveDetail) {
	s.mu.Lock()
	defer s.mu.Unlock()
	// Пока досчитывали, работу мог сменить новый сбор того же запроса.
	if s.searchJobs[key] != job {
		return
	}
	if s.holdAnswer(job, query, key, detail) {
		return
	}
	delete(s.searchJobs, key)
	s.cache[key] = liveCacheEntry{detail: detail, expires: s.now().Add(liveSearchTTL), digestAttempts: 1}
}

// holdAnswer держит ответ, негодный на сутки, в работе, а не в кэше. Вызывается под блокировкой.
func (s *TrendLiveService) holdAnswer(job *liveSearchJob, query, key string, detail LiveDetail) bool {
	var hold time.Duration
	reusable := s.reusableSearch(detail, query)
	if !reusable && job.stored == nil {
		s.logNotStored(query, detail)
	}
	switch {
	case detail.Rescoring:
		// Работа видна опросу, пока finishRescore не подставит новую оценку, но не дольше её срока.
		hold = liveClassifierStage + liveReopenPatience
	case detail.DigestsPending && (job.stored != nil || !reusable):
		hold = liveLateDigestWait + liveReopenPatience
	case reusable:
		return false
	default:
		// Скрытая вкладка опрашивает реже: готовую диагностику сохраняем до явной пересборки или конца окна.
		hold = liveReopenHold
	}
	job.expires = s.now().Add(hold)
	job.state = LiveSearchState{Query: query, Status: LiveSearchReady, Detail: &detail}
	delete(s.cache, key)
	return true
}

// rescoreStored при отказе модели возвращает ответ со старой оценкой: стёртая оценка хуже прежней.
func (s *TrendLiveService) rescoreStored(ctx context.Context, query string, detail LiveDetail) (LiveDetail, bool) {
	previous := detail
	scored := slices.Clone(detail.Candidates)
	for i := range scored {
		scored[i].ModelScore = nil
	}
	// Своя копия оговорок: модель дописывает в них отказ, а исходный ответ тем временем уходит человеку.
	detail.Topic.Limits = slices.Clone(detail.Topic.Limits)
	candidates, rejected := s.classifyCandidates(ctx, query, &detail.Topic, scored, detail.Papers, nil,
		slices.Clone(detail.Rejected))
	detail.Candidates, detail.Rejected, detail.ClassifierVersion = candidates, rejected, s.classifierVersion
	if !liveUsableStored(detail) {
		slog.Warn("live stored answer kept the previous model score", "query", query,
			"version", previous.ClassifierVersion)
		return previous, false
	}
	detail.Topic.Limits = s.liveMarkHistoryBudget(detail.Topic.Limits)
	detail.Counters.Selected, detail.Counters.Rejected = len(candidates), len(rejected)
	detail.Counters.ModelScored, detail.Counters.ModelAboveThreshold = 0, 0
	for _, candidate := range candidates {
		if candidate.ModelScore != nil {
			detail.Counters.ModelScored++
		}
		if liveModelAbove(candidate) {
			detail.Counters.ModelAboveThreshold++
		}
	}
	if s.reusableSearch(detail, query) {
		written, ok := s.saveSearch(context.WithoutCancel(ctx), query, detail, s.now().Add(liveSearchTTL))
		if !ok {
			return previous, false
		}
		detail = written
	}
	return detail, true
}

// measure считает и карточки без обязательного текста — тихую деградацию, которую иначе снаружи не видно.
func (s *TrendLiveService) measure(detail LiveDetail, seconds float64) {
	if s.recordStage == nil {
		return
	}
	s.recordStage(liveStageSearch, seconds)
	for _, candidate := range detail.Candidates {
		stage := liveStageCardWritten
		if !liveCardComplete(candidate) {
			stage = liveStageCardMissing
		}
		s.recordStage(stage, 0)
	}
}

func liveStepIndex(step string) int {
	for i, name := range liveSearchSteps() {
		if name == step {
			return i + 1
		}
	}
	return 0
}
