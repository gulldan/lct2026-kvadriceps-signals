package main

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"slices"
	"strings"
	"time"

	"golang.org/x/net/html"
)

// Каталог подписывает week25 как June25,2026, а обложка и ссылки выпуска — June23:
// дата берётся с первичной обложки, а не из календаря.
func patentIssueDate(body []byte, listed issue) (issue, error) {
	doc, err := html.Parse(bytes.NewReader(body))
	if err != nil {
		return listed, fmt.Errorf("parse Gazette cover: %w", err)
	}
	dates := []string{}
	official := false
	visit(doc, func(n *html.Node) {
		if n.Type != html.ElementNode {
			return
		}
		text := strings.Join(strings.Fields(content(n)), " ")
		if n.Data == elementTitle && text == "OFFICIAL GAZETTE" {
			official = true
		}
		if n.Data != "b" || !strings.HasPrefix(text, "PATENTS") || !strings.Contains(text, "Vol.") {
			return
		}
		for _, value := range patentDate.FindAllString(text, -1) {
			dates = append(dates, parsePatentDay(value))
		}
	})
	slices.Sort(dates)
	dates = slices.Compact(dates)
	if !official || len(dates) != 1 || dates[0] == "" {
		return listed, errors.New("gazette cover does not establish one issue date")
	}
	original, err := time.Parse(time.DateOnly, listed.Date)
	if err != nil {
		return listed, fmt.Errorf("parse listed Gazette date: %w", err)
	}
	actual, err := time.Parse(time.DateOnly, dates[0])
	if err != nil {
		return listed, fmt.Errorf("parse primary Gazette date: %w", err)
	}
	// Переиспользуемый недельный адрес не должен молча сменить год выпуска.
	if actual.Before(original.AddDate(0, 0, -7)) || actual.After(original.AddDate(0, 0, 7)) {
		return listed, errors.New("gazette cover is outside its listed issue week")
	}
	listed.Date = dates[0]
	return listed, nil
}

func (s *service) resolvePatentIssue(ctx context.Context, listed issue, data *snapshot) (issue, provenance, error) {
	address := listed.URL + "/"
	body, err := s.cached(ctx, address, listed.Date, data)
	if err != nil {
		return listed, provenance{}, fmt.Errorf("retrieve Gazette cover: %w", err)
	}
	resolved, err := patentIssueDate(body, listed)
	return resolved, provenance{URL: address, SHA256: hashBytes(body)}, err
}
