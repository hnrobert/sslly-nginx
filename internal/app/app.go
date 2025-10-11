package app

import (
	"fmt"
	"log"
	"os"

	"github.com/fsnotify/fsnotify"
	"github.com/sslly-nginx/internal/config"
	"github.com/sslly-nginx/internal/nginx"
	"github.com/sslly-nginx/internal/ssl"
	"github.com/sslly-nginx/internal/watcher"
)

const (
	configDir = "./configs"
	sslDir    = "./ssl"
	nginxConf = "/etc/nginx/nginx.conf"
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

	log.Println("Application started successfully")
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
					log.Printf("Config file changed: %s", event.Name)
					a.handleReload()
				}
			case err, ok := <-configWatcher.Errors:
				if !ok {
					return
				}
				log.Printf("Config watcher error: %v", err)
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
					log.Printf("SSL file changed: %s", event.Name)
					a.handleReload()
				}
			case err, ok := <-sslWatcher.Errors:
				if !ok {
					return
				}
				log.Printf("SSL watcher error: %v", err)
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
			if _, ok := certMap[domain]; !ok {
				log.Printf("WARNING: No certificate found for domain: %s (will serve over HTTP)", domain)
			}
		}
	}

	// Generate nginx configuration
	nginxConfig := nginx.GenerateConfig(cfg, certMap)

	// Write nginx configuration
	if err := os.WriteFile(nginxConf, []byte(nginxConfig), 0644); err != nil {
		return fmt.Errorf("failed to write nginx config: %w", err)
	}

	log.Println("Nginx configuration generated successfully")
	return nil
}

func (a *App) handleReload() {
	log.Println("Reloading configuration...")

	// Try to reload configuration
	if err := a.reload(); err != nil {
		log.Printf("ERROR: Failed to reload configuration: %v", err)
		a.restoreGoodConfiguration()
		return
	}

	// Reload nginx
	if err := a.nginxManager.Reload(); err != nil {
		log.Printf("ERROR: Failed to reload nginx: %v", err)
		a.restoreGoodConfiguration()
		if err := a.nginxManager.Reload(); err != nil {
			log.Printf("ERROR: Failed to restore nginx: %v", err)
		}
		return
	}

	// Check nginx health
	if err := a.nginxManager.CheckHealth(); err != nil {
		log.Printf("ERROR: Nginx health check failed after reload: %v", err)
		a.restoreGoodConfiguration()
		if err := a.nginxManager.Reload(); err != nil {
			log.Printf("ERROR: Failed to restore nginx: %v", err)
		}
		return
	}

	// Save the new good configuration
	a.saveGoodConfiguration()
	log.Println("Configuration reloaded successfully")
}

func (a *App) saveGoodConfiguration() {
	data, err := os.ReadFile(nginxConf)
	if err != nil {
		log.Printf("WARNING: Failed to save good configuration: %v", err)
		return
	}
	a.lastGoodConf = string(data)
}

func (a *App) restoreGoodConfiguration() {
	if a.lastGoodConf == "" {
		log.Println("WARNING: No good configuration to restore")
		return
	}

	if err := os.WriteFile(nginxConf, []byte(a.lastGoodConf), 0644); err != nil {
		log.Printf("ERROR: Failed to restore good configuration: %v", err)
	} else {
		log.Println("Restored previous good configuration")
	}
}
