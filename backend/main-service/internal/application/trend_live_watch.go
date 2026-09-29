package application

import (
	"cmp"
	"context"
	"errors"
	"fmt"
	"log/slog"
	"reflect"
	"slices"
	"strings"
	"sync"
	"time"
	"unicode/utf8"
)

// Подписки лежат в Valkey рядом с настройками фабрики: своя таблица потребовала бы миграции ради десятка записей.

const (
	LiveWatchTopic = "topic"
	LiveWatchQuery = "query"
	// LiveWatchBet оформляет сама сверка прогнозов, а не человек.
	LiveWatchBet = "bet"

	liveWatchPrefix = "rag:radar:watches:"
	// liveWatchEvery — двадцать часов, а не сутки: ночной снимок приходит в разное время.
	liveWatchEvery = 20 * time.Hour
	liveWatchTick  = 5 * time.Minute
	// liveWatchLimit держит список маленьким: он читается и переписывается целиком.
	liveWatchLimit    = 50
	liveWatchTitleMax = 200
)

var ErrLiveWatchInvalid = errors.New("live watch: invalid subject")

var ErrLiveWatchNotFound = errors.New("live watch: not found")

type LiveWatchKV interface {
	SetJSON(ctx context.Context, key string, v any, ttl time.Duration) error
	GetJSON(ctx context.Context, key string, dest any) (bool, error)
}

type LiveWatch struct {
	Kind      string           `json:"kind"`
	Target    string           `json:"target"`
	Title     string           `json:"title"`
	CreatedAt time.Time        `json:"created_at"`
	Digest    *LiveWatchDigest `json:"digest,omitempty"`
	DigestAt  time.Time        `json:"digest_at,omitzero"`
}

// LiveWatchDigest без текста: интерфейс говорит на трёх языках, а числа и имена у всех одни.
type LiveWatchDigest struct {
	Technologies []string `json:"technologies,omitempty"`
	// Stage — стадия самого направления, Stages — оставшихся в выдаче технологий.
	Stage   *LiveWatchShift   `json:"stage,omitempty"`
	Stages  []LiveWatchShift  `json:"stages,omitempty"`
	Sources int               `json:"sources,omitempty"`
	Market  int               `json:"market,omitempty"`
	Bet     *LiveWatchBetNews `json:"bet,omitempty"`
}

type LiveWatchBetNews struct {
	Status  string   `json:"status"`
	Changes []string `json:"changes"`
}

type LiveWatchShift struct {
	Name string `json:"name,omitempty"`
	From string `json:"from"`
	To   string `json:"to"`
}

// liveWatchRecord считает сводку от Baseline — последнего увиденного, поэтому непрочитанная сводка дорастает.
type liveWatchRecord struct {
	Watch     LiveWatch      `json:"watch"`
	Version   string         `json:"version"`
	CheckedAt time.Time      `json:"checked_at"`
	Baseline  liveWatchState `json:"baseline"`
	State     liveWatchState `json:"state"`
}

type liveWatchState struct {
	Stage        string          `json:"stage"`
	Sources      int             `json:"sources"`
	Market       int             `json:"market"`
	Technologies []liveWatchTech `json:"technologies"`
}

type liveWatchTech struct {
	ID    string `json:"id"`
	Name  string `json:"name"`
	Stage string `json:"stage"`
}

type LiveWatches struct {
	live  *TrendLiveService
	kv    LiveWatchKV
	users func(context.Context) ([]string, error)
	now   func() time.Time
	bets  *liveBetTracker
	// mu сериализует чтение-изменение-запись списка, чтобы сводка не затирала новую подписку. Потолок:
	// блокировка процесса — со второй репликой нужна проверка версии скриптом в Valkey.
	mu sync.Mutex
}

func NewLiveWatches(live *TrendLiveService, kv LiveWatchKV, users func(context.Context) ([]string, error),
) *LiveWatches {
	return &LiveWatches{live: live, kv: kv, users: users, now: time.Now}
}

func (w *LiveWatches) Start(ctx context.Context) {
	go func() {
		for {
			w.digest(ctx)
			select {
			case <-ctx.Done():
				return
			case <-time.After(liveWatchTick):
			}
		}
	}()
}

func (w *LiveWatches) List(ctx context.Context, user string) ([]LiveWatch, error) {
	records, err := w.load(ctx, user)
	if err != nil {
		return nil, err
	}
	out := make([]LiveWatch, 0, len(records))
	for _, record := range records {
		out = append(out, record.Watch)
	}
	return out, nil
}

// Watch берёт точкой отсчёта то, что карточка показывает сейчас: первая сводка расскажет только о новом.
func (w *LiveWatches) Watch(ctx context.Context, user, kind, target, title string) (LiveWatch, error) {
	target, err := liveWatchTarget(kind, target)
	if err != nil {
		return LiveWatch{}, err
	}
	if kind == LiveWatchBet {
		return LiveWatch{}, fmt.Errorf("%w: bets are watched by the radar itself", ErrLiveWatchInvalid)
	}
	title = strings.Join(strings.Fields(title), " ")
	if utf8.RuneCountInString(title) > liveWatchTitleMax {
		title = string([]rune(title)[:liveWatchTitleMax])
	}
	var versions map[string]string
	if kind == LiveWatchQuery {
		versions = w.queryVersions(ctx)
	}
	version, _ := w.version(kind, target, versions)
	state, found := w.state(ctx, kind, target)
	switch {
	case !found && kind == LiveWatchTopic:
		return LiveWatch{}, ErrLiveWatchNotFound
	case !found:
		version = ""
	}
	var watch LiveWatch
	err = w.update(ctx, user, func(records []liveWatchRecord) ([]liveWatchRecord, error) {
		if i := liveWatchIndex(records, kind, target); i >= 0 {
			watch = records[i].Watch
			return records, nil
		}
		if len(records) >= liveWatchLimit {
			return nil, fmt.Errorf("%w: at most %d watches", ErrLiveWatchInvalid, liveWatchLimit)
		}
		watch = LiveWatch{Kind: kind, Target: target, Title: cmp.Or(title, target), CreatedAt: w.now()}
		return append(records, liveWatchRecord{Watch: watch, Version: version, Baseline: state, State: state}), nil
	})
	return watch, err
}

func (w *LiveWatches) Unwatch(ctx context.Context, user, kind, target string) error {
	target, err := liveWatchTarget(kind, target)
	if err != nil {
		return err
	}
	return w.update(ctx, user, func(records []liveWatchRecord) ([]liveWatchRecord, error) {
		return slices.DeleteFunc(records, func(record liveWatchRecord) bool {
			return record.Watch.Kind == kind && record.Watch.Target == target
		}), nil
	})
}

func (w *LiveWatches) Ack(ctx context.Context, user, kind, target string) error {
	target, err := liveWatchTarget(kind, target)
	if err != nil {
		return err
	}
	return w.update(ctx, user, func(records []liveWatchRecord) ([]liveWatchRecord, error) {
		i := liveWatchIndex(records, kind, target)
		if i < 0 {
			return nil, ErrLiveWatchNotFound
		}
		records[i].Baseline = records[i].State
		records[i].Watch.Digest, records[i].Watch.DigestAt = nil, time.Time{}
		return records, nil
	})
}

func (w *LiveWatches) digest(ctx context.Context) {
	users, err := w.users(ctx)
	if err != nil {
		slog.Warn("live watches: users", "error", err)
		return
	}
	var versions map[string]string
	queries := func() map[string]string {
		if versions == nil {
			versions = w.queryVersions(ctx)
		}
		return versions
	}
	for _, user := range users {
		if ctx.Err() != nil {
			return
		}
		w.digestUser(ctx, user, queries)
	}
	if w.bets != nil {
		if _, err := w.CheckBets(ctx, "", "", false); err != nil {
			slog.Warn("live watches: bets", "error", err)
		}
	}
}

func (w *LiveWatches) digestUser(ctx context.Context, user string, queries func() map[string]string) {
	records, err := w.load(ctx, user)
	if err != nil {
		slog.Warn("live watches: read", "user", user, "error", err)
		return
	}
	seen := map[int]liveWatchRecord{}
	for i, record := range records {
		if record.Watch.Kind == LiveWatchBet || w.now().Sub(record.CheckedAt) < liveWatchEvery {
			continue
		}
		var versions map[string]string
		if record.Watch.Kind == LiveWatchQuery {
			versions = queries()
		}
		version, ready := w.version(record.Watch.Kind, record.Watch.Target, versions)
		if !ready || version == record.Version {
			continue
		}
		state, found := w.state(ctx, record.Watch.Kind, record.Watch.Target)
		if !found {
			continue
		}
		seen[i] = record
		records[i].observe(state, version, w.now())
	}
	if len(seen) > 0 {
		w.apply(ctx, user, seen, records)
	}
}

// apply пишет сверку, только если человек не тронул подписки за это время; тронутые сверятся в следующий проход.
func (w *LiveWatches) apply(ctx context.Context, user string, seen map[int]liveWatchRecord, next []liveWatchRecord) {
	err := w.update(ctx, user, func(current []liveWatchRecord) ([]liveWatchRecord, error) {
		for i, before := range seen {
			if j := liveWatchIndex(current, before.Watch.Kind, before.Watch.Target); j >= 0 &&
				reflect.DeepEqual(current[j], before) {
				current[j] = next[i]
			}
		}
		return current, nil
	})
	if err != nil {
		slog.Warn("live watches: write", "user", user, "error", err)
	}
}

func (r *liveWatchRecord) observe(state liveWatchState, version string, now time.Time) {
	// Без точки отсчёта подписка на запрос, чей ответ ещё не лёг в базу, начиналась бы со сводки обо всём.
	if r.Version == "" {
		r.Baseline = state
	}
	r.Version, r.CheckedAt, r.State = version, now, state
	digest := liveWatchDiff(r.Baseline, state)
	if digest == nil {
		r.Watch.Digest, r.Watch.DigestAt = nil, time.Time{}
		return
	}
	// Та же сводка на следующий день не всплывает новым уведомлением.
	if r.Watch.Digest == nil || !reflect.DeepEqual(*r.Watch.Digest, *digest) {
		r.Watch.DigestAt = now
	}
	r.Watch.Digest = digest
}

// liveWatchDiff не сообщает об убывании: ушедшее видно в карточке.
func liveWatchDiff(before, after liveWatchState) *LiveWatchDigest {
	digest := LiveWatchDigest{Sources: max(0, after.Sources-before.Sources), Market: max(0, after.Market-before.Market)}
	if before.Stage != "" && after.Stage != "" && before.Stage != after.Stage {
		digest.Stage = &LiveWatchShift{From: before.Stage, To: after.Stage}
	}
	for _, tech := range after.Technologies {
		i := slices.IndexFunc(before.Technologies, tech.same)
		switch {
		case i < 0:
			digest.Technologies = append(digest.Technologies, tech.Name)
		case before.Technologies[i].Stage != "" && tech.Stage != "" && before.Technologies[i].Stage != tech.Stage:
			digest.Stages = append(digest.Stages, LiveWatchShift{Name: tech.Name, From: before.Technologies[i].Stage,
				To: tech.Stage})
		}
	}
	if digest.Stage == nil && len(digest.Technologies)+len(digest.Stages)+digest.Sources+digest.Market == 0 {
		return nil
	}
	return &digest
}

// same узнаёт технологию по идентификатору и по имени: пересчёт может сменить одно из них.
func (t liveWatchTech) same(other liveWatchTech) bool {
	name := func(value string) string { return strings.Join(strings.Fields(strings.ToLower(value)), " ") }
	return (t.ID != "" && t.ID == other.ID) || (t.Name != "" && name(t.Name) == name(other.Name))
}

func liveWatchStateOf(detail LiveDetail) liveWatchState {
	state := liveWatchState{Stage: detail.Topic.Stage, Sources: detail.Counters.SourcesProcessed,
		Market: detail.Counters.Web + detail.Counters.Registry, Technologies: make([]liveWatchTech, 0, len(detail.Candidates))}
	for _, candidate := range detail.Candidates {
		state.Technologies = append(state.Technologies,
			liveWatchTech{ID: candidate.ID, Name: cmp.Or(candidate.Label, candidate.Name), Stage: candidate.Stage})
	}
	return state
}

// version — версия данных карточки и готовность к сверке: направление — когда технологии посчитаны по
// снимку, запрос — когда ответ лежит в базе.
func (w *LiveWatches) version(kind, target string, versions map[string]string) (string, bool) {
	if kind == LiveWatchQuery {
		version, ok := versions[target]
		return version, ok
	}
	s := w.live
	s.mu.RLock()
	defer s.mu.RUnlock()
	if s.snapshot == nil {
		return "", false
	}
	answer, known := s.directions[target]
	ready := s.directionTried[target] == s.snapshot.ObservedAt || (known && s.freshDirection(answer, s.snapshot))
	return s.snapshot.ObservedAt, ready
}

// state читает карточку так же, как её открывает человек.
func (w *LiveWatches) state(ctx context.Context, kind, target string) (liveWatchState, bool) {
	var (
		detail LiveDetail
		found  bool
	)
	if kind == LiveWatchQuery {
		detail, found = w.live.storedSearch(ctx, target, nil)
	} else {
		detail, found = w.live.Topic(target)
	}
	if !found {
		return liveWatchState{}, false
	}
	return liveWatchStateOf(detail), true
}

// queryVersions — даты сбора сохранённых запросов из истории, чтобы не читать полуторамегабайтные ответы.
// Потолок: история — последние пятьдесят запросов.
func (w *LiveWatches) queryVersions(ctx context.Context) map[string]string {
	history, err := w.live.SearchHistory(ctx, 0)
	versions := make(map[string]string, len(history.Entries))
	if err != nil {
		slog.Warn("live watches: search history", "error", err)
		return versions
	}
	for _, entry := range history.Entries {
		if _, known := versions[entry.Query]; !known {
			versions[entry.Query] = entry.ObservedAt.UTC().Format(time.RFC3339Nano)
		}
	}
	return versions
}

func (w *LiveWatches) load(ctx context.Context, user string) ([]liveWatchRecord, error) {
	records := []liveWatchRecord{}
	if _, err := w.kv.GetJSON(ctx, liveWatchPrefix+user, &records); err != nil {
		return nil, fmt.Errorf("live watches: read: %w", err)
	}
	return records, nil
}

func (w *LiveWatches) update(ctx context.Context, user string,
	change func([]liveWatchRecord) ([]liveWatchRecord, error),
) error {
	w.mu.Lock()
	defer w.mu.Unlock()
	records, err := w.load(ctx, user)
	if err != nil {
		return err
	}
	records, err = change(records)
	if err != nil {
		return err
	}
	if err := w.kv.SetJSON(ctx, liveWatchPrefix+user, records, 0); err != nil {
		return fmt.Errorf("live watches: write: %w", err)
	}
	return nil
}

func liveWatchIndex(records []liveWatchRecord, kind, target string) int {
	return slices.IndexFunc(records, func(record liveWatchRecord) bool {
		return record.Watch.Kind == kind && record.Watch.Target == target
	})
}

func liveWatchTarget(kind, target string) (string, error) {
	switch kind {
	case LiveWatchTopic, LiveWatchBet:
		target = strings.TrimSpace(target)
		if target == "" || utf8.RuneCountInString(target) > liveWatchTitleMax {
			return "", fmt.Errorf("%w: %s id", ErrLiveWatchInvalid, kind)
		}
		return target, nil
	case LiveWatchQuery:
		query, err := normalizeLiveQuery(target)
		if err != nil {
			return "", fmt.Errorf("%w: %w", ErrLiveWatchInvalid, err)
		}
		return query, nil
	}
	return "", fmt.Errorf("%w: kind %q", ErrLiveWatchInvalid, kind)
}
