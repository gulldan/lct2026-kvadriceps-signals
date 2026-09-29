package application

import (
	"archive/zip"
	"bytes"
	"context"
	"crypto/rand"
	"encoding/csv"
	"encoding/json/v2"
	"encoding/xml"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"log/slog"
	"path"
	"slices"
	"strconv"
	"strings"
	"time"
	"unicode/utf8"

	"golang.org/x/text/encoding/charmap"

	"github.com/example/main-service/internal/platform/aiclients"
)

// Каждая фраза стоит кредит OpenAlex, а бесплатный лимит адреса — тысяча в сутки на весь радар.
const (
	WeakSignalFileBytes = 2 << 20
	WeakSignalFileRows  = 300
	// WeakSignalFileBudget рассчитан на шестьсот фраз к OpenAlex по четыре одновременно.
	WeakSignalFileBudget = 10 * time.Minute
	// Фраз в строке столько же, сколько у строки обучающей выборки.
	weakSignalFilePhrases    = 3
	weakSignalGatewayPhrases = 3
	weakSignalGatewayKept    = 2
	// Короткое английское название и есть фраза: так записаны зрелые строки обучающей выборки.
	weakSignalNameWords  = 3
	weakSignalPhraseWait = 45 * time.Second
)

// Коды причин, а не фразы: интерфейс переводит их на три языка.
const (
	weakSignalNoName    = "no_name"
	weakSignalNoPhrases = "no_phrases"
	weakSignalNoLatin   = "no_latin"
	weakSignalTimeout   = "timeout"
	weakSignalNoWindow  = "window_unavailable"
	weakSignalNoHistory = "no_history"
	weakSignalNotFound  = "not_in_window"
)

// ErrWeakSignalFileBusy: проверка одна за раз — большой файл занимает шлюз на минуты и тратит долю лимита OpenAlex.
var ErrWeakSignalFileBusy = errors.New("weak signal file check is already running")

type WeakSignalFileRow struct {
	Number int    `json:"number"`
	Name   string `json:"name"`
	Label  *bool  `json:"label"`
	// LabelText — метка, которую разбор не понял: строка оценивается, но в меры не входит.
	LabelText   string             `json:"label_text,omitempty"`
	Phrases     []string           `json:"phrases"`
	PhrasesFrom string             `json:"phrases_from,omitempty"`
	Score       *float64           `json:"score"`
	Note        string             `json:"note,omitempty"`
	Features    []LiveModelFeature `json:"features,omitempty"`
}

// WeakSignalFileMetrics: воздержание — размеченная строка без оценки; это не ошибка модели, а нехватка данных,
// поэтому в P/R/F1 не входит и считается отдельно, вместе с числом сигналов среди таких строк.
type WeakSignalFileMetrics struct {
	Labeled       int     `json:"labeled"`
	Scored        int     `json:"scored"`
	Abstained     int     `json:"abstained"`
	AbstainedWeak int     `json:"abstained_weak"`
	UnknownLabels int     `json:"unknown_labels"`
	TP            int     `json:"tp"`
	FP            int     `json:"fp"`
	FN            int     `json:"fn"`
	TN            int     `json:"tn"`
	Precision     float64 `json:"precision"`
	Recall        float64 `json:"recall"`
	F1            float64 `json:"f1"`
	Accuracy      float64 `json:"accuracy"`
}

type WeakSignalFileReport struct {
	Model     string                 `json:"model"`
	Version   string                 `json:"version"`
	Threshold float64                `json:"threshold"`
	Month     string                 `json:"month"`
	Rows      []WeakSignalFileRow    `json:"rows"`
	Metrics   *WeakSignalFileMetrics `json:"metrics"`
}

func ParseWeakSignalFile(data []byte) ([]WeakSignalFileRow, error) {
	records, err := weakSignalRecords(data)
	if err != nil {
		return nil, err
	}
	rows := []WeakSignalFileRow{}
	named := false
	for i, raw := range records {
		record := make(map[string]string, len(raw))
		for key, value := range raw {
			record[weakSignalKey(key)] = strings.TrimSpace(value)
		}
		name, ok := weakSignalName(record)
		named = named || ok
		labelCell, _ := weakSignalLookup(record, "класс", "label", "слабый сигнал", "is weak signal", "weak signal",
			"class", "метка", "разметка", "target", "ground truth", "является слабым сигналом")
		phrasesCell, _ := weakSignalLookup(record, "phrases", "фразы")
		if name == "" && labelCell == "" && phrasesCell == "" {
			continue
		}
		row := WeakSignalFileRow{Number: i + 1, Name: name, Phrases: weakSignalPhrases(phrasesCell)}
		// Непонятная метка не роняет файл: строка оценивается, но в меры не входит и видна отдельно.
		if label, known := weakSignalLabel(labelCell); known {
			row.Label = label
		} else {
			row.LabelText = labelCell
		}
		if len(row.Phrases) > 0 {
			row.PhrasesFrom = "file"
		}
		rows = append(rows, row)
	}
	switch {
	case !named:
		return nil, errors.New("нет столбца с названием технологии: «Технология», «technology» или «name»")
	case len(rows) == 0:
		return nil, errors.New("в файле нет ни одной строки с технологией")
	case len(rows) > WeakSignalFileRows:
		return nil, fmt.Errorf("строк %d, а проверить за раз можно не больше %d", len(rows), WeakSignalFileRows)
	}
	return rows, nil
}

// weakSignalRecords узнаёт книгу Excel по сигнатуре zip раньше перекодировки: двоичный файл не UTF-8.
func weakSignalRecords(data []byte) ([]map[string]string, error) {
	if bytes.HasPrefix(data, []byte("PK\x03\x04")) {
		return weakSignalXLSX(data)
	}
	data = bytes.TrimPrefix(data, []byte("\xef\xbb\xbf"))
	// Excel под русской Windows сохраняет CSV в cp1251, а не в UTF-8.
	if !utf8.Valid(data) {
		decoded, err := charmap.Windows1251.NewDecoder().Bytes(data)
		if err != nil {
			return nil, fmt.Errorf("файл не в UTF-8 и не в Windows-1251: %w", err)
		}
		data = decoded
	}
	if trimmed := bytes.TrimSpace(data); len(trimmed) > 0 && (trimmed[0] == '[' || trimmed[0] == '{') {
		return weakSignalJSON(trimmed)
	}
	return weakSignalCSV(data)
}

// У организаторов столбец «Технология (слабый сигнал)», у скриптов — technology или name.
func weakSignalName(record map[string]string) (string, bool) {
	return weakSignalLookup(record, "технология", "technology", "name", "название")
}

// weakSignalHeaderRows: над шапкой таблицы организаторов стоит строка-титул, а сколько их будет в закрытом файле, неизвестно.
const weakSignalHeaderRows = 10

const weakSignalXMLBytes = 64 << 20

func weakSignalCSV(data []byte) ([]map[string]string, error) {
	// Excel в русской локали разделяет точкой с запятой, скрипты — запятой. Первая строка может быть
	// титулом без разделителей, поэтому считаются первые строки, а не одна.
	head := data
	for range weakSignalHeaderRows {
		if _, rest, ok := bytes.Cut(head, []byte("\n")); ok {
			head = rest
		}
	}
	head = data[:len(data)-len(head)]
	comma := ','
	for _, candidate := range []rune{';', '\t'} {
		if bytes.Count(head, []byte(string(candidate))) > bytes.Count(head, []byte(string(comma))) {
			comma = candidate
		}
	}
	reader := csv.NewReader(bytes.NewReader(data))
	reader.Comma, reader.FieldsPerRecord, reader.LazyQuotes = comma, -1, true
	lines, err := reader.ReadAll()
	if err != nil {
		return nil, fmt.Errorf("не удалось прочитать CSV: %w", err)
	}
	return weakSignalTable(lines)
}

// weakSignalTable ищет шапку среди первых строк — по столбцу с названием технологии; не нашлась — шапкой
// считается первая строка, и ошибку об отсутствии столбца назовёт разбор.
func weakSignalTable(lines [][]string) ([]map[string]string, error) {
	if len(lines) == 0 {
		return nil, errors.New("файл пуст")
	}
	header := 0
	for i, line := range lines[:min(len(lines), weakSignalHeaderRows)] {
		cells := make(map[string]string, len(line))
		for _, cell := range line {
			cells[weakSignalKey(cell)] = ""
		}
		if _, ok := weakSignalName(cells); ok {
			header = i
			break
		}
	}
	out := make([]map[string]string, 0, len(lines)-header-1)
	for _, line := range lines[header+1:] {
		record := make(map[string]string, len(line))
		for i, cell := range line[:min(len(line), len(lines[header]))] {
			record[lines[header][i]] = cell
		}
		out = append(out, record)
	}
	return out, nil
}

// weakSignalXLSX читает первый лист книги Excel стандартной библиотекой: книга — zip с XML, а из неё
// нужны только тексты ячеек. Файл организаторов приходит как есть, без пересохранения в CSV.
// ponytail: первый лист по имени файла в архиве, а не по порядку книги; хватает, пока лист с данными один.
func weakSignalXLSX(data []byte) ([]map[string]string, error) {
	archive, err := zip.NewReader(bytes.NewReader(data), int64(len(data)))
	if err != nil {
		return nil, fmt.Errorf("не удалось прочитать книгу Excel: %w", err)
	}
	var shared xlsxShared
	if err = xlsxPart(archive, "xl/sharedStrings.xml", &shared); err != nil && !errors.Is(err, fs.ErrNotExist) {
		return nil, err
	}
	sheets := []string{}
	for _, file := range archive.File {
		if path.Dir(file.Name) == "xl/worksheets" && path.Ext(file.Name) == ".xml" {
			sheets = append(sheets, file.Name)
		}
	}
	if len(sheets) == 0 {
		return nil, errors.New("в книге Excel нет листов")
	}
	var sheet xlsxSheet
	if err = xlsxPart(archive, slices.Min(sheets), &sheet); err != nil {
		return nil, err
	}
	lines := make([][]string, 0, len(sheet.Rows))
	for _, row := range sheet.Rows {
		line := []string{}
		for _, cell := range row.Cells {
			column := len(line)
			if cell.Ref != "" {
				column = xlsxColumn(cell.Ref)
			}
			for len(line) <= column {
				line = append(line, "")
			}
			line[column] = cell.text(shared.Items)
		}
		lines = append(lines, line)
	}
	return weakSignalTable(lines)
}

type xlsxText struct {
	Text string   `xml:"t"`
	Runs []string `xml:"r>t"`
}

func (t xlsxText) String() string { return t.Text + strings.Join(t.Runs, "") }

type xlsxShared struct {
	Items []xlsxText `xml:"si"`
}

type xlsxSheet struct {
	Rows []struct {
		Cells []xlsxCell `xml:"c"`
	} `xml:"sheetData>row"`
}

type xlsxCell struct {
	Ref    string   `xml:"r,attr"`
	Type   string   `xml:"t,attr"`
	Value  string   `xml:"v"`
	Inline xlsxText `xml:"is"`
}

func (c xlsxCell) text(shared []xlsxText) string {
	switch c.Type {
	case "s":
		if i, err := strconv.Atoi(c.Value); err == nil && i >= 0 && i < len(shared) {
			return shared[i].String()
		}
		return ""
	case "inlineStr":
		return c.Inline.String()
	}
	return c.Value
}

// xlsxColumn переводит «AB12» в номер столбца с нуля: пустые ячейки Excel не пишет, и позиция важна.
func xlsxColumn(ref string) int {
	column := 0
	for _, r := range ref {
		if r < 'A' || r > 'Z' {
			break
		}
		column = column*26 + int(r-'A'+1)
	}
	return max(column-1, 0)
}

func xlsxPart(archive *zip.Reader, name string, into any) error {
	file, err := archive.Open(name)
	if err != nil {
		return fmt.Errorf("книга Excel: %w", err)
	}
	defer func() { _ = file.Close() }()
	// Сжатый файл ограничен двумя мегабайтами, а распакованный XML — нет: предел не даёт zip-бомбе съесть память.
	if err = xml.NewDecoder(io.LimitReader(file, weakSignalXMLBytes)).Decode(into); err != nil {
		return fmt.Errorf("книга Excel, %s: %w", name, err)
	}
	return nil
}

func weakSignalJSON(data []byte) ([]map[string]string, error) {
	var items []map[string]any
	if err := json.Unmarshal(data, &items); err != nil {
		return nil, fmt.Errorf("JSON должен быть массивом объектов: %w", err)
	}
	out := make([]map[string]string, len(items))
	for i, item := range items {
		out[i] = make(map[string]string, len(item))
		for key, value := range item {
			out[i][key] = weakSignalCell(value)
		}
	}
	return out, nil
}

func weakSignalCell(value any) string {
	switch v := value.(type) {
	case string:
		return v
	case float64:
		return strconv.FormatFloat(v, 'f', -1, 64)
	case bool:
		return strconv.FormatBool(v)
	case []any:
		parts := make([]string, len(v))
		for i, item := range v {
			parts[i] = weakSignalCell(item)
		}
		return strings.Join(parts, ";")
	}
	return ""
}

// weakSignalKey: у организаторов столбец «Технология (слабый сигнал)», у скриптов — is_weak_signal.
func weakSignalKey(key string) string {
	key, _, _ = strings.Cut(strings.ToLower(key), "(")
	key = strings.NewReplacer("ё", "е", "_", " ", "-", " ").Replace(key)
	return strings.Join(strings.Fields(strings.Trim(key, " ?*:.«»\"")), " ")
}

func weakSignalLookup(record map[string]string, keys ...string) (string, bool) {
	for _, key := range keys {
		if value, ok := record[key]; ok {
			return value, true
		}
	}
	return "", false
}

// weakSignalLabel: угасший хайп, провал и зрелая технология для модели одно — «не слабый сигнал».
func weakSignalLabel(cell string) (*bool, bool) {
	key := weakSignalKey(cell)
	if value, err := strconv.ParseBool(key); err == nil {
		return &value, true
	}
	// Выгрузка из pandas пишет метку числом с дробью: «1.0» и «0.0».
	if value, err := strconv.ParseFloat(key, 64); err == nil && (value == 0 || value == 1) {
		return new(value == 1), true
	}
	switch key {
	case "":
		return nil, true
	case "yes", "да", "слабый сигнал", "weak signal", "weak", "signal", "сигнал", "positive":
		return new(true), true
	case "no", "нет", "зрелая", "зрелая технология", "mature", "провалилась", "провал", "failed",
		"шум", "noise", "hype", "хайп", "угасший хайп", "не слабый сигнал", "not weak signal", "не сигнал",
		"negative":
		return new(false), true
	}
	return nil, false
}

func weakSignalPhrases(cell string) []string {
	out := []string{}
	for part := range strings.FieldsFuncSeq(cell, func(r rune) bool { return strings.ContainsRune(",;|\n", r) }) {
		phrase := strings.Join(strings.Fields(strings.ToLower(part)), " ")
		if phrase != "" && !slices.Contains(out, phrase) && len(out) < weakSignalFilePhrases {
			out = append(out, phrase)
		}
	}
	return out
}

// Промпт поиска радара не годится: он раскрывает тему в подобласти, а признаки сняты по названию механизма.
// Примеры в промпте намеренно не из эталона.
const weakSignalPhrasesSystem = "You pick search phrases that find one technology in arXiv paper titles and abstracts. " +
	"The user gives the technology, often in Russian, sometimes with a market framing and English hints in brackets. " +
	"Answer with a JSON array of three lowercase English phrases, most specific first. Each phrase is two words, " +
	"three at most, in singular: a collocation that papers write verbatim for this very technology — its standard " +
	"name, the expansion of its abbreviation, or the noun phrase of its core mechanism. Reuse an English name from " +
	"the brackets when it is such a term. Do not add qualifiers such as \"ai-native\", \"proactive\", \"adaptive\", " +
	"\"scanning\", \"detection\", \"platform\", \"system\", \"agents\" unless they are part of the standard term. " +
	"Never a broader field, an application domain, a product or company name, or marketing wording."

type TechnologyPhraser func(ctx context.Context, name string) ([]string, error)

func NewTechnologyPhraser(gen aiclients.Generator, configured func(context.Context) bool) TechnologyPhraser {
	return func(ctx context.Context, name string) ([]string, error) {
		if !configured(ctx) {
			return nil, errLLMNotConfigured
		}
		out, err := liveGenerateWithin(ctx, gen, weakSignalPhraseWait, aiclients.GenRequest{
			System: weakSignalPhrasesSystem, Query: name, MaxTokens: 80, Temperature: 0})
		if err != nil {
			return nil, fmt.Errorf("technology phrases: %w", err)
		}
		phrases := ParseSearchTerms(out.Text, weakSignalGatewayPhrases)
		if len(phrases) == 0 {
			return nil, fmt.Errorf("unusable technology phrases: %q", strings.TrimSpace(out.Text))
		}
		return phrases, nil
	}
}

func (s *TrendLiveService) SetTechnologyPhraser(phrase TechnologyPhraser) { s.phrase = phrase }

type WeakSignalFileCheck struct {
	ID     string                `json:"id"`
	Status string                `json:"status"`
	Report *WeakSignalFileReport `json:"report,omitempty"`
}

// StartWeakSignalFileCheck отвечает сразу: большой файл проверяется дольше, чем сервер держит HTTP-ответ.
// ponytail: проверка живёт в памяти реплики; в Valkey, когда реплик станет больше.
func (s *TrendLiveService) StartWeakSignalFileCheck(ctx context.Context, rows []WeakSignalFileRow,
) (WeakSignalFileCheck, error) {
	model, err := LoadWeakSignalModel()
	if err != nil {
		return WeakSignalFileCheck{}, err
	}
	s.mu.RLock()
	snapshot := s.snapshot
	s.mu.RUnlock()
	if snapshot == nil || len(snapshot.Periods) == 0 {
		return WeakSignalFileCheck{}, ErrLiveNotReady
	}
	if !s.fileCheck.TryLock() {
		return WeakSignalFileCheck{}, ErrWeakSignalFileBusy
	}
	check := WeakSignalFileCheck{ID: rand.Text(), Status: "running"}
	s.mu.Lock()
	s.fileResult = check
	s.mu.Unlock()
	periods := snapshot.Periods
	// Ушедшая страница не отменяет проверку: результат заберёт следующий опрос.
	ctx, cancel := context.WithTimeout(context.WithoutCancel(ctx), WeakSignalFileBudget)
	go func() {
		defer s.fileCheck.Unlock()
		defer cancel()
		report := s.ScoreWeakSignalRows(ctx, model, periods, rows)
		s.mu.Lock()
		s.fileResult = WeakSignalFileCheck{ID: check.ID, Status: "done", Report: &report}
		s.mu.Unlock()
	}()
	return check, nil
}

func (s *TrendLiveService) WeakSignalFileResult(id string) (WeakSignalFileCheck, bool) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.fileResult, id != "" && s.fileResult.ID == id
}

// ScoreWeakSignalRows собирает признаки тем же кодом, что и для кандидатов радара.
func (s *TrendLiveService) ScoreWeakSignalRows(ctx context.Context, model WeakSignalModel, periods []LivePeriod,
	rows []WeakSignalFileRow,
) WeakSignalFileReport {
	month := periods[len(periods)-1].ID
	s.fillPhrases(ctx, rows, s.phrase)
	weakSignalKeepFound(rows, s.phraseWindows(ctx, weakSignalAllPhrases(rows), periods))
	// Остальные источники — после отбора фраз шлюза: отсеянная фраза не тратит ни кредит OpenAlex, ни вызов модели.
	evidence := s.weakSignalEvidence(ctx, ctx, weakSignalAllPhrases(rows), periods, model.Embedding.Model)
	for i := range rows {
		row := &rows[i]
		if row.Note != "" {
			continue
		}
		phrases := slices.DeleteFunc(slices.Clone(row.Phrases), func(p string) bool { return weakSignalWindowPhrase(p) == "" })
		windowed, found, historied := 0, 0, 0
		for _, phrase := range phrases {
			e := evidence[phrase]
			if e.Window != nil {
				windowed++
				found += min(e.Window.Documents, 1)
			}
			if e.History != nil {
				historied++
			}
		}
		switch {
		case len(phrases) == 0:
			row.Note = weakSignalNoLatin
			continue
		case windowed < len(phrases):
			row.Note = weakSignalNoWindow
			if ctx.Err() != nil {
				row.Note = weakSignalTimeout
			}
			continue
		case historied < len(phrases):
			// Без истории хотя бы одной фразы самая устоявшаяся может оказаться не той: оценка идёт, но с оговоркой.
			row.Note = weakSignalNoHistory
		case found == 0:
			row.Note = weakSignalNotFound
		}
		prediction := model.prediction("", phrases, evidence, month)
		row.Score, row.Features = &prediction.Value, prediction.Features
	}
	return WeakSignalFileReport{Model: model.Model, Version: model.Version, Threshold: model.Threshold, Month: month,
		Rows: rows, Metrics: weakSignalFileMetrics(rows, model.Threshold)}
}

// weakSignalAllPhrases пропускает фразы без латинских слов: окно их не найдёт, и источники на них не тратятся.
func weakSignalAllPhrases(rows []WeakSignalFileRow) []string {
	phrases := []string{}
	for _, row := range rows {
		for _, phrase := range row.Phrases {
			if weakSignalWindowPhrase(phrase) != "" {
				phrases = append(phrases, phrase)
			}
		}
	}
	slices.Sort(phrases)
	return slices.Compact(phrases)
}

// weakSignalKeepFound: фраза без работ в окне признаков не даёт; не нашлась ни одна — остаётся первая, с оговоркой.
func weakSignalKeepFound(rows []WeakSignalFileRow, windows map[string]LivePhraseWindow) {
	for i := range rows {
		row := &rows[i]
		if row.PhrasesFrom != "gateway" {
			continue
		}
		found := slices.DeleteFunc(slices.Clone(row.Phrases), func(phrase string) bool {
			return windows[phrase].Documents == 0
		})
		if len(found) == 0 {
			found = row.Phrases[:1]
		}
		row.Phrases = found[:min(len(found), weakSignalGatewayKept)]
	}
}

// fillPhrases отдаёт подбору половину срока: без времени на окно и историю фразы бесполезны.
func (s *TrendLiveService) fillPhrases(ctx context.Context, rows []WeakSignalFileRow, phraser TechnologyPhraser) {
	pending := []int{}
	for i := range rows {
		row := &rows[i]
		english := !liveNeedsTranslation(row.Name) && !strings.ContainsAny(row.Name, "(),;") &&
			len(strings.Fields(row.Name)) <= weakSignalNameWords
		switch {
		case len(row.Phrases) > 0:
		case row.Name == "":
			row.Note = weakSignalNoName
		case english:
			row.Phrases, row.PhrasesFrom = weakSignalPhrases(row.Name), "name"
		case phraser == nil:
			row.Note = weakSignalNoPhrases
		default:
			pending = append(pending, i)
		}
	}
	if deadline, ok := ctx.Deadline(); ok {
		var cancel context.CancelFunc
		ctx, cancel = context.WithDeadline(ctx, time.Now().Add(time.Until(deadline)/2))
		defer cancel()
	}
	s.eachConcurrently(ctx, len(pending), func(j int) {
		row := &rows[pending[j]]
		phrases, err := phraser(ctx, row.Name)
		switch {
		case err == nil:
			row.Phrases, row.PhrasesFrom = phrases, "gateway"
		case ctx.Err() != nil:
			row.Note = weakSignalTimeout
		case errors.Is(err, errLLMNotConfigured):
			row.Note = weakSignalNoPhrases
		default:
			slog.Warn("weak signal file phrases", "row", row.Number, "error", err)
			row.Note = weakSignalNoPhrases
		}
	})
	for _, i := range pending {
		if len(rows[i].Phrases) == 0 && rows[i].Note == "" {
			rows[i].Note = weakSignalTimeout
		}
	}
}

// weakSignalWindowPhrase — фраза через дефисы: одним словом сборщик ищет её буквально, без перевода.
func weakSignalWindowPhrase(phrase string) string {
	return strings.Join(liveTokens(phrase), "-")
}

func weakSignalFileMetrics(rows []WeakSignalFileRow, threshold float64) *WeakSignalFileMetrics {
	m := WeakSignalFileMetrics{}
	for _, row := range rows {
		if row.LabelText != "" {
			m.UnknownLabels++
		}
		if row.Label == nil {
			continue
		}
		m.Labeled++
		if row.Score == nil {
			m.Abstained++
			if *row.Label {
				m.AbstainedWeak++
			}
			continue
		}
		m.Scored++
		switch weak := *row.Score >= threshold; {
		case weak && *row.Label:
			m.TP++
		case weak:
			m.FP++
		case *row.Label:
			m.FN++
		default:
			m.TN++
		}
	}
	if m.Labeled == 0 {
		return nil
	}
	ratio := func(a, b int) float64 {
		if b == 0 {
			return 0
		}
		return float64(a) / float64(b)
	}
	m.Precision, m.Recall = ratio(m.TP, m.TP+m.FP), ratio(m.TP, m.TP+m.FN)
	m.F1, m.Accuracy = ratio(2*m.TP, 2*m.TP+m.FP+m.FN), ratio(m.TP+m.TN, m.Scored)
	return &m
}
