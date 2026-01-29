package watcher

import (
	"os"
	"path/filepath"
	"testing"
	"time"
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

	// Creating a file at root should produce an event.
	rootFile := filepath.Join(tmp, "ok.txt")
	if err := os.WriteFile(rootFile, []byte("ok"), 0644); err != nil {
		t.Fatalf("write root file: %v", err)
	}
	select {
	case <-w.Events:
		// ok
	case err := <-w.Errors:
		t.Fatalf("watcher error: %v", err)
	case <-time.After(2 * time.Second):
		t.Fatalf("expected event for %s", rootFile)
	}

	// Creating a file under ignored dir should NOT produce an event.
	ignoredFile := filepath.Join(ignored, "ignored.txt")
	if err := os.WriteFile(ignoredFile, []byte("ignored"), 0644); err != nil {
		t.Fatalf("write ignored file: %v", err)
	}

	select {
	case ev := <-w.Events:
		t.Fatalf("unexpected event from ignored dir: %+v", ev)
	case err := <-w.Errors:
		t.Fatalf("watcher error: %v", err)
	case <-time.After(300 * time.Millisecond):
		// ok
	}
}
