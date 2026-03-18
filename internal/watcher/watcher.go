package watcher

import (
	"os"
	"path/filepath"
	"strings"
	"sync"

	"github.com/fsnotify/fsnotify"
)

type Watcher struct {
	watcher *fsnotify.Watcher
	Events  chan fsnotify.Event
	Errors  chan error
	done    chan struct{}

	mu          sync.Mutex
	watchedDirs map[string]struct{}
}

func New(dir string) (*Watcher, error) {
	watcher, err := fsnotify.NewWatcher()
	if err != nil {
		return nil, err
	}

	w := &Watcher{
		watcher: watcher,
		Events:  make(chan fsnotify.Event, 64),
		Errors:  make(chan error, 16),
		done:    make(chan struct{}),
		watchedDirs: make(map[string]struct{}),
	}

	// Add directory and all subdirectories
	if err := w.addRecursive(dir); err != nil {
		watcher.Close()
		return nil, err
	}

	// Forward events
	go func() {
		defer close(w.done)
		for {
			select {
			case event, ok := <-watcher.Events:
				if !ok {
					return
				}
				w.maybeAddNewDirWatches(event)
				w.Events <- event
			case err, ok := <-watcher.Errors:
				if !ok {
					return
				}
				w.Errors <- err
			}
		}
	}()

	return w, nil
}

func (w *Watcher) addRecursive(dir string) error {
	return filepath.Walk(dir, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}

		if info.IsDir() {
			if shouldSkipWatchDir(path) {
				return filepath.SkipDir
			}
			if err := w.addWatchDir(path); err != nil {
				return err
			}
			// logger.Info("Watching directory: %s", path)
		}

		return nil
	})
}

func (w *Watcher) maybeAddNewDirWatches(event fsnotify.Event) {
	if event.Op&fsnotify.Create != fsnotify.Create &&
		event.Op&fsnotify.Write != fsnotify.Write &&
		event.Op&fsnotify.Chmod != fsnotify.Chmod &&
		event.Op&fsnotify.Rename != fsnotify.Rename {
		return
	}

	info, err := os.Stat(event.Name)
	if err != nil || !info.IsDir() {
		return
	}

	if err := w.addRecursive(event.Name); err != nil {
		select {
		case w.Errors <- err:
		default:
		}
	}
}

func (w *Watcher) addWatchDir(path string) error {
	// deduplicate	
	clean := filepath.Clean(path)

	w.mu.Lock()
	if _, exists := w.watchedDirs[clean]; exists {
		w.mu.Unlock()
		return nil
	}
	w.mu.Unlock()

	if err := w.watcher.Add(clean); err != nil {
		return err
	}

	w.mu.Lock()
	w.watchedDirs[clean] = struct{}{}
	w.mu.Unlock()

	return nil
}

func (w *Watcher) Stop() {
	if w.watcher != nil {
		_ = w.watcher.Close()
	}
	if w.done != nil {
		<-w.done
	}
	// Safe to close channels after forwarder goroutine exits.
	if w.Events != nil {
		close(w.Events)
	}
	if w.Errors != nil {
		close(w.Errors)
	}
}

func shouldSkipWatchDir(path string) bool {
	// Normalize separators so checks work across platforms.
	p := filepath.ToSlash(path)

	// Ignore internal runtime/backup folders to avoid feedback loops and excessive watches.
	// We match by path segment so nested snapshots are also excluded.
	ignoredSegments := []string{"/.sslly-backups/", "/.sslly-runtime/", "/.git/"}
	for _, seg := range ignoredSegments {
		if strings.Contains(p, seg) {
			return true
		}
	}

	// Also ignore the directory itself if it ends with those names (walk may call with no trailing slash).
	base := filepath.Base(p)
	return base == ".sslly-backups" || base == ".sslly-runtime" || base == ".git"
}
