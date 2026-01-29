package app

import (
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/fsnotify/fsnotify"
	"github.com/hnrobert/sslly-nginx/internal/backup"
	"github.com/hnrobert/sslly-nginx/internal/config"
	"github.com/hnrobert/sslly-nginx/internal/logger"
	"github.com/hnrobert/sslly-nginx/internal/nginx"
	"github.com/hnrobert/sslly-nginx/internal/ssl"
	"github.com/hnrobert/sslly-nginx/internal/watcher"
)

const (
	configDir             = "./configs"
	sslDir                = "./ssl"
	runtimeDir            = "./configs/.sslly-runtime"
	nginxConf             = "/etc/nginx/nginx.conf"
	configFilePath        = "/app/configs/config.yaml"
	defaultConfigFilePath = "/etc/sslly/configs/config.yaml"
)

type App struct {
	configWatcher *watcher.Watcher
	sslWatcher    *watcher.Watcher
	config        *config.Config
	nginxManager  *nginx.Manager
	lastGoodConf  string
	activeCertMap map[string]ssl.Certificate
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

	if err := ensureConfigFile(configFilePath, defaultConfigFilePath); err != nil {
		return fmt.Errorf("config initialization failed: %w", err)
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
	logDomainSummary(a.config, a.activeCertMap, time.Now())

	// Save the good configuration
	a.saveGoodConfiguration()

	// Setup watchers
	if err := a.setupWatchers(); err != nil {
		return fmt.Errorf("failed to setup watchers: %w", err)
	}

	logger.Info("Application started successfully")
	return nil
}

func ensureConfigFile(destPath, defaultPath string) error {
	if _, err := os.Stat(destPath); err == nil {
		return nil
	} else if !os.IsNotExist(err) {
		return err
	}

	if _, err := os.Stat(defaultPath); err != nil {
		return fmt.Errorf("default config not found at %s: %w", defaultPath, err)
	}

	if err := os.MkdirAll(filepath.Dir(destPath), 0755); err != nil {
		return err
	}

	src, err := os.Open(defaultPath)
	if err != nil {
		return err
	}
	defer src.Close()

	dst, err := os.OpenFile(destPath, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, 0666)
	if err != nil {
		return err
	}
	defer dst.Close()

	if _, err := io.Copy(dst, src); err != nil {
		return err
	}

	// Ensure permissive permissions so host user can write if necessary
	// Files: 0666 (rw for all), Dirs: 0777 (rwx for all)
	if err := os.Chmod(destPath, 0666); err != nil {
		logger.Warn("failed to chmod %s: %v", destPath, err)
	}
	if err := os.Chmod(filepath.Dir(destPath), 0777); err != nil {
		logger.Warn("failed to chmod %s: %v", filepath.Dir(destPath), err)
	}

	// If running as root inside the image, attempt to chown files to UID/GID 1000:1000
	if os.Geteuid() == 0 {
		if err := os.Chown(destPath, 1000, 1000); err != nil {
			logger.Warn("failed to chown %s: %v", destPath, err)
		}
		if err := os.Chown(filepath.Dir(destPath), 1000, 1000); err != nil {
			logger.Warn("failed to chown %s: %v", filepath.Dir(destPath), err)
		}
	}

	logger.Info("Config file not found, copied default config: %s -> %s", defaultPath, destPath)
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

// ensureDirWritable makes a directory and its existing contents writable by any user,
// and attempts to chown to UID/GID 1000 when running as root.
func ensureDirWritable(dir string) error {
	// Create if not exists
	if err := os.MkdirAll(dir, 0777); err != nil {
		return err
	}

	// Walk entries and set permissive permissions
	entries, err := os.ReadDir(dir)
	if err != nil {
		return err
	}
	for _, e := range entries {
		p := filepath.Join(dir, e.Name())
		if e.IsDir() {
			if err := os.Chmod(p, 0777); err != nil {
				logger.Warn("failed to chmod dir %s: %v", p, err)
			}
		} else {
			if err := os.Chmod(p, 0666); err != nil {
				logger.Warn("failed to chmod file %s: %v", p, err)
			}
		}
		// Attempt chown if root
		if os.Geteuid() == 0 {
			if err := os.Chown(p, 1000, 1000); err != nil {
				// Not fatal
				logger.Warn("failed to chown %s: %v", p, err)
			}
		}
	}

	// Finally ensure dir itself has permissive perms and ownership
	if err := os.Chmod(dir, 0777); err != nil {
		logger.Warn("failed to chmod dir %s: %v", dir, err)
	}
	if os.Geteuid() == 0 {
		if err := os.Chown(dir, 1000, 1000); err != nil {
			logger.Warn("failed to chown dir %s: %v", dir, err)
		}
	}

	return nil
}

func (a *App) setupWatchers() error {
	// Watch config directory
	configWatcher, err := watcher.New(configDir)
	if err != nil {
		return fmt.Errorf("failed to create config watcher: %w", err)
	}
	a.configWatcher = configWatcher

	// Watch SSL directory
	sslWatcher, err := watcher.New(sslDir)
	if err != nil {
		return fmt.Errorf("failed to create ssl watcher: %w", err)
	}
	a.sslWatcher = sslWatcher

	// Handle config changes
	go func() {
		for {
			select {
			case event, ok := <-configWatcher.Events:
				if !ok {
					return
				}
				if isInternalConfigPath(event.Name) {
					continue
				}
				if event.Op&fsnotify.Write == fsnotify.Write || event.Op&fsnotify.Create == fsnotify.Create {
					logger.Info("Config file changed: %s", event.Name)
					a.scheduleReload()
				}
			case err, ok := <-configWatcher.Errors:
				if !ok {
					return
				}
				logger.Error("Config watcher error: %v", err)
			}
		}
	}()

	// Handle SSL changes
	go func() {
		for {
			select {
			case event, ok := <-sslWatcher.Events:
				if !ok {
					return
				}
				if event.Op&fsnotify.Write == fsnotify.Write ||
					event.Op&fsnotify.Create == fsnotify.Create ||
					event.Op&fsnotify.Remove == fsnotify.Remove {
					logger.Info("SSL file changed: %s", event.Name)
					a.scheduleReload()
				}
			case err, ok := <-sslWatcher.Errors:
				if !ok {
					return
				}
				logger.Error("SSL watcher error: %v", err)
			}
		}
	}()

	return nil
}

func isInternalConfigPath(p string) bool {
	pp := filepath.ToSlash(p)
	return strings.Contains(pp, "/.sslly-backups/") || strings.Contains(pp, "/.sslly-runtime/")
}

func (a *App) scheduleReload() {
	const debounceWindow = 800 * time.Millisecond

	a.reloadDebounceMu.Lock()
	a.reloadDebounceSeq++
	seq := a.reloadDebounceSeq
	if a.reloadDebounceTimer != nil {
		a.reloadDebounceTimer.Stop()
	}
	a.reloadDebounceTimer = time.AfterFunc(debounceWindow, func() {
		a.reloadDebounceMu.Lock()
		if seq != a.reloadDebounceSeq {
			a.reloadDebounceMu.Unlock()
			return
		}
		a.reloadDebounceMu.Unlock()
		a.handleReload()
	})
	a.reloadDebounceMu.Unlock()
}

func (a *App) reload(snapshotID string) error {
	// Load configuration
	cfg, err := config.Load(configDir)
	if err != nil {
		return fmt.Errorf("failed to load config: %w", err)
	}
	a.config = cfg

	// Scan SSL certificates
	certMap, err := ssl.ScanCertificates(sslDir)
	if err != nil {
		return fmt.Errorf("failed to scan certificates: %w", err)
	}

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
	nginxConfig := nginx.GenerateConfig(cfg, activeCertMap)

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
	return nil
}

func collectBaseDomains(cfg *config.Config) map[string]struct{} {
	out := make(map[string]struct{})
	if cfg == nil {
		return out
	}
	for _, domainPaths := range cfg.Ports {
		for _, domainPath := range domainPaths {
			base := domainPath
			if idx := strings.Index(base, "/"); idx > 0 {
				base = base[:idx]
			}
			base = strings.ToLower(strings.TrimSpace(base))
			if base == "" {
				continue
			}
			out[base] = struct{}{}
		}
	}
	return out
}

func runtimeRootAbs() (string, error) {
	return filepath.Abs(runtimeDir)
}

func runtimeStageDirAbs(snapshotID string) (string, error) {
	root, err := runtimeRootAbs()
	if err != nil {
		return "", err
	}
	return filepath.Join(root, "stage", snapshotID), nil
}

func runtimeCurrentDirAbs() (string, error) {
	root, err := runtimeRootAbs()
	if err != nil {
		return "", err
	}
	return filepath.Join(root, "current"), nil
}

func runtimeOldDirAbs() (string, error) {
	root, err := runtimeRootAbs()
	if err != nil {
		return "", err
	}
	return filepath.Join(root, "old"), nil
}

func sanitizeDomainForFileName(domain string) string {
	domain = strings.TrimSpace(domain)
	domain = strings.ToLower(domain)
	if domain == "" {
		return "unknown"
	}
	return strings.Map(func(r rune) rune {
		switch {
		case r >= 'a' && r <= 'z':
			return r
		case r >= '0' && r <= '9':
			return r
		case r == '.' || r == '-' || r == '_':
			return r
		default:
			return '_'
		}
	}, domain)
}

func copyFileContents(srcPath, dstPath string) error {
	data, err := os.ReadFile(srcPath)
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(dstPath), 0755); err != nil {
		return err
	}
	if err := os.WriteFile(dstPath, data, 0644); err != nil {
		return err
	}
	if info, err := os.Stat(srcPath); err == nil {
		_ = os.Chmod(dstPath, info.Mode().Perm())
	}
	return nil
}

func stageRuntimeCertificates(snapshotID string, cfg *config.Config, scanned map[string]ssl.Certificate) (map[string]ssl.Certificate, error) {
	stageDir, err := runtimeStageDirAbs(snapshotID)
	if err != nil {
		return nil, err
	}
	currentDir, err := runtimeCurrentDirAbs()
	if err != nil {
		return nil, err
	}

	// Fresh stage.
	_ = os.RemoveAll(stageDir)
	if err := os.MkdirAll(filepath.Join(stageDir, "certs"), 0755); err != nil {
		return nil, err
	}

	active := make(map[string]ssl.Certificate)
	for baseDomain := range collectBaseDomains(cfg) {
		cert, ok := ssl.FindCertificate(scanned, baseDomain)
		if !ok {
			continue
		}
		if cert.KeyPath == "" {
			continue
		}

		safe := sanitizeDomainForFileName(baseDomain)
		certExt := strings.ToLower(filepath.Ext(cert.CertPath))
		if certExt == "" {
			certExt = ".pem"
		}
		keyExt := strings.ToLower(filepath.Ext(cert.KeyPath))
		if keyExt == "" {
			keyExt = ".key"
		}

		stageCertPath := filepath.Join(stageDir, "certs", safe+certExt)
		stageKeyPath := filepath.Join(stageDir, "certs", safe+keyExt)
		if err := copyFileContents(cert.CertPath, stageCertPath); err != nil {
			return nil, fmt.Errorf("copy cert for %s: %w", baseDomain, err)
		}
		if err := copyFileContents(cert.KeyPath, stageKeyPath); err != nil {
			return nil, fmt.Errorf("copy key for %s: %w", baseDomain, err)
		}

		active[baseDomain] = ssl.Certificate{
			CertPath: filepath.Join(currentDir, "certs", safe+certExt),
			KeyPath:  filepath.Join(currentDir, "certs", safe+keyExt),
			NotAfter: cert.NotAfter,
		}
	}

	return active, nil
}

func writeRuntimeNginxConf(snapshotID string, nginxConfig string) error {
	stageDir, err := runtimeStageDirAbs(snapshotID)
	if err != nil {
		return err
	}
	p := filepath.Join(stageDir, "nginx", "nginx.conf")
	if err := os.MkdirAll(filepath.Dir(p), 0755); err != nil {
		return err
	}
	return os.WriteFile(p, []byte(nginxConfig), 0644)
}

func activateRuntimeSnapshot(snapshotID string) error {
	stageDir, err := runtimeStageDirAbs(snapshotID)
	if err != nil {
		return err
	}
	currentDir, err := runtimeCurrentDirAbs()
	if err != nil {
		return err
	}
	oldDir, err := runtimeOldDirAbs()
	if err != nil {
		return err
	}
	root, err := runtimeRootAbs()
	if err != nil {
		return err
	}
	if err := os.MkdirAll(root, 0755); err != nil {
		return err
	}

	_ = os.RemoveAll(oldDir)
	if _, err := os.Stat(currentDir); err == nil {
		if err := os.Rename(currentDir, oldDir); err != nil {
			return err
		}
	}
	// Ensure the stage exists.
	if _, err := os.Stat(stageDir); err != nil {
		// Best-effort rollback.
		_ = os.Rename(oldDir, currentDir)
		return err
	}
	if err := os.Rename(stageDir, currentDir); err != nil {
		// Best-effort rollback.
		_ = os.Rename(oldDir, currentDir)
		return err
	}
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

	logDomainSummary(a.config, a.activeCertMap, time.Now())
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
	// Prefer restoring the last-good snapshot so the on-disk config/ssl also matches
	// what nginx is currently serving.
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
