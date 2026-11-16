package api

import (
	"context"
	"time"

	v1 "github.com/hnrobert/sslly-nginx/gen/hnrobert/sslly/v1"
	"github.com/hnrobert/sslly-nginx/internal/logger"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

type authService struct {
	v1.UnimplementedAuthServiceServer
	srv *Server
}

// Authenticate exchanges (name, token) — the long-lived users.yaml
// credential — for a short-lived opaque session token. This is the ONLY RPC
// the long-lived token is good for; every other RPC requires the session.
func (a *authService) Authenticate(ctx context.Context, req *v1.AuthenticateRequest) (*v1.AuthenticateResponse, error) {
	if req.GetName() == "" || req.GetToken() == "" {
		return nil, status.Error(codes.InvalidArgument, "name and token are required")
	}
	u, err := a.srv.cfg.Users.VerifyCredentials(req.GetName(), req.GetToken())
	if err != nil {
		// Same error for unknown user and wrong token: no enumeration oracle.
		logger.Info("API auth failed: name=%s", req.GetName())
		return nil, status.Error(codes.Unauthenticated, "invalid credentials")
	}

	token, expiresAt, err := a.srv.sessions.issue(u.Name)
	if err != nil {
		return nil, status.Errorf(codes.Internal, "issue session: %v", err)
	}
	logger.Info("API session issued: user=%s token=%s.. expires=%s",
		u.Name, fingerprintToken(token), expiresAt.Format(time.RFC3339))
	return &v1.AuthenticateResponse{
		SessionToken: token,
		ExpiresAt:    expiresAt.Unix(),
	}, nil
}

// Logout revokes the session the request itself authenticated with.
func (a *authService) Logout(ctx context.Context, _ *v1.LogoutRequest) (*v1.LogoutResponse, error) {
	if tok := sessionTokenFromContext(ctx); tok != "" {
		a.srv.sessions.revoke(tok)
	}
	return &v1.LogoutResponse{}, nil
}

// sessionTokenFromContext returns the bearer this request authenticated with
// (used by Logout to revoke itself).
func sessionTokenFromContext(ctx context.Context) string {
	tok, _ := ctx.Value(sessionCtxKey).(string)
	return tok
}
