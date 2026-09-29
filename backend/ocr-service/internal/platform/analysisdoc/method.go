package analysisdoc

import _ "embed"

// DevelopmentMethod генерирует signal-worker/worker.py --print-method; воркеры сверяют ровно эти
// байты со своим установленным кодом.
//
//go:embed method.json
var DevelopmentMethod string
