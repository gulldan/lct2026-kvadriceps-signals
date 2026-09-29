package grpcx

import (
	"context"
	"crypto/tls"
	"crypto/x509"
	"errors"
	"fmt"
	"os"
	"slices"
	"strings"

	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/credentials"
	"google.golang.org/grpc/credentials/insecure"
	"google.golang.org/grpc/peer"
	"google.golang.org/grpc/status"
)

type rpcTransport struct{ tls *tls.Config }

func transportTLS() (rpcTransport, error) {
	mode := os.Getenv("GRPC_TLS_MODE")
	if mode == "" || mode == "disabled" {
		if os.Getenv("APP_ENV") == "production" {
			return rpcTransport{}, errors.New("production requires GRPC_TLS_MODE=mtls")
		}
		return rpcTransport{}, nil
	}
	if mode != "mtls" {
		return rpcTransport{}, errors.New("unsupported GRPC_TLS_MODE")
	}
	ca, err := os.ReadFile(os.Getenv("GRPC_TLS_CA"))
	if err != nil {
		return rpcTransport{}, fmt.Errorf("read RPC CA: %w", err)
	}
	roots := x509.NewCertPool()
	if !roots.AppendCertsFromPEM(ca) {
		return rpcTransport{}, errors.New("invalid RPC CA")
	}
	certificate, err := tls.LoadX509KeyPair(os.Getenv("GRPC_TLS_CERT"), os.Getenv("GRPC_TLS_KEY"))
	if err != nil {
		return rpcTransport{}, fmt.Errorf("read RPC identity: %w", err)
	}
	leaf, err := x509.ParseCertificate(certificate.Certificate[0])
	if err != nil {
		return rpcTransport{}, fmt.Errorf("parse RPC certificate: %w", err)
	}
	identity := os.Getenv("GRPC_SERVICE_ID")
	if len(leaf.URIs) != 1 || !knownService(identity) || leaf.URIs[0].String() != "spiffe://rag.local/service/"+identity {
		return rpcTransport{}, errors.New("RPC certificate identity does not match configured service")
	}
	return rpcTransport{
		tls: &tls.Config{
			MinVersion:   tls.VersionTLS13,
			Certificates: []tls.Certificate{certificate},
			RootCAs:      roots,
			ClientCAs:    roots,
			ClientAuth:   tls.RequireAndVerifyClientCert,
		},
	}, nil
}

func clientTransport() (credentials.TransportCredentials, error) {
	conf, err := transportTLS()
	if err != nil {
		return nil, err
	}
	if conf.tls == nil {
		return insecure.NewCredentials(), nil
	}
	return credentials.NewTLS(conf.tls), nil
}

func knownService(id string) bool {
	return slices.Contains([]string{authIdentity, mainIdentity, "db-service", llmIdentity, chunkIdentity, "source-harvester",
		archiveIdentity, "pdf-parser", "office-parser", "email-parser", "db-parser", "ocr-service", "vlm-service",
		"signal-worker", "cluster-worker", "discovery-worker", "raptor-worker", "graph-compute"}, id)
}

func parserService(id string) bool {
	return slices.Contains(
		[]string{
			archiveIdentity,
			"pdf-parser",
			"office-parser",
			"email-parser",
			"db-parser",
			"ocr-service",
			"vlm-service",
		},
		id,
	)
}

const (
	llmIdentity     = "llm-service"
	chunkIdentity   = "chunk-splitter"
	parserGroup     = "parser"
	authIdentity    = "auth"
	mainIdentity    = "main-service"
	archiveIdentity = "archive-worker"
	dbMainRPCs      = `ListUsers CreateDocument GetDocument FindDocumentByHash ListDocuments UpdateDocumentStatus SetDocumentSource
 CreateChat GetChat ListChats AddMessage ListMessages ListModels CreateKPI GetKPI ListKPIs UpdateKPI DeleteKPI
 AttachKpiDocument AttachKpiDocuments ListKpiDocuments DetachKpiDocument CreateCluster GetClusterHead PublishClusters
 GetCluster ListClusters UpdateCluster DeleteClusters CreateHypothesis GetHypothesis ListHypotheses UpdateHypothesis
 AddHypothesisRevision ListHypothesisRevisions ListHypothesisEvidence SetLLMUsageDaily ListLLMUsageDaily
 ListAppSettings ApplyAppSettings`
)

type rpcGrant struct{ identity, methods string }

// rpcAllowed запрещает неизвестный метод, даже если сертификат доверенный.
func rpcAllowed(id, full string) bool {
	service, method, ok := strings.Cut(strings.TrimPrefix(full, "/"), "/")
	if !ok || !knownService(id) {
		return false
	}
	for _, grant := range serviceGrants(service) {
		matches := grant.identity == id || grant.identity == parserGroup && parserService(id) || grant.identity == "known"
		if matches && slices.Contains(strings.Fields(grant.methods), method) {
			return true
		}
	}
	return false
}
func serviceGrants(service string) []rpcGrant {
	switch service {
	case "grpc.health.v1.Health":
		return []rpcGrant{{"known", "Check Watch List"}}
	case "rag.db.v1.AuthSessions":
		return []rpcGrant{{authIdentity, "Create Rotate Revoke Check"}, {mainIdentity, "Check"}}
	case "rag.db.v1.DocumentQueries":
		return []rpcGrant{{mainIdentity, "Page Stats"}}
	case "rag.db.v1.ChatTurns":
		return []rpcGrant{{mainIdentity, "Begin Finish Fail"}}
	case "rag.db.v1.HypothesisJobs":
		return []rpcGrant{{mainIdentity, "Enqueue Get List Claim Renew Finish"}}
	case "rag.db.v1.IngestionControl":
		return []rpcGrant{{parserGroup, "QueueEvent"}, {chunkIdentity, "QueueEvent"}, {mainIdentity, "Requeue"}}
	case "rag.db.v1.DbService":
		return []rpcGrant{{authIdentity, "GetUserByUsername CreateUser"}, {mainIdentity, dbMainRPCs},
			{parserGroup, "GetDocument UpdateDocumentStatus SetDocumentTitle SetDocumentKind SetDocumentMeta"},
			{chunkIdentity, "GetDocument ListDocuments UpdateDocumentStatus SetDocumentTitle SetDocumentKind SetDocumentMeta"},
			{archiveIdentity, "CreateDocument"}, {llmIdentity, "GetDocument ListDocuments"}}
	case "rag.indexdoc.v1.IndexService":
		return []rpcGrant{{chunkIdentity, "Begin Renew Prepare Activate Finish Read"},
			{mainIdentity, "Read SetAccess"}, {llmIdentity, "Read"}}
	case "rag.harvest.v1.HarvestService":
		return []rpcGrant{{"source-harvester", "Start Quota CommitPage Pending Ack Finish Requested"},
			{mainIdentity, "Request Progress"}}
	case "rag.technology.v1.TechnologyService":
		return []rpcGrant{{mainIdentity, "ReadRevision Search ReadTechnology Review Reviews"},
			{llmIdentity, "ReadRevision Search ReadTechnology"}}
	case "rag.analysis.v1.AnalysisService":
		return []rpcGrant{
			{mainIdentity, "Freeze Read Submit ReadJob StartDescription ReadDescription ReadLiveSearch SaveLiveSearch ListLiveSearches"},
			{"signal-worker", "Claim Renew Validate Publish Fail Read"}, {llmIdentity, "ClaimDescription CompleteDescription"}}
	case "rag.llm.v1.RagService":
		return []rpcGrant{{mainIdentity, "Answer DocumentChunks"}, {chunkIdentity, "DocumentChunks"}}
	default:
		return nil
	}
}

func authorizeRPC(ctx context.Context, method string) error {
	p, ok := peer.FromContext(ctx)
	if !ok {
		return status.Error(codes.Unauthenticated, "service identity required")
	}
	info, ok := p.AuthInfo.(credentials.TLSInfo)
	if !ok || len(info.State.VerifiedChains) == 0 || len(info.State.PeerCertificates) == 0 {
		return status.Error(codes.Unauthenticated, "verified service identity required")
	}
	leaf := info.State.PeerCertificates[0]
	if len(leaf.URIs) != 1 {
		return status.Error(codes.PermissionDenied, "service identity denied")
	}
	id, ok := strings.CutPrefix(leaf.URIs[0].String(), "spiffe://rag.local/service/")
	if !ok || !rpcAllowed(id, method) {
		return status.Error(codes.PermissionDenied, "RPC is not allowed for this service")
	}
	return nil
}

func authorizationInterceptor(enabled bool) grpc.UnaryServerInterceptor {
	return func(ctx context.Context, req any, info *grpc.UnaryServerInfo, next grpc.UnaryHandler) (any, error) {
		if enabled {
			if err := authorizeRPC(ctx, info.FullMethod); err != nil {
				return nil, err
			}
		}
		return next(ctx, req)
	}
}
func streamAuthorizationInterceptor(enabled bool) grpc.StreamServerInterceptor {
	return func(srv any, stream grpc.ServerStream, info *grpc.StreamServerInfo, next grpc.StreamHandler) error {
		if enabled {
			if err := authorizeRPC(stream.Context(), info.FullMethod); err != nil {
				return err
			}
		}
		return next(srv, stream)
	}
}
