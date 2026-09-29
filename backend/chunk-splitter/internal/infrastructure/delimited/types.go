package delimited

// DataRow нумеруется с заголовка: Number первой записи данных равен 2.
type DataRow struct {
	Number int
	Cells  []string
}

type TransformResult struct {
	Text      string
	Rows      int
	Columns   int
	Delimiter rune
	Truncated bool
}
