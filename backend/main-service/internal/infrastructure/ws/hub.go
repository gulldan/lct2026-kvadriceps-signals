package ws

import (
	"context"
	"sync"
	"time"

	"github.com/gorilla/websocket"
	"github.com/rs/zerolog"
	"google.golang.org/protobuf/proto"

	"github.com/example/main-service/internal/platform/contracts"
	"github.com/example/main-service/internal/platform/jsonx"
	"github.com/example/main-service/internal/platform/logger"

	commonv1 "github.com/example/main-service/internal/platform/genproto/common/v1"
)

const writeWait = 10 * time.Second

// Client пишет в соединение только из writePump через send: gorilla не допускает конкурентных писателей.
type Client struct {
	hub     *Hub
	ownerID string
	conn    *websocket.Conn
	send    chan []byte
}

type Hub struct {
	mu      sync.RWMutex
	clients map[string]map[*Client]struct{}
	log     zerolog.Logger

	onIndexed func(context.Context, string)
}

func NewHub(log zerolog.Logger) *Hub {
	return &Hub{
		mu:      sync.RWMutex{},
		clients: make(map[string]map[*Client]struct{}),
		log:     log,
	}
}

// SetOnIndexed вызывать один раз до запуска консьюмера событий: поле читается без блокировки.
func (h *Hub) SetOnIndexed(fn func(context.Context, string)) { h.onIndexed = fn }

// Register запускает писателя; вызывающий обязан затем крутить ReadLoop, иначе обрыв не заметится.
func (h *Hub) Register(ownerID string, conn *websocket.Conn) *Client {
	c := &Client{
		hub:     h,
		ownerID: ownerID,
		conn:    conn,
		send:    make(chan []byte, 64),
	}
	h.mu.Lock()
	set, ok := h.clients[ownerID]
	if !ok {
		set = make(map[*Client]struct{})
		h.clients[ownerID] = set
	}
	set[c] = struct{}{}
	h.mu.Unlock()

	go c.writePump()
	return c
}

// unregister идемпотентен: его зовут писатель, читатель и Broadcast, а повторный close(send) паникует.
func (h *Hub) unregister(c *Client) {
	h.mu.Lock()
	defer h.mu.Unlock()
	set, ok := h.clients[c.ownerID]
	if !ok {
		return
	}
	if _, ok := set[c]; ok {
		delete(set, c)
		close(c.send)
		if len(set) == 0 {
			delete(h.clients, c.ownerID)
		}
	}
}

// Broadcast отключает клиента с переполненным буфером, а не блокирует рассылку.
func (h *Hub) Broadcast(ownerID string, payload []byte) {
	h.mu.RLock()
	set := h.clients[ownerID]
	targets := make([]*Client, 0, len(set))
	for c := range set {
		targets = append(targets, c)
	}
	h.mu.RUnlock()

	for _, c := range targets {
		select {
		case c.send <- payload:
		default:
			h.log.Warn().Str("owner_id", ownerID).Msg("dropping slow websocket client")
			c.close()
		}
	}
}

// ingestionEventView — контракт WS с браузером; вслед за protobuf его не менять.
type ingestionEventView struct {
	DocumentID   string `json:"document_id"`
	OwnerID      string `json:"owner_id"`
	Status       string `json:"status"`
	Message      string `json:"message,omitempty"`
	Timestamp    string `json:"timestamp"`
	StageCurrent int32  `json:"stage_current,omitempty"`
	StageTotal   int32  `json:"stage_total,omitempty"`
}

// HandleEvent только логирует ошибку декодирования, чтобы не останавливать поток событий с автоподтверждением.
func (h *Hub) HandleEvent(ctx context.Context, body []byte) error {
	var evt commonv1.IngestionEvent
	if err := proto.Unmarshal(body, &evt); err != nil {
		logger.From(ctx).Warn().Err(err).Msg("ignoring malformed ingestion event")
		return nil
	}
	if evt.GetOwnerId() == "" {
		return nil
	}
	if h.onIndexed != nil && evt.GetStatus() == contracts.StatusIndexed {
		h.onIndexed(ctx, evt.GetOwnerId())
	}
	view := ingestionEventView{
		DocumentID: evt.GetDocumentId(),
		OwnerID:    evt.GetOwnerId(),
		Status:     evt.GetStatus(),
		Message:    evt.GetMessage(),
		Timestamp:  evt.GetTimestamp(),
	}
	if evt.GetStageTotal() > 0 {
		view.StageCurrent = evt.GetStageCurrent()
		view.StageTotal = evt.GetStageTotal()
	}
	payload, err := jsonx.Marshal(view)
	if err != nil {
		logger.From(ctx).Warn().Err(err).Msg("encode ingestion event view")
		return nil
	}
	h.Broadcast(evt.GetOwnerId(), payload)
	return nil
}

func (c *Client) close() {
	c.hub.unregister(c)
	_ = c.conn.Close()
}

// ReadLoop нужен, чтобы gorilla обработала управляющие кадры и заметила обрыв; блокирует до закрытия
// соединения или отмены ctx.
func (c *Client) ReadLoop(ctx context.Context) {
	defer c.close()

	// Без закрытия по отмене ctx блокирующее чтение не прервётся.
	stop := context.AfterFunc(ctx, func() { _ = c.conn.Close() })
	defer stop()

	for {
		if _, _, err := c.conn.ReadMessage(); err != nil {
			return
		}
	}
}

func (c *Client) writePump() {
	for payload := range c.send {
		_ = c.conn.SetWriteDeadline(time.Now().Add(writeWait))
		if err := c.conn.WriteMessage(websocket.TextMessage, payload); err != nil {
			c.hub.unregister(c)
			_ = c.conn.Close()
			return
		}
	}
	_ = c.conn.SetWriteDeadline(time.Now().Add(writeWait))
	_ = c.conn.WriteMessage(websocket.CloseMessage, websocket.FormatCloseMessage(websocket.CloseNormalClosure, ""))
}
