package dbclient

import (
	"context"
	"errors"
	"fmt"

	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"

	"github.com/example/main-service/internal/platform/contracts"
	"github.com/example/main-service/internal/platform/grpcx"

	commonv1 "github.com/example/main-service/internal/platform/genproto/common/v1"
	dbv1 "github.com/example/main-service/internal/platform/genproto/db/v1"
	healthpb "google.golang.org/grpc/health/grpc_health_v1"
)

var ErrNotFound = errors.New("dbclient: not found")

type Client struct {
	conn *grpc.ClientConn
	api  dbv1.DbServiceClient
}

func New(addr string) (*Client, error) {
	conn, err := grpcx.Dial(addr)
	if err != nil {
		return nil, fmt.Errorf("dial db-service: %w", err)
	}
	return &Client{conn: conn, api: dbv1.NewDbServiceClient(conn)}, nil
}

func (c *Client) Close() error {
	if err := c.conn.Close(); err != nil {
		return fmt.Errorf("close db connection: %w", err)
	}
	return nil
}

func (c *Client) Ping(ctx context.Context) error {
	if _, err := healthpb.NewHealthClient(c.conn).Check(ctx, &healthpb.HealthCheckRequest{}); err != nil {
		return fmt.Errorf("db-service health: %w", err)
	}
	return nil
}

func mapErr(err error) error {
	if err == nil {
		return nil
	}
	if status.Code(err) == codes.NotFound {
		return ErrNotFound
	}
	return err
}

// GetUserByUsername возвращает пользователя вместе с хэшем пароля.
func (c *Client) GetUserByUsername(ctx context.Context, username string) (*commonv1.User, error) {
	u, err := c.api.GetUserByUsername(ctx, &dbv1.GetUserByUsernameRequest{Username: username})
	return u, mapErr(err)
}

func (c *Client) CreateUser(ctx context.Context, username, passwordHash string, roles []string) (*commonv1.User, error) {
	u, err := c.api.CreateUser(ctx, &dbv1.CreateUserRequest{
		Username: username, PasswordHash: passwordHash, Roles: roles,
	})
	return u, mapErr(err)
}

// ListUsers никогда не заполняет хэши паролей.
func (c *Client) ListUsers(ctx context.Context) ([]*commonv1.User, error) {
	resp, err := c.api.ListUsers(ctx, &dbv1.ListUsersRequest{})
	if err != nil {
		return nil, mapErr(err)
	}
	return resp.GetUsers(), nil
}

func (c *Client) CreateDocument(ctx context.Context, req *dbv1.CreateDocumentRequest) (*commonv1.Document, error) {
	d, err := c.api.CreateDocument(ctx, req)
	return d, mapErr(err)
}

// FindDocumentByHash ищет только среди документов владельца, чтобы дедупликация не связала
// загрузку одного арендатора с чужим документом.
func (c *Client) FindDocumentByHash(ctx context.Context, ownerID, hash string) (*commonv1.Document, error) {
	d, err := c.api.FindDocumentByHash(ctx, &dbv1.FindDocumentByHashRequest{OwnerId: ownerID, ContentHash: hash})
	return d, mapErr(err)
}

func (c *Client) GetDocument(ctx context.Context, id string) (*commonv1.Document, error) {
	d, err := c.api.GetDocument(ctx, &dbv1.GetDocumentRequest{Id: id})
	return d, mapErr(err)
}

func (c *Client) ListDocuments(
	ctx context.Context,
	ownerID string,
	options ...contracts.DocumentScan,
) ([]*commonv1.Document, error) {
	var opt contracts.DocumentScan
	if len(options) > 0 {
		opt = options[0]
	}
	resp, err := c.api.ListDocuments(ctx, &dbv1.ListDocumentsRequest{OwnerId: ownerID,
		BeforeId: opt.BeforeID, Status: opt.Status, Kind: opt.Kind, Limit: int32(opt.Limit)})
	if err != nil {
		return nil, mapErr(err)
	}
	return resp.GetDocuments(), nil
}

func (c *Client) UpdateDocumentStatus(ctx context.Context, id, status, message string, chunkCount *int32) error {
	_, err := c.api.UpdateDocumentStatus(ctx, &dbv1.UpdateDocumentStatusRequest{
		Id: id, Status: status, Message: message, ChunkCount: chunkCount, Generation: contracts.DocumentGeneration(ctx),
	})
	return mapErr(err)
}

func (c *Client) SetDocumentTitle(ctx context.Context, id, title string) error {
	_, err := c.api.SetDocumentTitle(ctx, &dbv1.SetDocumentTitleRequest{Id: id, Title: title})
	return mapErr(err)
}

func (c *Client) SetDocumentKind(ctx context.Context, id, kind string) error {
	_, err := c.api.SetDocumentKind(ctx, &dbv1.SetDocumentKindRequest{Id: id, Kind: kind})
	return mapErr(err)
}

func (c *Client) SetDocumentMeta(ctx context.Context, id, author, publishedAt, sourceRef string) error {
	_, err := c.api.SetDocumentMeta(ctx, &dbv1.SetDocumentMetaRequest{
		Id: id, Author: author, PublishedAt: publishedAt, SourceRef: sourceRef,
	})
	return mapErr(err)
}

// CreateChat: source — страница, с которой начат разговор.
func (c *Client) CreateChat(ctx context.Context, ownerID, title, source string) (*commonv1.Chat, error) {
	ch, err := c.api.CreateChat(ctx, &dbv1.CreateChatRequest{OwnerId: ownerID, Title: title, Source: source})
	return ch, mapErr(err)
}

func (c *Client) GetChat(ctx context.Context, id string) (*commonv1.Chat, error) {
	ch, err := c.api.GetChat(ctx, &dbv1.GetChatRequest{Id: id})
	return ch, mapErr(err)
}

// ListChats: limit <= 0 — весь список; allOwners — история всех пользователей для администратора.
func (c *Client) ListChats(
	ctx context.Context, ownerID string, limit, offset int, allOwners bool,
) ([]*commonv1.Chat, int64, error) {
	resp, err := c.api.ListChats(ctx, &dbv1.ListChatsRequest{
		OwnerId: ownerID, Limit: int32(limit), Offset: int32(offset), AllOwners: allOwners,
	})
	if err != nil {
		return nil, 0, mapErr(err)
	}
	return resp.GetChats(), resp.GetTotal(), nil
}

// AddMessage: meta — сырой JSON происхождения ({model, cached, trace}), у сообщений пользователя пустой.
func (c *Client) AddMessage(
	ctx context.Context, chatID, role, content string, sources []*commonv1.Source, meta string,
) (*commonv1.Message, error) {
	m, err := c.api.AddMessage(ctx, &dbv1.AddMessageRequest{
		ChatId: chatID, Role: role, Content: content, Sources: sources, Meta: meta,
	})
	return m, mapErr(err)
}

func (c *Client) ListMessages(ctx context.Context, chatID string) ([]*commonv1.Message, error) {
	resp, err := c.api.ListMessages(ctx, &dbv1.ListMessagesRequest{ChatId: chatID})
	if err != nil {
		return nil, mapErr(err)
	}
	return resp.GetMessages(), nil
}

func (c *Client) ListModels(ctx context.Context) ([]*commonv1.Model, error) {
	resp, err := c.api.ListModels(ctx, &dbv1.ListModelsRequest{})
	if err != nil {
		return nil, mapErr(err)
	}
	return resp.GetModels(), nil
}

func (c *Client) CreateKPI(ctx context.Context, req *dbv1.CreateKPIRequest) (*commonv1.KPI, error) {
	k, err := c.api.CreateKPI(ctx, req)
	return k, mapErr(err)
}

func (c *Client) GetKPI(ctx context.Context, id string) (*commonv1.KPI, error) {
	k, err := c.api.GetKPI(ctx, &dbv1.GetKPIRequest{Id: id})
	return k, mapErr(err)
}

func (c *Client) ListKPIs(ctx context.Context, ownerID string) ([]*commonv1.KPI, error) {
	resp, err := c.api.ListKPIs(ctx, &dbv1.ListKPIsRequest{OwnerId: ownerID})
	if err != nil {
		return nil, mapErr(err)
	}
	return resp.GetKpis(), nil
}

func (c *Client) SetLLMUsageDaily(
	ctx context.Context, day, model, operation string,
	requests, promptTokens, completionTokens, costNanoUSD int64,
) error {
	_, err := c.api.SetLLMUsageDaily(ctx, &dbv1.SetLLMUsageDailyRequest{Row: &dbv1.LLMUsageDailyRow{
		Day:              day,
		Model:            model,
		Operation:        operation,
		Requests:         requests,
		PromptTokens:     promptTokens,
		CompletionTokens: completionTokens,
		CostNanoUsd:      costNanoUSD,
	}})
	if err != nil {
		return mapErr(err)
	}
	return nil
}

// ListLLMUsageDaily: обе границы диапазона дней включительно.
func (c *Client) ListLLMUsageDaily(ctx context.Context, fromDay, toDay string) ([]*dbv1.LLMUsageDailyRow, error) {
	resp, err := c.api.ListLLMUsageDaily(ctx, &dbv1.ListLLMUsageDailyRequest{FromDay: fromDay, ToDay: toDay})
	if err != nil {
		return nil, mapErr(err)
	}
	return resp.GetRows(), nil
}

func (c *Client) UpdateKPI(ctx context.Context, kpi *commonv1.KPI) error {
	_, err := c.api.UpdateKPI(ctx, &dbv1.UpdateKPIRequest{Kpi: kpi})
	return mapErr(err)
}

// DeleteKPI удаляет цель, а связанные гипотезы остаются с обнулённым kpi_id.
func (c *Client) DeleteKPI(ctx context.Context, id string) error {
	_, err := c.api.DeleteKPI(ctx, &dbv1.DeleteKPIRequest{Id: id})
	return mapErr(err)
}

// AttachKPIDocuments передаёт аутентифицированного читателя и весь атомарный набор.
func (c *Client) AttachKPIDocuments(ctx context.Context, readerID, kpiID string, documentIDs []string, role string) error {
	_, err := c.api.AttachKpiDocuments(ctx, &dbv1.AttachKpiDocumentsRequest{
		ReaderId: readerID, KpiId: kpiID, DocumentIds: documentIDs, Role: role,
	})
	return mapErr(err)
}

func (c *Client) ListKPIDocuments(ctx context.Context, readerID, kpiID string) ([]*dbv1.KpiDocumentLink, error) {
	resp, err := c.api.ListKpiDocuments(ctx, &dbv1.ListKpiDocumentsRequest{KpiId: kpiID, ReaderId: readerID})
	if err != nil {
		return nil, mapErr(err)
	}
	return resp.GetLinks(), nil
}

func (c *Client) DetachKPIDocument(ctx context.Context, kpiID, documentID string) error {
	_, err := c.api.DetachKpiDocument(ctx, &dbv1.DetachKpiDocumentRequest{KpiId: kpiID, DocumentId: documentID})
	return mapErr(err)
}

func (c *Client) CreateCluster(ctx context.Context, req *dbv1.CreateClusterRequest) (*commonv1.Cluster, error) {
	cl, err := c.api.CreateCluster(ctx, req)
	return cl, mapErr(err)
}

func (c *Client) GetCluster(ctx context.Context, id string) (*commonv1.Cluster, error) {
	cl, err := c.api.GetCluster(ctx, &dbv1.GetClusterRequest{Id: id})
	return cl, mapErr(err)
}

func (c *Client) ListClusters(ctx context.Context, ownerID string) ([]*commonv1.Cluster, error) {
	resp, err := c.api.ListClusters(ctx, &dbv1.ListClustersRequest{OwnerId: ownerID})
	if err != nil {
		return nil, mapErr(err)
	}
	return resp.GetClusters(), nil
}

func (c *Client) UpdateCluster(ctx context.Context, cluster *commonv1.Cluster) error {
	_, err := c.api.UpdateCluster(ctx, &dbv1.UpdateClusterRequest{Cluster: cluster})
	return mapErr(err)
}

func (c *Client) DeleteClusters(ctx context.Context, ownerID string) error {
	_, err := c.api.DeleteClusters(ctx, &dbv1.DeleteClustersRequest{OwnerId: ownerID})
	return mapErr(err)
}

func (c *Client) CreateHypothesis(
	ctx context.Context, req *dbv1.CreateHypothesisRequest,
) (*commonv1.Hypothesis, error) {
	h, err := c.api.CreateHypothesis(ctx, req)
	return h, mapErr(err)
}

func (c *Client) GetHypothesis(ctx context.Context, id string) (*commonv1.Hypothesis, error) {
	h, err := c.api.GetHypothesis(ctx, &dbv1.GetHypothesisRequest{Id: id})
	return h, mapErr(err)
}

func (c *Client) ListHypotheses(
	ctx context.Context, req *dbv1.ListHypothesesRequest,
) ([]*commonv1.Hypothesis, error) {
	resp, err := c.api.ListHypotheses(ctx, req)
	if err != nil {
		return nil, mapErr(err)
	}
	return resp.GetHypotheses(), nil
}

func (c *Client) UpdateHypothesis(ctx context.Context, req *dbv1.UpdateHypothesisRequest) error {
	_, err := c.api.UpdateHypothesis(ctx, req)
	return mapErr(err)
}

func (c *Client) AddHypothesisRevision(
	ctx context.Context, rev *commonv1.HypothesisRevision,
) (*commonv1.HypothesisRevision, error) {
	r, err := c.api.AddHypothesisRevision(ctx, &dbv1.AddHypothesisRevisionRequest{Revision: rev})
	return r, mapErr(err)
}

func (c *Client) ListHypothesisRevisions(
	ctx context.Context, hypothesisID string,
) ([]*commonv1.HypothesisRevision, error) {
	resp, err := c.api.ListHypothesisRevisions(ctx, &dbv1.ListHypothesisRevisionsRequest{HypothesisId: hypothesisID})
	if err != nil {
		return nil, mapErr(err)
	}
	return resp.GetRevisions(), nil
}

func (c *Client) ListHypothesisEvidence(
	ctx context.Context, hypothesisID string,
) ([]*commonv1.HypothesisEvidence, error) {
	resp, err := c.api.ListHypothesisEvidence(ctx, &dbv1.ListHypothesisEvidenceRequest{HypothesisId: hypothesisID})
	if err != nil {
		return nil, mapErr(err)
	}
	return resp.GetEvidence(), nil
}
