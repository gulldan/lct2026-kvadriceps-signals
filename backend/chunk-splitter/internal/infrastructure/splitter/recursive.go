package splitter

import (
	"slices"
	"strings"

	"github.com/example/chunk-splitter/internal/domain"
)

// По убыванию силы разрыва; "" — запасное жёсткое деление.
func separators() []string { return []string{"\n\n", "\n", "。", "！", "？", "；", " ", ""} }

// Recursive меряет бюджет размера функцией measure (руны или токены), а перекрытие всегда в рунах:
// это заимствованный контекст, точность там не важна.
type Recursive struct {
	size    int
	overlap int
	measure func(string) int
}

func (r *Recursive) Configuration() (int, int) { return r.size, r.overlap }

// NewRecursive зажимает overlap в [0, size-1], чтобы разбиение всегда продвигалось.
func NewRecursive(size, overlap int) *Recursive {
	return NewRecursiveWithMeasure(size, overlap, runeLen)
}

// NewRecursiveWithMeasure: зажим overlap смешивает единицы, но защищает только от патологической настройки.
func NewRecursiveWithMeasure(maxSize, overlap int, measure func(string) int) *Recursive {
	if maxSize <= 0 {
		maxSize = 1000
	}
	if overlap < 0 {
		overlap = 0
	}
	if overlap >= maxSize {
		overlap = maxSize - 1
	}
	if measure == nil {
		measure = runeLen
	}
	return &Recursive{size: maxSize, overlap: overlap, measure: measure}
}

type piece struct {
	text    string
	section string
}

// Split держит таблицу целой (с ограниченным превышением), чтобы строку данных не разрезало.
func (r *Recursive) Split(text string) []domain.Chunk {
	if strings.TrimSpace(text) == "" {
		return nil
	}

	var assembled []piece
	for _, b := range segment(text) {
		assembled = append(assembled, r.chunkBlock(b)...)
	}
	// Пустые структурные промежутки не должны становиться копиями предыдущего перекрытия.
	assembled = slices.DeleteFunc(assembled, func(p piece) bool { return strings.TrimSpace(p.text) == "" })
	assembled = r.applyOverlap(assembled)

	chunks := make([]domain.Chunk, 0, len(assembled))
	for _, p := range assembled {
		if strings.TrimSpace(p.text) == "" {
			continue
		}
		// Индекс присваивается после фильтра, чтобы нумерация оставалась сплошной.
		chunks = append(chunks, domain.Chunk{Index: len(chunks), Text: p.text, Section: p.section})
	}
	return chunks
}

func (r *Recursive) chunkBlock(b block) []piece {
	if b.kind == kindTable && r.measure(b.text) <= r.size*tableOverageFactor {
		return []piece{{text: b.text, section: b.section}}
	}
	merged := r.mergePieces(r.splitRecursive(b.text, 0))
	out := make([]piece, len(merged))
	for i, c := range merged {
		out[i] = piece{text: c, section: b.section}
	}
	return out
}

func (r *Recursive) splitRecursive(text string, sepIdx int) []string {
	if r.measure(text) <= r.size {
		return []string{text}
	}
	seps := separators()
	if sepIdx >= len(seps) {
		return r.hardSplit(text)
	}

	sep := seps[sepIdx]
	if sep == "" {
		// Пустой разделитель — «между каждой руной»; hardSplit даёт куски по размеру, а не по одной руне.
		return r.hardSplit(text)
	}

	parts := strings.Split(text, sep)
	out := make([]string, 0, len(parts))
	for idx, p := range parts {
		// Разделитель остаётся в куске, чтобы склейка восстановила исходник со структурой строк
		// (таблицы, формулы, списки), а не схлопнула её в пробелы.
		piece := p
		if idx < len(parts)-1 {
			piece += sep
		}
		if piece == "" {
			continue
		}
		if r.measure(piece) <= r.size {
			out = append(out, piece)
			continue
		}
		out = append(out, r.splitRecursive(piece, sepIdx+1)...)
	}
	return out
}

func (r *Recursive) hardSplit(text string) []string {
	runes := []rune(text)
	var out []string
	for i := 0; i < len(runes); i += r.size {
		end := i + r.size
		if end > len(runes) {
			end = len(runes)
		}
		out = append(out, string(runes[i:end]))
	}
	return out
}

// Склейка без разделителя: каждый кусок уже несёт свой.
func (r *Recursive) mergePieces(pieces []string) []string {
	var (
		chunks  []string
		current []string
		curLen  int
	)

	flush := func() {
		if len(current) == 0 {
			return
		}
		chunks = append(chunks, strings.Join(current, ""))
		current = current[:0]
		curLen = 0
	}

	for _, p := range pieces {
		pLen := r.measure(p)
		if curLen+pLen > r.size && len(current) > 0 {
			flush()
		}
		// Кусок больше окна (только краевые случаи hardSplit, он в рунах) становится отдельным чанком.
		if pLen > r.size {
			flush()
			chunks = append(chunks, p)
			continue
		}
		current = append(current, p)
		curLen += pLen
	}
	flush()
	return chunks
}

// applyOverlap сохраняет раздел самого чанка: подставленный хвост — заимствованный контекст, а не смена раздела.
func (r *Recursive) applyOverlap(chunks []piece) []piece {
	if r.overlap == 0 || len(chunks) <= 1 {
		return chunks
	}
	out := make([]piece, len(chunks))
	out[0] = chunks[0]
	for i := 1; i < len(chunks); i++ {
		prev := []rune(chunks[i-1].text)
		tail := prev
		if len(prev) > r.overlap {
			tail = prev[len(prev)-r.overlap:]
		}
		out[i] = piece{text: string(tail) + chunks[i].text, section: chunks[i].section}
	}
	return out
}

func runeLen(s string) int { return len([]rune(s)) }
