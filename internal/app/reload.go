package app

import (
	"fmt"
	"os"
	"time"

	"github.com/hnrobert/sslly-nginx/internal/config"
	"github.com/hnrobert/sslly-nginx/internal/logger"
	"github.com/hnrobert/sslly-nginx/internal/nginx"
	"github.com/hnrobert/sslly-nginx/internal/ssl"
)

func (a *App) reload(snapshotID string) error {
	// Load configuration
	cfg, err := config.Load(configDir)
	if err != nil {
		return fmt.Errorf("failed to load config: %w", err)
	}

	// Static sites: turn directory entries in proxy.yaml into localhost ports
	// by starting an internal file server per mapping (or reusing existing ones).
	effectiveCfg, finalizeStatic, err := a.prepareStaticSitesForReload(cfg)
	if err != nil {
		return fmt.Errorf("failed to prepare static sites: %w", err)
	}
	// Default: if anything below fails, stop newly-started static servers.
	success := false
	defer func() { finalizeStatic(success) }()

	a.config = effectiveCfg

	// Apply log configuration
	ssllyLevel := "info"
	if cfg.Log.SSLLY.Level != "" {
		ssllyLevel = cfg.Log.SSLLY.Level
	}
	nginxLevel := "info"
	if cfg.Log.Nginx.Level != "" {
		nginxLevel = cfg.Log.Nginx.Level
	}

	// Apply nginx stderr configuration
	nginxStderrAs := "error" // Default: treat stderr as error level
	if cfg.Log.Nginx.StderrAs != "" {
		nginxStderrAs = cfg.Log.Nginx.StderrAs
	}

	// If stderr_show is not explicitly configured, use the same as stderr_as
	nginxStderrShow := nginxStderrAs
	if cfg.Log.Nginx.StderrShow != "" {
		nginxStderrShow = cfg.Log.Nginx.StderrShow
	}

	logger.SetSSLLYLevel(ssllyLevel)
	logger.SetNginxLevel(nginxLevel)
	logger.SetNginxStderrLevel(nginxStderrShow)

	// Scan SSL certificates
	certMap, report, err := ssl.ScanCertificatesWithReport(sslDir)
	if err != nil {
		return fmt.Errorf("failed to scan certificates: %w", err)
	}
	a.sslReport = report

	// Stage runtime cert cache for configured domains.
	if snapshotID == "" {
		snapshotID = time.Now().UTC().Format("20060102T150405.000000000Z")
	}
	activeCertMap, err := stageRuntimeCertificates(snapshotID, cfg, certMap)
	if err != nil {
		return fmt.Errorf("failed to stage runtime certificates: %w", err)
	}

	// Keep the latest active cert map for summarized logging.
	a.activeCertMap = activeCertMap

	// Generate nginx configuration
	nginxConfig := nginx.GenerateConfig(effectiveCfg, activeCertMap)

	// Store generated nginx.conf into runtime cache as well.
	if err := writeRuntimeNginxConf(snapshotID, nginxConfig); err != nil {
		return fmt.Errorf("failed to write runtime nginx.conf: %w", err)
	}
	// Activate runtime cache for this snapshot so nginx -t / reload reads stable cert paths.
	if err := activateRuntimeSnapshot(snapshotID); err != nil {
		return fmt.Errorf("failed to activate runtime snapshot: %w", err)
	}

	// Write nginx configuration
	if err := os.WriteFile(nginxConf, []byte(nginxConfig), 0644); err != nil {
		return fmt.Errorf("failed to write nginx config: %w", err)
	}

	logger.Info("Nginx configuration generated successfully")
	success = true
	return nil
}

func (a *App) handleReload() {
	a.reloadMu.Lock()
	defer a.reloadMu.Unlock()

	logger.Info("Reloading configuration...")

	snapID := ""
	if a.backupManager != nil {
		if id, err := a.backupManager.Begin(); err != nil {
			logger.Warn("failed to begin reload snapshot: %v", err)
		} else {
			snapID = id
		}
	}

	// Try to reload configuration
	if err := a.reload(snapID); err != nil {
		logger.Error("Failed to reload configuration: %v", err)
		if snapID != "" {
			_ = a.backupManager.Abort(snapID)
		}
		a.restoreGoodConfiguration()
		return
	}

	// Reload nginx
	if err := a.nginxManager.Reload(); err != nil {
		logger.Error("Failed to reload nginx: %v", err)
		if snapID != "" {
			_ = a.backupManager.Abort(snapID)
		}
		a.restoreGoodConfiguration()
		if err := a.nginxManager.Reload(); err != nil {
			logger.Error("Failed to restore nginx: %v", err)
		}
		return
	}

	// Check nginx health
	if err := a.nginxManager.CheckHealth(); err != nil {
		logger.Error("Nginx health check failed after reload: %v", err)
		if snapID != "" {
			_ = a.backupManager.Abort(snapID)
		}
		a.restoreGoodConfiguration()
		if err := a.nginxManager.Reload(); err != nil {
			logger.Error("Failed to restore nginx: %v", err)
		}
		return
	}

	if snapID != "" {
		if err := a.backupManager.Commit(snapID); err != nil {
			logger.Warn("failed to commit reload snapshot: %v", err)
		}
	}

	// Save the new good configuration
	a.saveGoodConfiguration()

	logDomainSummary(a.config, a.activeCertMap, a.sslReport, time.Now())
	logger.Info("Configuration reloaded successfully")
}

func (a *App) saveGoodConfiguration() {
	data, err := os.ReadFile(nginxConf)
	if err != nil {
		logger.Warn("Failed to save good configuration: %v", err)
		return
	}
	a.lastGoodConf = string(data)
}

func (a *App) restoreGoodConfiguration() {
	// Prefer restoring the last-good snapshot. Snapshot restores are intentionally
	// limited to the runtime cache and nginx.conf (never user-owned configs/ or ssl/).
	if a.backupManager != nil {
		if err := a.backupManager.RestoreLastGood(); err == nil {
			logger.Info("Restored previous good configuration snapshot")
			// Keep in-memory fallback in sync.
			a.saveGoodConfiguration()
			return
		} else {
			logger.Warn("Failed to restore good snapshot: %v", err)
		}
	}

	if a.lastGoodConf == "" {
		logger.Warn("No good configuration to restore")
		return
	}

	if err := os.WriteFile(nginxConf, []byte(a.lastGoodConf), 0644); err != nil {
		logger.Error("Failed to restore good configuration: %v", err)
	} else {
		logger.Info("Restored previous good configuration")
	}
}
