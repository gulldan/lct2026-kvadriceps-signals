package application

import (
	"bytes"
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/json/jsontext"
	"encoding/json/v2"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"slices"
)

type liveStore interface {
	Put(ctx context.Context, key string, r io.Reader, size int64, contentType string) error
	GetBytes(ctx context.Context, key string) ([]byte, error)
}

func (s *TrendLiveService) load(ctx context.Context) {
	raw, err := s.store.GetBytes(ctx, liveSnapshotKey)
	if err != nil {
		// Без снимка карта пуста и направления не пересчитываются: 06.10 радар так молча простоял полдня.
		slog.Warn("radar snapshot unavailable", "error", err)
		return
	}
	s.loadSnapshot(raw)
}

func (s *TrendLiveService) loadSnapshot(raw []byte) {
	hash := sha256.Sum256(raw)
	s.mu.RLock()
	unchanged := s.snapshot != nil && s.loadedHash == hash
	s.mu.RUnlock()
	if unchanged {
		return
	}
	var snapshot LiveSnapshot
	if err := json.Unmarshal(raw, &snapshot); err != nil || snapshot.Version != liveVersion ||
		(snapshot.Method != liveMethod && snapshot.Method != livePriorMethod) {
		slog.Warn("radar snapshot rejected", "error", err, "version", snapshot.Version, "method", snapshot.Method)
		return
	}
	liveAnnotate(snapshot.Topics, snapshot.Discovered)
	s.mu.Lock()
	// Кэш ответов операторов переживает новый снимок: ответ ложится в базу только после поздних
	// пересказов, и 08.10 опрос между переизданием снимка и записью получил из базы ответ 27.09.
	s.snapshot, s.status = &snapshot, "ready"
	s.loadedHash = hash
	s.mu.Unlock()
}

func (s *TrendLiveService) publish(ctx context.Context, snapshot *LiveSnapshot) error {
	if err := ctx.Err(); err != nil {
		return fmt.Errorf("live: publication canceled: %w", err)
	}
	if s.writerGuard != nil {
		_, release, guardErr := s.writerGuard(ctx)
		if guardErr != nil {
			return guardErr
		}
		release()
	}
	if version, ok := ctx.Value(liveVersionContext{}).(liveWriteVersion); ok {
		snapshot.WriterEpoch = version.epoch
	}
	// Журнал прочитанных страниц источника одинаков у всех направлений и занимал почти половину снимка: 06.10
	// снимок перерос лимит чтения хранилища, и радар встал. Страницам он не нужен, полный журнал хранит сборщик.
	for _, topics := range []*[]LiveTopic{&snapshot.Topics, &snapshot.Discovered} {
		*topics = slices.Clone(*topics)
		for i := range *topics {
			(*topics)[i] = withoutProvenance((*topics)[i])
		}
	}
	raw, err := json.Marshal(snapshot, json.Deterministic(true))
	if err != nil {
		return fmt.Errorf("live: encode snapshot: %w", err)
	}
	if err := s.writeSnapshot(ctx, raw); err != nil {
		return fmt.Errorf("live: persist snapshot: %w", err)
	}
	s.mu.Lock()
	s.snapshot, s.status, s.lastError = snapshot, "ready", ""
	s.loadedHash = sha256.Sum256(raw)
	clear(s.cache)
	s.mu.Unlock()
	return nil
}

type liveVersionStore interface {
	GetVersion(context.Context, string) ([]byte, string, error)
	PutIfVersion(context.Context, string, io.Reader, int64, string, string) error
}

type liveWriteVersion struct{ etag, epoch string }

type liveVersionContext struct{}

func (s *TrendLiveService) prepareWrite(ctx context.Context) (context.Context, error) {
	versioned, ok := s.store.(liveVersionStore)
	if !ok {
		if s.writerGuard != nil {
			return ctx, errors.New("distributed radar writer requires conditional object storage")
		}
		s.load(ctx)
		return ctx, nil
	}
	raw, version, err := versioned.GetVersion(ctx, liveSnapshotKey)
	if err != nil {
		return ctx, err
	}
	// ETag снимка меняется до тяжёлой работы: приостановленный старый процесс не опубликует поверх нового писателя.
	document, err := decodeWriterSnapshot(raw)
	if err != nil {
		return ctx, err
	}
	if len(raw) > 0 {
		s.loadSnapshot(raw)
	}
	epoch := rand.Text()
	value, err := json.Marshal(epoch)
	if err != nil {
		return ctx, fmt.Errorf("encode writer identity: %w", err)
	}
	document["writer_epoch"] = value
	claim, err := json.Marshal(document, json.Deterministic(true))
	if err != nil {
		return ctx, fmt.Errorf("encode writer claim: %w", err)
	}
	if err = versioned.PutIfVersion(
		ctx, liveSnapshotKey, bytes.NewReader(claim), int64(len(claim)), "application/json", version,
	); err != nil {
		return ctx, fmt.Errorf("claim radar snapshot: %w", err)
	}
	current, etag, err := versioned.GetVersion(ctx, liveSnapshotKey)
	if err != nil {
		return ctx, fmt.Errorf("read claimed snapshot: %w", err)
	}
	if !bytes.Equal(current, claim) {
		return ctx, errors.New("radar writer claim superseded")
	}
	s.loadSnapshot(claim)
	return context.WithValue(ctx, liveVersionContext{}, liveWriteVersion{etag: etag, epoch: epoch}), nil
}

func decodeWriterSnapshot(raw []byte) (map[string]jsontext.Value, error) {
	document := map[string]jsontext.Value{}
	if len(raw) == 0 {
		return document, nil
	}
	if err := json.Unmarshal(raw, &document); err != nil {
		return nil, fmt.Errorf("decode writer claim: %w", err)
	}
	if _, hasVersion := document["version"]; !hasVersion {
		return document, nil
	}
	var header struct {
		Version string `json:"version"`
		Method  string `json:"method"`
	}
	if err := json.Unmarshal(raw, &header); err != nil {
		return nil, fmt.Errorf("read snapshot version: %w", err)
	}
	if header.Version != liveVersion || (header.Method != liveMethod && header.Method != livePriorMethod) {
		return nil, errors.New("unsupported radar snapshot version; refusing replacement")
	}
	return document, nil
}

func (s *TrendLiveService) writeSnapshot(ctx context.Context, raw []byte) error {
	if versioned, ok := s.store.(liveVersionStore); ok {
		version, ok := ctx.Value(liveVersionContext{}).(liveWriteVersion)
		if !ok {
			return errors.New("radar publication has no observed object version")
		}
		return versioned.PutIfVersion(ctx, liveSnapshotKey, bytes.NewReader(raw), int64(len(raw)), "application/json", version.etag)
	}
	return s.store.Put(ctx, liveSnapshotKey, bytes.NewReader(raw), int64(len(raw)), "application/json")
}
