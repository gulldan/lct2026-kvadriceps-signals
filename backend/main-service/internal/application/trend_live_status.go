package application

import (
	"context"
	"errors"
	"slices"
	"strings"
)

var ErrLiveBusy = errors.New("radar collection is already running")

// Коды причины отказа сбора: интерфейс подбирает по коду готовую фразу, а текст остаётся для журналов.
const (
	LiveFailureCanceled = "canceled"
	LiveFailureSource   = "source"
	LiveFailureOther    = "other"
)

func LiveFailureCode(text string) string {
	switch {
	case strings.TrimSpace(text) == "":
		return ""
	case strings.Contains(text, "context canceled"), strings.Contains(text, "writer interrupted"):
		return LiveFailureCanceled
	case strings.Contains(text, "source status"), strings.Contains(text, "connection refused"),
		strings.Contains(text, "no such host"):
		return LiveFailureSource
	default:
		return LiveFailureOther
	}
}

type LiveCollectorChannel struct {
	SourceSet         string `json:"source_set"`
	AttachedSourceSet string `json:"attached_source_set"`
	Collecting        bool   `json:"collecting"`
	Name              string `json:"name"`
	Status            string `json:"status"`
	ObservedAt        string `json:"observed_at"`
	AttachedAt        string `json:"attached_at"`
	NextRunAt         string `json:"next_run_at"`
	Error             string `json:"error"`
	ErrorCode         string `json:"error_code"`
}

type LiveCollectorRun struct {
	Kind       string `json:"kind"`
	StartedAt  string `json:"started_at"`
	FinishedAt string `json:"finished_at"`
	Success    bool   `json:"success"`
	Error      string `json:"error"`
}

type LiveCollectionStatus struct {
	Phase         string                 `json:"phase"`
	CheckedAt     string                 `json:"checked_at"`
	NextRunAt     string                 `json:"next_run_at"`
	LastSuccessAt string                 `json:"last_success_at"`
	LastSourceAt  string                 `json:"last_source_at"`
	Error         string                 `json:"error"`
	ErrorCode     string                 `json:"error_code"`
	Channels      []LiveCollectorChannel `json:"channels"`
	Runs          []LiveCollectorRun     `json:"runs"`
}

// CollectedChannel читает подтверждение из снимка: публичный обзор удаляет даты и версии каналов,
// а сохранённые запросы пользователей не участвуют в фоновом доборе источников.
func (s *TrendLiveService) CollectedChannel(name string) LiveCollectorChannel {
	s.mu.RLock()
	defer s.mu.RUnlock()
	channel := LiveCollectorChannel{Name: name, Status: "unavailable"}
	if s.snapshot == nil {
		return channel
	}
	for _, topic := range slices.Concat(s.snapshot.Topics, s.snapshot.Discovered) {
		var data *LiveChannel
		switch name {
		case "patents":
			data = topic.Patents
		case "web":
			data = topic.Web
		case "patents_cn":
			data = topic.PatentsCN
		}
		if data == nil || data.ObservedAt == "" {
			channel.AttachedAt, channel.AttachedSourceSet = "", ""
			return channel
		}
		if channel.AttachedAt == "" {
			channel.AttachedAt, channel.AttachedSourceSet = data.ObservedAt, data.SourceSet
		}
		if data.ObservedAt < channel.AttachedAt {
			channel.AttachedAt = data.ObservedAt
		}
		if data.SourceSet != channel.AttachedSourceSet {
			channel.AttachedSourceSet = "mixed"
		}
	}
	return channel
}

// SetWriterGuard вызывается до запуска любого сборщика.
func (s *TrendLiveService) SetWriterGuard(guard func(context.Context) (context.Context, func(), error)) {
	s.writerGuard = guard
}

// SetCollectionReader вызывается до запуска читателей.
func (s *TrendLiveService) SetCollectionReader(read func(context.Context) (*LiveCollectionStatus, error)) {
	s.collectionReader = read
}

func (s *TrendLiveService) readCollection(ctx context.Context) {
	if s.collectionReader == nil {
		return
	}
	status, err := s.collectionReader(ctx)
	if err != nil || status == nil {
		return
	}
	// Код считается при чтении: запись прошлой версии сборщика иначе пришла бы без кода.
	status.ErrorCode = LiveFailureCode(status.Error)
	for i := range status.Channels {
		status.Channels[i].ErrorCode = LiveFailureCode(status.Channels[i].Error)
	}
	s.mu.Lock()
	s.collection = status
	s.mu.Unlock()
}

func (s *TrendLiveService) beginWrite(ctx context.Context) (context.Context, func(), error) {
	s.mu.Lock()
	if s.refreshing {
		s.mu.Unlock()
		return ctx, nil, ErrLiveBusy
	}
	s.refreshing = true
	s.mu.Unlock()
	finish := func() {
		s.mu.Lock()
		s.refreshing = false
		s.mu.Unlock()
	}
	if s.writerGuard == nil {
		prepared, err := s.prepareWrite(ctx)
		if err != nil {
			finish()
			return ctx, nil, err
		}
		return prepared, finish, nil
	}
	guarded, release, err := s.writerGuard(ctx)
	if err != nil {
		finish()
		return ctx, nil, err
	}
	prepared, err := s.prepareWrite(guarded)
	if err != nil {
		release()
		finish()
		return ctx, nil, err
	}
	return prepared, func() { release(); finish() }, nil
}
