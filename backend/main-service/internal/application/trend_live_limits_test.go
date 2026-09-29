package application

import (
	"encoding/json/v2"
	"testing"
)

func TestLiveLimitReadsStoredStringsAndObjects(t *testing.T) {
	var limits []LiveLimit
	raw := `["arxiv: 10 of 20 papers enumerated",
		{"code":"discovery_below_floor","values":{"directions":11,"papers":30},"text":"…"}]`
	if err := json.Unmarshal([]byte(raw), &limits); err != nil {
		t.Fatalf("старый снимок больше не читается: %v", err)
	}
	if len(limits) != 2 || limits[0].Code != "" || limits[0].Text == "" {
		t.Fatalf("строка без кода прочитана неверно: %+v", limits)
	}
	if limits[1].Code != liveLimitBelowFloor || limits[1].Values["directions"] != 11 ||
		limits[1].Values["papers"] != 30 {
		t.Fatalf("числа не доехали до интерфейса: %+v", limits[1])
	}
}
