package grpcserver

import (
	"context"
	"maps"
	"slices"

	dbv1 "github.com/example/db-service/internal/platform/genproto/db/v1"
)

func (s *Server) ListAppSettings(ctx context.Context, _ *dbv1.ListAppSettingsRequest) (*dbv1.ListAppSettingsResponse, error) {
	snapshot, err := s.svc.AppSettings(ctx)
	if err != nil {
		return nil, toStatus(err)
	}
	out := &dbv1.ListAppSettingsResponse{Revision: snapshot.Revision}
	for _, key := range slices.Sorted(maps.Keys(snapshot.Values)) {
		out.Settings = append(out.Settings, &dbv1.AppSetting{Key: key, Value: snapshot.Values[key]})
	}
	return out, nil
}

func (s *Server) ApplyAppSettings(
	ctx context.Context,
	req *dbv1.ApplyAppSettingsRequest,
) (*dbv1.ApplyAppSettingsResponse, error) {
	changes := make(map[string]*string, len(req.GetChanges()))
	for _, change := range req.GetChanges() {
		changes[change.GetKey()] = change.Value
	}
	revision, err := s.svc.ApplyAppSettings(ctx, req.GetExpectedRevision(), req.GetActorId(), changes)
	if err != nil {
		return nil, toStatus(err)
	}
	return &dbv1.ApplyAppSettingsResponse{Revision: revision}, nil
}
