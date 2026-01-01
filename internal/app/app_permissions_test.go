package app

import (
	"os"
	"path/filepath"
	"testing"
)

func TestEnsureDirWritable(t *testing.T) {
	tmp := t.TempDir()
	// create files and dirs
	os.MkdirAll(filepath.Join(tmp, "subdir"), 0755)
	os.WriteFile(filepath.Join(tmp, "file.txt"), []byte("x"), 0644)
	os.WriteFile(filepath.Join(tmp, "subdir", "inner.txt"), []byte("y"), 0644)

	if err := ensureDirWritable(tmp); err != nil {
		t.Fatalf("ensureDirWritable failed: %v", err)
	}

	// check perms
	if info, err := os.Stat(filepath.Join(tmp, "file.txt")); err != nil {
		t.Fatalf("stat file failed: %v", err)
	} else {
		perm := info.Mode().Perm()
		if perm&0666 != 0666 {
			t.Fatalf("expected file perms to include 0666, got %o", perm)
		}
	}

	if info, err := os.Stat(tmp); err != nil {
		t.Fatalf("stat dir failed: %v", err)
	} else {
		perm := info.Mode().Perm()
		if perm&0777 != 0777 {
			t.Fatalf("expected dir perms to include 0777, got %o", perm)
		}
	}
}
