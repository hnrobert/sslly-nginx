package watcher

import (
	"log"
	"os"
	"path/filepath"

	"github.com/fsnotify/fsnotify"
)

type Watcher struct {
	watcher *fsnotify.Watcher
	Events  chan fsnotify.Event
	Errors  chan error
}

func New(dir string) (*Watcher, error) {
	watcher, err := fsnotify.NewWatcher()
	if err != nil {
		return nil, err
	}

	w := &Watcher{
		watcher: watcher,
		Events:  make(chan fsnotify.Event),
		Errors:  make(chan error),
	}

	// Add directory and all subdirectories
	if err := w.addRecursive(dir); err != nil {
		watcher.Close()
		return nil, err
	}

	// Forward events
	go func() {
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
			log.Printf("Watching directory: %s", path)
		}

		return nil
	})
}

func (w *Watcher) Stop() {
	w.watcher.Close()
	close(w.Events)
	close(w.Errors)
}
