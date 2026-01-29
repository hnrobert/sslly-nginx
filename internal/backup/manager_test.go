package backup

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
)

func TestCrashRecoveryRestoresLastGood(t *testing.T) {
	tmp := t.TempDir()
	configDir := filepath.Join(tmp, "configs")
	sslDir := filepath.Join(tmp, "ssl")
	runtimeDir := filepath.Join(tmp, "runtime")
	nginxConf := filepath.Join(tmp, "nginx.conf")

	if err := os.MkdirAll(configDir, 0755); err != nil {
		t.Fatalf("mkdir config: %v", err)
	}
	if err := os.MkdirAll(sslDir, 0755); err != nil {
		t.Fatalf("mkdir ssl: %v", err)
	}
	if err := os.MkdirAll(filepath.Join(runtimeDir, "current"), 0755); err != nil {
		t.Fatalf("mkdir runtime: %v", err)
	}

	goodConfig := []byte("1234:\n  - good.example.com\n")
	if err := os.WriteFile(filepath.Join(configDir, "config.yaml"), goodConfig, 0644); err != nil {
		t.Fatalf("write config: %v", err)
	}
	if err := os.WriteFile(filepath.Join(sslDir, "note.txt"), []byte("good"), 0644); err != nil {
		t.Fatalf("write ssl file: %v", err)
	}
	if err := os.WriteFile(nginxConf, []byte("good-nginx"), 0644); err != nil {
		t.Fatalf("write nginx conf: %v", err)
	}
	if err := os.WriteFile(filepath.Join(runtimeDir, "current", "active.txt"), []byte("good-runtime"), 0644); err != nil {
		t.Fatalf("write runtime: %v", err)
	}

	m1, err := NewManager(DefaultBackupRoot(configDir), configDir, sslDir, runtimeDir, nginxConf)
	if err != nil {
		t.Fatalf("new manager: %v", err)
	}
	id1, err := m1.Begin()
	if err != nil {
		t.Fatalf("begin: %v", err)
	}
	if err := m1.Commit(id1); err != nil {
		t.Fatalf("commit: %v", err)
	}

	// Mutate to a "bad" state and start a new in-progress snapshot (simulate crash).
	if err := os.WriteFile(filepath.Join(configDir, "config.yaml"), []byte("9999:\n  - bad.example.com\n"), 0644); err != nil {
		t.Fatalf("write bad config: %v", err)
	}
	if err := os.WriteFile(filepath.Join(sslDir, "note.txt"), []byte("bad"), 0644); err != nil {
		t.Fatalf("write bad ssl: %v", err)
	}
	if err := os.WriteFile(nginxConf, []byte("bad-nginx"), 0644); err != nil {
		t.Fatalf("write bad nginx: %v", err)
	}
	if err := os.WriteFile(filepath.Join(runtimeDir, "current", "active.txt"), []byte("bad-runtime"), 0644); err != nil {
		t.Fatalf("write bad runtime: %v", err)
	}
	_, err = m1.Begin()
	if err != nil {
		t.Fatalf("begin 2: %v", err)
	}
	// intentionally neither Commit nor Abort

	m2, err := NewManager(DefaultBackupRoot(configDir), configDir, sslDir, runtimeDir, nginxConf)
	if err != nil {
		t.Fatalf("new manager 2: %v", err)
	}
	restored, err := m2.MaybeRestoreAfterCrash()
	if err != nil {
		t.Fatalf("maybe restore: %v", err)
	}
	if !restored {
		t.Fatalf("expected restore to happen")
	}

	gotCfg, err := os.ReadFile(filepath.Join(configDir, "config.yaml"))
	if err != nil {
		t.Fatalf("read config: %v", err)
	}
	if string(gotCfg) == string(goodConfig) {
		t.Fatalf("config unexpectedly restored; rollback must not modify user configs")
	}

	gotSSL, err := os.ReadFile(filepath.Join(sslDir, "note.txt"))
	if err != nil {
		t.Fatalf("read ssl: %v", err)
	}
	if string(gotSSL) == "good" {
		t.Fatalf("ssl unexpectedly restored; rollback must not modify user ssl")
	}

	gotNginx, err := os.ReadFile(nginxConf)
	if err != nil {
		t.Fatalf("read nginx: %v", err)
	}
	if string(gotNginx) != "good-nginx" {
		t.Fatalf("nginx conf not restored, got: %q", string(gotNginx))
	}

	gotRuntime, err := os.ReadFile(filepath.Join(runtimeDir, "current", "active.txt"))
	if err != nil {
		t.Fatalf("read runtime: %v", err)
	}
	if string(gotRuntime) != "good-runtime" {
		t.Fatalf("runtime not restored, got: %q", string(gotRuntime))
	}

	// Verify the in-progress marker is cleared.
	stateBytes, err := os.ReadFile(filepath.Join(DefaultBackupRoot(configDir), "state.json"))
	if err != nil {
		t.Fatalf("read state: %v", err)
	}
	var st state
	if err := json.Unmarshal(stateBytes, &st); err != nil {
		t.Fatalf("parse state: %v", err)
	}
	if st.InProgress != "" {
		t.Fatalf("expected inProgress cleared, got: %q", st.InProgress)
	}
	if st.LastGood == "" {
		t.Fatalf("expected lastGood set")
	}
}

func TestAbortClearsInProgress(t *testing.T) {
	tmp := t.TempDir()
	configDir := filepath.Join(tmp, "configs")
	sslDir := filepath.Join(tmp, "ssl")
	runtimeDir := filepath.Join(tmp, "runtime")
	nginxConf := filepath.Join(tmp, "nginx.conf")

	if err := os.MkdirAll(configDir, 0755); err != nil {
		t.Fatalf("mkdir config: %v", err)
	}
	if err := os.MkdirAll(sslDir, 0755); err != nil {
		t.Fatalf("mkdir ssl: %v", err)
	}
	if err := os.MkdirAll(filepath.Join(runtimeDir, "current"), 0755); err != nil {
		t.Fatalf("mkdir runtime: %v", err)
	}
	if err := os.WriteFile(filepath.Join(configDir, "config.yaml"), []byte("1234:\n  - example.com\n"), 0644); err != nil {
		t.Fatalf("write config: %v", err)
	}

	m, err := NewManager(DefaultBackupRoot(configDir), configDir, sslDir, runtimeDir, nginxConf)
	if err != nil {
		t.Fatalf("new manager: %v", err)
	}
	id, err := m.Begin()
	if err != nil {
		t.Fatalf("begin: %v", err)
	}
	if err := m.Abort(id); err != nil {
		t.Fatalf("abort: %v", err)
	}

	m2, err := NewManager(DefaultBackupRoot(configDir), configDir, sslDir, runtimeDir, nginxConf)
	if err != nil {
		t.Fatalf("new manager 2: %v", err)
	}
	restored, err := m2.MaybeRestoreAfterCrash()
	if err != nil {
		t.Fatalf("maybe restore: %v", err)
	}
	if restored {
		t.Fatalf("did not expect restore after abort")
	}
}
