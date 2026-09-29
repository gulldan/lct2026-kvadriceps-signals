package domain

import "context"

type AppSettingsSnapshot struct {
	Revision int64
	Values   map[string]string
}

// AppSettingsRepository сохраняет пакет настроек с проверкой ожидаемой ревизии.
type AppSettingsRepository interface {
	Snapshot(context.Context) (AppSettingsSnapshot, error)
	Apply(context.Context, int64, string, map[string]*string) (int64, error)
}
