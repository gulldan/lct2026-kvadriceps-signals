package httpapi

import (
	"context"
	"fmt"
	"github.com/example/main-service/internal/platform/jwt"
	"github.com/example/main-service/internal/platform/logger"
	"math"
	"net/http"
	"strconv"
	"strings"

	"github.com/example/main-service/internal/application"
	"github.com/example/main-service/internal/platform/config"
	"github.com/example/main-service/internal/platform/dbclient"
	"github.com/example/main-service/internal/platform/httpx"
	"github.com/example/main-service/internal/platform/runtimecfg"
	"github.com/example/main-service/internal/platform/valkey"
)

// AppSettingsStore хранит оверрайды в Postgres и зеркалит весь набор в Valkey, откуда их с коротким
// кэшем читают все сервисы, — настройки применяются без редеплоя.
type AppSettingsStore struct {
	db *dbclient.Client
	kv *valkey.Client
}

func NewAppSettingsStore(db *dbclient.Client, kv *valkey.Client) *AppSettingsStore {
	return &AppSettingsStore{db: db, kv: kv}
}

func (s *AppSettingsStore) Republish(ctx context.Context) error {
	snapshot, err := s.db.ReadAppSettings(ctx)
	if err != nil {
		return err
	}
	snapshot.Values["RAG_SETTINGS_REVISION"] = strconv.FormatInt(snapshot.Revision, 10)
	return runtimecfg.Publish(ctx, s.kv, snapshot.Values)
}

func (s *AppSettingsStore) apply(ctx context.Context, revision int64, actor string, changes map[string]*string) error {
	if _, err := s.db.ApplyAppSettings(ctx, revision, actor, changes); err != nil {
		return err
	}
	if err := s.Republish(ctx); err != nil {
		// База уже зафиксировала пакет; фоновая синхронизация повторит публикацию.
		logger.From(ctx).Error().Err(err).Msg("settings saved; runtime publication pending")
	}
	return nil
}

// settingSpec: изменяемость параметра задаёт общая политика, а не тип поля интерфейса.
type settingSpec struct {
	Key   string `json:"key"`
	Kind  string `json:"kind"`
	Group string `json:"group"`
	Def   string `json:"default"`
}

const (
	kindString = "string"
	kindBool   = "bool"
	kindNumber = "number"
	kindSecret = "secret"

	groupLLM        = "llm"
	groupPubsearch  = "pubsearch"
	groupGeneration = "generation"

	boolTrue = "true"
)

func appSettingSpecs() []settingSpec {
	return []settingSpec{
		{Key: "VLLM_URL", Kind: kindString, Group: groupLLM, Def: ""},
		{Key: "VLLM_MODEL", Kind: kindString, Group: groupLLM, Def: ""},
		{Key: "VLLM_API_KEY", Kind: kindSecret, Group: groupLLM, Def: ""},
		{Key: "RAG_REASONER_MODEL", Kind: kindString, Group: groupLLM, Def: ""},
		{Key: modelDisplayKey, Kind: kindString, Group: groupLLM, Def: modelDisplayDefault},
		{Key: "RADAR_LLM_WORKERS", Kind: kindNumber, Group: groupLLM, Def: strconv.Itoa(application.LiveDefaultWorkers)},
		{Key: "RADAR_LLM_RPM", Kind: kindNumber, Group: groupLLM, Def: "0"},
		{Key: "LLM_RUB_PER_USD", Kind: kindNumber, Group: groupLLM, Def: "90"},
		{Key: "LLM_COST_CURRENCY", Kind: kindString, Group: groupLLM, Def: "₽"},
		{Key: "LLM_PRICES", Kind: kindString, Group: groupLLM, Def: ""},
		{Key: "PUBSEARCH_ENABLED", Kind: kindBool, Group: groupPubsearch, Def: boolTrue},
		{Key: "PUBSEARCH_MAILTO", Kind: kindString, Group: groupPubsearch, Def: ""},
		{Key: "PUBSEARCH_RECENT_YEARS", Kind: kindNumber, Group: groupPubsearch, Def: "5"},
		{Key: "RAG_GEN_BREADTH", Kind: kindBool, Group: groupGeneration, Def: boolTrue},
		{Key: "RAG_GEN_QUALITY_FLOOR", Kind: kindNumber, Group: groupGeneration, Def: "0.35"},
		{Key: "KPI_SUGGEST", Kind: kindBool, Group: groupGeneration, Def: boolTrue},
	}
}

func specFor(key string) (settingSpec, bool) {
	for _, s := range appSettingSpecs() {
		if s.Key == key {
			return s, true
		}
	}
	return settingSpec{}, false
}

func validSettingValue(spec settingSpec, v string) bool {
	switch spec.Kind {
	case kindBool:
		switch strings.ToLower(strings.TrimSpace(v)) {
		case "1", "t", boolTrue, "yes", "on", "0", "f", "false", "no", "off":
			return true
		}
		return false
	case kindNumber:
		number, err := strconv.ParseFloat(strings.TrimSpace(v), 64)
		return err == nil && !math.IsNaN(number) && !math.IsInf(number, 0)
	default:
		return true
	}
}

type appSettingView struct {
	settingSpec
	Override    string `json:"override"`
	HasOverride bool   `json:"hasOverride"`
	EnvValue    string `json:"envValue"`
	EnvSet      bool   `json:"envSet"`
	EnvOnly     bool   `json:"envOnly"`
	Source      string `json:"source"`
}

func (s *AppSettingsStore) view(ctx context.Context) ([]appSettingView, int64, error) {
	snapshot, err := s.db.ReadAppSettings(ctx)
	if err != nil {
		return nil, 0, err
	}
	return appSettingViews(snapshot.Values), snapshot.Revision, nil
}

func appSettingViews(overrides map[string]string) []appSettingView {
	specs := appSettingSpecs()
	out := make([]appSettingView, 0, len(specs))
	for _, spec := range specs {
		ov, hasOv := overrides[spec.Key]
		env := config.Get(spec.Key, "")
		v := appSettingView{
			settingSpec: spec,
			Override:    ov,
			HasOverride: hasOv,
			EnvValue:    env,
			EnvSet:      env != "",
			EnvOnly:     !config.CanOverride(spec.Key),
			Source:      "default",
		}
		if v.EnvOnly {
			v.Def, v.Override, v.EnvValue = "", "", ""
			v.HasOverride = false
		}
		switch {
		case v.HasOverride:
			v.Source = "db"
		case env != "":
			v.Source = "env"
		}
		out = append(out, v)
	}
	return out
}

// getAppSettings для адресов, контактов и секретов отдаёт лишь признак наличия в окружении.
func (a *API) getAppSettings(w http.ResponseWriter, r *http.Request) {
	if !a.requireRole(w, r, "admin") {
		return
	}
	if a.appSettings == nil {
		httpx.Error(w, http.StatusServiceUnavailable, "settings store is not configured")
		return
	}
	views, revision, err := a.appSettings.view(r.Context())
	if err != nil {
		a.fail(w, fmt.Errorf("list settings: %w", err))
		return
	}
	httpx.JSON(w, http.StatusOK, map[string]any{"settings": views, "revision": revision})
}

// putAppSettings: пустое или null значение снимает оверрайд, возвращая env или дефолт.
func (a *API) putAppSettings(w http.ResponseWriter, r *http.Request) {
	if !a.requireRole(w, r, "admin") {
		return
	}
	if a.appSettings == nil {
		httpx.Error(w, http.StatusServiceUnavailable, "settings store is not configured")
		return
	}
	var body struct {
		Values   map[string]*string `json:"values"`
		Revision *int64             `json:"revision"`
	}
	if err := decodeResource(r, &body); err != nil || len(body.Values) == 0 || body.Revision == nil || *body.Revision < 0 {
		httpx.Error(w, http.StatusBadRequest, "values object is required")
		return
	}
	for key, val := range body.Values {
		spec, known := specFor(key)
		if !known {
			httpx.Error(w, http.StatusBadRequest, "unknown setting: "+key)
			return
		}
		if !config.CanOverride(key) {
			httpx.Error(w, http.StatusBadRequest, "setting is configured through environment: "+key)
			return
		}
		if val != nil && *val != "" && !validSettingValue(spec, *val) {
			httpx.Error(w, http.StatusBadRequest, "invalid value for "+key)
			return
		}
	}
	claims, _ := jwt.ClaimsFromContext(r.Context())
	if err := a.appSettings.apply(r.Context(), *body.Revision, claims.UserID, body.Values); err != nil {
		a.fail(w, fmt.Errorf("save settings: %w", err))
		return
	}
	views, revision, err := a.appSettings.view(r.Context())
	if err != nil {
		a.fail(w, fmt.Errorf("list settings: %w", err))
		return
	}
	httpx.JSON(w, http.StatusOK, map[string]any{"settings": views, "revision": revision})
}
