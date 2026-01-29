package watcher

import (
	"os"
	"path/filepath"

	"github.com/fsnotify/fsnotify"
)

type Watcher struct {
	watcher *fsnotify.Watcher
	Events  chan fsnotify.Event
	Errors  chan error
	done    chan struct{}
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
			if err := w.watcher.Add(path); err != nil {
				return err
			}
			// logger.Info("Watching directory: %s", path)
		}

		return nil
	})
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
