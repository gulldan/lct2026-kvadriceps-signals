package main

import (
	"context"
	"encoding/json/v2"
	"errors"
	"fmt"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"syscall"
	"time"

	"github.com/example/source-harvester/internal/platform/config"
	"github.com/example/source-harvester/internal/platform/storage"
	amqp "github.com/rabbitmq/amqp091-go"
)

const progressPath = "/tmp/harvester-progress.json"

// Срок двигает сам планировщик, а не фоновый таймер; простаивающий планировщик спит до следующего раунда.
type progress struct {
	PID      int       `json:"pid"`
	Deadline time.Time `json:"deadline"`
}

func recordProgress(path string, duration time.Duration) error {
	raw, err := json.Marshal(progress{PID: os.Getpid(), Deadline: time.Now().Add(duration)})
	if err != nil {
		return errors.New("encode scheduler progress")
	}
	f, err := os.CreateTemp(filepath.Dir(path), ".harvester-progress-")
	if err != nil {
		return errors.New("create scheduler progress")
	}
	defer func() { _ = f.Close(); _ = os.Remove(f.Name()) }()
	if _, err = f.Write(raw); err != nil {
		return errors.New("write scheduler progress")
	}
	if err = f.Close(); err != nil {
		return errors.New("close scheduler progress")
	}
	if err := os.Rename(f.Name(), path); err != nil {
		return fmt.Errorf("publish scheduler progress: %w", err)
	}
	return nil
}

func trackProgress(once bool, timeout, interval time.Duration, run func(scheduledScope) error) func(scheduledScope) error {
	if once {
		return run
	}
	return func(scope scheduledScope) error {
		if err := recordProgress(progressPath, timeout+30*time.Second); err != nil {
			return err
		}
		err := run(scope)
		if progressErr := recordProgress(progressPath, interval+30*time.Second); progressErr != nil {
			return progressErr
		}
		return err
	}
}

func checkProgress(path string, now time.Time) error {
	raw, err := os.ReadFile(path) // #nosec G304 -- fixed local runtime path, or test temporary directory.
	if err != nil || len(raw) > 1024 {
		return errors.New("scheduler has not started")
	}
	var state progress
	if json.Unmarshal(raw, &state, json.RejectUnknownMembers(true)) != nil || state.PID < 1 || !now.Before(state.Deadline) {
		return errors.New("scheduler progress expired or invalid")
	}
	process, err := os.FindProcess(state.PID)
	if err != nil || process.Signal(syscall.Signal(0)) != nil {
		return errors.New("scheduler process unavailable")
	}
	return nil
}

func healthcheck(ctx context.Context) error {
	if err := checkProgress(progressPath, time.Now()); err != nil {
		return err
	}
	ctx, cancel := context.WithTimeout(ctx, 8*time.Second)
	defer cancel()
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, config.Get("DB_READY_URL", "http://db-service:9090/readyz"), nil)
	if err != nil {
		return errors.New("invalid DB readiness address")
	}
	response, err := http.DefaultClient.Do(req)
	if err != nil {
		return errors.New("DB readiness unavailable")
	}
	_ = response.Body.Close()
	if response.StatusCode != http.StatusOK {
		return errors.New("DB is not ready")
	}
	objects, err := storage.Open(ctx, storage.ConfigFromEnv())
	if err != nil || objects.Ping(ctx) != nil {
		return errors.New("object store is not ready")
	}
	return brokerReady(ctx)
}

func brokerReady(ctx context.Context) error {
	dialer := &net.Dialer{Timeout: 3 * time.Second}
	connection, err := amqp.DialConfig(os.Getenv("RABBITMQ_URL"), amqp.Config{
		Dial: func(network, address string) (net.Conn, error) {
			conn, err := dialer.DialContext(ctx, network, address)
			if err == nil {
				deadline, _ := ctx.Deadline()
				err = conn.SetDeadline(deadline)
				if err != nil {
					_ = conn.Close()
				}
			}
			if err != nil {
				return nil, fmt.Errorf("connect broker readiness probe: %w", err)
			}
			return conn, nil
		},
	})
	if err != nil {
		return errors.New("broker is not ready")
	}
	_ = connection.Close()
	return nil
}
