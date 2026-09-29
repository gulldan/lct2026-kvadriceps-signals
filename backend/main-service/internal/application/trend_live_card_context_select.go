package application

import (
	"cmp"
	"slices"
	"strings"
	"unicode/utf8"
)

const liveCardContextGap = "\n\n[…]\n\n"

// Длинная вводная не вытесняет поздние результаты. Выбор не зависит от желаемого преимущества или запроса.
func liveSelectCardContext(document liveCardContextDocument, limit int) (string, []LiveCardContextRange) {
	if limit <= 0 {
		return "", nil
	}
	if length := utf8.RuneCountInString(document.text); length <= limit {
		return document.text, []LiveCardContextRange{{StartRune: 0, EndRune: length}}
	}
	order := make([]int, 0, len(document.blocks))
	for index, block := range document.blocks {
		if !strings.HasPrefix(block.kind, "h") {
			order = append(order, index)
		}
	}
	slices.SortStableFunc(order, func(a, b int) int {
		return cmp.Compare(liveCardBlockPriority(document.blocks, a), liveCardBlockPriority(document.blocks, b))
	})
	chosen := make([]bool, len(document.blocks))
	for _, index := range order {
		group := liveCardBlockGroup(document.blocks, index)
		cost := 0
		for _, member := range group {
			if !chosen[member] {
				cost += utf8.RuneCountInString(document.blocks[member].text) + utf8.RuneCountInString(liveCardContextGap)
			}
		}
		if cost > limit {
			continue
		}
		limit -= cost
		for _, member := range group {
			chosen[member] = true
		}
	}
	return liveJoinCardBlocks(document, chosen)
}

func liveCardBlockPriority(blocks []liveCardContextBlock, index int) int {
	block := blocks[index]
	var headings strings.Builder
	for _, parent := range block.headings {
		headings.WriteString(strings.ToLower(blocks[parent].text))
		headings.WriteByte(' ')
	}
	section := headings.String()
	for rank, names := range []string{
		"limitation discussion",
		"result evaluation experiment performance",
		"conclusion",
		"method approach framework design architecture",
	} {
		if slices.ContainsFunc(strings.Fields(names), func(name string) bool { return strings.Contains(section, name) }) {
			if block.kind == liveCardTableBlock {
				return rank * 2
			}
			return rank*2 + 1
		}
	}
	if strings.Contains(section, "reference") || strings.Contains(section, "bibliograph") {
		return 12
	}
	if block.kind == liveCardTableBlock {
		return 8
	}
	return 9
}

// Таблица неделима: шапка, все строки, подпись и ближайшие абзацы условий уходят одним пакетом.
func liveCardBlockGroup(blocks []liveCardContextBlock, index int) []int {
	members := []int{index}
	if blocks[index].kind == liveCardTableBlock {
		for _, direction := range []int{-1, 1} {
			paragraphs := 0
			for distance := 1; distance <= 8 && paragraphs < 2; distance++ {
				neighbor := index + direction*distance
				if neighbor < 0 || neighbor >= len(blocks) {
					break
				}
				if blocks[neighbor].kind == "p" {
					members = append(members, neighbor)
					paragraphs++
				}
			}
		}
	}
	for _, member := range slices.Clone(members) {
		members = append(members, blocks[member].headings...)
	}
	slices.Sort(members)
	return slices.Compact(members)
}

func liveJoinCardBlocks(document liveCardContextDocument, chosen []bool) (string, []LiveCardContextRange) {
	var text strings.Builder
	ranges := make([]LiveCardContextRange, 0, len(chosen))
	previous := -2
	for index, yes := range chosen {
		if !yes {
			continue
		}
		if text.Len() > 0 {
			if previous+1 == index {
				text.WriteString("\n\n")
			} else {
				text.WriteString(liveCardContextGap)
			}
		}
		block := document.blocks[index]
		text.WriteString(block.text)
		ranges = append(ranges, block.span)
		previous = index
	}
	return text.String(), ranges
}
