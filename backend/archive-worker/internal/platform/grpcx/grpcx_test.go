package grpcx_test

import (
	"context"
	"net"
	"testing"
	"time"

	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"

	"github.com/example/archive-worker/internal/platform/grpcx"
	"github.com/example/archive-worker/internal/platform/logger"

	healthpb "google.golang.org/grpc/health/grpc_health_v1"
)

func startServer(t *testing.T, register func(grpc.ServiceRegistrar)) string {
	t.Helper()
	var lc net.ListenConfig
	lis, err := lc.Listen(context.Background(), "tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("listen: %v", err)
	}
	addr := lis.Addr().String()
	if cerr := lis.Close(); cerr != nil {
		t.Fatalf("close probe listener: %v", cerr)
	}

	srv := grpcx.NewServer(logger.New("test", "error"))
	if register != nil {
		register(srv.Registrar())
	}

	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() { done <- srv.Run(ctx, addr) }()

	t.Cleanup(func() {
		cancel()
		select {
		case err := <-done:
			if err != nil {
				t.Errorf("Run returned error on shutdown: %v", err)
			}
		case <-time.After(10 * time.Second):
			t.Error("Run did not return after context cancel")
		}
	})
	return addr
}

func dialReady(t *testing.T, addr string) *grpc.ClientConn {
	t.Helper()
	conn, err := grpcx.Dial(addr)
	if err != nil {
		t.Fatalf("Dial: %v", err)
	}
	t.Cleanup(func() { _ = conn.Close() })
	conn.Connect()

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	for {
		s := conn.GetState()
		if s.String() == "READY" {
			return conn
		}
		if !conn.WaitForStateChange(ctx, s) {
			t.Fatalf("connection never became ready (last state %s)", s)
		}
	}
}

func TestServerHealthServing(t *testing.T) {
	addr := startServer(t, nil)
	conn := dialReady(t, addr)

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	resp, err := healthpb.NewHealthClient(conn).Check(ctx, &healthpb.HealthCheckRequest{Service: ""})
	if err != nil {
		t.Fatalf("health Check: %v", err)
	}
	if resp.GetStatus() != healthpb.HealthCheckResponse_SERVING {
		t.Fatalf("health status = %v, want SERVING", resp.GetStatus())
	}
}

func TestRecoveryInterceptor(t *testing.T) {
	desc := &grpc.ServiceDesc{
		ServiceName: "test.Panic",
		HandlerType: (*any)(nil),
		Methods: []grpc.MethodDesc{{
			MethodName: "Boom",
			Handler: func(_ any, _ context.Context, dec func(any) error, _ grpc.UnaryServerInterceptor) (any, error) {
				if err := dec(nil); err != nil {
					return nil, err
				}
				panic("handler exploded")
			},
		}},
		Metadata: "test",
	}
	addr := startServer(t, func(r grpc.ServiceRegistrar) {
		r.RegisterService(desc, struct{}{})
	})
	conn := dialReady(t, addr)

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	err := conn.Invoke(ctx, "/test.Panic/Boom", &healthpb.HealthCheckRequest{}, &healthpb.HealthCheckResponse{})
	if status.Code(err) != codes.Internal {
		t.Fatalf("Invoke code = %v, want Internal", status.Code(err))
	}
}

func TestLoggingInterceptorPassesThrough(t *testing.T) {
	desc := &grpc.ServiceDesc{
		ServiceName: "test.Echo",
		HandlerType: (*any)(nil),
		Methods: []grpc.MethodDesc{{
			MethodName: "Ping",
			Handler: func(_ any, ctx context.Context, dec func(any) error, _ grpc.UnaryServerInterceptor) (any, error) {
				var in healthpb.HealthCheckRequest
				if err := dec(&in); err != nil {
					return nil, err
				}
				logger.From(ctx).Info().Msg("handled")
				return &healthpb.HealthCheckResponse{Status: healthpb.HealthCheckResponse_SERVING}, nil
			},
		}},
		Metadata: "test",
	}
	addr := startServer(t, func(r grpc.ServiceRegistrar) {
		r.RegisterService(desc, struct{}{})
	})
	conn := dialReady(t, addr)

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	var out healthpb.HealthCheckResponse
	if err := conn.Invoke(ctx, "/test.Echo/Ping", &healthpb.HealthCheckRequest{Service: "x"}, &out); err != nil {
		t.Fatalf("Invoke: %v", err)
	}
	if out.GetStatus() != healthpb.HealthCheckResponse_SERVING {
		t.Fatalf("response status = %v, want SERVING", out.GetStatus())
	}
}

func TestRegistrarReturnsServer(t *testing.T) {
	srv := grpcx.NewServer(logger.New("test", "error"))
	if srv.Registrar() == nil {
		t.Fatal("Registrar returned nil")
	}
	srv.Registrar().RegisterService(&grpc.ServiceDesc{
		ServiceName: "test.Reg",
		HandlerType: (*any)(nil),
		Metadata:    "test",
	}, struct{}{})
}

func TestRunListenError(t *testing.T) {
	srv := grpcx.NewServer(logger.New("test", "error"))
	if err := srv.Run(context.Background(), "256.256.256.256:0"); err == nil {
		t.Fatal("Run on an invalid address should return an error")
	}
}

func TestDialBadTarget(t *testing.T) {
	if _, err := grpcx.Dial("\x00://bad"); err == nil {
		t.Fatal("Dial of a malformed target should return an error")
	}
}

func TestDialLazyConnect(t *testing.T) {
	conn, err := grpcx.Dial("127.0.0.1:0")
	if err != nil {
		t.Fatalf("Dial: %v", err)
	}
	if err := conn.Close(); err != nil {
		t.Fatalf("Close: %v", err)
	}
}
