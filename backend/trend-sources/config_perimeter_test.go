package main

import (
	"encoding/json/v2"
	"net/url"
	"os"
	"testing"
)

// Лента с доменом вне периметра молча не собирается: клиент отклоняет запрос, и записей просто нет.
func TestConfiguredFeedsStayInsidePerimeter(t *testing.T) {
	raw, err := os.ReadFile("../../infra/trend-sources.json")
	if err != nil {
		t.Skipf("конфигурация недоступна: %v", err)
	}
	var config sourceConfig
	if err := json.Unmarshal(raw, &config); err != nil {
		t.Fatalf("разбор конфигурации: %v", err)
	}
	if len(config.Web) == 0 {
		t.Fatal("в конфигурации нет ни одной ленты")
	}
	for _, source := range config.Web {
		parsed, err := url.Parse(source.URL)
		if err != nil {
			t.Errorf("%s: адрес не разобран: %v", source.Name, err)
			continue
		}
		if err := allowedAddress(parsed, config.AllowedDomains); err != nil {
			t.Errorf("%s: %v", source.Name, err)
		}
		if source.Name == "" || source.Industry == "" {
			t.Errorf("%s: у ленты нет названия или отрасли", source.URL)
		}
	}
}
