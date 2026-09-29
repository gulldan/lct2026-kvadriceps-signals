package main

import (
	"context"
	"sync"

	"github.com/example/llm-service/internal/infrastructure/aiadapters"
	"github.com/example/llm-service/internal/interfaces/grpcserver"
	"github.com/example/llm-service/internal/platform/aiclients"
	"github.com/example/llm-service/internal/platform/config"
	"github.com/example/llm-service/internal/platform/dbclient"
)

func startDescriptions(ctx context.Context, server *grpcserver.Server, db *dbclient.Client,
	generator aiclients.Generator, usage aiadapters.UsageRecorder,
) func() {
	ctx, cancel := context.WithCancel(ctx)
	var worker sync.WaitGroup
	if config.GetBool("SIGNAL_DESCRIPTIONS", false) {
		// Каждый цикл берёт свою лизу; параллельность ограничена общими слотами
		// сервера, а число циклов — тем, сколько описаний шлюз тянет разом.
		describer := aiadapters.NewDescriber(generator, usage)
		for range max(1, config.GetInt("DESCRIPTION_WORKERS", 8)) {
			worker.Go(func() { server.RunDescriptions(ctx, db, describer) })
		}
	}
	return func() { cancel(); worker.Wait() }
}
