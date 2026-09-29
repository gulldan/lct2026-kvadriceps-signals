package grpcx

import (
	"context"
	"fmt"
	"github.com/example/auth/internal/platform/contracts"
	"google.golang.org/grpc/metadata"
	"time"

	"google.golang.org/grpc"
	"google.golang.org/grpc/keepalive"
)

// maxMsgSize действует в обе стороны: стандартные 4 МиБ ломают DocumentChunks на больших
// многостраничных документах, а 64 МиБ всё ещё ограничивают память.
const maxMsgSize = 64 << 20

// Повтор разрешён только для явно перечисленных чтений. Ошибка ответа на запись
// не доказывает, что сервер не успел её выполнить.
const retryServiceConfig = `{
  "methodConfig": [{
    "name": [
      {"service":"rag.db.v1.DbService","method":"GetDocument"},
      {"service":"rag.db.v1.DbService","method":"FindDocumentByHash"},
      {"service":"rag.db.v1.DbService","method":"ListDocuments"},
      {"service":"rag.db.v1.DbService","method":"GetChat"},
      {"service":"rag.db.v1.DbService","method":"ListChats"},
      {"service":"rag.db.v1.DbService","method":"ListMessages"},
      {"service":"rag.db.v1.DbService","method":"GetHypothesis"},
      {"service":"rag.db.v1.DbService","method":"ListHypotheses"},
      {"service":"rag.db.v1.DbService","method":"GetKPI"},
      {"service":"rag.db.v1.DbService","method":"GetCluster"}
    ],
    "retryPolicy": {
      "maxAttempts": 5,
      "initialBackoff": "0.2s",
      "maxBackoff": "5s",
      "backoffMultiplier": 2.0,
      "retryableStatusCodes": ["UNAVAILABLE"]
    }
  }]
}`

func Dial(addr string) (*grpc.ClientConn, error) {
	transport, err := clientTransport()
	if err != nil {
		return nil, err
	}
	conn, err := grpc.NewClient(
		addr,
		grpc.WithTransportCredentials(transport),
		grpc.WithDefaultServiceConfig(retryServiceConfig),
		grpc.WithChainUnaryInterceptor(hypothesisLeaseClient),
		// Пингуем простаивающие соединения, чтобы молча оборванный TCP (NAT, таймаут простоя) обнаружился
		// и переподключился, а не уронил следующий RPC.
		grpc.WithKeepaliveParams(keepalive.ClientParameters{
			Time:                30 * time.Second,
			Timeout:             10 * time.Second,
			PermitWithoutStream: true,
		}),
		grpc.WithDefaultCallOptions(
			grpc.MaxCallRecvMsgSize(maxMsgSize),
			grpc.MaxCallSendMsgSize(maxMsgSize),
		),
	)
	if err != nil {
		return nil, fmt.Errorf("grpc dial %s: %w", addr, err)
	}
	return conn, nil
}

func hypothesisLeaseClient(
	ctx context.Context,
	method string,
	req, reply any,
	cc *grpc.ClientConn,
	invoke grpc.UnaryInvoker,
	opts ...grpc.CallOption,
) error {
	lease := contracts.HypothesisLeaseFrom(ctx)
	if lease.ID != "" {
		ctx = metadata.AppendToOutgoingContext(
			ctx,
			"x-hypothesis-job",
			lease.ID,
			"x-hypothesis-token",
			lease.Token,
			"x-hypothesis-step",
			lease.Step,
		)
	}
	return invoke(ctx, method, req, reply, cc, opts...)
}
