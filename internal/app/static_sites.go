package app

import (
	"context"
	"fmt"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/hnrobert/sslly-nginx/internal/config"
	"github.com/hnrobert/sslly-nginx/internal/logger"
)

type runningStaticSite struct {
	OriginalKey string
	Dir         string
	Port        int
	Server      *http.Server
	Listener    net.Listener
}

func (s *runningStaticSite) stop() {
	if s == nil {
		return
	}
	ctx, cancel := context.WithTimeout(context.Background(), 800*time.Millisecond)
	defer cancel()
	if s.Server != nil {
		_ = s.Server.Shutdown(ctx)
	}
	if s.Listener != nil {
		_ = s.Listener.Close()
	}
}

func (a *App) stopAllStaticSites() {
	for _, s := range a.staticSites {
		s.stop()
	}
	a.staticSites = make(map[string]*runningStaticSite)
}

func (a *App) prepareStaticSitesForReload(cfg *config.Config) (*config.Config, func(success bool), error) {
	if cfg == nil {
		return cfg, func(bool) {}, nil
	}
	if a.staticSites == nil {
		a.staticSites = make(map[string]*runningStaticSite)
	}

	type desired struct {
		key     string
		dir     string
		hasPort bool
		port    int
		domains []string
	}

	desiredSites := make(map[string]desired)
	for k, domains := range cfg.Ports {
		spec, ok, err := config.ParseStaticSiteKey(k)
		if err != nil {
			logger.Error("Invalid static site mapping %q: %v", k, err)
			continue
		}
		if !ok {
			continue
		}
		desiredSites[k] = desired{key: k, dir: spec.Dir, hasPort: spec.HasPort, port: spec.Port, domains: domains}
	}

	// Fast path: no static sites.
	if len(desiredSites) == 0 {
		return cfg, func(bool) {}, nil
	}

	// Determine which existing sites we can keep as-is.
	keep := make(map[string]*runningStaticSite)
	for key, want := range desiredSites {
		if cur, ok := a.staticSites[key]; ok {
			if sameDir(cur.Dir, want.dir) {
				if want.hasPort {
					if cur.Port == want.port {
						keep[key] = cur
					}
				} else {
					// Auto-port site: keep the existing port to avoid churn.
					keep[key] = cur
				}
			}
		}
	}

	// Stage new sites (do not stop old ones yet).
	pendingAdds := make(map[string]*runningStaticSite)
	reservedPorts := make(map[int]struct{})
	for _, s := range keep {
		reservedPorts[s.Port] = struct{}{}
	}
	for _, want := range desiredSites {
		if want.hasPort {
			reservedPorts[want.port] = struct{}{}
		}
	}

	// Avoid auto-allocating ports that already exist as numeric upstream keys.
	for k := range cfg.Ports {
		// Only care about raw numeric keys like "1234".
		ks := strings.TrimSpace(strings.TrimSuffix(k, ":"))
		if ks == "" {
			continue
		}
		if strings.HasPrefix(ks, ".") || strings.HasPrefix(ks, "/") {
			continue
		}
		if _, err := strconv.Atoi(ks); err == nil {
			if p, err := strconv.Atoi(ks); err == nil {
				reservedPorts[p] = struct{}{}
			}
		}
	}

	var errs []error
	for key, want := range desiredSites {
		if _, ok := keep[key]; ok {
			continue
		}

		absDir := want.dir
		// Keep relative paths relative to current working dir (/app in container).
		if !filepath.IsAbs(absDir) {
			absDir = filepath.Clean(absDir)
		}
		if st, err := os.Stat(absDir); err != nil || !st.IsDir() {
			err := fmt.Errorf("static site %q path is not a directory: %s", key, absDir)
			errs = append(errs, err)
			logger.Error("%v", err)
			continue
		}

		port := want.port
		ln, chosenPort, err := func() (net.Listener, int, error) {
			if want.hasPort {
				if _, inUse := reservedPorts[port]; inUse {
					// If it's reserved by another desired/kept static mapping, treat as conflict.
					return nil, 0, fmt.Errorf("port %d is already reserved", port)
				}
				l, err := net.Listen("tcp", fmt.Sprintf("127.0.0.1:%d", port))
				return l, port, err
			}

			for p := 10000; p <= 65535; p++ {
				if _, inUse := reservedPorts[p]; inUse {
					continue
				}
				l, err := net.Listen("tcp", fmt.Sprintf("127.0.0.1:%d", p))
				if err != nil {
					continue
				}
				return l, p, nil
			}
			return nil, 0, fmt.Errorf("no available port found starting from 10000")
		}()
		if err != nil {
			err := fmt.Errorf("static site %q failed to bind port: %w", key, err)
			errs = append(errs, err)
			logger.Error("%v", err)
			continue
		}
		port = chosenPort

		reservedPorts[port] = struct{}{}

		srv := &http.Server{
			Handler: http.FileServer(http.Dir(absDir)),
		}
		site := &runningStaticSite{OriginalKey: key, Dir: absDir, Port: port, Server: srv, Listener: ln}
		pendingAdds[key] = site

		go func(key string, s *runningStaticSite) {
			logger.Info("Static site enabled: %s -> 127.0.0.1:%d", key, s.Port)
			err := s.Server.Serve(s.Listener)
			if err != nil && err != http.ErrServerClosed {
				logger.Error("Static site server %s stopped: %v", key, err)
			}
		}(key, site)
	}

	// Build effective config: rewrite static keys to numeric ports, drop invalid ones.
	effective := *cfg
	effective.Ports = make(map[string][]string, len(cfg.Ports))
	for k, v := range cfg.Ports {
		if _, ok := desiredSites[k]; ok {
			continue
		}
		effective.Ports[k] = append([]string(nil), v...)
	}
	for key, want := range desiredSites {
		port := 0
		if s, ok := keep[key]; ok {
			port = s.Port
		} else if s, ok := pendingAdds[key]; ok {
			port = s.Port
		} else {
			// This static mapping failed to start; skip it.
			continue
		}
		portKey := strconv.Itoa(port)
		if _, exists := effective.Ports[portKey]; exists {
			// Avoid silently merging different destinations.
			logger.Error("static site %q cannot use port %d because proxy.yaml already contains key %q; skipping", key, port, portKey)
			continue
		}
		effective.Ports[portKey] = append([]string(nil), want.domains...)
	}

	finalize := func(success bool) {
		if !success {
			for _, s := range pendingAdds {
				s.stop()
			}
			return
		}

		// Stop removed/replaced sites.
		for key, cur := range a.staticSites {
			if _, ok := keep[key]; ok {
				continue
			}
			cur.stop()
		}

		next := make(map[string]*runningStaticSite)
		for key, s := range keep {
			next[key] = s
		}
		for key, s := range pendingAdds {
			next[key] = s
		}
		a.staticSites = next
	}

	// Non-fatal: log collected errors and continue.
	if len(errs) > 0 {
		logger.Warn("Static site errors: %d mapping(s) failed (others continue)", len(errs))
	}

	return &effective, finalize, nil
}

func sameDir(a, b string) bool {
	a = filepath.Clean(strings.TrimSpace(a))
	b = filepath.Clean(strings.TrimSpace(b))
	return a == b
}
