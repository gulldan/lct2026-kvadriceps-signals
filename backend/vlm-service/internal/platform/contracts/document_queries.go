package contracts

type DocumentQuery struct {
	ReaderID  string
	AllOwners bool
	Query     string
	Status    string
	FileType  string
	BeforeID  string
	Limit     int
	Offset    int
}

// DocumentStats содержит только агрегаты доступного читателю корпуса.
type DocumentStats struct {
	Total          int            `json:"total"`
	ByStatus       map[string]int `json:"by_status"`
	ByFileType     map[string]int `json:"by_file_type"`
	TotalSizeBytes int64          `json:"total_size_bytes"`
	TotalChunks    int64          `json:"total_chunks"`
	UploadsByDay   []DocumentDay  `json:"uploads_by_day"`
	TopByChunks    []DocumentTop  `json:"top_by_chunks"`
	IndexedPct     int            `json:"indexed_pct"`
}

// DocumentDay: день календарный, по UTC.
type DocumentDay struct {
	Day   string `json:"day"`
	Count int    `json:"count"`
}

type DocumentTop struct {
	ID         string `json:"id"`
	Filename   string `json:"filename"`
	Title      string `json:"title,omitempty"`
	ChunkCount int32  `json:"chunk_count"`
}

// DocumentScan задаёт страницу внутреннего обхода без удаления повторных представлений.
type DocumentScan struct {
	BeforeID string
	Status   string
	Kind     string
	Limit    int
}
