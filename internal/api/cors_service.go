package api

import (
	"context"
	"errors"

	v1 "github.com/hnrobert/sslly-nginx/gen/hnrobert/sslly/v1"
	"github.com/hnrobert/sslly-nginx/internal/config"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

type corsService struct {
	v1.UnimplementedCorsServiceServer
	srv *Server
}

func (c *corsService) ListCorsRules(ctx context.Context, _ *v1.ListCorsRulesRequest) (*v1.ListCorsRulesResponse, error) {
	user := userFromContext(ctx)
	cfg, err := config.Load(c.srv.cfg.ConfigDir)
	if err != nil {
		return nil, status.Errorf(codes.Internal, "load config: %v", err)
	}

	resp := &v1.ListCorsRulesResponse{}
	for _, key := range sortedCorsKeys(cfg.CORS) {
		if !CanSee(user, config.SurfaceCORS, []Resource{{Domain: key}}) {
			continue
		}
		resp.Rules = append(resp.Rules, corsRuleFromConfig(key, cfg.CORS[key]))
	}
	return resp, nil
}

func (c *corsService) SetCorsRule(ctx context.Context, req *v1.SetCorsRuleRequest) (*v1.SetCorsRuleResponse, error) {
	rule := req.GetRule()
	if rule == nil || rule.GetKey() == "" {
		return nil, status.Error(codes.InvalidArgument, "rule with key is required")
	}

	presence, err := corsPresenceFromMask(maskPaths(req.GetUpdateMask()))
	if err != nil {
		return nil, status.Errorf(codes.InvalidArgument, "%v", err)
	}
	// Only meaningful when both fields are being written; unmasked values in
	// the message are ignored, and cross-layer conflicts are the user's call.
	if presence.AllowCredentials && presence.AllowOrigin &&
		rule.GetAllowCredentials() && rule.GetAllowOrigin() == "*" {
		return nil, status.Error(codes.InvalidArgument,
			"allow_credentials requires a specific allow_origin (not \"*\")")
	}
	cfg := config.CORSConfig{
		AllowOrigin:      rule.GetAllowOrigin(),
		AllowMethods:     rule.GetAllowMethods(),
		AllowHeaders:     rule.GetAllowHeaders(),
		ExposeHeaders:    rule.GetExposeHeaders(),
		MaxAge:           int(rule.GetMaxAge()),
		AllowCredentials: rule.GetAllowCredentials(),
	}
	cfg.SetPresence(presence)

	if err := Authorize(userFromContext(ctx), config.SurfaceCORS, config.ModeReadWrite,
		[]Resource{{Domain: rule.GetKey()}}); err != nil {
		return nil, err
	}

	prev, err := c.srv.cfg.Editor.SetCorsRule(rule.GetKey(), cfg, presence)
	if err != nil {
		return nil, status.Errorf(codes.Internal, "write cors.yaml: %v", err)
	}

	// Echo the rule as it now exists (explicit fields = the presence mask).
	return &v1.SetCorsRuleResponse{
		Rule: &v1.CMsgCorsRule{
			Key:              rule.GetKey(),
			AllowOrigin:      rule.GetAllowOrigin(),
			AllowMethods:     rule.GetAllowMethods(),
			AllowHeaders:     rule.GetAllowHeaders(),
			ExposeHeaders:    rule.GetExposeHeaders(),
			MaxAge:           rule.GetMaxAge(),
			AllowCredentials: rule.GetAllowCredentials(),
			ExplicitFields:   maskFromCORSPresence(presence),
		},
		Apply: c.srv.apply(config.CorsConfigFile, prev),
	}, nil
}

func (c *corsService) DeleteCorsRule(ctx context.Context, req *v1.DeleteCorsRuleRequest) (*v1.DeleteCorsRuleResponse, error) {
	if req.GetKey() == "" {
		return nil, status.Error(codes.InvalidArgument, "key is required")
	}

	loaded, err := config.Load(c.srv.cfg.ConfigDir)
	if err != nil {
		return nil, status.Errorf(codes.Internal, "load config: %v", err)
	}
	if _, ok := loaded.CORS[req.GetKey()]; !ok {
		return nil, status.Errorf(codes.NotFound, "cors rule %q not found", req.GetKey())
	}

	if err := Authorize(userFromContext(ctx), config.SurfaceCORS, config.ModeReadWrite,
		[]Resource{{Domain: req.GetKey()}}); err != nil {
		return nil, err
	}

	prev, err := c.srv.cfg.Editor.DeleteCorsRule(req.GetKey())
	if err != nil {
		if errors.Is(err, config.ErrEntryNotFound) {
			return nil, status.Errorf(codes.NotFound, "cors rule %q not found", req.GetKey())
		}
		return nil, status.Errorf(codes.Internal, "write cors.yaml: %v", err)
	}

	return &v1.DeleteCorsRuleResponse{
		Apply: c.srv.apply(config.CorsConfigFile, prev),
	}, nil
}
