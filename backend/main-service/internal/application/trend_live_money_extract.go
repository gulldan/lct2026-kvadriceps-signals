package application

import (
	"cmp"
	"encoding/json/v2"
	"html"
	"math"
	"regexp"
	"slices"
	"strconv"
	"strings"
	"unicode"
	"unicode/utf8"
)

// Ленты сделок пишут на шести языках: общий словарь событий радара знает только английский и русский.

const (
	LiveMoneyRound       = "round"
	LiveMoneyGrant       = "grant"
	LiveMoneyAccelerator = "accelerator"
	// LiveMoneyDevelopment — обязательство института развития или фонда: деньги в страну, а не раунд стартапа.
	LiveMoneyDevelopment = "development"
	// LiveMoneyResearch — научный грант организации, а не деньги компании.
	LiveMoneyResearch = "research"

	liveMoneyStagePreSeed = "pre_seed"
	liveMoneyStageSeed    = "seed"
	liveMoneyStageA       = "series_a"
	liveMoneyStageUnknown = "unknown"

	liveMoneyBasisText = "text"
	liveMoneyBasisName = "name"

	// Раунд больше $150 млн не бывает ранним, даже если заметка не называет серию.
	liveMoneyEarlyCeiling = 150e6
	// Меньше $10 тыс. — обрезанная сумма («$1» от «$1 млрд») или личные вложения, а не раунд.
	liveMoneyEarlyFloor = 1e4
	liveMoneyPerRecord  = 15
)

type liveRubric struct {
	ID       string   `json:"id"`
	Label    string   `json:"label"`
	Patterns []string `json:"patterns"`
}

// liveRubrics разбирает сгенерированный каталог; его целостность держит тест генератора и тест слоя.
func liveRubrics() []liveRubric {
	var rubrics []liveRubric
	if json.Unmarshal([]byte(liveRubricCatalog), &rubrics) != nil {
		return []liveRubric{}
	}
	return rubrics
}

type liveMoneyPatterns struct {
	deal, reject, fund, footer, accelerator, joined, preSeed, seed, seriesA, amount, actor, object, investors,
	headed *regexp.Regexp
}

// Границы слов `\b` в Go только ASCII: после кириллицы и букв с диакритикой слово закрывает `(?:\P{L}|$)`.
func liveMoneyRules() liveMoneyPatterns {
	return liveMoneyPatterns{
		// Глагол сделки рядом с суммой или стадией: одно «raises» чаще значит «поднимает вопрос».
		deal: regexp.MustCompile(`(?i)\b(?:raise[sd]?|raising|secure[sd]?|lands?|landed|bags?|nabs?|closes?|closed|` +
			`completes?|completed)\b.{0,60}?(?:[$€£]\s?\d|\d+(?:[.,]\d+)?\s?(?:k|m|mn|million|billion)\b|\bround\b|` +
			`\bseed\b|\bseries a\b|\bfunding\b|\binvestment\b)|\b(?:led|co-led|leads)\b.{0,40}?\b(?:round|seed|` +
			`series a|investment)\b|\b(?:pre-seed|seed|series a|angel) (?:round|funding|investment)\b|` +
			`yatırım aldı|tohum turu|투자\s?유치|시드\s?투자|pendanaan|` +
			// Русский: «привлёк» без суммы или слова «инвестиции» рядом — это клиенты и внимание, а «получил» без них — выручка.
			`(?:привл[её]к(?:ла|ли|ло)?|закрыл[аи]?|вложил[аи]?(?:сь)?|(?:про)?инвестировал[аи]?)\s[^.;]{0,60}?` +
			`(?:инвестиц|раунд|финансиров|млн|млрд|тыс|миллион|миллиард|\$|€|₽|руб)|` +
			`получил[аи]?\s(?:\p{L}+\s){0,3}?(?:инвестиц|финансиров|раунд)|посевн\p{L}* (?:раунд|инвестиц|финансиров)|` +
			`предпосевн|бизнес-ангел|раунд\p{L}* серии [aа]|` +
			// Французский и португальский.
			`lève|a levé|levée de (?:fonds|\d)|boucle (?:un|une|son|sa) |tour de table|` +
			`annonce (?:un|une) (?:levée|financement|tour de table|série a|seed)|` +
			`capt(?:a|ou|aram)\s(?:R\$|US\$|\$|\d|aporte|investimento|rodada)|captação de (?:R\$|US\$|\d)|` +
			`receb(?:e|eu|em) (?:um )?aporte|aporte de (?:R\$|US\$|\d)|rodada (?:seed|pré-seed|semente|pré-semente|série a|anjo|` +
			`de investimento)|levant(?:a|ou) (?:R\$|US\$|\d)`),
		reject: regexp.MustCompile(`(?i)\bseries [b-h]\b|\bgrowth (?:round|equity)\b|\bipo\b|\bacquir|\bacquisition\b|` +
			`\bmerger\b|\bdebt\b|\bloans?\b|securiti[sz]ation|credit (?:line|facility)|\bbonds?\b|` +
			`fail(?:ed|ing|s)? to (?:secure|raise|close)|ceased operations|shut(?:s|ting)? down|` +
			`^(?:if|when|how|why|what|should)\b|\bto date\b|\bin total\b|\bso far\b|\btotal of\b|` +
			`\b(?:plans?|aims?|looks?|looking|seeks?|seeking|hopes?) to raise\b|\bwill raise\b|` +
			`\b\d+\s+(?:start-?ups|companies|deals|rounds)\b|\b(?:fell|rose|dropped|declined|grew)\b|` +
			`\b(?:start-?ups|companies|ventures|founders) (?:raising|that raised)\b|\bamount raised\b|\d\s?%|` +
			`시리즈\s?[b-h]|인수|satın al|` +
			// Русский: намерение, покупка, долг, статистика и пересказ прошлых лет.
			`привлечь|переговор|готовится|купил[аи]?|покуп|поглотил|сери[яи] (?:[b-h]|[бвгд])(?:\P{L}|$)|приобр[её]л|приобрет|` +
			`слияни|выкуп|акци[ийя]|` +
			`кредит|за[её]м\P{L}|займ|облигаци|долг(?:\P{L}|$)|поручительств|оборотн|совокупн|суммарн|в сумме|` +
			`в общей сложности|общий объ[её]м|за вс[её] время|годом ранее|в \d+(?:,\d+)? раз|` +
			`\d+\s+(?:сдел\p{L}+|стартап\p{L}+|компани\p{L}+|раунд\p{L}+|инвестор\p{L}+)|вырос|упал\P{L}|снизил|сократил|` +
			`выручк|прибыл|(?:19\d\d|20[01]\d|202[0-5])(?:-м| году| года)|` +
			// Французский и португальский.
			`série [b-h]|refinanc|bancaire|dette|prêt|obligataire|rachat|rachète|acquiert|cette semaine|au total|` +
			`depuis sa création|cumul|(?:pretende|planeja|busca|quer|espera|deve|vai) (?:captar|levantar)|fidc|debênture|` +
			`dívida|empréstimo|linha de crédito|aquisição|adquir|\bcompra d[aeo]\b|no total|ao todo|acumulad`),
		fund: regexp.MustCompile(`(?i)\b(?:closes?|closed|launch\w*|unveil\w*|raise[sd]?)\b[^.]{0,50}?\bfund\b|` +
			`\bfund (?:i|ii|iii|iv|v)\b|펀드 결성|fon kur|` +
			// «Привлёк $111 млн в первый фонд» — сбор фонда, но «привлёк 100 млн от фонда» — сделка стартапа.
			`(?:привл[её]к|закрыл|запустил|сформировал|собрал|создал)\p{L}*\s[^.;]{0,40}?\s(?:в|новый|первый|второй|третий|` +
			`венчурный|свой)\s(?:\p{L}+\s){0,2}?фонд(?:а|ы|ов|е|у|ом)?(?:\P{L}|$)|фонд\p{L}* (?:объ[её]мом|размером|на \d)|` +
			`(?:lance|lève|clôture|boucle) (?:un|son|le|sa) (?:\p{L}+ ){0,2}?fonds\b|` +
			`\bfundo\b[^.;]{0,40}?\b(?:anuncia|lança|capta|levanta)`),
		footer: regexp.MustCompile(`(?i)^the post |appeared first on|^read more|^подробнее|\bin this issue\b|` +
			`est apparu en premier sur|aparece(?:u)? primeiro em`),
		accelerator: regexp.MustCompile(`(?i)\by combinator\b|\btechstars\b|\b500 global\b|\bantler\b|\bseedcamp\b|` +
			`\bentrepreneur first\b|\baccelerator (?:program|programme|cohort|batch)\b|\bdemo day\b|акселератор|` +
			`hızlandırma programı|액셀러레이팅`),
		joined: regexp.MustCompile(`(?i)\b(?:joins?|joined|selected|accepted|graduat\w*|admitted|cohort|batch)\b|` +
			`вош[её]л|отобран`),
		preSeed: regexp.MustCompile(`(?i)pre-seed|pre seed|프리\s?시드|ön tohum|предпосевн|пре-сид|pré-seed|pré-amorçage|pré-semente`),
		seed:    regexp.MustCompile(`(?i)\bseed\b|\bangel\b|amorçage|tohum|시드|посевн|бизнес-ангел|semente|\banjo\b`),
		seriesA: regexp.MustCompile(`(?i)\bseries a\b|série a|seri a|시리즈\s?a|프리\s?a|раунд\p{L}*(?: серии)? [aа](?:\P{L}|$)|` +
			`сери[яи] [aа](?:\P{L}|$)`),
		// Русская сумма стоит первой: при «$7 млн» общий вариант взял бы одни «$7».
		amount: regexp.MustCompile(`(?i)(?:R\$|US\$|[$€£₽])?\s?\d+(?:[.,]\d+)*\s?(?:млн|млрд|трлн|тыс\.?|миллион\p{L}*|` +
			`миллиард\p{L}*|тысяч\p{L}*)\s?(?:руб\p{L}*|долл\p{L}*|евро|\$|₽|€)?|` +
			`(?:R\$|US\$|[$€£])\s?\d+(?:[.,]\d+)*\s?(?:million|billion|bn|mn|m|b|k|milh(?:ão|ões)|bilh(?:ão|ões)|mil)?\b|` +
			`\d+(?:[.,]\d+)?\s?(?:million|billion|mn|m|bn|k)\s?(?:dollars|euros|usd|eur)\b|` +
			`\d+(?:[.,]\d+)?\s?(?:m€|k€|millions? d['’]euros|millions? de dollars)|` +
			`\d+(?:[.,]\d+)?\s?(?:milyon|milyar)\s?(?:dolar|euro|tl)|\d+(?:[.,]\d+)?\s?(?:억|조)\s?원`),
		actor: regexp.MustCompile(`^(.{2,120}?)\s+(?i:raises|raised|secures|secured|closes|closed|lands|landed|nabs|` +
			`bags|completes|lève|a levé|annonce|boucle|obtient|décroche|mendapat|meraih|привл[её]к(?:ла|ли|ло)?|закрыл[аи]?|` +
			`вложил[аи]?(?:сь)?|(?:про)?инвестировал[аи]?|получил[аи]?|capta|captou|recebe|recebeu|anuncia|anunciou|levanta|` +
			`levantou)(?:\P{L}|$)`),
		// «Фонд вложил 100 млн в стартап X» — компания стоит после глагола, а перед ним инвестор.
		object: regexp.MustCompile(`(?:вложил|инвестировал)\p{L}*\s[^.;]{0,50}?\sв\s(?:(?:стартап|компанию|проект|сервис|` +
			`разработчика|производителя|платформу|бизнес)\s)?(«[^»]{2,40}»|\p{Lu}[^\s,.;:—(]{1,30}(?:\s\p{Lu}[^\s,.;:—(]{1,30}){0,3})`),
		investors: regexp.MustCompile(`(?i)(?:\b(?:led by|backed by|men[ée]e? par|conduit par|liderliğinde|` +
			`liderad[ao] (?:por|pel[ao])|` +
			`com participação d[aeo]|liderança d[aeo])\s|auprès d['’]|при участии\s|с участием\s|под руководством\s|во главе с\s|` +
			`лид-инвестор\p{L}*(?: выступил\p{L}*| стал\p{L}*)?\s|(?:инвестици\p{L}*|средства|финансирование|раунд\p{L}*|млн|млрд|` +
			`тыс\.?|руб\p{L}*|долл\p{L}*|евро) от(?: фонд\p{L}*)?\s)\s*([^.;:()]{3,90})`),
		headed: regexp.MustCompile(`(?i)yatırım aldı|투자\s?유치|시드\s?투자`),
	}
}

// liveMoneyFromRecord разбирает предложения, а не запись целиком: недельный обзор называет десяток сделок,
// а заметка о сделке пересказывает её в заголовке, в тексте и в подвале ленты.
func liveMoneyFromRecord(rules liveMoneyPatterns, rubrics []liveRubric, source liveMoneyFeed,
	title, text, url, date string) []LiveMoneyEvent {
	events := []LiveMoneyEvent{}
	companies, amounts := map[string]bool{}, map[string]bool{}
	title, text = html.UnescapeString(title), html.UnescapeString(text)
	for i, sentence := range append([]string{title}, liveEventSentence.Split(text, -1)...) {
		sentence = strings.Join(strings.Fields(sentence), " ")
		event, ok := liveMoneyFromSentence(rules, sentence)
		if !ok || len(events) >= liveMoneyPerRecord {
			continue
		}
		company, amount := strings.ToLower(event.Company), ""
		if usd := liveMoneyAmountUSD(event.Amount); usd > 0 {
			amount = strconv.FormatFloat(math.Round(usd/1e4), 'f', 0, 64)
		}
		// Та же компания или та же сумма в одной заметке — пересказ уже найденной сделки.
		if companies[company] && company != "" || amounts[amount] && amount != "" ||
			company == "" && amount == "" && len(events) > 0 {
			continue
		}
		companies[company], amounts[amount] = true, true
		event.Region, event.Date, event.Title, event.URL = source.region, date, title, url
		event.Source, event.License, event.Quote, event.Text = source.name, source.license, sentence, sentence
		// Рубрика берётся из самой сделки, а заголовок и начало текста — запасной контекст заметки.
		scope := sentence
		if i == 0 || len(events) == 0 {
			scope += " " + title + " " + firstRunes(text, 400)
		}
		if event.Rubric = liveRubricOf(rubrics, scope); event.Rubric != "" {
			event.RubricBasis = liveMoneyBasisText
		}
		event.ID = liveMoneyID(source.name, url, sentence)
		events = append(events, event)
	}
	return events
}

func liveMoneyFromSentence(rules liveMoneyPatterns, sentence string) (LiveMoneyEvent, bool) {
	size := len([]rune(sentence))
	if size < 20 || size > liveEventQuoteMax || rules.footer.MatchString(sentence) || !rules.deal.MatchString(sentence) ||
		rules.reject.MatchString(sentence) || rules.fund.MatchString(sentence) {
		return LiveMoneyEvent{}, false
	}
	amount := strings.TrimSpace(rules.amount.FindString(sentence))
	if usd := liveMoneyAmountUSD(amount); usd > liveMoneyEarlyCeiling || amount != "" && usd < liveMoneyEarlyFloor {
		return LiveMoneyEvent{}, false
	}
	event := LiveMoneyEvent{Kind: LiveMoneyRound, Stage: liveMoneyStage(rules, sentence), Amount: amount,
		Company: liveMoneyCompanyName(liveMoneyCompany(rules, sentence))}
	if rules.accelerator.MatchString(sentence) && (amount == "" || rules.joined.MatchString(sentence)) {
		event.Kind, event.Stage = LiveMoneyAccelerator, LiveMoneyAccelerator
	}
	// Ни компании, ни стадии — это рассуждение о деньгах; раунд без компании и без суммы нечем показать.
	if event.Stage == liveMoneyStageUnknown && (amount == "" || event.Company == "") ||
		amount == "" && event.Company == "" && event.Kind == LiveMoneyRound {
		return LiveMoneyEvent{}, false
	}
	if match := rules.investors.FindStringSubmatch(sentence); match != nil {
		event.Investors = strings.TrimSpace(match[1])
		// «led by Lowercarbon to develop vaccines» — после цели раунда инвесторов нет.
		for _, stop := range []string{" to ", " pour ", " afin ", " para ", " на ", " для ", ", а "} {
			event.Investors, _, _ = strings.Cut(event.Investors, stop)
		}
	}
	return event, true
}

func liveMoneyStage(rules liveMoneyPatterns, text string) string {
	switch {
	case rules.preSeed.MatchString(text):
		return liveMoneyStagePreSeed
	case rules.seriesA.MatchString(text):
		return liveMoneyStageA
	case rules.seed.MatchString(text):
		return liveMoneyStageSeed
	}
	return liveMoneyStageUnknown
}

// liveMoneyCompany оставляет имя собственное: «Oslo-based Solvera has» — это Solvera, «Norwegian startup» — никто.
func liveMoneyCompany(rules liveMoneyPatterns, sentence string) string {
	subject := ""
	if match := rules.object.FindStringSubmatch(sentence); match != nil {
		return strings.Trim(match[1], "«»")
	}
	if match := rules.actor.FindStringSubmatch(sentence); match != nil {
		subject = match[1]
	} else if head, _, ok := strings.Cut(sentence, ","); ok && len([]rune(head)) <= 60 && rules.headed.MatchString(sentence) {
		// Турецкие и корейские заметки начинают с «Компания, сумма, глагол»; в английской фразе до запятой — инвестор.
		subject = head
	}
	// «Flai, a San Francisco based startup» — имя стоит до приложения, а не после него.
	for _, apposition := range []string{" , a ", ", a ", " , an ", ", an ", ", the "} {
		if head, _, ok := strings.Cut(subject, apposition); ok {
			subject = head
		}
	}
	cyrillic := strings.ContainsFunc(subject, func(r rune) bool { return unicode.Is(unicode.Cyrillic, r) })
	if cyrillic {
		name, head := liveMoneyCyrillicSubject(subject)
		if name != "" {
			return name
		}
		subject = head
	} else if i := strings.LastIndex(subject, ", "); i >= 0 {
		subject = subject[i+2:]
	}
	if strings.ContainsFunc(subject, func(r rune) bool { return unicode.Is(unicode.Hangul, r) }) {
		return strings.TrimSpace(firstRunes(subject, 30))
	}
	name := liveMoneyProperTail(subject)
	// «Еще вложили…»: одно кириллическое слово в начале фразы — скорее наречие, чем имя; имена в новостях латиницей или в «».
	if cyrillic && len(name) == 1 && !strings.ContainsFunc(name[0], func(r rune) bool { return r < unicode.MaxASCII }) {
		return ""
	}
	return strings.Join(name, " ")
}

// liveMoneyCyrillicSubject: русское подлежащее кончается до первой запятой и до «из Казани», имя часто в «кавычках».
func liveMoneyCyrillicSubject(subject string) (name, head string) {
	for _, tail := range []string{",", " из ", " в ", " с ", " от "} {
		subject, _, _ = strings.Cut(subject, tail)
	}
	if _, after, ok := strings.Cut(subject, "«"); ok {
		name, _, _ = strings.Cut(after, "»")
	}
	return name, subject
}

func liveMoneyProperTail(subject string) []string {
	words := strings.Fields(strings.Trim(subject, " «»\"'—-:,"))
	for len(words) > 0 && slices.Contains([]string{"has", "have", "just", "today", "now", "also", "officially"},
		strings.ToLower(words[len(words)-1])) {
		words = words[:len(words)-1]
	}
	start := len(words)
	for start > 0 && liveMoneyProper(words[start-1]) {
		start--
	}
	name := words[start:]
	for len(name) > 1 && (strings.HasSuffix(name[0], "’s") || strings.HasSuffix(name[0], "'s")) {
		name = name[1:]
	}
	return name
}

func liveMoneyProper(word string) bool {
	first, lower := []rune(word)[0], strings.ToLower(word)
	// «Американская компания X» — прилагательные на -ский/-ская в начале фразы тоже с заглавной.
	return (unicode.IsUpper(first) || unicode.IsDigit(first)) && !strings.Contains(lower, "-based") &&
		!strings.Contains(lower, "стартап") && !strings.HasSuffix(lower, "ский") && !strings.HasSuffix(lower, "ская") &&
		!slices.Contains(strings.Fields("they it we the this our its their startup fintech компания проект сервис "+
			"разработчик фонд команда платформа"), lower)
}

// liveMoneyAmountUSD — грубая оценка только для отсечения поздних раундов: рубль, реал и вона по грубому курсу,
// остальные валюты один к одному. Точность не нужна, оценка служит только потолку раннего раунда.
func liveMoneyAmountUSD(amount string) float64 {
	value, currency := liveMoneyAmount(amount)
	return value / cmp.Or(map[string]float64{"RUB": 80, "BRL": 5.5, "KRW": 1400}[currency], 1)
}

// liveMoneyAmount разбирает сумму полным числом в исходной валюте: «$13.9mn» — 13,9 млн USD, «SEK 500K» —
// 500 тыс. SEK. Пересчёт по курсу исказил бы сумму на экране, поэтому валюта остаётся своей.
func liveMoneyAmount(amount string) (float64, string) {
	lower := strings.ToLower(amount)
	digits := strings.TrimFunc(strings.Map(func(r rune) rune {
		if unicode.IsDigit(r) || r == '.' || r == ',' {
			return r
		}
		return ' '
	}, lower), unicode.IsSpace)
	digits, _, _ = strings.Cut(digits, " ")
	if whole, frac, ok := strings.Cut(digits, ","); ok && len(frac) == 3 && !strings.Contains(frac, ",") {
		digits = whole + frac
	}
	value, err := strconv.ParseFloat(strings.ReplaceAll(strings.ReplaceAll(digits, ",", "."), "..", "."), 64)
	if err != nil {
		return 0, ""
	}
	currency := liveMoneyCurrency(lower)
	// Порядок важен: «mil» входит в «million» и «milhões», поэтому тысячи проверяются после миллионов.
	scale := []struct {
		unit   string
		factor float64
	}{{"трлн", 1e12}, {"billion", 1e9}, {"milyar", 1e9}, {"млрд", 1e9}, {"миллиард", 1e9}, {"bilh", 1e9}, {"bn", 1e9},
		{"million", 1e6}, {"milyon", 1e6}, {"млн", 1e6}, {"миллион", 1e6}, {"milh", 1e6}, {"mn", 1e6}, {"m€", 1e6},
		{"тыс", 1e3}, {"k€", 1e3}, {"mil", 1e3}, {"조", 1e12}, {"억", 1e8}}
	for _, s := range scale {
		if strings.Contains(lower, s.unit) {
			return value * s.factor, currency
		}
	}
	switch {
	case strings.HasSuffix(lower, "b"):
		return value * 1e9, currency
	case strings.HasSuffix(lower, "m"):
		return value * 1e6, currency
	case strings.HasSuffix(lower, "k"):
		return value * 1e3, currency
	}
	return value, currency
}

// liveMoneyCurrency — код ISO 4217 по знаку или слову суммы; «r$» и «nt$» стоят раньше голого «$».
// «10 млн» без валюты остаётся без кода: гадать, рубли это или доллары, нельзя.
func liveMoneyCurrency(lower string) string {
	for _, currency := range []struct{ code, marks string }{{"BRL", "r$"}, {"TWD", "nt$"},
		{"USD", "$ долл dollar dolar usd"}, {"EUR", "€ eur евро"}, {"GBP", "£"}, {"RUB", "₽ руб"}, {"SEK", "sek"},
		{"KRW", "원"}} {
		for mark := range strings.FieldsSeq(currency.marks) {
			if strings.Contains(lower, mark) {
				return currency.code
			}
		}
	}
	if strings.HasSuffix(lower, " tl") {
		return "TRY"
	}
	return ""
}

// Слова, из которых имя компании не складывается: регионы, страны, отрасли и служебные слова лент. Разбор текста
// брал «Africa» из «Startups in Africa raised…» и «Business» из рубрики ленты.
const liveMoneyGenericWords = " africa african europe european asia asian america americas american latin latam " +
	"mena middle east eastern west western north northern south southern central southeast nordic nordics gulf " +
	"global world international nigeria nigerian kenya kenyan egypt egyptian ghana ghanaian morocco moroccan rwanda " +
	"ethiopia tanzania uganda senegal tunisia brazil brazilian mexico mexican colombia colombian chile chilean " +
	"argentina peru india indian indonesia indonesian korea korean china chinese japan singapore vietnam turkey " +
	"turkish saudi arabia uae israel israeli france french germany german spain spanish uk british us usa " +
	"czech poland polish sweden swedish norway norwegian finland finnish denmark danish estonia estonian ukraine " +
	"ukrainian netherlands dutch swiss austrian belgian irish italian portuguese greek canadian australian " +
	"business businesses startup startups company companies firm firms fintech healthtech edtech agritech agtech " +
	"cleantech climatetech proptech insurtech deeptech tech technology technologies ai saas b2b climate energy " +
	"mobility logistics health venture ventures capital fund funds funding investment investments investor " +
	"investors deal deals round rounds seed series week weekly month monthly today news report roundup digest " +
	"update sector industry market markets the a an and of in this its new top other local " +
	"россия российский казахстан бизнес компания стартап стартапы фонд инвесторы "

// Глаголы заголовка: имя с ними — заголовок в Title Case («EBRD Backs Nigerian Grid-Tech Startup»), а не компания.
const liveMoneyHeadlineVerbs = " backs backed invests invested announces announced launches unveils partners joins " +
	"gets wins receives leads led says plans expands acquires eyes targets funds funded "

// liveMoneyNameWords: ленты называют компанию двумя-тремя словами, а шесть и больше — уже пересказ заметки.
const liveMoneyNameWords = 5

// liveMoneyCompanyName возвращает пустую строку, когда разбор принял за компанию регион, отрасль или заголовок:
// пустая компания честнее неверной, а сделку держат сумма и ссылка.
var (
	liveMoneyInvestorJoin = regexp.MustCompile(`(?i)[,\s]*\bwith (?:the )?participation (?:from|of)\b[\s:]*`)
	liveMoneyInvestorTail = regexp.MustCompile(`(?i)\s+(?:and|with|plus)\s+(?:a|an|the)?\s*[$€£¥₹\d].*$`)
)

func liveMoneyCompanyName(name string) string {
	name = strings.Trim(strings.Join(strings.Fields(name), " "), " «»\"'“”—-:,.…")
	words := strings.Fields(strings.ToLower(name))
	if len(words) == 0 || len(words) > liveMoneyNameWords || !strings.ContainsFunc(name, unicode.IsLetter) {
		return ""
	}
	if slices.ContainsFunc(words, func(word string) bool { return strings.Contains(liveMoneyHeadlineVerbs, " "+word+" ") }) {
		return ""
	}
	// Лента пишет страну или отрасль перед именем: «Healthtech Skedia», «Czech FLÆ Robotics» — компания дальше.
	fields := strings.Fields(name)
	for len(fields) > 1 && strings.Contains(liveMoneyGenericWords, " "+strings.ToLower(fields[0])+" ") {
		fields, words = fields[1:], words[1:]
	}
	name = strings.Join(fields, " ")
	// Притяжательное «Nigeria’s» — та же страна: без суффикса оно ищется в словаре.
	if slices.ContainsFunc(words, func(word string) bool {
		word = strings.TrimSuffix(strings.TrimSuffix(strings.Trim(word, ",.:;"), "’s"), "'s")
		return !strings.Contains(liveMoneyGenericWords, " "+word+" ")
	}) {
		return name
	}
	return ""
}

// liveMoneyInvestorNames оставляет перечень имён: «existing investors» и «a group of angel investors from Lagos»
// никого не называют. Имя начинается с заглавной или с буквы без регистра, как в корейском и китайском.
func liveMoneyInvestorNames(investors string) string {
	// Хвост фразы о сумме («Jump Capital and an $8») и связка «with participation from» — не имена.
	investors = liveMoneyInvestorJoin.ReplaceAllString(investors, ", ")
	investors = strings.Trim(liveMoneyInvestorTail.ReplaceAllString(investors, ""), " ,;…")
	named, plain := 0, 0
	for word := range strings.FieldsSeq(investors) {
		first, _ := utf8.DecodeRuneInString(strings.TrimLeft(word, "«\"'“("))
		switch {
		case unicode.IsDigit(first) || unicode.IsLetter(first) && !unicode.IsLower(first):
			named++
		case utf8.RuneCountInString(word) > 3:
			plain++
		}
	}
	if named == 0 || plain > named+1 {
		return ""
	}
	return investors
}

// liveMoneyTidy чистит события при сборке и при чтении сохранённой доски. Имена и инвесторов правит только у лент:
// реестр и набор дают их отдельным полем, и длинное «FRAUNHOFER GESELLSCHAFT ZUR …» там — настоящее имя.
func liveMoneyTidy(events []LiveMoneyEvent) []LiveMoneyEvent {
	feeds := map[string]bool{}
	for _, feed := range liveMoneyFeeds() {
		feeds[feed.name] = true
	}
	out := events[:0]
	for _, event := range events {
		event.AmountValue, event.Currency = liveMoneyAmount(event.Amount)
		if feeds[event.Source] {
			if event.Company = liveMoneyCompanyName(event.Company); strings.EqualFold(event.Company, event.Title) {
				event.Company = ""
			}
			event.Investors = liveMoneyInvestorNames(event.Investors)
			// Без компании сделку держат только сумма и ссылка на первоисточник.
			if event.Company == "" && (event.AmountValue <= 0 || event.URL == "") {
				continue
			}
		}
		out = append(out, event)
	}
	return out
}

// liveRubricOf повторяет SQL research_topic и researchDomain фронта: одна рубрика, первая по приоритету.
func liveRubricOf(rubrics []liveRubric, text string) string {
	normalized := " " + strings.ToLower(text) + " "
	for _, rubric := range rubrics {
		for _, pattern := range rubric.Patterns {
			if liveLikeMatch(normalized, pattern) {
				return rubric.ID
			}
		}
	}
	return ""
}

// liveLikeMatch — LIKE '%pattern%', где % внутри шаблона означает любой фрагмент.
func liveLikeMatch(text, pattern string) bool {
	for part := range strings.SplitSeq(pattern, "%") {
		i := strings.Index(text, part)
		if i < 0 {
			return false
		}
		text = text[i+len(part):]
	}
	return true
}

func firstRunes(text string, limit int) string {
	runes := []rune(text)
	return string(runes[:min(len(runes), limit)])
}

func liveMoneyID(parts ...string) string {
	return "money:" + liveCardTextHash(strings.Join(parts, "\x00"))[:16]
}
