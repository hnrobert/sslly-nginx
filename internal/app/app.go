package app

import (
	"fmt"
	"io"
	"os"
	"path/filepath"

	"github.com/fsnotify/fsnotify"
	"github.com/hnrobert/sslly-nginx/internal/config"
	"github.com/hnrobert/sslly-nginx/internal/logger"
	"github.com/hnrobert/sslly-nginx/internal/nginx"
	"github.com/hnrobert/sslly-nginx/internal/ssl"
	"github.com/hnrobert/sslly-nginx/internal/watcher"
)

const (
	configDir             = "./configs"
	sslDir                = "./ssl"
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
}

func New() (*App, error) {
	return &App{
		nginxManager: nginx.NewManager(),
	}, nil
}

func (a *App) Start() error {
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

	// Initial configuration load and nginx setup
	if err := a.reload(); err != nil {
		return fmt.Errorf("initial setup failed: %w", err)
	}

	// Start nginx
	if err := a.nginxManager.Start(); err != nil {
		return fmt.Errorf("failed to start nginx: %w", err)
	}

	// Verify nginx is healthy
	if err := a.nginxManager.CheckHealth(); err != nil {
		return fmt.Errorf("nginx health check failed after initial start: %w", err)
	}

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
				if event.Op&fsnotify.Write == fsnotify.Write || event.Op&fsnotify.Create == fsnotify.Create {
					logger.Info("Config file changed: %s", event.Name)
					a.handleReload()
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
					a.handleReload()
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

func (a *App) reload() error {
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

	// Log warnings for domains without certificates (but don't fail)
	for _, domains := range cfg.Ports {
		for _, domain := range domains {
			if _, ok := ssl.FindCertificate(certMap, domain); !ok {
				logger.Warn("No certificate found for domain: %s (will serve over HTTP)", domain)
			}
		}
	}

	// Generate nginx configuration
	nginxConfig := nginx.GenerateConfig(cfg, certMap)

	// Write nginx configuration
	if err := os.WriteFile(nginxConf, []byte(nginxConfig), 0644); err != nil {
		return fmt.Errorf("failed to write nginx config: %w", err)
	}

	logger.Info("Nginx configuration generated successfully")
	return nil
}

func (a *App) handleReload() {
	logger.Info("Reloading configuration...")

	// Try to reload configuration
	if err := a.reload(); err != nil {
		logger.Error("Failed to reload configuration: %v", err)
		a.restoreGoodConfiguration()
		return
	}

	// Reload nginx
	if err := a.nginxManager.Reload(); err != nil {
		logger.Error("Failed to reload nginx: %v", err)
		a.restoreGoodConfiguration()
		if err := a.nginxManager.Reload(); err != nil {
			logger.Error("Failed to restore nginx: %v", err)
		}
		return
	}

	// Check nginx health
	if err := a.nginxManager.CheckHealth(); err != nil {
		logger.Error("Nginx health check failed after reload: %v", err)
		a.restoreGoodConfiguration()
		if err := a.nginxManager.Reload(); err != nil {
			logger.Error("Failed to restore nginx: %v", err)
		}
		return
	}

	// Save the new good configuration
	a.saveGoodConfiguration()
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
