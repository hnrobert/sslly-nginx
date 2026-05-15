package app

import (
	"fmt"
	"os"
	"path/filepath"
	"time"

	"github.com/fsnotify/fsnotify"
	"github.com/hnrobert/sslly-nginx/internal/logger"
	"github.com/hnrobert/sslly-nginx/internal/watcher"
)

func isEffectiveConfigPath(p string) bool {
	base := filepath.Base(p)
	return base == "proxy.yaml" || base == "cors.yaml" || base == "logs.yaml"
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
				if !isEffectiveConfigPath(event.Name) {
					continue
				}
				if event.Op&fsnotify.Write == fsnotify.Write ||
					event.Op&fsnotify.Create == fsnotify.Create ||
					event.Op&fsnotify.Rename == fsnotify.Rename ||
					event.Op&fsnotify.Remove == fsnotify.Remove ||
					event.Op&fsnotify.Chmod == fsnotify.Chmod {
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
					event.Op&fsnotify.Rename == fsnotify.Rename ||
					event.Op&fsnotify.Remove == fsnotify.Remove ||
					event.Op&fsnotify.Chmod == fsnotify.Chmod {
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

	// Watch /etc/nginx/nginx.conf for manual edits
	etcWatcher, err := fsnotify.NewWatcher()
	if err != nil {
		return fmt.Errorf("failed to create nginx.conf watcher: %w", err)
	}
	if err := etcWatcher.Add(nginxConf); err != nil {
		etcWatcher.Close()
		logger.Warn("Could not watch %s: %v", nginxConf, err)
	} else {
		go func() {
			for {
				select {
				case event, ok := <-etcWatcher.Events:
					if !ok {
						return
					}
					if event.Op&fsnotify.Write == fsnotify.Write ||
						event.Op&fsnotify.Create == fsnotify.Create {
						if a.isNginxWatchSuppressed() {
							continue
						}
						logger.Info("Manual edit detected: %s", event.Name)
						runtimeConf := runtimeNginxConfPath()
						a.handleNginxConfEdit(nginxConf, runtimeConf)
					}
				case err, ok := <-etcWatcher.Errors:
					if !ok {
						return
					}
					logger.Error("nginx.conf watcher error: %v", err)
				}
			}
		}()
	}

	// Watch runtime nginx.conf for manual edits
	if err := a.reRegisterRuntimeNginxWatcher(); err != nil {
		logger.Warn("Could not watch runtime nginx.conf: %v", err)
	}

	return nil
}

// reRegisterRuntimeNginxWatcher closes any existing runtime nginx.conf watcher
// and creates a new one. Called after each activateRuntimeSnapshot because the
// current/ directory is replaced by rename, invalidating the previous inode.
func (a *App) reRegisterRuntimeNginxWatcher() error {
	if a.runtimeNginxWatcher != nil {
		a.runtimeNginxWatcher.Close()
		a.runtimeNginxWatcher = nil
	}

	runtimeConf := runtimeNginxConfPath()
	if _, err := os.Stat(runtimeConf); err != nil {
		// File doesn't exist yet; will be registered on next snapshot activation.
		return nil
	}

	w, err := fsnotify.NewWatcher()
	if err != nil {
		return err
	}
	if err := w.Add(runtimeConf); err != nil {
		w.Close()
		return err
	}
	a.runtimeNginxWatcher = w

	go func() {
		for {
			select {
			case event, ok := <-w.Events:
				if !ok {
					return
				}
				if event.Op&fsnotify.Write == fsnotify.Write ||
					event.Op&fsnotify.Create == fsnotify.Create {
					if a.isNginxWatchSuppressed() {
						continue
					}
					logger.Info("Manual edit detected: %s", event.Name)
					a.handleNginxConfEdit(runtimeConf, nginxConf)
				}
			case err, ok := <-w.Errors:
				if !ok {
					return
				}
				logger.Error("runtime nginx.conf watcher error: %v", err)
			}
		}
	}()

	return nil
}

// runtimeNginxConfPath returns the absolute path to the runtime nginx.conf.
func runtimeNginxConfPath() string {
	abs, err := filepath.Abs(filepath.Join(runtimeDir, "current", "nginx", "nginx.conf"))
	if err != nil {
		return filepath.Join(runtimeDir, "current", "nginx", "nginx.conf")
	}
	return abs
}

// suppressNginxWatch marks both nginx.conf watchers to ignore events for 2s.
func (a *App) suppressNginxWatch() {
	a.suppressNginxMu.Lock()
	a.suppressNginxWatchUntil = time.Now().Add(2 * time.Second)
	a.suppressNginxMu.Unlock()
}

func (a *App) isNginxWatchSuppressed() bool {
	a.suppressNginxMu.Lock()
	defer a.suppressNginxMu.Unlock()
	return time.Now().Before(a.suppressNginxWatchUntil)
}

// handleNginxConfEdit handles a manual edit on src: syncs to dst, validates, reloads.
func (a *App) handleNginxConfEdit(src, dst string) {
	a.reloadMu.Lock()
	defer a.reloadMu.Unlock()

	data, err := os.ReadFile(src)
	if err != nil {
		logger.Error("Failed to read %s: %v", src, err)
		return
	}

	// Suppress watchers before writing to dst.
	a.suppressNginxWatch()
	if err := os.WriteFile(dst, data, 0644); err != nil {
		logger.Error("Failed to sync nginx.conf to %s: %v", dst, err)
		return
	}

	if err := a.nginxManager.Reload(); err != nil {
		logger.Error("nginx reload failed after manual edit: %v", err)
		a.restoreGoodConfiguration()
		if err := a.nginxManager.Reload(); err != nil {
			logger.Error("Failed to restore nginx after bad manual edit: %v", err)
		}
		return
	}

	a.saveGoodConfiguration()
	logger.Info("nginx reloaded from manual edit of %s", src)
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
