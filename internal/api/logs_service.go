package api

import (
	"context"

	v1 "github.com/hnrobert/sslly-nginx/gen/hnrobert/sslly/v1"
	"github.com/hnrobert/sslly-nginx/internal/config"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

type logsService struct {
	v1.UnimplementedLogsServiceServer
	srv *Server
}

func (l *logsService) GetLogsConfig(ctx context.Context, _ *v1.GetLogsConfigRequest) (*v1.GetLogsConfigResponse, error) {
	if err := Authorize(userFromContext(ctx), config.SurfaceLogs, config.ModeRead, nil); err != nil {
		return nil, err
	}
	cfg, err := config.Load(l.srv.cfg.ConfigDir)
	if err != nil {
		return nil, status.Errorf(codes.Internal, "load config: %v", err)
	}
	return &v1.GetLogsConfigResponse{Config: logsFromConfig(cfg.Log)}, nil
}

func (l *logsService) UpdateLogsConfig(ctx context.Context, req *v1.UpdateLogsConfigRequest) (*v1.UpdateLogsConfigResponse, error) {
	lc := req.GetConfig()
	if lc == nil {
		return nil, status.Error(codes.InvalidArgument, "config is required")
	}

	mask, err := logsPresenceFromMask(maskPaths(req.GetUpdateMask()))
	if err != nil {
		return nil, status.Errorf(codes.InvalidArgument, "%v", err)
	}
	if err := validateLevels(lc, mask); err != nil {
		return nil, status.Errorf(codes.InvalidArgument, "%v", err)
	}

	if err := Authorize(userFromContext(ctx), config.SurfaceLogs, config.ModeReadWrite, nil); err != nil {
		return nil, err
	}

	cfg := config.LogConfig{
		SSLLY: config.LogLevelConfig{Level: lc.GetSsllyLevel()},
		Nginx: config.NginxLogConfig{
			Level:      lc.GetNginxLevel(),
			StderrAs:   lc.GetNginxStderrAs(),
			StderrShow: lc.GetNginxStderrShow(),
		},
	}
	prev, err := l.srv.cfg.Editor.UpdateLogsConfig(cfg, mask)
	if err != nil {
		return nil, status.Errorf(codes.Internal, "write logs.yaml: %v", err)
	}

	return &v1.UpdateLogsConfigResponse{
		Config: lc,
		Apply:  l.srv.apply(config.LogsConfigFile, prev),
	}, nil
}
