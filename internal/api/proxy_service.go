package api

import (
	"context"
	"errors"

	v1 "github.com/hnrobert/sslly-nginx/gen/hnrobert/sslly/v1"
	"github.com/hnrobert/sslly-nginx/internal/config"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

type proxyService struct {
	v1.UnimplementedProxyServiceServer
	srv *Server
}

func (p *proxyService) ListProxyEntries(ctx context.Context, _ *v1.ListProxyEntriesRequest) (*v1.ListProxyEntriesResponse, error) {
	user := userFromContext(ctx)
	cfg, err := config.Load(p.srv.cfg.ConfigDir)
	if err != nil {
		return nil, status.Errorf(codes.Internal, "load config: %v", err)
	}

	resp := &v1.ListProxyEntriesResponse{}
	for _, key := range cfg.OrderedPorts { // declaration order
		listeners := cfg.Ports[key]
		if !CanSee(user, config.SurfaceProxy, entryResources(key, listeners)) {
			continue
		}
		resp.Entries = append(resp.Entries, &v1.CMsgProxyEntry{
			UpstreamKey:  key,
			ListenerKeys: listeners,
		})
	}
	return resp, nil
}

func (p *proxyService) SetProxyEntry(ctx context.Context, req *v1.SetProxyEntryRequest) (*v1.SetProxyEntryResponse, error) {
	entry := req.GetEntry()
	if entry == nil || entry.GetUpstreamKey() == "" {
		return nil, status.Error(codes.InvalidArgument, "entry with upstream_key is required")
	}
	if len(entry.GetListenerKeys()) == 0 {
		return nil, status.Errorf(codes.InvalidArgument, "entry %q needs at least one listener key", entry.GetUpstreamKey())
	}

	// Pre-write validation: reject structurally broken mappings (protocol
	// mismatches, static listeners, ...) before touching the file.
	certs := p.srv.certDomains()
	for _, lk := range entry.GetListenerKeys() {
		_, errs, _ := config.ValidateMapping(entry.GetUpstreamKey(), lk, certs[domainOfListener(lk)])
		if len(errs) > 0 {
			return nil, status.Errorf(codes.InvalidArgument, "listener %q: %v", lk, errs[0])
		}
	}

	// Authorize the complete NEW state (not the delta) so a scoped user can
	// never widen an entry beyond their grant.
	if err := Authorize(userFromContext(ctx), config.SurfaceProxy, config.ModeReadWrite,
		entryResources(entry.GetUpstreamKey(), entry.GetListenerKeys())); err != nil {
		return nil, err
	}

	prev, err := p.srv.cfg.Editor.SetProxyEntry(entry.GetUpstreamKey(), entry.GetListenerKeys())
	if err != nil {
		return nil, status.Errorf(codes.Internal, "write proxy.yaml: %v", err)
	}

	return &v1.SetProxyEntryResponse{
		Entry: entry,
		Apply: p.srv.apply(config.ProxyConfigFile, prev),
	}, nil
}

func (p *proxyService) DeleteProxyEntry(ctx context.Context, req *v1.DeleteProxyEntryRequest) (*v1.DeleteProxyEntryResponse, error) {
	if req.GetUpstreamKey() == "" {
		return nil, status.Error(codes.InvalidArgument, "upstream_key is required")
	}

	// Authorize the entry as it exists on disk.
	cfg, err := config.Load(p.srv.cfg.ConfigDir)
	if err != nil {
		return nil, status.Errorf(codes.Internal, "load config: %v", err)
	}
	listeners, ok := cfg.Ports[req.GetUpstreamKey()]
	if !ok {
		return nil, status.Errorf(codes.NotFound, "upstream key %q not found", req.GetUpstreamKey())
	}
	if err := Authorize(userFromContext(ctx), config.SurfaceProxy, config.ModeReadWrite,
		entryResources(req.GetUpstreamKey(), listeners)); err != nil {
		return nil, err
	}

	prev, err := p.srv.cfg.Editor.DeleteProxyEntry(req.GetUpstreamKey())
	if err != nil {
		if errors.Is(err, config.ErrEntryNotFound) {
			return nil, status.Errorf(codes.NotFound, "upstream key %q not found", req.GetUpstreamKey())
		}
		return nil, status.Errorf(codes.Internal, "write proxy.yaml: %v", err)
	}

	return &v1.DeleteProxyEntryResponse{
		Apply: p.srv.apply(config.ProxyConfigFile, prev),
	}, nil
}

func (p *proxyService) GetNoTrailingSlash(ctx context.Context, _ *v1.GetNoTrailingSlashRequest) (*v1.GetNoTrailingSlashResponse, error) {
	user := userFromContext(ctx)
	cfg, err := config.Load(p.srv.cfg.ConfigDir)
	if err != nil {
		return nil, status.Errorf(codes.Internal, "load config: %v", err)
	}

	resp := &v1.GetNoTrailingSlashResponse{}
	for _, lk := range cfg.NoTrailingSlash {
		if CanSee(user, config.SurfaceProxy, listenerResources([]string{lk})) {
			resp.ListenerKeys = append(resp.ListenerKeys, lk)
		}
	}
	return resp, nil
}

func (p *proxyService) SetNoTrailingSlash(ctx context.Context, req *v1.SetNoTrailingSlashRequest) (*v1.SetNoTrailingSlashResponse, error) {
	keys := req.GetListenerKeys()
	if len(keys) == 0 {
		return nil, status.Error(codes.InvalidArgument, "listener_keys must not be empty (use it to clear the list)")
	}
	if err := Authorize(userFromContext(ctx), config.SurfaceProxy, config.ModeReadWrite,
		listenerResources(keys)); err != nil {
		return nil, err
	}

	prev, err := p.srv.cfg.Editor.SetNoTrailingSlash(keys)
	if err != nil {
		return nil, status.Errorf(codes.Internal, "write proxy.yaml: %v", err)
	}

	return &v1.SetNoTrailingSlashResponse{
		Apply: p.srv.apply(config.ProxyConfigFile, prev),
	}, nil
}
