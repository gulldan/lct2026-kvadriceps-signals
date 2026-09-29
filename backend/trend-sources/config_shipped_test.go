package main

import (
	"encoding/json"
	"os"
	"testing"
)

func TestShippedSourceConfigIsValid(t *testing.T) {
	body, err := os.ReadFile("../../infra/trend-sources.json")
	if err != nil {
		t.Skipf("конфигурация недоступна: %v", err)
	}
	config := defaultConfig()
	if err := json.Unmarshal(body, &config); err != nil {
		t.Fatalf("конфигурация не разбирается: %v", err)
	}
	if err := validateSourceConfig(config); err != nil {
		t.Fatalf("конфигурация не проходит проверку сервиса: %v", err)
	}
	if len(config.Web) < 40 {
		t.Fatalf("набор веб-источников неожиданно короткий: %d", len(config.Web))
	}
}
