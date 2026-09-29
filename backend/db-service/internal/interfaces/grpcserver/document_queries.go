package grpcserver

import (
	"context"
	"encoding/json"
	"github.com/example/db-service/internal/domain"
	"github.com/example/db-service/internal/platform/contracts"
	dbv1 "github.com/example/db-service/internal/platform/genproto/db/v1"
)

type DocumentQueries struct {
	dbv1.UnimplementedDocumentQueriesServer
	repo domain.DocumentQueries
}

func NewDocumentQueries(repo domain.DocumentQueries) *DocumentQueries {
	return &DocumentQueries{repo: repo}
}
func documentQuery(in *dbv1.DocumentPageRequest) contracts.DocumentQuery {
	return contracts.DocumentQuery{ReaderID: in.GetReaderId(), AllOwners: in.GetAllOwners(), Query: in.GetQuery(),
		Status: in.GetStatus(), FileType: in.GetFileType(), BeforeID: in.GetBeforeId(), Limit: int(
			in.GetLimit(),
		), Offset: int(
			in.GetOffset(),
		)}
}

func (s *DocumentQueries) Page(ctx context.Context, in *dbv1.DocumentPageRequest) (*dbv1.DocumentPageResponse, error) {
	p, err := s.repo.Page(ctx, documentQuery(in))
	if err != nil {
		return nil, toStatus(err)
	}
	out := &dbv1.DocumentPageResponse{Total: p.Total, NextCursor: p.NextCursor}
	for _, d := range p.Documents {
		out.Documents = append(out.Documents, documentPB(d))
	}
	return out, nil
}

func (s *DocumentQueries) Stats(ctx context.Context, in *dbv1.DocumentPageRequest) (*dbv1.DocumentStatsResponse, error) {
	stats, err := s.repo.Stats(ctx, documentQuery(in))
	if err != nil {
		return nil, toStatus(err)
	}
	raw, err := json.Marshal(stats)
	if err != nil {
		return nil, toStatus(err)
	}
	return &dbv1.DocumentStatsResponse{Json: raw}, nil
}
