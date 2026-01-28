package watcher

import (
	"os"
	"path/filepath"
	"testing"
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
