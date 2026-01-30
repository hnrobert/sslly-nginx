package watcher

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/fsnotify/fsnotify"
)

func TestNewAndStop(t *testing.T) {
	tmp := t.TempDir()
	if err := os.MkdirAll(filepath.Join(tmp, "sub"), 0755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}

	w, err := New(tmp)
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	w.Stop()
}

func TestWatcher_SkipsInternalDirs(t *testing.T) {
	tmp := t.TempDir()
	// Create an internal directory that should be ignored by watcher.
	ignored := filepath.Join(tmp, ".sslly-backups", "snapshots", "x", "ssl")
	if err := os.MkdirAll(ignored, 0755); err != nil {
		t.Fatalf("mkdir ignored: %v", err)
	}

	w, err := New(tmp)
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	defer w.Stop()

	// Give fsnotify a tiny moment to set up watches.
	time.Sleep(20 * time.Millisecond)

	// Creating a file at root should produce an event. fsnotify may emit multiple events
	// (CREATE/WRITE/CHMOD), so we wait until we see any event for this specific file.
	rootFile := filepath.Join(tmp, "ok.txt")
	if err := os.WriteFile(rootFile, []byte("ok"), 0644); err != nil {
		t.Fatalf("write root file: %v", err)
	}
	if err := waitForEventOnPath(w.Events, w.Errors, rootFile, 2*time.Second); err != nil {
		t.Fatalf("expected event for %s: %v", rootFile, err)
	}

	// Drain any remaining queued events from the root file write so they don't
	// affect the ignored-dir assertion below.
	drainEvents(w.Events, 150*time.Millisecond)

	// Creating a file under ignored dir should NOT produce an event.
	ignoredFile := filepath.Join(ignored, "ignored.txt")
	if err := os.WriteFile(ignoredFile, []byte("ignored"), 0644); err != nil {
		t.Fatalf("write ignored file: %v", err)
	}

	// Observe for a short window; fail only if we see an event under the ignored path.
	deadline := time.NewTimer(400 * time.Millisecond)
	defer deadline.Stop()
	for {
		select {
		case ev := <-w.Events:
			if isUnderIgnoredDir(ev) {
				t.Fatalf("unexpected event from ignored dir: %+v", ev)
			}
			// ignore unrelated events (e.g. delayed ok.txt WRITE/CHMOD)
		case err := <-w.Errors:
			t.Fatalf("watcher error: %v", err)
		case <-deadline.C:
			return
		}
	}
}

func waitForEventOnPath(events <-chan fsnotify.Event, errors <-chan error, path string, timeout time.Duration) error {
	timer := time.NewTimer(timeout)
	defer timer.Stop()
	for {
		select {
		case ev := <-events:
			if ev.Name == path {
				return nil
			}
		case err := <-errors:
			return err
		case <-timer.C:
			return os.ErrDeadlineExceeded
		}
	}
}

func drainEvents(events <-chan fsnotify.Event, d time.Duration) {
	timer := time.NewTimer(d)
	defer timer.Stop()
	for {
		select {
		case <-events:
			// drain
		case <-timer.C:
			return
		}
	}
}

func isUnderIgnoredDir(ev fsnotify.Event) bool {
	name := filepath.ToSlash(ev.Name)
	return strings.Contains(name, "/.sslly-backups/") || strings.Contains(name, "/.sslly-runtime/")
}
