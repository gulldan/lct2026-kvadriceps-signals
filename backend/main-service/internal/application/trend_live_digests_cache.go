package application

import (
	"context"
	"log/slog"
	"slices"
)

// Полный ответ не пересобирается из-за пересказа, но и бесконечно занимать шлюз ремонтом нельзя.
const liveCachedDigestAttempts = 3

// Вызывается под блокировкой: параллельные опросы получают один и тот же незавершённый проход.
func (s *TrendLiveService) repairCachedDigests(ctx context.Context, query, key string, entry liveCacheEntry) liveCacheEntry {
	if ctx.Err() != nil || s.digest == nil || entry.detail.DigestsPending ||
		entry.digestAttempts >= liveCachedDigestAttempts || len(liveUndigested(entry.detail)) == 0 {
		return entry
	}
	entry.digestAttempts++
	entry.detail.DigestsPending = true
	go func() {
		work, cancel := context.WithTimeout(context.WithoutCancel(ctx), liveLateDigestWait)
		defer cancel()
		filled, _, err := s.backfillSourceDigests(work, entry.detail)
		filled.DigestsPending = false
		if err != nil {
			slog.Info("live cached answer digests unavailable", "query", query, "error", err)
		}
		s.mu.RLock()
		current, known := s.cache[key]
		s.mu.RUnlock()
		if !known || current.detail.RunID != entry.detail.RunID || current.digestAttempts != entry.digestAttempts {
			return
		}
		// До окончания записи следующий опрос не должен начать проход, который сохранится раньше этого.
		written, ok := s.saveSearch(work, query, filled, entry.expires)
		if !ok {
			written, _ = liveDigestFailureLimits(entry.detail)
			written.DigestsPending = false
		}
		s.mu.Lock()
		defer s.mu.Unlock()
		current, known = s.cache[key]
		if !known || current.detail.RunID != entry.detail.RunID || current.digestAttempts != entry.digestAttempts ||
			current.detail.storeVersion != entry.detail.storeVersion {
			return
		}
		if written.RunID != entry.detail.RunID {
			current.digestAttempts = 1
		}
		current.detail = written
		s.cache[key] = current
	}()
	return entry
}

// Отказ виден после завершения прохода; успех снимает только оговорки о пересказе, сохраняя остальные проверки.
func liveDigestFailureLimits(detail LiveDetail) (LiveDetail, bool) {
	missing := liveUndigested(detail)
	changed := false
	detail.Candidates = slices.Clone(detail.Candidates)
	for i, candidate := range detail.Candidates {
		limits := slices.DeleteFunc(slices.Clone(candidate.Limits), func(limit LiveLimit) bool {
			return limit.Code == liveLimitDigestFailed
		})
		for _, id := range liveShownSourceIDs(candidate) {
			if slices.Contains(missing, id) {
				limits = append(limits, liveLimitOf(liveLimitDigestFailed,
					"русская интерпретация источника "+id+" недоступна: фоновый проход не завершил пересказ", nil))
			}
		}
		// Оговорки других проверок скопированы без изменений, поэтому сравниваются только код и текст.
		changed = changed || !slices.EqualFunc(candidate.Limits, limits, func(a, b LiveLimit) bool {
			return a.Code == b.Code && a.Text == b.Text
		})
		detail.Candidates[i].Limits = limits
	}
	return detail, changed
}
