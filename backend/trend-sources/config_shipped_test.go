package main

import (
	"errors"
	"os"
	"testing"
)

func TestShippedSourceConfigIsValid(t *testing.T) {
	config, err := loadConfig("../../infra/trend-sources.json")
	if errors.Is(err, os.ErrNotExist) {
		t.Skipf("конфигурация недоступна: %v", err)
	}
	if err != nil {
		t.Fatalf("конфигурация не проходит проверку сервиса: %v", err)
	}
	if len(config.Web) < 40 {
		t.Fatalf("набор веб-источников неожиданно короткий: %d", len(config.Web))
	}
}
