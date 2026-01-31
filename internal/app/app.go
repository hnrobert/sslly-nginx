package app

import (
	"fmt"
	"sync"
	"time"

	"github.com/hnrobert/sslly-nginx/internal/backup"
	"github.com/hnrobert/sslly-nginx/internal/config"
	"github.com/hnrobert/sslly-nginx/internal/logger"
	"github.com/hnrobert/sslly-nginx/internal/nginx"
	"github.com/hnrobert/sslly-nginx/internal/ssl"
	"github.com/hnrobert/sslly-nginx/internal/watcher"
)

const (
	configDir           = "./configs"
	sslDir              = "./ssl"
	runtimeDir          = "./configs/.sslly-runtime"
	nginxConf           = "/etc/nginx/nginx.conf"
)

type App struct {
	configWatcher *watcher.Watcher
	sslWatcher    *watcher.Watcher
	config        *config.Config
	nginxManager  *nginx.Manager
	lastGoodConf  string
	activeCertMap map[string]ssl.Certificate
	sslReport     ssl.ScanReport
	backupManager *backup.Manager
	reloadMu      sync.Mutex

	reloadDebounceMu    sync.Mutex
	reloadDebounceTimer *time.Timer
	reloadDebounceSeq   uint64
}

func New() (*App, error) {
	return &App{
		nginxManager: nginx.NewManager(),
	}, nil
}

func (a *App) Start() error {
	// Ensure backup manager is available early (so crash recovery can work before reload).
	if a.backupManager == nil {
		bm, err := backup.NewManager(backup.DefaultBackupRoot(configDir), configDir, sslDir, runtimeDir, nginxConf)
		if err != nil {
			return fmt.Errorf("failed to initialize backup manager: %w", err)
		}
		a.backupManager = bm
	}

	// Ensure mount dirs are writable by host/container users
	if err := ensureDirWritable("/app/configs"); err != nil {
		logger.Warn("failed to ensure /app/configs is writable: %v", err)
	}
	if err := ensureDirWritable("/app/ssl"); err != nil {
		logger.Warn("failed to ensure /app/ssl is writable: %v", err)
	}

	// Migrate legacy config.yaml/config.yml BEFORE creating proxy.yaml,
	// so existing user configuration always wins.
	if err := config.Prepare(configDir); err != nil {
		return fmt.Errorf("config preparation failed: %w", err)
	}

	// If the previous run crashed mid-reload, rollback to last known-good configuration.
	if restored, err := a.backupManager.MaybeRestoreAfterCrash(); err != nil {
		return fmt.Errorf("failed crash recovery restore: %w", err)
	} else if restored {
		logger.Warn("Detected previous crash mid-reload; restored last known-good configuration")
	}

	snapID, err := a.backupManager.Begin()
	if err != nil {
		logger.Warn("failed to begin startup snapshot: %v", err)
		snapID = ""
	}

	// Initial configuration load and nginx setup
	if err := a.reload(snapID); err != nil {
		if snapID != "" {
			_ = a.backupManager.Abort(snapID)
		}
		return fmt.Errorf("initial setup failed: %w", err)
	}

	// Start nginx
	if err := a.nginxManager.Start(); err != nil {
		if snapID != "" {
			_ = a.backupManager.Abort(snapID)
		}
		return fmt.Errorf("failed to start nginx: %w", err)
	}

	// Verify nginx is healthy
	if err := a.nginxManager.CheckHealth(); err != nil {
		if snapID != "" {
			_ = a.backupManager.Abort(snapID)
		}
		return fmt.Errorf("nginx health check failed after initial start: %w", err)
	}

	if snapID != "" {
		if err := a.backupManager.Commit(snapID); err != nil {
			logger.Warn("failed to commit startup snapshot: %v", err)
		}
	}

	// Print a single summary after everything is successfully applied.
	logDomainSummary(a.config, a.activeCertMap, a.sslReport, time.Now())

	// Save the good configuration
	a.saveGoodConfiguration()

	// Setup watchers
	if err := a.setupWatchers(); err != nil {
		return fmt.Errorf("failed to setup watchers: %w", err)
	}

	logger.Info("Application started successfully")
	return nil
}

func (a *App) Stop() {
	if a.configWatcher != nil {
		a.configWatcher.Stop()
	}
	if a.sslWatcher != nil {
		a.sslWatcher.Stop()
	}
	a.reloadDebounceMu.Lock()
	if a.reloadDebounceTimer != nil {
		a.reloadDebounceTimer.Stop()
		a.reloadDebounceTimer = nil
	}
	a.reloadDebounceMu.Unlock()
	a.nginxManager.Stop()
}
