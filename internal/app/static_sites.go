package app

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/hnrobert/sslly-nginx/internal/config"
	"github.com/hnrobert/sslly-nginx/internal/logger"
)

// runningStaticSite tracks active static site configuration
type runningStaticSite struct {
	OriginalKey string
	Dir         string
	RoutePath   string
	HasIndex    bool
}

func (a *App) stopAllStaticSites() {
	// No-op: we no longer run HTTP servers for static sites
	a.staticSites = make(map[string]*runningStaticSite)
}

// prepareStaticSitesForReload validates static site directories and returns
// an updated config with RuntimeStaticSites populated for nginx config generation.
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
		route   string
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
		desiredSites[k] = desired{key: k, dir: spec.Dir, route: spec.RoutePath, domains: domains}
	}

	// Fast path: no static sites.
	if len(desiredSites) == 0 {
		return cfg, func(bool) {}, nil
	}

	var errs []error
	validSites := make(map[string]*runningStaticSite)

	// Validate all static site directories
	for key, want := range desiredSites {
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

		hasIndex := false
		if st, err := os.Stat(filepath.Join(absDir, "index.html")); err == nil && !st.IsDir() {
			hasIndex = true
		}

		validSites[key] = &runningStaticSite{
			OriginalKey: key,
			Dir:         absDir,
			RoutePath:   want.route,
			HasIndex:    hasIndex,
		}
		logger.Info("Static site validated: %s -> %s (hasIndex: %v, route: %q)", key, absDir, hasIndex, want.route)
	}

	// Build effective config: populate RuntimeStaticSites
	effective := *cfg
	effective.RuntimeStaticSites = make(map[string]config.StaticSiteSpec)
	for key, site := range validSites {
		effective.RuntimeStaticSites[key] = config.StaticSiteSpec{
			Dir:       site.Dir,
			RoutePath: site.RoutePath,
		}
	}

	finalize := func(success bool) {
		if success {
			// Update running sites
			a.staticSites = validSites
		}
	}

	// Non-fatal: log collected errors and continue.
	if len(errs) > 0 {
		logger.Warn("Static site errors: %d mapping(s) failed (others continue)", len(errs))
	}

	return &effective, finalize, nil
}

// applyStaticSiteRoute appends route path to domains that don't already have a path
func applyStaticSiteRoute(domains []string, route string) []string {
	route = strings.TrimSpace(route)
	if route == "" || route == "/" {
		return append([]string(nil), domains...)
	}
	if !strings.HasPrefix(route, "/") {
		route = "/" + route
	}

	out := make([]string, 0, len(domains))
	for _, d := range domains {
		ds := strings.TrimSpace(d)
		if ds == "" {
			continue
		}
		if strings.Contains(ds, "/") {
			out = append(out, ds)
			continue
		}
		out = append(out, ds+route)
	}
	return out
}

// sameDir compares two directory paths for equality
func sameDir(a, b string) bool {
	a = filepath.Clean(strings.TrimSpace(a))
	b = filepath.Clean(strings.TrimSpace(b))
	return a == b
}
