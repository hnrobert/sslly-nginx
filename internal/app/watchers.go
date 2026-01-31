package app

import (
	"fmt"
	"time"

	"github.com/fsnotify/fsnotify"
	"github.com/hnrobert/sslly-nginx/internal/logger"
	"github.com/hnrobert/sslly-nginx/internal/watcher"
)

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
