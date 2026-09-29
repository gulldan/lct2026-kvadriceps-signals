package dbclient

import (
	"context"
	"time"

	dbv1 "github.com/example/archive-worker/internal/platform/genproto/db/v1"
)

type AppSettingsSnapshot struct {
	Values   map[string]string
	Revision int64
}

func (c *Client) ReadAppSettings(ctx context.Context) (AppSettingsSnapshot, error) {
	ctx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	resp, err := c.api.ListAppSettings(ctx, &dbv1.ListAppSettingsRequest{})
	if err != nil {
		return AppSettingsSnapshot{}, mapErr(err)
	}
	out := AppSettingsSnapshot{Values: make(map[string]string, len(resp.GetSettings())), Revision: resp.GetRevision()}
	for _, setting := range resp.GetSettings() {
		out.Values[setting.GetKey()] = setting.GetValue()
	}
	return out, nil
}

// ApplyAppSettings отправляет один атомарный пакет с ожидаемой ревизией.
func (c *Client) ApplyAppSettings(ctx context.Context, expected int64, actor string, changes map[string]*string) (int64, error) {
	ctx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	req := &dbv1.ApplyAppSettingsRequest{ExpectedRevision: expected, ActorId: actor}
	for key, value := range changes {
		req.Changes = append(req.Changes, &dbv1.AppSettingChange{Key: key, Value: value})
	}
	resp, err := c.api.ApplyAppSettings(ctx, req)
	if err != nil {
		return 0, mapErr(err)
	}
	return resp.GetRevision(), nil
}
