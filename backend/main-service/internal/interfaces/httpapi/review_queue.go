package httpapi

import (
	"cmp"
	"net/http"
	"strconv"

	"github.com/example/main-service/internal/platform/contracts"
	"github.com/example/main-service/internal/platform/httpx"
	"github.com/example/main-service/internal/platform/messaging"
)

// ReviewQueue читает отложенные сообщения, не забирая их из очереди.
type ReviewQueue interface {
	PeekDead(limit int) ([]messaging.DeadMessage, error)
}

func (a *API) SetReviewQueue(queue ReviewQueue) { a.review = queue }

type reviewResponse struct {
	Messages []messaging.DeadMessage `json:"messages"`
	ByClass  map[string]int          `json:"by_class"`
}

// readReviewQueue делит сбои по классам: требующие человека и исчерпавшие повторы лежат в одной очереди.
func (a *API) readReviewQueue(w http.ResponseWriter, r *http.Request) {
	if !a.requireRole(w, r, "admin", "operator") {
		return
	}
	if a.review == nil {
		httpx.Error(w, http.StatusServiceUnavailable, "review queue unavailable")
		return
	}
	limit, err := strconv.Atoi(cmp.Or(r.URL.Query().Get("limit"), "50"))
	if err != nil || limit < 1 || limit > 500 {
		httpx.Error(w, http.StatusBadRequest, "limit must be a number between 1 and 500")
		return
	}
	messages, err := a.review.PeekDead(limit)
	if err != nil {
		httpx.Error(w, http.StatusInternalServerError, "review queue unreadable")
		return
	}
	byClass := map[string]int{}
	for i, message := range messages {
		// Сообщения, отправленные в DLQ самим брокером, класса не несут и по построению временные.
		if message.Class == "" {
			messages[i].Class = contracts.FailureTransient
		}
		byClass[messages[i].Class]++
	}
	w.Header().Set("Cache-Control", "no-store")
	httpx.JSON(w, http.StatusOK, reviewResponse{Messages: messages, ByClass: byClass})
}
