package application

import (
	"archive/zip"
	"bytes"
	"strings"
	"testing"
)

func TestWeakSignalFile(t *testing.T) {
	csv := "№;Технология (слабый сигнал);Почему это слабый сигнал;класс;Фразы\n" +
		"1;Сканеры MCP-серверов;растёт;слабый сигнал;tool poisoning, MCP security\n" +
		"2;Wi-Fi;;зрелая;\n" +
		";;;;\n" +
		"4;Метавселенная;;шум;\n" +
		"5;Роевая робототехника;;;\n"
	rows, err := ParseWeakSignalFile([]byte(csv))
	if err != nil {
		t.Fatal(err)
	}
	if len(rows) != 4 || rows[0].Name != "Сканеры MCP-серверов" || rows[3].Number != 5 || rows[3].Label != nil ||
		!*rows[0].Label || *rows[1].Label || *rows[2].Label ||
		strings.Join(rows[0].Phrases, "|") != "tool poisoning|mcp security" || rows[0].PhrasesFrom != "file" {
		t.Fatalf("csv rows: %+v", rows)
	}
	rows, err = ParseWeakSignalFile([]byte(`[{"name": "Zigbee", "label": 0, "phrases": ["zigbee"]},
		{"technology": "Agent identity", "is_weak_signal": true}]`))
	if err != nil || len(rows) != 2 || *rows[0].Label || !*rows[1].Label || rows[0].Phrases[0] != "zigbee" {
		t.Fatalf("json rows: %+v, %v", rows, err)
	}
	rows, err = ParseWeakSignalFile([]byte("Технология;класс\nWi-Fi;может быть\n"))
	if err != nil || rows[0].Label != nil || rows[0].LabelText != "может быть" {
		t.Fatalf("unknown label must stay on the row, not fail the file: %+v, %v", rows, err)
	}
	// Книга как у организаторов: титул над шапкой, пропущенный первый столбец, строки из общего словаря и прямо в ячейке.
	var book bytes.Buffer
	archive := zip.NewWriter(&book)
	for name, body := range map[string]string{
		"xl/sharedStrings.xml": `<sst><si><t>100 сигналов (сентябрь 2026)</t></si><si><t>Технология (слабый сигнал)</t></si>` +
			`<si><r><t>Agent </t></r><r><t>identity</t></r></si><si><t>Метка</t></si></sst>`,
		"xl/worksheets/sheet1.xml": `<worksheet><sheetData><row r="1"><c r="B1" t="s"><v>0</v></c></row>` +
			`<row r="2"><c r="B2" t="s"><v>1</v></c><c r="C2" t="s"><v>3</v></c></row>` +
			`<row r="3"><c r="B3" t="s"><v>2</v></c><c r="C3"><v>1</v></c></row>` +
			`<row r="4"><c r="B4" t="inlineStr"><is><t>Wi-Fi</t></is></c><c r="C4" t="inlineStr"><is><t>нет</t></is></c></row>` +
			`</sheetData></worksheet>`,
	} {
		w, _ := archive.Create(name)
		_, _ = w.Write([]byte(body))
	}
	if err = archive.Close(); err != nil {
		t.Fatal(err)
	}
	rows, err = ParseWeakSignalFile(book.Bytes())
	if err != nil || len(rows) != 2 || rows[0].Name != "Agent identity" || !*rows[0].Label || rows[1].Name != "Wi-Fi" ||
		*rows[1].Label {
		t.Fatalf("xlsx rows: %+v, %v", rows, err)
	}
	score := func(v float64) *float64 { return &v }
	metrics := weakSignalFileMetrics([]WeakSignalFileRow{
		{Label: new(true), Score: score(0.9)}, {Label: new(true), Score: score(0.1)},
		{Label: new(false), Score: score(0.5)}, {Label: new(false), Score: score(0.2)},
		{Label: new(true)}, {Score: score(0.9)}, {LabelText: "может быть", Score: score(0.9)},
	}, 0.4)
	if metrics.Labeled != 5 || metrics.Scored != 4 || metrics.Abstained != 1 || metrics.AbstainedWeak != 1 ||
		metrics.UnknownLabels != 1 || metrics.TP != 1 || metrics.FP != 1 || metrics.FN != 1 ||
		metrics.TN != 1 || metrics.Precision != 0.5 || metrics.Recall != 0.5 || metrics.F1 != 0.5 || metrics.Accuracy != 0.5 {
		t.Fatalf("metrics: %+v", metrics)
	}
}
