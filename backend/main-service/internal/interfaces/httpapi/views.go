package httpapi

import (
	"encoding/json"

	commonv1 "github.com/example/main-service/internal/platform/genproto/common/v1"
	llmv1 "github.com/example/main-service/internal/platform/genproto/llm/v1"
)

// JSON-формат для браузера старше перехода на gRPC и меняться не должен. Списки отдаются не-nil,
// чтобы пустая коллекция давала [], а не null.

type documentView struct {
	ID             string          `json:"id"`
	OwnerID        string          `json:"owner_id"`
	Filename       string          `json:"filename"`
	Title          string          `json:"title,omitempty"`
	Kind           string          `json:"kind,omitempty"`
	Author         string          `json:"author,omitempty"`
	PublishedAt    string          `json:"published_at,omitempty"`
	SourceRef      string          `json:"source_ref,omitempty"`
	MIMEType       string          `json:"mime_type"`
	Size           int64           `json:"size"`
	ObjectKey      string          `json:"object_key"`
	Status         string          `json:"status"`
	StatusMsg      string          `json:"status_msg,omitempty"`
	ChunkCount     int32           `json:"chunk_count"`
	CreatedAt      string          `json:"created_at"`
	UpdatedAt      string          `json:"updated_at"`
	Duplicate      bool            `json:"duplicate,omitempty"`
	SourceMetadata json.RawMessage `json:"source_metadata,omitempty"`
}

func newDocumentView(d *commonv1.Document) documentView {
	return documentView{
		ID:             d.GetId(),
		OwnerID:        d.GetOwnerId(),
		Filename:       d.GetFilename(),
		Title:          d.GetTitle(),
		Kind:           d.GetKind(),
		Author:         d.GetAuthor(),
		PublishedAt:    d.GetPublishedAt(),
		SourceRef:      d.GetSourceRef(),
		MIMEType:       d.GetMimeType(),
		Size:           d.GetSize(),
		ObjectKey:      d.GetObjectKey(),
		Status:         d.GetStatus(),
		StatusMsg:      d.GetStatusMsg(),
		ChunkCount:     d.GetChunkCount(),
		CreatedAt:      d.GetCreatedAt(),
		UpdatedAt:      d.GetUpdatedAt(),
		SourceMetadata: json.RawMessage(d.GetSourceMetadata()),
	}
}

func newDocumentViews(docs []*commonv1.Document) []documentView {
	out := make([]documentView, 0, len(docs))
	for _, d := range docs {
		out = append(out, newDocumentView(d))
	}
	return out
}

// adminDocumentView без object_key и owner_id: панели их не читают, а ключ хранилища раздувал ответ.
type adminDocumentView struct {
	ID          string `json:"id"`
	Filename    string `json:"filename"`
	Title       string `json:"title,omitempty"`
	Kind        string `json:"kind,omitempty"`
	Author      string `json:"author,omitempty"`
	PublishedAt string `json:"published_at,omitempty"`
	SourceRef   string `json:"source_ref,omitempty"`
	MIMEType    string `json:"mime_type"`
	Size        int64  `json:"size"`
	Status      string `json:"status"`
	StatusMsg   string `json:"status_msg,omitempty"`
	ChunkCount  int32  `json:"chunk_count"`
	CreatedAt   string `json:"created_at"`
	UpdatedAt   string `json:"updated_at"`
}

func newAdminDocumentViews(docs []*commonv1.Document) []adminDocumentView {
	out := make([]adminDocumentView, 0, len(docs))
	for _, d := range docs {
		out = append(out, adminDocumentView{
			ID:          d.GetId(),
			Filename:    d.GetFilename(),
			Title:       d.GetTitle(),
			Kind:        d.GetKind(),
			Author:      d.GetAuthor(),
			PublishedAt: d.GetPublishedAt(),
			SourceRef:   d.GetSourceRef(),
			MIMEType:    d.GetMimeType(),
			Size:        d.GetSize(),
			Status:      d.GetStatus(),
			StatusMsg:   d.GetStatusMsg(),
			ChunkCount:  d.GetChunkCount(),
			CreatedAt:   d.GetCreatedAt(),
			UpdatedAt:   d.GetUpdatedAt(),
		})
	}
	return out
}

// userAccountView не содержит хэша пароля.
type userAccountView struct {
	ID        string   `json:"id"`
	Username  string   `json:"username"`
	Roles     []string `json:"roles"`
	CreatedAt string   `json:"created_at"`
}

func newUserViews(users []*commonv1.User) []userAccountView {
	out := make([]userAccountView, 0, len(users))
	for _, u := range users {
		out = append(out, userAccountView{
			ID:        u.GetId(),
			Username:  u.GetUsername(),
			Roles:     u.GetRoles(),
			CreatedAt: u.GetCreatedAt(),
		})
	}
	return out
}

type chunkView struct {
	ID    string `json:"id"`
	Index int32  `json:"index"`
	Text  string `json:"text"`
}

type documentChunksView struct {
	Document documentView `json:"document"`
	Chunks   []chunkView  `json:"chunks"`
}

func newDocumentChunksView(d *commonv1.Document, chunks []*llmv1.DocumentChunk) documentChunksView {
	out := make([]chunkView, 0, len(chunks))
	for _, c := range chunks {
		out = append(out, chunkView{ID: c.GetId(), Index: c.GetIndex(), Text: c.GetText()})
	}
	return documentChunksView{Document: newDocumentView(d), Chunks: out}
}

type chatView struct {
	ID            string `json:"id"`
	OwnerID       string `json:"owner_id"`
	OwnerUsername string `json:"owner_username,omitempty"`
	Title         string `json:"title"`
	Source        string `json:"source,omitempty"`
	CreatedAt     string `json:"created_at"`
}

type chatListView struct {
	Items []chatView `json:"items"`
	Total int64      `json:"total"`
}

func newChatView(c *commonv1.Chat) chatView {
	return chatView{
		ID:            c.GetId(),
		OwnerID:       c.GetOwnerId(),
		OwnerUsername: c.GetOwnerUsername(),
		Title:         c.GetTitle(),
		Source:        c.GetSource(),
		CreatedAt:     c.GetCreatedAt(),
	}
}

func newChatViews(chats []*commonv1.Chat) []chatView {
	out := make([]chatView, 0, len(chats))
	for _, c := range chats {
		out = append(out, newChatView(c))
	}
	return out
}

type sourceView struct {
	DocumentID     string  `json:"document_id"`
	Filename       string  `json:"filename"`
	ChunkID        string  `json:"chunk_id"`
	Snippet        string  `json:"snippet"`
	Score          float64 `json:"score"`
	CharStart      int32   `json:"char_start,omitempty"`
	CharEnd        int32   `json:"char_end,omitempty"`
	PageStart      int32   `json:"page_start,omitempty"`
	PageEnd        int32   `json:"page_end,omitempty"`
	SectionHeading string  `json:"section_heading,omitempty"`
	// Какая ветка поиска нашла фрагмент и, для фрагментов из сводки RAPTOR, id точки сводки.
	Origin          string `json:"origin,omitempty"`
	RaptorSummaryID string `json:"raptor_summary_id,omitempty"`
	IndexSnapshot   string `json:"source_index_snapshot,omitempty"`
}

// newSourceViews оставляет пустой список nil, чтобы у «sources» работал omitempty.
func newSourceViews(sources []*commonv1.Source) []sourceView {
	if len(sources) == 0 {
		return nil
	}
	out := make([]sourceView, 0, len(sources))
	for _, s := range sources {
		out = append(out, sourceView{
			DocumentID:      s.GetDocumentId(),
			Filename:        s.GetFilename(),
			ChunkID:         s.GetChunkId(),
			Snippet:         s.GetSnippet(),
			Score:           s.GetScore(),
			CharStart:       s.GetCharStart(),
			CharEnd:         s.GetCharEnd(),
			PageStart:       s.GetPageStart(),
			PageEnd:         s.GetPageEnd(),
			SectionHeading:  s.GetSectionHeading(),
			Origin:          s.GetOrigin(),
			RaptorSummaryID: s.GetRaptorSummaryId(),
			IndexSnapshot:   s.GetSourceIndexSnapshot(),
		})
	}
	return out
}

type messageView struct {
	TurnID    string          `json:"turn_id,omitempty"`
	TurnState string          `json:"turn_state,omitempty"`
	ID        string          `json:"id"`
	ChatID    string          `json:"chat_id"`
	Role      string          `json:"role"`
	Content   string          `json:"content"`
	Sources   []sourceView    `json:"sources,omitempty"`
	CreatedAt string          `json:"created_at"`
	Meta      json.RawMessage `json:"meta,omitempty"`
}

func newMessageView(m *commonv1.Message) messageView {
	view := messageView{
		TurnID: m.GetTurnId(), TurnState: m.GetTurnState(),
		ID:        m.GetId(),
		ChatID:    m.GetChatId(),
		Role:      m.GetRole(),
		Content:   m.GetContent(),
		Sources:   newSourceViews(m.GetSources()),
		CreatedAt: m.GetCreatedAt(),
	}
	if meta := m.GetMeta(); meta != "" && meta != "{}" {
		view.Meta = json.RawMessage(meta)
	}
	return view
}

func newMessageViews(msgs []*commonv1.Message) []messageView {
	out := make([]messageView, 0, len(msgs))
	for _, m := range msgs {
		out = append(out, newMessageView(m))
	}
	return out
}

type modelView struct {
	ID      string `json:"id"`
	Name    string `json:"name"`
	Role    string `json:"role"`
	Backend string `json:"backend"`
}

func newModelViews(models []*commonv1.Model) []modelView {
	out := make([]modelView, 0, len(models))
	for _, m := range models {
		out = append(out, modelView{
			ID:      m.GetId(),
			Name:    m.GetName(),
			Role:    m.GetRole(),
			Backend: m.GetBackend(),
		})
	}
	return out
}
