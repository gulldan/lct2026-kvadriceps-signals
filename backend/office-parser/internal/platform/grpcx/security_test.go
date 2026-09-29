package grpcx

import (
	"context"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/pem"
	"math/big"
	"net"
	"net/url"
	"os"
	"path/filepath"
	"testing"
	"time"

	commonv1 "github.com/example/office-parser/internal/platform/genproto/common/v1"
	dbv1 "github.com/example/office-parser/internal/platform/genproto/db/v1"
	"github.com/rs/zerolog"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/credentials/insecure"
	"google.golang.org/grpc/status"
)

type identityProbe struct {
	dbv1.UnimplementedDbServiceServer
}

func (identityProbe) GetDocument(context.Context, *dbv1.GetDocumentRequest) (*commonv1.Document, error) {
	return &commonv1.Document{Id: "allowed"}, nil
}
func (identityProbe) CreateUser(context.Context, *dbv1.CreateUserRequest) (*commonv1.User, error) {
	return &commonv1.User{Id: "allowed"}, nil
}

func TestMTLSChecksPeerIdentityAndRPCPrivilege(t *testing.T) {
	dir := t.TempDir()
	caKey, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	ca := &x509.Certificate{SerialNumber: big.NewInt(
		1,
	), Subject: pkix.Name{
		CommonName: "test CA",
	}, IsCA: true, BasicConstraintsValid: true,
		NotBefore: time.Now().Add(-time.Minute), NotAfter: time.Now().Add(time.Hour), KeyUsage: x509.KeyUsageCertSign}
	der, err := x509.CreateCertificate(rand.Reader, ca, ca, &caKey.PublicKey, caKey)
	if err != nil {
		t.Fatal(err)
	}
	caPath := filepath.Join(dir, "ca.pem")
	if err = os.WriteFile(caPath, pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: der}), 0600); err != nil {
		t.Fatal(err)
	}
	configure := func(id string) {
		t.Helper()
		key, e := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
		if e != nil {
			t.Fatal(e)
		}
		uri, e := url.Parse("spiffe://rag.local/service/" + id)
		if e != nil {
			t.Fatal(e)
		}
		leaf := &x509.Certificate{SerialNumber: big.NewInt(time.Now().UnixNano()), Subject: pkix.Name{CommonName: id},
			NotBefore: time.Now().Add(-time.Minute), NotAfter: time.Now().Add(time.Hour),
			DNSNames: []string{id}, IPAddresses: []net.IP{net.ParseIP("127.0.0.1")}, URIs: []*url.URL{uri},
			KeyUsage: x509.KeyUsageDigitalSignature, ExtKeyUsage: []x509.ExtKeyUsage{
				x509.ExtKeyUsageServerAuth,
				x509.ExtKeyUsageClientAuth,
			}}
		signed, e := x509.CreateCertificate(rand.Reader, leaf, ca, &key.PublicKey, caKey)
		if e != nil {
			t.Fatal(e)
		}
		private, e := x509.MarshalPKCS8PrivateKey(key)
		if e != nil {
			t.Fatal(e)
		}
		certPath, keyPath := filepath.Join(dir, id+".pem"), filepath.Join(dir, id+".key")
		for path, block := range map[string]*pem.Block{
			certPath: {
				Type:  "CERTIFICATE",
				Bytes: signed,
			},
			keyPath: {
				Type:  "PRIVATE KEY",
				Bytes: private,
			},
		} {
			if e = os.WriteFile(path, pem.EncodeToMemory(block), 0600); e != nil {
				t.Fatal(e)
			}
		}
		t.Setenv("APP_ENV", "production")
		t.Setenv("GRPC_TLS_MODE", "mtls")
		t.Setenv("GRPC_TLS_CA", caPath)
		t.Setenv("GRPC_TLS_CERT", certPath)
		t.Setenv("GRPC_TLS_KEY", keyPath)
		t.Setenv("GRPC_SERVICE_ID", id)
	}
	configure("db-service")
	server := NewServer(zerolog.Nop())
	if server.initErr != nil {
		t.Fatal(server.initErr)
	}
	dbv1.RegisterDbServiceServer(server.Registrar(), identityProbe{})
	listener, err := (&net.ListenConfig{}).Listen(t.Context(), "tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	go func() { _ = server.gs.Serve(listener) }()
	t.Cleanup(server.gs.Stop)
	configure("pdf-parser")
	parser, err := Dial(listener.Addr().String())
	if err != nil {
		t.Fatal(err)
	}
	defer parser.Close()
	ctx, cancel := context.WithTimeout(t.Context(), 3*time.Second)
	defer cancel()
	api := dbv1.NewDbServiceClient(parser)
	if _, err = api.GetDocument(ctx, &dbv1.GetDocumentRequest{}); err != nil {
		t.Fatal(err)
	}
	if _, err = api.CreateUser(ctx, &dbv1.CreateUserRequest{}); status.Code(err) != codes.PermissionDenied {
		t.Fatalf("parser administrative RPC: %v", err)
	}
	configure("auth")
	auth, err := Dial(listener.Addr().String())
	if err != nil {
		t.Fatal(err)
	}
	defer auth.Close()
	if _, err = dbv1.NewDbServiceClient(auth).CreateUser(ctx, &dbv1.CreateUserRequest{}); err != nil {
		t.Fatal(err)
	}
	unauthenticated, err := grpc.NewClient(listener.Addr().String(), grpc.WithTransportCredentials(insecure.NewCredentials()))
	if err != nil {
		t.Fatal(err)
	}
	defer unauthenticated.Close()
	if _, err = dbv1.NewDbServiceClient(unauthenticated).CreateUser(ctx, &dbv1.CreateUserRequest{}); err == nil {
		t.Fatal("plaintext client accepted")
	}
	t.Setenv("GRPC_TLS_MODE", "")
	if _, err = Dial(listener.Addr().String()); err == nil {
		t.Fatal("production accepted plaintext configuration")
	}
	if err = NewServer(zerolog.Nop()).Run(ctx, "127.0.0.1:0"); err == nil {
		t.Fatal("production server accepted plaintext configuration")
	}
}
