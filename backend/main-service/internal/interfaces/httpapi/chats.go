package httpapi

import (
	"net/http"

	"github.com/example/main-service/internal/domain"
	"github.com/example/main-service/internal/platform/httpx"
	"github.com/example/main-service/internal/platform/jwt"
)

func (a *API) createChat(w http.ResponseWriter, r *http.Request) {
	ownerID, ok := ownerFromContext(r)
	if !ok {
		httpx.Error(w, http.StatusUnauthorized, "missing claims")
		return
	}
	var req struct {
		Title  string `json:"title"`
		Source string `json:"source"`
	}
	if err := httpx.Decode(r, &req); err != nil {
		httpx.Error(w, http.StatusBadRequest, err.Error())
		return
	}
	chat, err := a.chat.CreateChat(r.Context(), ownerID, req.Title, req.Source)
	if err != nil {
		a.fail(w, err)
		return
	}
	httpx.JSON(w, http.StatusCreated, newChatView(chat))
}

func (a *API) listChats(w http.ResponseWriter, r *http.Request) {
	ownerID, ok := ownerFromContext(r)
	if !ok {
		httpx.Error(w, http.StatusUnauthorized, "missing claims")
		return
	}
	limit := queryInt(r, "limit", 0)
	offset := queryInt(r, "offset", 0)
	// «Вся история» (все пользователи) доступна только администратору.
	all := false
	if r.URL.Query().Get("all") == "1" {
		if claims, okClaims := jwt.ClaimsFromContext(r.Context()); okClaims {
			all = hasAnyRole(claims, "admin")
		}
	}
	chats, total, err := a.chat.ListChats(r.Context(), ownerID, limit, offset, all)
	if err != nil {
		a.fail(w, err)
		return
	}
	httpx.JSON(w, http.StatusOK, chatListView{Items: newChatViews(chats), Total: total})
}

func (a *API) getChat(w http.ResponseWriter, r *http.Request) {
	ownerID, ok := ownerFromContext(r)
	if !ok {
		httpx.Error(w, http.StatusUnauthorized, "missing claims")
		return
	}
	chat, err := a.chat.GetChat(r.Context(), ownerID, r.PathValue("id"))
	if err != nil {
		a.fail(w, err)
		return
	}
	httpx.JSON(w, http.StatusOK, newChatView(chat))
}

func (a *API) listMessages(w http.ResponseWriter, r *http.Request) {
	ownerID, ok := ownerFromContext(r)
	if !ok {
		httpx.Error(w, http.StatusUnauthorized, "missing claims")
		return
	}
	msgs, err := a.chat.MessagePage(
		r.Context(),
		ownerID,
		r.PathValue(
			"id",
		),
		r.URL.Query().Get(
			"before",
		),
		min(
			max(
				queryInt(
					r,
					"limit",
					100,
				),
				1,
			),
			200,
		),
	)
	if err != nil {
		a.fail(w, err)
		return
	}
	httpx.JSON(w, http.StatusOK, newMessageViews(msgs))
}

func (a *API) postMessage(w http.ResponseWriter, r *http.Request) {
	ownerID, ok := ownerFromContext(r)
	if !ok {
		httpx.Error(w, http.StatusUnauthorized, "missing claims")
		return
	}
	var req struct {
		Content   string `json:"content"`
		RequestID string `json:"request_id"`
	}
	if err := httpx.Decode(r, &req); err != nil {
		httpx.Error(w, http.StatusBadRequest, err.Error())
		return
	}
	if req.Content == "" || len(req.Content) > 65536 || req.RequestID == "" || len(req.RequestID) > 128 {
		httpx.Error(w, http.StatusBadRequest, "content and request_id are required within limits")
		return
	}
	msg, err := a.chat.Ask(r.Context(), domain.AskCommand{
		OwnerID:   ownerID,
		RequestID: req.RequestID,
		ChatID:    r.PathValue("id"),
		Content:   req.Content,
	})
	if err != nil {
		a.fail(w, err)
		return
	}
	httpx.JSON(w, http.StatusCreated, newMessageView(msg))
}

func (a *API) listModels(w http.ResponseWriter, r *http.Request) {
	models, err := a.chat.ListModels(r.Context())
	if err != nil {
		a.fail(w, err)
		return
	}
	httpx.JSON(w, http.StatusOK, newModelViews(models))
}
