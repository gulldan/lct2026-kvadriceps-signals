package splitter

import "unicode"

// LocateOffsets сопоставляет чанки с источником по проекции с нормализованными пробелами: края и перекрытие
// у чанков другие. Чанк, не найденный при движении вперёд (начала не убывают, концы строго растут),
// получает [0, 0], а не догадку.
func LocateOffsets(source string, chunkTexts []string) [][2]int {
	out := make([][2]int, len(chunkTexts))
	if len(chunkTexts) == 0 {
		return out
	}

	norm, mapNormToOrig := normalizeWithMap(source)

	// Перекрытие может повторить короткий чанк целиком: начало может совпасть, но конец обязан продвинуться,
	// иначе повтор займёт старый промежуток.
	cursor, lastEnd := 0, 0
	for i, ct := range chunkTexts {
		needle := normalize(ct)
		if len(needle) == 0 {
			out[i] = [2]int{0, 0}
			continue
		}
		ns := indexFrom(norm, needle, cursor)
		for ns >= 0 && ns+len(needle) <= lastEnd {
			ns = indexFrom(norm, needle, ns+1)
		}
		if ns < 0 {
			// Курсор не двигается: единичный промах не должен сбить все следующие чанки.
			out[i] = [2]int{0, 0}
			continue
		}
		ne := ns + len(needle)
		out[i] = [2]int{mapNormToOrig[ns], mapNormToOrig[ne]}
		cursor, lastEnd = ns, ne
	}
	return out
}

// Пробельные промежутки схлопываются в один пробел, края обрезаются. Схлопнутый промежуток отображается
// на свою первую руну; последний элемент карты — страж len(norm) → len(orig) для полуоткрытых концов.
func normalizeWithMap(s string) (norm []rune, mapNormToOrig []int) {
	orig := []rune(s)
	norm = make([]rune, 0, len(orig))
	mapNormToOrig = make([]int, 0, len(orig)+1)

	pendingSpace := false
	spaceOrig := 0
	for i, r := range orig {
		if unicode.IsSpace(r) {
			if !pendingSpace {
				pendingSpace = true
				spaceOrig = i
			}
			continue
		}
		if pendingSpace {
			if len(norm) > 0 {
				norm = append(norm, ' ')
				mapNormToOrig = append(mapNormToOrig, spaceOrig)
			}
			pendingSpace = false
		}
		norm = append(norm, r)
		mapNormToOrig = append(mapNormToOrig, i)
	}

	mapNormToOrig = append(mapNormToOrig, len(orig))
	return norm, mapNormToOrig
}

func normalize(s string) []rune {
	out := make([]rune, 0, len(s))
	pendingSpace := false
	for _, r := range s {
		if unicode.IsSpace(r) {
			pendingSpace = true
			continue
		}
		if pendingSpace {
			if len(out) > 0 {
				out = append(out, ' ')
			}
			pendingSpace = false
		}
		out = append(out, r)
	}
	return out
}

// Вызывающие гарантируют непустую иглу.
func indexFrom(haystack, needle []rune, from int) int {
	if from < 0 {
		from = 0
	}
	for i := from; i+len(needle) <= len(haystack); i++ {
		match := true
		for j := range needle {
			if haystack[i+j] != needle[j] {
				match = false
				break
			}
		}
		if match {
			return i
		}
	}
	return -1
}
