package main

import "fmt"

import (
	"context"
	"time"

	"github.com/example/db-service/internal/infrastructure/postgres"
	"github.com/example/db-service/internal/platform/config"
	"github.com/example/db-service/internal/platform/contracts"
	"github.com/example/db-service/internal/platform/messaging"
	"github.com/rs/zerolog"
	"google.golang.org/protobuf/proto"
)

func dispatchIngestion(ctx context.Context, repo *postgres.DocumentRepo, log zerolog.Logger) {
	for ctx.Err() == nil {
		if err := dispatchConnected(ctx, repo, log); err != nil && ctx.Err() == nil {
			log.Error().Err(err).Msg("ingestion dispatch pending")
		}
		select {
		case <-ctx.Done():
			return
		case <-time.After(2 * time.Second):
		}
	}
}

func dispatchConnected(ctx context.Context, repo *postgres.DocumentRepo, log zerolog.Logger) error {
	conn, err := messaging.Dial(ctx, config.MustGet("RABBITMQ_URL"), log)
	if err != nil {
		return err
	}
	defer func() { _ = conn.Close() }()
	if err = conn.DeclareTopology(); err != nil {
		return err
	}
	pub, err := conn.NewPublisher()
	if err != nil {
		return err
	}
	defer func() { _ = pub.Close() }()
	for ctx.Err() == nil {
		operation, cancel := context.WithTimeout(ctx, 15*time.Second)
		sent, err := repo.DispatchOne(operation, func(ctx context.Context, route string, event proto.Message) error {
			return pub.PublishProto(ctx, contracts.ExchangeIngestion, route, event)
		})
		cancel()
		if err != nil {
			return err
		}
		if !sent {
			select {
			case <-ctx.Done():
				if operationErr := ctx.Err(); operationErr != nil {
					return fmt.Errorf("dispatch connected: %w", operationErr)
				}
				return nil
			case <-time.After(time.Second):
			}
		}
	}
	if operationErr := ctx.Err(); operationErr != nil {
		return fmt.Errorf("dispatch connected: %w", operationErr)
	}
	return nil
}
