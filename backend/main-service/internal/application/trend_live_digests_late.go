package application

import (
	"bytes"
	"cmp"
	"context"
	"encoding/json/v2"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"maps"
	"net/http"
	"slices"
	"strings"
	"sync"
	"time"
)

// Пересказы пишутся после ответа, чтобы не удлинять поиск; страница опрашивает, пока их нет.

const (
	// Под нагрузкой один пересказ занимает 20–30 с.
	liveLateDigestWait = 5 * time.Minute
	// Следующий поиск человека идёт тем же шлюзом: весь пул фону не отдаётся.
	liveLateDigestWorkers = 16
	liveAbstractsBatch    = 500
	liveLimitDigestFailed = "digest_unavailable"
)

type liveDigest struct{ text, model string }

// Тот же набор источников, что показывает страница.
func liveShownSourceIDs(topic LiveTopic) []string {
	ids := slices.Concat(topic.PaperIDs, topic.EvidenceIDs)
	for _, evidence := range [][]LiveClaimEvidence{liveCardEvidence(topic.Card), liveMarketEvidence(topic.Market),
		liveAdoptionEvidence(topic.Adoption)} {
		for _, item := range evidence {
			ids = append(ids, item.SourceID)
		}
	}
	if topic.Qualification != nil {
		for _, item := range topic.Qualification.Evidence {
			ids = append(ids, item.SourceID)
		}
	}
	slices.Sort(ids)
	return slices.Compact(ids)
}

func liveCardEvidence(card *LiveCard) []LiveClaimEvidence {
	if card == nil {
		return nil
	}
	return card.Evidence
}

func liveMarketEvidence(market *LiveMarket) []LiveClaimEvidence {
	if market == nil {
		return nil
	}
	return market.Evidence
}

func liveAdoptionEvidence(adoption *LiveAdoption) []LiveClaimEvidence {
	if adoption == nil {
		return nil
	}
	return adoption.Evidence
}

func liveUndigested(detail LiveDetail) []string {
	shown := map[string]bool{}
	for _, paper := range detail.Papers {
		shown[paper.ID] = paper.Title != ""
	}
	missing := []string{}
	for _, candidate := range detail.Candidates {
		for _, id := range liveShownSourceIDs(candidate) {
			passport := detail.Passports[id]
			if shown[id] && passport.Language != "ru" && (passport.Digest == "" || passport.DigestModel == "") &&
				!slices.Contains(missing, id) {
				missing = append(missing, id)
			}
		}
	}
	return missing
}

func (s *TrendLiveService) digestSources(ctx context.Context, ids []string, titles, abstracts map[string]string,
) map[string]liveDigest {
	out := map[string]liveDigest{}
	if s.digest == nil {
		return out
	}
	var mu sync.Mutex
	var wg sync.WaitGroup
	slot := make(chan struct{}, liveLateDigestWorkers)
	for _, id := range ids {
		if ctx.Err() != nil {
			break
		}
		select {
		case slot <- struct{}{}:
		case <-ctx.Done():
			wg.Wait()
			return out
		}
		wg.Go(func() {
			defer func() { <-slot }()
			if ctx.Err() != nil {
				return
			}
			text, model, err := s.digest(ctx, titles[id], abstracts[id])
			if err != nil || text == "" || model == "" {
				slog.Warn("live source digest unavailable", "source_id", id, "error", err,
					"empty_text", text == "", "empty_model", model == "")
				return
			}
			mu.Lock()
			out[id] = liveDigest{text: liveDigestScope(text, abstracts[id]), model: model}
			mu.Unlock()
		})
	}
	wg.Wait()
	return out
}

// Добранная без карточки получает описанием пересказ основного источника, как в liveFallbackSummaries.
func liveWithDigests(detail LiveDetail, digests map[string]liveDigest) LiveDetail {
	detail.Passports = maps.Clone(detail.Passports)
	if detail.Passports == nil {
		detail.Passports = map[string]LivePaperPassport{}
	}
	for id, digest := range digests {
		if digest.text == "" || digest.model == "" {
			continue
		}
		passport := detail.Passports[id]
		passport.Digest, passport.DigestModel = digest.text, digest.model
		detail.Passports[id] = passport
	}
	detail, _ = liveDescribed(detail)
	detail, _ = liveDigestFailureLimits(detail)
	detail.DigestsPending, detail.lateDigests = false, nil
	return detail
}

// Сохранённая редакция издателя важнее аннотации из окна: та могла появиться до исправления автора.
func (s *TrendLiveService) startLateDigests(ctx context.Context, detail *LiveDetail,
	metadata map[string]LivePaperMetadata,
) {
	missing := liveUndigested(*detail)
	if s.digest == nil || len(missing) == 0 {
		return
	}
	titles, abstracts := liveDigestInputs(*detail, missing)
	for _, id := range missing {
		abstracts[id] = cmp.Or(abstracts[id], metadata[id].Abstract)
	}
	done := make(chan map[string]liveDigest, 1)
	go func() {
		late, cancel := context.WithTimeout(context.WithoutCancel(ctx), liveLateDigestWait)
		defer cancel()
		done <- s.digestSources(late, missing, titles, abstracts)
	}()
	detail.DigestsPending, detail.lateDigests = true, done
}

func liveDigestInputs(detail LiveDetail, ids []string) (titles, abstracts map[string]string) {
	titles, abstracts = map[string]string{}, map[string]string{}
	for _, id := range ids {
		if text := detail.SourceRevisions[id].Text; text != "" {
			abstracts[id] = text
		}
	}
	for _, paper := range detail.Papers {
		if slices.Contains(ids, paper.ID) {
			titles[paper.ID] = paper.Title
		}
	}
	return titles, abstracts
}

// Не сохраняет сам: дооценка тоже пересохраняет ответ, и две записи наперегонки теряли бы правку.
// Ошибка — окно сборщика не загружено, и лучше подождать аннотацию.
func (s *TrendLiveService) backfillDigests(ctx context.Context, detail LiveDetail) (LiveDetail, bool, error) {
	detail, assessed := s.backfillTZ(ctx, detail)
	detail, described := liveDescribed(detail)
	detail, filled, err := s.backfillSourceDigests(ctx, detail)
	return detail, assessed || described || filled, err
}

func (s *TrendLiveService) backfillSourceDigests(ctx context.Context, detail LiveDetail) (LiveDetail, bool, error) {
	missing := liveUndigested(detail)
	if s.digest == nil || len(missing) == 0 {
		return detail, false, nil
	}
	detail, changed := liveDigestFailureLimits(detail)
	titles, abstracts := liveDigestInputs(detail, missing)
	unresolved := slices.DeleteFunc(slices.Clone(missing), func(id string) bool { return abstracts[id] != "" })
	if len(unresolved) > 0 {
		window, err := s.windowAbstracts(ctx, unresolved)
		if err != nil {
			return detail, changed, err
		}
		for _, id := range unresolved {
			abstracts[id] = window[id]
		}
	}
	digests := s.digestSources(ctx, missing, titles, abstracts)
	if len(digests) == 0 {
		return detail, changed, nil
	}
	slog.Info("live stored answer digests", "missing", len(missing), "written", len(digests))
	return liveWithDigests(detail, digests), true, nil
}

// Копии в памяти снимаются, иначе открытый раньше ответ до конца суток показывался бы прежним.
func (s *TrendLiveService) resaveStored(ctx context.Context, query string, detail LiveDetail) {
	if _, ok := s.saveSearch(ctx, query, detail, s.now().Add(liveSearchTTL)); !ok {
		return
	}
	s.mu.Lock()
	maps.DeleteFunc(s.cache, func(key string, entry liveCacheEntry) bool {
		return strings.HasSuffix(key, "\n"+query) && entry.detail.RunID == detail.RunID &&
			entry.detail.storeVersion == detail.storeVersion
	})
	s.mu.Unlock()
}

// WindowAbstracts возвращает false, пока окно ещё не загружено.
func (s *TrendLiveService) WindowAbstracts(ids []string) (map[string]string, bool) {
	s.mu.RLock()
	metadata := s.windowMeta
	s.mu.RUnlock()
	out := map[string]string{}
	for _, id := range ids {
		if abstract := metadata[id].Abstract; abstract != "" {
			out[id] = abstract
		}
	}
	return out, len(metadata) > 0
}

var errLiveWindowNotLoaded = errors.New("radar collector window is not loaded yet")

func (s *TrendLiveService) windowAbstracts(ctx context.Context, ids []string) (map[string]string, error) {
	if local, loaded := s.WindowAbstracts(ids); loaded || s.collectorURL == "" {
		return local, nil
	}
	out := map[string]string{}
	client := &http.Client{Timeout: time.Minute}
	for start := 0; start < len(ids); start += liveAbstractsBatch {
		body, err := json.Marshal(struct {
			IDs []string `json:"ids"`
		}{ids[start:min(len(ids), start+liveAbstractsBatch)]})
		if err != nil {
			return nil, fmt.Errorf("encode abstracts request: %w", err)
		}
		req, err := http.NewRequestWithContext(ctx, http.MethodPost,
			strings.TrimRight(s.collectorURL, "/")+"/v1/abstracts", bytes.NewReader(body))
		if err != nil {
			return nil, fmt.Errorf("abstracts request: %w", err)
		}
		req.Header.Set("Content-Type", "application/json")
		resp, err := client.Do(req)
		if err != nil {
			return nil, fmt.Errorf("radar collector abstracts: %w", err)
		}
		raw, err := io.ReadAll(io.LimitReader(resp.Body, liveMaxBody))
		_ = resp.Body.Close()
		if resp.StatusCode == http.StatusServiceUnavailable {
			return nil, errLiveWindowNotLoaded
		}
		var batch map[string]string
		if err != nil || resp.StatusCode != http.StatusOK || json.Unmarshal(raw, &batch) != nil {
			return nil, fmt.Errorf("radar collector abstracts: status %d", resp.StatusCode)
		}
		maps.Copy(out, batch)
	}
	return out, nil
}

// Прогрев доходит не до всех ответов, поэтому пересказы дописываются и при открытии.
func (s *TrendLiveService) startStoredDigests(ctx context.Context, query string, detail *LiveDetail,
	late func(LiveDetail),
) {
	if !s.liveIncomplete(*detail) {
		return
	}
	detail.DigestsPending = true
	stored := *detail
	go func() {
		work, cancel := context.WithTimeout(context.WithoutCancel(ctx), liveLateDigestWait)
		defer cancel()
		filled, changed, err := s.backfillDigests(work, stored)
		filled.DigestsPending = false
		if err != nil {
			slog.Info("live stored answer digests wait for the collector window", "query", query, "error", err)
		}
		if changed {
			if written, ok := s.saveSearch(work, query, filled, s.now().Add(liveSearchTTL)); ok {
				filled = written
			} else {
				filled, _ = liveDigestFailureLimits(stored)
				filled.DigestsPending = false
			}
		}
		late(filled)
	}()
}

type liveDigestRun struct {
	run      string
	done     bool
	attempts int
}

// Бесконечный повтор держал бы страницу в опросе и шлюз занятым.
const liveDirectionAttempts = 3

// OpenTopic пишет недостающее в фоне, а страница опрашивает, пока оно не появится, вместо пересчёта всех направлений.
func (s *TrendLiveService) OpenTopic(ctx context.Context, id string) (LiveDetail, bool) {
	detail, ok := s.Topic(id)
	if !ok || !s.liveIncomplete(detail) {
		return detail, ok
	}
	s.mu.Lock()
	answer, known := s.directions[id]
	run, started := s.directionDigests[id]
	same := started && run.run == answer.RunID
	start := known && (!same || run.done && run.attempts < liveDirectionAttempts)
	if start {
		attempts := 1
		if same {
			attempts = run.attempts + 1
		}
		s.directionDigests[id] = liveDigestRun{run: answer.RunID, attempts: attempts}
	}
	detail.DigestsPending = start || same && !run.done
	snapshot := s.snapshot
	s.mu.Unlock()
	if start {
		go s.completeDirection(context.WithoutCancel(ctx), id, answer.RunID, snapshot, detail)
	}
	return detail, true
}

func (s *TrendLiveService) liveIncomplete(detail LiveDetail) bool {
	return s.digest != nil && len(liveUndigested(detail)) > 0 || liveNeedsTZ(detail) || liveNeedsSummary(detail)
}

// completeDirection кладёт дописанный ответ в память и хранилище, только если его не сменил пересчёт.
func (s *TrendLiveService) completeDirection(ctx context.Context, id, runID string, snapshot *LiveSnapshot,
	detail LiveDetail,
) {
	ctx, cancel := context.WithTimeout(ctx, liveLateDigestWait)
	defer cancel()
	// Страница перестаёт опрашивать, когда ответ уже и в памяти, и в хранилище.
	defer func() {
		s.mu.Lock()
		run := s.directionDigests[id]
		run.done = true
		s.directionDigests[id] = run
		s.mu.Unlock()
	}()
	missing := liveUndigested(detail)
	titles, abstracts := liveDigestInputs(detail, missing)
	for _, source := range missing {
		abstracts[source] = cmp.Or(abstracts[source], snapshot.PaperMetadata[source].Abstract)
	}
	digests := s.digestSources(ctx, missing, titles, abstracts)
	s.mu.RLock()
	answer, ok := s.directions[id]
	s.mu.RUnlock()
	if !ok || answer.RunID != runID {
		return
	}
	candidates := answer.Candidates
	if liveNeedsTZ(detail) {
		assessed, _ := s.backfillTZ(ctx, LiveDetail{Candidates: candidates, Topic: detail.Topic, Papers: detail.Papers})
		candidates = assessed.Candidates
	}
	metadata := maps.Collect(maps.All(answer.Metadata))
	for source, digest := range digests {
		meta, have := metadata[source]
		if !have {
			meta = snapshot.PaperMetadata[source]
		}
		meta.Digest, meta.DigestModel = digest.text, digest.model
		metadata[source] = meta
	}
	candidates = slices.Clone(candidates)
	liveFallbackSummaries(candidates, metadata)
	s.mu.Lock()
	if current, ok := s.directions[id]; !ok || current.RunID != runID {
		s.mu.Unlock()
		return
	}
	answer.Candidates, answer.Metadata = candidates, metadata
	s.directions[id] = answer
	s.mu.Unlock()
	raw, err := json.Marshal(answer)
	if err == nil {
		err = s.store.Put(ctx, liveDirectionsPrefix+id+".json", bytes.NewReader(raw), int64(len(raw)), "application/json")
	}
	if err != nil {
		slog.Warn("live direction digests store", "topic", id, "error", err)
	}
	slog.Info("live direction completed", "topic", id, "missing", len(missing), "written", len(digests))
}

func liveNeedsSummary(detail LiveDetail) bool {
	return slices.ContainsFunc(detail.Candidates, func(topic LiveTopic) bool {
		return (topic.Card == nil || topic.Card.Summary == "") &&
			slices.ContainsFunc(slices.Concat(topic.PaperIDs, topic.EvidenceIDs), func(id string) bool {
				return detail.Passports[id].Digest != ""
			})
	})
}

func liveDescribed(detail LiveDetail) (LiveDetail, bool) {
	if !liveNeedsSummary(detail) {
		return detail, false
	}
	digests := make(map[string]LivePaperMetadata, len(detail.Passports))
	for id, passport := range detail.Passports {
		digests[id] = LivePaperMetadata{Digest: passport.Digest}
	}
	detail.Candidates = slices.Clone(detail.Candidates)
	liveFallbackSummaries(detail.Candidates, digests)
	return detail, true
}

func liveNeedsTZ(detail LiveDetail) bool {
	return slices.ContainsFunc(detail.Candidates, func(topic LiveTopic) bool { return topic.TZ == nil })
}

// События берутся теми же источниками, что у поиска, но без проверки моделью: ни шлюз, ни слот поиска не заняты.
func (s *TrendLiveService) backfillTZ(ctx context.Context, detail LiveDetail) (LiveDetail, bool) {
	if !liveNeedsTZ(detail) {
		return detail, false
	}
	candidates := slices.Clone(detail.Candidates)
	umbrella := detail.Topic
	umbrella.Limits = slices.Clone(umbrella.Limits)
	documents := make(map[string]StudyPaper, len(detail.Papers))
	for _, paper := range detail.Papers {
		documents[paper.ID] = paper
	}
	s.askEvents(ctx, candidates, nil)
	s.takeEvents(ctx, &umbrella, candidates, nil, time.Now().Add(liveEventFetchTimeout))
	for i := range candidates {
		if candidates[i].TZ == nil {
			liveAssessTZ(&candidates[i], documents)
		}
	}
	detail.Candidates, detail.Topic = candidates, umbrella
	return detail, true
}
