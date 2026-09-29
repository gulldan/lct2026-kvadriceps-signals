package httpapi

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"net/http"
	"time"
)

// cachedJSON сериализует через encoding/json, а не sonic из httpx.JSON: sonic не сортирует ключи карт,
// и валидатор менялся бы от ответа к ответу.
func (a *API) cachedJSON(w http.ResponseWriter, r *http.Request, value any) {
	raw, err := json.Marshal(value)
	if err != nil {
		a.fail(w, err)
		return
	}
	serveCachedJSON(w, r, raw)
}

// serveCachedJSON: private — ответ зависит от прав владельца, no-cache — браузер переспрашивает и получает 304.
// http.ServeContent понимает и слабый валидатор, в который nginx превращает ETag при сжатии.
func serveCachedJSON(w http.ResponseWriter, r *http.Request, raw []byte) {
	sum := sha256.Sum256(raw)
	w.Header().Set("ETag", `"`+hex.EncodeToString(sum[:])+`"`)
	w.Header().Set("Cache-Control", "private, no-cache")
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.Header().Set("X-Content-Type-Options", "nosniff")
	http.ServeContent(w, r, "", time.Time{}, bytes.NewReader(raw))
}
