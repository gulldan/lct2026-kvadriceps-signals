package application

import (
	"cmp"
	"slices"
	"strings"
	"unicode"
	"unicode/utf8"
)

const (
	// После отсева зрелого из сорока оставалось около десяти, а выдаче нужно пятнадцать.
	liveDiscoverMax       = 90
	liveDiscoverMinPapers = 30
	liveDiscoverRecent    = 15
	liveDiscoverEarly     = 2
	liveGenericShare      = 0.03
	liveGenericMin        = 50
	liveOverlapSkip       = 0.7
	liveStandaloneMin     = 0.2
)

// Оболочка заголовка из liveStopword сюда не входит: «method» в «Zeroth Order Method» группу продолжает.
func liveFunctionWord(token string) bool {
	switch token {
	case "a", "an", "the", "of", "for", "and", "or", "in", "on", "with", "via", "to", "from", "by", "using", "at", "as",
		"is", "are", "be", "into", "over", "under", "through", "without", "beyond", "towards", "toward", "vs", "versus",
		"per", "its", "their", "our", "we", "you", "it", "this", "that", "these", "those", "than", "then", "when",
		"where", "how", "what", "which", "who", "not", "no", "do", "does", "can", "may", "new", "your", "all", "any",
		// «Off» не случайно: «trade-off» распадается на «trade» и «offs», и без частицы «Trade Offs» выходит
		// технологией.
		"between", "among", "amongst", "against", "about", "across", "within", "during", "off", "onto",
		"upon", "whether", "while", "such", "each", "both", "other", "others", "more", "most", "less",
		"least", "very", "own", "also", "yet", "but", "if", "so", "there", "here":
		return true
	}
	return false
}

func liveStopword(token string) bool {
	if liveFunctionWord(token) {
		return true
	}
	switch token {
	// Оболочка научного заголовка не может начинать или заканчивать фразу, иначе «Graph Based» выходит технологией.
	case "based", "method", "methods", "approach", "approaches", "technique", "techniques", "framework",
		"frameworks", "study", "studies", "survey", "review", "analysis", "novel", "efficient",
		"robust", "improved", "enhanced", "case", "results", "evaluation", "comparison", "application",
		"applications", "problem", "problems", "solution", "solutions",
		// Исследовательское действие называет, что сделали с предметом, а не сам предмет.
		"experiment", "experiments", "measurement", "measurements", "investigation", "investigations",
		"characterization", "characterisation", "observation", "observations", "simulation", "simulations",
		// Разговор о предмете, а не предмет: так заголовок обрамляет содержание с любой стороны.
		"challenge", "challenges", "opportunity", "opportunities", "perspective", "perspectives",
		"insight", "insights", "progress", "advance", "advances", "overview", "report", "reports":
		return true
	}
	return false
}

// Проверяется только последнее слово: оно различает «pressure sensor» (устройство) и «stack pressure» (величину).
func liveObjectHead(stem string) bool {
	switch stem {
	// Измеряемая величина: у неё есть значение и единица, но нет применения.
	case "pressure", "temperature", "stress", "strain", "deformation", "displacement", "viscosity",
		"conductivity", "resistivity", "resistance", "impedance", "voltage", "capacitance", "density",
		"porosity", "thickness", "capacity", "efficiency", "accuracy", "sensitivity", "selectivity",
		"stability", "durability", "reliability", "performance", "rate", "ratio", "coefficient",
		"threshold", "amplitude", "concentration", "distribution", "variability", "uncertainty":
		return true
	// Явление и свойство: оно происходит или наблюдается, его нельзя внедрить.
	case "dynamic", "kinetic", "mechanic", "thermodynamic", "physic", "chemistry", "rheology",
		"morphology", "property", "behaviour", "behavior", "phenomenon", "phenomena", "transition",
		"transport", "diffusion", "degradation", "corrosion", "aging", "ageing", "failure",
		"formation", "nucleation", "evolution", "growth", "interaction", "effect", "influence",
		"impact", "role", "state", "activity", "cohesion", "disease", "infection":
		return true
	}
	return false
}

func liveStem(token string) string {
	if len(token) > 5 && strings.HasSuffix(token, "ies") {
		return token[:len(token)-3] + "y"
	}
	if len(token) > 5 && (strings.HasSuffix(token, "sses") || strings.HasSuffix(token, "ches") || strings.HasSuffix(token, "shes")) {
		return token[:len(token)-2]
	}
	if len(token) > 3 && strings.HasSuffix(token, "s") && !strings.HasSuffix(token, "ss") &&
		!strings.HasSuffix(token, "us") && !strings.HasSuffix(token, "is") {
		return token[:len(token)-1]
	}
	return token
}

// liveRawTokens хранит исходный регистр для написания фразы (LLM, RL, GUI).
func liveTokens(title string) []string {
	return liveRawTokens(strings.ToLower(title))
}

func liveRawTokens(title string) []string {
	letter := func(r rune) bool { return (r|0x20 >= 'a' && r|0x20 <= 'z') || (r >= '0' && r <= '9') }
	fields := strings.FieldsFunc(title, func(r rune) bool { return !letter(r) })
	tokens := fields[:0]
	for _, field := range fields {
		if len(field) >= 2 && strings.ContainsFunc(field, func(r rune) bool { return r|0x20 >= 'a' && r|0x20 <= 'z' }) {
			tokens = append(tokens, field)
		}
	}
	return tokens
}

// standalone — ни слева, ни справа от фразы в заголовке нет ещё одного содержательного слова.
type liveGram struct {
	key, surface string
	standalone   bool
}

func liveGrams(tokens []string, generic map[string]bool) []liveGram {
	return liveGramsOf(tokens, tokens, generic)
}

func liveGramsOf(tokens, raw []string, generic map[string]bool) []liveGram {
	grams := []liveGram{}
	stems := make([]string, len(tokens))
	for i, token := range tokens {
		stems[i] = liveStem(token)
	}
	for n := 2; n <= 3; n++ {
		for i := 0; i+n <= len(tokens); i++ {
			window := stems[i : i+n]
			if liveStopword(window[0]) || liveStopword(window[n-1]) || liveObjectHead(window[n-1]) {
				continue
			}
			if !slices.ContainsFunc(window, func(token string) bool { return !generic[token] && !liveStopword(token) }) {
				continue
			}
			key := strings.Join(window, " ")
			alone := liveGroupEdge(stems, i-1) && liveGroupEdge(stems, i+n)
			// Фраза может встретиться в заголовке дважды, и засчитывается лучшее вхождение.
			if at := slices.IndexFunc(grams, func(g liveGram) bool { return g.key == key }); at >= 0 {
				grams[at].standalone = grams[at].standalone || alone
				continue
			}
			grams = append(grams, liveGram{key: key, surface: strings.Join(raw[i:i+n], " "), standalone: alone})
		}
	}
	return grams
}

func liveGroupEdge(stems []string, i int) bool {
	return i < 0 || i >= len(stems) || liveFunctionWord(stems[i])
}

// titles — число разных заголовков с фразой, standalone — где она целая именная группа.
type liveShape struct{ titles, standalone int }

// Определение («Large Scale») целой группой почти не стоит; порог низкий, потому что настоящее имя
// часто служит и определением.
func liveFragmentPhrase(shape liveShape) bool {
	return shape.titles >= 3 && float64(shape.standalone) < liveStandaloneMin*float64(shape.titles)
}

type liveCandidate struct {
	gram    string
	surface string
	// Ряды и стадия — по titled, свидетельства и квалификация — по papers.
	papers   []StudyPaper
	titled   []StudyPaper
	mentions int
}

// Пока заголовочных нет (кандидат семейства без разбора по заголовкам), ряды считаются по всем документам.
func (c liveCandidate) dated() []StudyPaper {
	if len(c.titled) > 0 {
		return c.titled
	}
	return c.papers
}

type liveTally struct{ early, recent, total int }

// earlyDiv: окно радара требует почти отсутствия (10), корпус запроса — роста (2), он тематичен с начала.
func liveDiscover(papers []StudyPaper, metadata map[string]LivePaperMetadata,
	periods []LivePeriod, curated []liveFamily, recentMin, earlyDiv int,
) []liveCandidate {
	return liveDiscoverWithCoverage(papers, metadata, periods, curated, recentMin, earlyDiv, true)
}

func liveDiscoverWithCoverage(papers []StudyPaper, metadata map[string]LivePaperMetadata,
	periods []LivePeriod, curated []liveFamily, recentMin, earlyDiv int, temporalComplete bool,
) []liveCandidate {
	complete := 0
	for _, period := range periods {
		if period.Complete {
			complete++
		}
	}
	if temporalComplete && complete < 12 || len(papers) == 0 {
		return nil
	}
	generic := liveGenericTokens(papers)
	known := map[string]bool{}
	for _, family := range curated {
		for _, phrase := range []string{family.name, family.query.HN} {
			stems := []string{}
			for _, token := range liveTokens(phrase) {
				stems = append(stems, liveStem(token))
			}
			known[strings.Join(stems, " ")] = true
		}
	}
	// Половины окна не равны по объёму: ранний счёт приводится к объёму свежей.
	early, recent := liveHalfSizes(papers, periods)
	scale := 1.0
	if early > 0 {
		scale = float64(recent) / float64(early)
	}
	candidates := []liveCandidate{}
	for gram, row := range liveTallies(papers, periods, len(periods), generic) {
		adjusted := int(float64(row.early)*scale + 0.5)
		// Счёт по заголовкам решает только, что фраза новая; хватает ли ей документов до допуска, решается в
		// liveDedupe по членам с аннотациями: у фразы с двумя заголовками часто есть ещё пять аннотаций.
		eligible := row.recent >= recentMin && adjusted <= max(liveDiscoverEarly, row.recent/earlyDiv)
		mentions := row.recent
		if !temporalComplete {
			// Наблюдения предлагают сущность для проверки; рост выборки её не подтверждает.
			eligible, mentions = row.total >= recentMin, row.total
		}
		if !known[gram] && eligible {
			candidates = append(candidates, liveCandidate{gram: gram, mentions: mentions})
		}
	}
	slices.SortFunc(candidates, func(a, b liveCandidate) int {
		return cmp.Or(cmp.Compare(b.mentions, a.mentions), cmp.Compare(strings.Count(a.gram, " "), strings.Count(b.gram, " ")),
			cmp.Compare(a.gram, b.gram))
	})
	return liveDedupe(candidates, papers, metadata, generic)
}

func liveTallies(papers []StudyPaper, periods []LivePeriod, complete int, generic map[string]bool) map[string]*liveTally {
	counts := map[string]*liveTally{}
	for _, paper := range papers {
		idx := livePeriodIndex(periods, paper.Date)
		if idx < 0 || idx >= complete {
			continue
		}
		early := idx >= 0 && idx < 6
		recent := idx >= complete-6 && idx < complete
		for _, gram := range liveGrams(liveTokens(paper.Title), generic) {
			row := counts[gram.key]
			if row == nil {
				row = &liveTally{}
				counts[gram.key] = row
			}
			row.total++
			if early {
				row.early++
			} else if recent {
				row.recent++
			}
		}
	}
	return counts
}

func liveGenericTokens(papers []StudyPaper) map[string]bool {
	frequency := map[string]int{}
	for _, paper := range papers {
		for _, token := range slices.Compact(slices.Sorted(slices.Values(liveTokens(paper.Title)))) {
			frequency[token]++
		}
	}
	generic := map[string]bool{}
	for token, n := range frequency {
		if n > max(liveGenericMin, int(liveGenericShare*float64(len(papers)))) {
			generic[token] = true
		}
	}
	return generic
}

func liveDedupe(candidates []liveCandidate, papers []StudyPaper, metadata map[string]LivePaperMetadata,
	generic map[string]bool) []liveCandidate {
	members, titled, surfaces, shapes := liveMembers(candidates, papers, metadata, generic)
	kept := []liveCandidate{}
	for _, candidate := range candidates {
		if len(kept) >= liveDiscoverMax {
			break
		}
		own := members[candidate.gram]
		// Меньше документов, чем требует допуск, — обречённый кандидат занимал бы место в пуле.
		if len(own) < liveDiscoverMinDocuments {
			continue
		}
		// Обрывок снимается до пересечения, иначе он занимает место своей группы и та уходит как его дубль.
		if liveFragmentPhrase(shapes[candidate.gram]) {
			continue
		}
		ids := map[string]bool{}
		for _, paper := range own {
			ids[paper.ID] = true
		}
		overlapping := slices.ContainsFunc(kept, func(other liveCandidate) bool {
			shared := 0
			for _, paper := range other.papers {
				if ids[paper.ID] {
					shared++
				}
			}
			return float64(shared) >= liveOverlapSkip*float64(len(own))
		})
		if !overlapping {
			candidate.papers = own
			candidate.titled = titled[candidate.gram]
			candidate.surface = liveCommonSurface(surfaces[candidate.gram], candidate.gram)
			kept = append(kept, candidate)
		}
	}
	return kept
}

// Документы засчитываются и по аннотации: по одним заголовкам видна малая часть свидетельств.
func liveMembers(candidates []liveCandidate, papers []StudyPaper, metadata map[string]LivePaperMetadata,
	generic map[string]bool) (map[string][]StudyPaper, map[string][]StudyPaper, map[string]map[string]int, map[string]liveShape) {
	wanted := map[string]bool{}
	for _, candidate := range candidates {
		wanted[candidate.gram] = true
	}
	members := map[string][]StudyPaper{}
	titled := map[string][]StudyPaper{}
	surfaces := map[string]map[string]int{}
	shapes := map[string]liveShape{}
	counted := map[string]bool{}
	for _, paper := range papers {
		carried := map[string]bool{}
		for _, gram := range liveGramsOf(liveTokens(paper.Title), liveRawTokens(paper.Title), generic) {
			if !wanted[gram.key] {
				continue
			}
			if !carried[gram.key] {
				carried[gram.key] = true
				members[gram.key] = append(members[gram.key], paper)
				titled[gram.key] = append(titled[gram.key], paper)
			}
			if surfaces[gram.key] == nil {
				surfaces[gram.key] = map[string]int{}
			}
			surfaces[gram.key][gram.surface]++
			liveCountTitle(shapes, counted, gram, paper.Title)
		}
		// Попутное упоминание в аннотации выглядело бы месяцем ровного потока: аннотация добавляет только документ.
		for _, gram := range liveGrams(liveTokens(metadata[paper.ID].Abstract), generic) {
			if !wanted[gram.key] || carried[gram.key] {
				continue
			}
			carried[gram.key] = true
			members[gram.key] = append(members[gram.key], paper)
		}
	}
	return members, titled, surfaces, shapes
}

// Один текст приходит из разных каналов, и без этого повтор выглядел бы самостоятельностью фразы.
func liveCountTitle(shapes map[string]liveShape, counted map[string]bool, gram liveGram, title string) {
	seen := gram.key + "\n" + title
	if counted[seen] {
		return
	}
	counted[seen] = true
	shape := shapes[gram.key]
	shape.titles++
	if gram.standalone {
		shape.standalone++
	}
	shapes[gram.key] = shape
}

func liveCommonSurface(forms map[string]int, fallback string) string {
	best, count := fallback, 0
	for form, n := range forms {
		if n > count || (n == count && form < best) {
			best, count = form, n
		}
	}
	return best
}

func liveCandidateFamily(candidate liveCandidate) liveFamily {
	words := strings.Fields(cmp.Or(candidate.surface, candidate.gram))
	name := make([]string, 0, len(words))
	for _, word := range words {
		if liveStopword(strings.ToLower(word)) || word != strings.ToLower(word) {
			name = append(name, word)
			continue
		}
		// По символу, а не по байту: имя от модели бывает не ASCII, и word[:1] резал бы букву пополам.
		first, size := utf8.DecodeRuneInString(word)
		name = append(name, string(unicode.ToUpper(first))+word[size:])
	}
	phrase := strings.ToLower(strings.Join(words, " "))
	return liveFamily{id: "ngram:" + strings.ReplaceAll(candidate.gram, " ", "-"), name: strings.Join(name, " "),
		query: LiveQuery{Arxiv: `ti:"` + phrase + `"`, GitHub: `"` + phrase + `"`, HN: phrase}}
}

func liveHalfSizes(papers []StudyPaper, periods []LivePeriod) (early, recent int) {
	for _, paper := range papers {
		switch idx := livePeriodIndex(periods, paper.Date); {
		case idx < 0:
		case idx < 6:
			early++
		case idx >= len(periods)-6:
			recent++
		}
	}
	return early, recent
}
