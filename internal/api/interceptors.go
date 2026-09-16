package api

import (
	"context"
	"runtime/debug"
	"strings"
	"time"

	"github.com/hnrobert/sslly-nginx/internal/config"
	"github.com/hnrobert/sslly-nginx/internal/logger"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/metadata"
	"google.golang.org/grpc/status"
)

type ctxKey string

const (
	userCtxKey    ctxKey = "sslly-api-user"
	sessionCtxKey ctxKey = "sslly-api-session"
)

// userFromContext returns the authenticated user attached by the auth
// interceptor, or nil.
func userFromContext(ctx context.Context) *config.User {
	u, _ := ctx.Value(userCtxKey).(*config.User)
	return u
}

// recoveryInterceptor turns panics into Internal errors instead of killing
// the server.
func recoveryInterceptor(ctx context.Context, req any, info *grpc.UnaryServerInfo, handler grpc.UnaryHandler) (resp any, err error) {
	defer func() {
		if r := recover(); r != nil {
			logger.Error("API panic method=%s: %v\n%s", info.FullMethod, r, debug.Stack())
			err = status.Errorf(codes.Internal, "internal error")
		}
	}()
	return handler(ctx, req)
}

// auditInterceptor logs every call (including auth failures — it chains
// outside the auth interceptor, so the authenticated user is not in the
// context yet) with the resulting status code. The user name for the log is
// resolved from the bearer token in the request metadata.
func auditInterceptor(users *config.UserStore) grpc.UnaryServerInterceptor {
	return func(ctx context.Context, req any, info *grpc.UnaryServerInfo, handler grpc.UnaryHandler) (any, error) {
		start := time.Now()
		resp, err := handler(ctx, req)

		user := "anonymous"
		if u := userFromContext(ctx); u != nil {
			user = u.Name // set by an inner auth pass (unusual ordering)
		} else if md, ok := metadata.FromIncomingContext(ctx); ok {
			if tok := bearerToken(md); tok != "" {
				if u, verr := users.VerifyToken(tok); verr == nil {
					user = u.Name
				}
			}
		}
		code := codes.OK
		if err != nil {
			code = status.Code(err)
		}
		logger.Info("API method=%s user=%s code=%s duration=%s",
			info.FullMethod, user, code, time.Since(start).Round(time.Millisecond))
		return resp, err
	}
}

// authAllowlist are method prefixes that skip bearer authentication.
var authAllowlist = []string{"grpc.health.v1.", "grpc.reflection.", "/hnrobert.sslly.v1.AuthService/Authenticate"}

// authInterceptor resolves the Bearer SESSION token (issued by
// AuthService.Authenticate) to its user. The long-lived users.yaml tokens are
// deliberately NOT accepted here — they can only be exchanged for a session
// via Authenticate. A broken or missing users.yaml fails closed for every
// non-allowlisted call (sessions re-validate the credential fingerprint on
// each request).
func authInterceptor(users *config.UserStore, sessions *sessionStore) grpc.UnaryServerInterceptor {
	return func(ctx context.Context, req any, info *grpc.UnaryServerInfo, handler grpc.UnaryHandler) (any, error) {
		for _, prefix := range authAllowlist {
			if strings.HasPrefix(info.FullMethod, prefix) {
				return handler(ctx, req)
			}
		}

		md, ok := metadata.FromIncomingContext(ctx)
		if !ok {
			return nil, status.Error(codes.Unauthenticated, "missing metadata")
		}
		token := bearerToken(md)
		if token == "" {
			return nil, status.Error(codes.Unauthenticated, "missing bearer token")
		}
		name, ok := sessions.lookup(token)
		if !ok {
			return nil, status.Error(codes.Unauthenticated, "invalid or expired session")
		}
		// Resolve the CURRENT user record (permissions may have changed since
		// the session was issued; a deleted user fails closed).
		u, err := users.UserByName(name)
		if err != nil {
			return nil, status.Error(codes.Unauthenticated, "invalid session")
		}
		ctx = context.WithValue(ctx, sessionCtxKey, token)
		return handler(context.WithValue(ctx, userCtxKey, u), req)
	}
}

func bearerToken(md metadata.MD) string {
	for _, v := range md.Get("authorization") {
		if len(v) > 7 && strings.EqualFold(v[:7], "bearer ") {
			return strings.TrimSpace(v[7:])
		}
	}
	return ""
}
