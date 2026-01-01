package app

import (
	"os"
	"path/filepath"
	"testing"
)

func TestEnsureConfigFile_CopyAndPermissions(t *testing.T) {
	tmpDir := t.TempDir()
	defaultFile := filepath.Join(tmpDir, "default.yaml")
	destinationDir := filepath.Join(tmpDir, "dest")
	destinationFile := filepath.Join(destinationDir, "config.yaml")

	// Create default file
	if err := os.WriteFile(defaultFile, []byte("foo: bar\n"), 0644); err != nil {
		t.Fatalf("failed to write default file: %v", err)
	}

	// Ensure destination does not exist
	if err := ensureConfigFile(destinationFile, defaultFile); err != nil {
		t.Fatalf("ensureConfigFile failed: %v", err)
	}

	// Check destination exists and content matches
	data, err := os.ReadFile(destinationFile)
	if err != nil {
		t.Fatalf("failed to read destination file: %v", err)
	}
	if string(data) != "foo: bar\n" {
		t.Fatalf("unexpected destination content: %s", string(data))
	}

	// Check permissions include owner writable/readable
	info, err := os.Stat(destinationFile)
	if err != nil {
		t.Fatalf("failed to stat destination file: %v", err)
	}
	perm := info.Mode().Perm()
	if perm&0644 != 0644 {
		t.Fatalf("destination file perms expected to include 0644, got %o", perm)
	}

	// Calling again should no-op
	if err := ensureConfigFile(destinationFile, defaultFile); err != nil {
		t.Fatalf("ensureConfigFile failed on existing file: %v", err)
	}
}
