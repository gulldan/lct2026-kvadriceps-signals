package analysisdoc

import "time"

// Живой поиск хранится в базе, а не в памяти процесса: перезапуск и вторая реплика не собирают
// заново, а близкий запрос переиспользует собранное.

// LiveSearchRead: TermsKey — отсортированные термины после перевода, по ним находится близкий
// запрос, заданный другими словами.
type LiveSearchRead struct {
	Query       string `json:"query"`
	TermsKey    string `json:"terms_key,omitempty"`
	WithVersion bool   `json:"with_version,omitzero"`
}

type LiveSearchRecord struct {
	URL           string `json:"url"`
	Channel       string `json:"channel"`
	Source        string `json:"source,omitempty"`
	Title         string `json:"title,omitempty"`
	PublishedOn   string `json:"published_on,omitempty"`
	Language      string `json:"language,omitempty"`
	PublisherType string `json:"publisher_type,omitempty"`
	Trust         string `json:"trust,omitempty"`
	TrustBasis    string `json:"trust_basis,omitempty"`
	ContentHash   string `json:"content_hash,omitempty"`
}

// LiveSearchSave: Summary отделён от Detail, потому что карта направлений читает историю целиком,
// а Detail весит до полутора мегабайт.
type LiveSearchSave struct {
	// nil сохраняет совместимость; пустая строка требует отсутствия снимка, непустая — совпадения версии.
	IfMatch *string `json:"if_match,omitzero"`
	// Новый сбор может заменить вспомогательную правку исходного запуска, но не результат другого сбора.
	BaselineRunID string             `json:"baseline_run_id,omitempty"`
	Query         string             `json:"query"`
	TermsKey      string             `json:"terms_key"`
	Terms         []string           `json:"terms,omitempty"`
	Papers        int                `json:"papers"`
	ObservedAt    time.Time          `json:"observed_at"`
	ExpiresAt     time.Time          `json:"expires_at"`
	Detail        []byte             `json:"detail"`
	Summary       []byte             `json:"summary,omitempty"`
	Records       []LiveSearchRecord `json:"records,omitempty"`
}

// LiveSearchStored: Found без Fresh — устарело; данные можно обновить, а не собирать с нуля.
type LiveSearchStored struct {
	Version    string    `json:"version,omitempty"`
	Found      bool      `json:"found"`
	Fresh      bool      `json:"fresh"`
	Query      string    `json:"query,omitempty"`
	TermsKey   string    `json:"terms_key,omitempty"`
	Terms      []string  `json:"terms,omitempty"`
	Papers     int       `json:"papers"`
	Sources    int       `json:"sources"`
	ObservedAt time.Time `json:"observed_at,omitzero"`
	ExpiresAt  time.Time `json:"expires_at,omitzero"`
	Detail     []byte    `json:"detail,omitempty"`
}

// LiveSearchList: нулевой Limit — умолчание на стороне базы.
type LiveSearchList struct {
	Limit int `json:"limit,omitempty"`
}

type LiveSearchEntry struct {
	Query string `json:"query"`
	// Fresh считает база, а не браузер: срок жизни ответа принадлежит хранилищу.
	Fresh      bool      `json:"fresh"`
	Terms      []string  `json:"terms,omitempty"`
	Papers     int       `json:"papers"`
	Sources    int       `json:"sources"`
	ObservedAt time.Time `json:"observed_at,omitzero"`
	ExpiresAt  time.Time `json:"expires_at,omitzero"`
	// Пуст у поисков, сохранённых до появления поля.
	Summary []byte `json:"summary,omitempty"`
}

type LiveSearchHistory struct {
	Entries []LiveSearchEntry `json:"entries"`
}
