package grpcx

import (
	"context"
	"fmt"
	"net"
	"time"

	"github.com/example/office-parser/internal/platform/contracts"
	"github.com/rs/zerolog"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/credentials"
	"google.golang.org/grpc/health"
	"google.golang.org/grpc/keepalive"
	"google.golang.org/grpc/metadata"
	"google.golang.org/grpc/status"

	"github.com/example/office-parser/internal/platform/logger"

	healthpb "google.golang.org/grpc/health/grpc_health_v1"
)

type Server struct {
	initErr error
	gs      *grpc.Server
	log     zerolog.Logger
}

func NewServer(log zerolog.Logger) *Server {
	conf, initErr := transportTLS()
	options := []grpc.ServerOption{
		grpc.ChainUnaryInterceptor(
			recoveryInterceptor(log),
			loggingInterceptor(log),
			authorizationInterceptor(conf.tls != nil),
		),
		grpc.ChainStreamInterceptor(streamAuthorizationInterceptor(conf.tls != nil)),
		grpc.MaxRecvMsgSize(maxMsgSize),
		grpc.MaxSendMsgSize(maxMsgSize),
		// Принимаем пинги поддержания связи от клиента (раз в 30 с), в том числе на простаивающих соединениях.
		grpc.KeepaliveEnforcementPolicy(keepalive.EnforcementPolicy{
			MinTime:             10 * time.Second,
			PermitWithoutStream: true,
		}),
		grpc.KeepaliveParams(keepalive.ServerParameters{
			Time:    30 * time.Second,
			Timeout: 10 * time.Second,
		}),
	}
	if conf.tls != nil {
		options = append(options, grpc.Creds(credentials.NewTLS(conf.tls)))
	}
	gs := grpc.NewServer(options...)
	hs := health.NewServer()
	hs.SetServingStatus("", healthpb.HealthCheckResponse_SERVING)
	healthpb.RegisterHealthServer(gs, hs)
	return &Server{gs: gs, log: log, initErr: initErr}
}

func (s *Server) Registrar() grpc.ServiceRegistrar { return s.gs }

func (s *Server) Run(ctx context.Context, addr string) error {
	if s.initErr != nil {
		return s.initErr
	}
	var lc net.ListenConfig
	lis, err := lc.Listen(ctx, "tcp", addr)
	if err != nil {
		return fmt.Errorf("grpc listen %s: %w", addr, err)
	}
	go func() {
		<-ctx.Done()
		s.gs.GracefulStop()
	}()
	s.log.Info().Str("addr", addr).Msg("grpc server listening")
	if err := s.gs.Serve(lis); err != nil {
		return fmt.Errorf("grpc serve: %w", err)
	}
	return nil
}

func recoveryInterceptor(log zerolog.Logger) grpc.UnaryServerInterceptor {
	return func(
		ctx context.Context, req any, info *grpc.UnaryServerInfo, handler grpc.UnaryHandler,
	) (resp any, err error) {
		defer func() {
			if r := recover(); r != nil {
				log.Error().Interface("panic", r).Str("method", info.FullMethod).Msg("recovered panic")
				err = status.Error(codes.Internal, "internal error")
			}
		}()
		return handler(ctx, req)
	}
}

func loggingInterceptor(log zerolog.Logger) grpc.UnaryServerInterceptor {
	return func(
		ctx context.Context, req any, info *grpc.UnaryServerInfo, handler grpc.UnaryHandler,
	) (any, error) {
		start := time.Now()
		// Логгер сервера кладётся в контекст вызова, чтобы logger.From(ctx) в обработчиках наследовал тег сервиса.
		if md, ok := metadata.FromIncomingContext(ctx); ok {
			lease := contracts.HypothesisLease{}
			if values := md.Get("x-hypothesis-job"); len(values) == 1 {
				lease.ID = values[0]
			}
			if values := md.Get("x-hypothesis-token"); len(values) == 1 {
				lease.Token = values[0]
			}
			if values := md.Get("x-hypothesis-step"); len(values) == 1 {
				lease.Step = values[0]
			}
			if lease.ID != "" {
				ctx = contracts.WithHypothesisLease(ctx, lease)
			}
		}
		resp, err := handler(logger.Into(ctx, log), req)
		log.Info().
			Str("method", info.FullMethod).
			Str("code", status.Code(err).String()).
			Dur("took", time.Since(start)).
			Msg("grpc call")
		return resp, err
	}
}
