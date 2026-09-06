// Package api serves the sslly-nginx control plane: a gRPC server plus a
// grpc-gateway HTTP/JSON façade (POST-only routes) over the same services.
// Mutating RPCs edit the YAML files (comment-preserving, atomic), run the
// validated reload pipeline synchronously, and report the outcome in-band;
// on failure the YAML is restored so the files always match the running
// nginx configuration.
package api

import (
	"context"
	"errors"
	"fmt"
	"net"
	"net/http"

	v1 "github.com/hnrobert/sslly-nginx/gen/hnrobert/sslly/v1"
	"github.com/hnrobert/sslly-nginx/internal/config"
	"github.com/hnrobert/sslly-nginx/internal/logger"
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"
	"google.golang.org/grpc/health"
	healthpb "google.golang.org/grpc/health/grpc_health_v1"
	"google.golang.org/grpc/reflection"

	"github.com/grpc-ecosystem/grpc-gateway/v2/runtime"
)

// Env-var names (documented in docs/API.md):
//
//	SSLLY_API_HTTP_ADDR   gateway JSON endpoint (default ":9080")
//	SSLLY_API_GRPC_ADDR   native gRPC endpoint   (default ":9081")
const (
	EnvHTTPAddr   = "SSLLY_API_HTTP_ADDR"
	EnvGRPCAddr   = "SSLLY_API_GRPC_ADDR"
	EnvAdminToken = "SSLLY_API_ADMIN_TOKEN"

	defaultHTTPAddr = ":9080"
	defaultGRPCAddr = ":9081"

	// APIPrefix is the URL prefix every gateway route is mounted under
	// (POST <APIPrefix>/<RpcName>). Bumping or changing it is a serving-layer
	// decision; the proto annotations stay bare RPC method names.
	APIPrefix = "/v1"
)

// Config wires the API server to its dependencies. Reload runs the validated
// reload pipeline (app.ApplyReload); CertDomains feeds protocol validation
// with the domains that have TLS certificates; Dial overrides the
// gateway->gRPC connection for tests (bufconn).
type Config struct {
	GRPCAddr    string
	HTTPAddr    string
	ConfigDir   string
	Reload      func() error
	Users       *config.UserStore
	Editor      *config.Editor
	CertDomains func() map[string]bool
	Dial        func(ctx context.Context, target string) (*grpc.ClientConn, error)
}

// Server is the control-plane dual stack.
type Server struct {
	cfg        Config
	grpcServer *grpc.Server
	conn       *grpc.ClientConn
	httpServer *http.Server
	httpLis    net.Listener
	grpcAddr   string
}

// New builds the gRPC server (interceptors + services + health + reflection).
// Call Start to listen, or ServeGRPC/GatewayHandler directly in tests.
func New(cfg Config) *Server {
	if cfg.GRPCAddr == "" {
		cfg.GRPCAddr = defaultGRPCAddr
	}
	if cfg.HTTPAddr == "" {
		cfg.HTTPAddr = defaultHTTPAddr
	}
	s := &Server{cfg: cfg}

	s.grpcServer = grpc.NewServer(grpc.ChainUnaryInterceptor(
		recoveryInterceptor,
		auditInterceptor,
		authInterceptor(cfg.Users),
	))
	v1.RegisterProxyServiceServer(s.grpcServer, &proxyService{srv: s})
	v1.RegisterCorsServiceServer(s.grpcServer, &corsService{srv: s})
	v1.RegisterLogsServiceServer(s.grpcServer, &logsService{srv: s})
	v1.RegisterUsersServiceServer(s.grpcServer, &usersService{srv: s})
	healthpb.RegisterHealthServer(s.grpcServer, health.NewServer())
	reflection.Register(s.grpcServer)
	return s
}

// Start brings up both listeners. It returns once both are serving.
func (s *Server) Start() error {
	lis, err := net.Listen("tcp", s.cfg.GRPCAddr)
	if err != nil {
		return fmt.Errorf("listen grpc %s: %w", s.cfg.GRPCAddr, err)
	}
	s.grpcAddr = lis.Addr().String()
	go func() {
		if err := s.grpcServer.Serve(lis); err != nil {
			logger.Error("API grpc serve stopped: %v", err)
		}
	}()

	conn, err := s.dialGRPC(context.Background(), s.grpcAddr)
	if err != nil {
		s.grpcServer.Stop()
		return fmt.Errorf("gateway dial grpc: %w", err)
	}
	s.conn = conn

	handler, err := s.gatewayHandler(context.Background(), conn)
	if err != nil {
		s.grpcServer.Stop()
		return fmt.Errorf("build gateway: %w", err)
	}

	s.httpLis, err = net.Listen("tcp", s.cfg.HTTPAddr)
	if err != nil {
		s.conn.Close()
		s.grpcServer.Stop()
		return fmt.Errorf("listen http %s: %w", s.cfg.HTTPAddr, err)
	}
	s.httpServer = &http.Server{Handler: handler}
	go func() {
		if err := s.httpServer.Serve(s.httpLis); err != nil && !errors.Is(err, http.ErrServerClosed) {
			logger.Error("API http serve stopped: %v", err)
		}
	}()

	logger.Info("API listening: http(json)=%s grpc=%s", s.httpLis.Addr().String(), s.grpcAddr)
	return nil
}

// ServeGRPC serves gRPC on the given listener (blocking). Test entrypoint.
func (s *Server) ServeGRPC(lis net.Listener) error {
	s.grpcAddr = lis.Addr().String()
	return s.grpcServer.Serve(lis)
}

func (s *Server) dialGRPC(ctx context.Context, target string) (*grpc.ClientConn, error) {
	if s.cfg.Dial != nil {
		return s.cfg.Dial(ctx, target)
	}
	return grpc.NewClient(target, grpc.WithTransportCredentials(insecure.NewCredentials()))
}

// gatewayHandler builds the HTTP façade: /healthz (unauthenticated) and the
// /v1/ gateway routes over the given gRPC connection.
func (s *Server) gatewayHandler(ctx context.Context, conn *grpc.ClientConn) (http.Handler, error) {
	gw := runtime.NewServeMux()
	if err := v1.RegisterProxyServiceHandler(ctx, gw, conn); err != nil {
		return nil, err
	}
	if err := v1.RegisterCorsServiceHandler(ctx, gw, conn); err != nil {
		return nil, err
	}
	if err := v1.RegisterLogsServiceHandler(ctx, gw, conn); err != nil {
		return nil, err
	}
	if err := v1.RegisterUsersServiceHandler(ctx, gw, conn); err != nil {
		return nil, err
	}

	mux := http.NewServeMux()
	mux.HandleFunc("/healthz", func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"status":"ok"}` + "\n"))
	})
	// The URL version prefix and routing namespace live HERE (the serving
	// layer), not in the proto annotations: protos only carry the bare RPC
	// method name as the path, and the gateway is mounted under the prefix.
	// Final routes: POST <APIPrefix>/<RpcName>, e.g. POST /v1/UpdateLogsConfig.
	mux.Handle(APIPrefix+"/", http.StripPrefix(APIPrefix, gw))
	return mux, nil
}

// Stop shuts the API down gracefully within ctx: HTTP first (stop taking new
// requests), then gRPC (drain in-flight RPCs).
func (s *Server) Stop(ctx context.Context) {
	if s.httpServer != nil {
		if err := s.httpServer.Shutdown(ctx); err != nil {
			logger.Error("API http shutdown: %v", err)
		}
	}
	if s.conn != nil {
		_ = s.conn.Close()
	}
	if s.grpcServer != nil {
		done := make(chan struct{})
		go func() {
			s.grpcServer.GracefulStop()
			close(done)
		}()
		select {
		case <-done:
		case <-ctx.Done():
			s.grpcServer.Stop()
		}
	}
}

// certDomains returns the certificate-domain set (nil-safe).
func (s *Server) certDomains() map[string]bool {
	if s.cfg.CertDomains == nil {
		return nil
	}
	return s.cfg.CertDomains()
}

// apply runs the post-write sanity gate and the validated reload pipeline.
// prev/file allow restoring the YAML when the pipeline rejects the change.
// A nil Reload means "no reload" (tests / embedded use) and always applies.
func (s *Server) apply(file string, prev []byte) *v1.CMsgApplyResult {
	failed := func(reason string) *v1.CMsgApplyResult {
		if err := s.cfg.Editor.RestoreFile(file, prev); err != nil {
			logger.Error("API failed to restore %s after rejected change: %v", file, err)
		}
		return &v1.CMsgApplyResult{Applied: false, Error: reason}
	}

	// Sanity gate: the written files must parse and validate before nginx
	// even sees them.
	cfg, err := config.Load(s.cfg.ConfigDir)
	if err == nil {
		_, errs, _ := config.ValidateConfig(cfg, s.certDomains())
		if len(errs) > 0 {
			err = errors.New(errs[0].Error())
		}
	}
	if err != nil {
		return failed("validation: " + err.Error())
	}

	if s.cfg.Reload == nil {
		return &v1.CMsgApplyResult{Applied: true}
	}
	if err := s.cfg.Reload(); err != nil {
		// The pipeline already rolled nginx back to the last good state;
		// realign the YAML source with it.
		return failed(err.Error())
	}
	return &v1.CMsgApplyResult{Applied: true}
}
