package config

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestEnsureUsersFileBootstrapFromEnv(t *testing.T) {
	dir := t.TempDir()

	token, err := EnsureUsersFile(dir, "env-admin-token")
	if err != nil {
		t.Fatalf("EnsureUsersFile: %v", err)
	}
	if token != "" {
		t.Fatalf("env-provided token must not be echoed back, got %q", token)
	}

	// The bootstrapped admin can authenticate with the env token.
	store := LoadUserStore(dir)
	u, err := store.VerifyToken("env-admin-token")
	if err != nil {
		t.Fatalf("VerifyToken: %v", err)
	}
	if u.Name != "admin" {
		t.Fatalf("expected admin, got %q", u.Name)
	}
	if len(u.Permissions) != 4 {
		t.Fatalf("expected bootstrap admin with 4 surfaces, got %d", len(u.Permissions))
	}

	// Second call is a no-op when the file exists.
	if _, err := EnsureUsersFile(dir, "other-token"); err != nil {
		t.Fatalf("second EnsureUsersFile: %v", err)
	}
	if u2, err := store.VerifyToken("other-token"); err == nil {
		t.Fatalf("existing file must not be rewritten, but %q authenticated", u2.Name)
	}
}

func TestEnsureUsersFileRandomToken(t *testing.T) {
	dir := t.TempDir()

	token, err := EnsureUsersFile(dir, "")
	if err != nil {
		t.Fatalf("EnsureUsersFile: %v", err)
	}
	if len(token) != 48 { // 24 bytes hex
		t.Fatalf("expected 48-char random token, got %d chars", len(token))
	}
	if _, err := LoadUserStore(dir).VerifyToken(token); err != nil {
		t.Fatalf("random bootstrap token must verify: %v", err)
	}

	// The plaintext token never appears in the file.
	data, err := os.ReadFile(filepath.Join(dir, "users.yaml"))
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(data), token) {
		t.Fatal("plaintext token leaked into users.yaml")
	}
}

func TestUserStoreCacheInvalidation(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "users.yaml")

	write := func(content string) {
		if err := os.WriteFile(path, []byte(content), 0666); err != nil {
			t.Fatal(err)
		}
	}

	write("users:\n  - name: alice\n    token_hash: " + HashToken("alice-token") + "\n    permissions:\n      - surface: logs\n        mode: read\n")
	store := LoadUserStore(dir)
	if _, err := store.VerifyToken("alice-token"); err != nil {
		t.Fatalf("alice should verify: %v", err)
	}
	if _, err := store.VerifyToken("bob-token"); err == nil {
		t.Fatal("bob must not verify")
	}

	// Replace the file with bob only; same size is not guaranteed, but mtime
	// granularity can be coarse — force a size change to be robust.
	write("users:\n  - name: bob\n    token_hash: " + HashToken("bob-token") + "\n    permissions:\n      - surface: logs\n        mode: read-write\n")
	// Ensure mtime advances past the cache even on coarse-grained filesystems.
	future := time.Now().Add(2 * time.Second)
	if err := os.Chtimes(path, future, future); err != nil {
		t.Fatal(err)
	}

	if _, err := store.VerifyToken("alice-token"); err == nil {
		t.Fatal("stale cache: alice should be gone after reload")
	}
	u, err := store.VerifyToken("bob-token")
	if err != nil {
		t.Fatalf("bob should verify after reload: %v", err)
	}
	if u.Permissions[0].Mode != ModeReadWrite {
		t.Fatalf("expected read-write, got %q", u.Permissions[0].Mode)
	}
}

func TestUserStoreFailClosed(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "users.yaml")

	cases := []struct {
		name    string
		content string
	}{
		{"unparseable", "users: ["},
		{"unknown surface", "users:\n  - name: a\n    token_hash: " + HashToken("t") + "\n    permissions:\n      - surface: nope\n        mode: read\n"},
		{"unknown mode", "users:\n  - name: a\n    token_hash: " + HashToken("t") + "\n    permissions:\n      - surface: logs\n        mode: write\n"},
		{"empty name", "users:\n  - name: \"\"\n    token_hash: " + HashToken("t") + "\n    permissions:\n      - surface: logs\n        mode: read\n"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if err := os.WriteFile(path, []byte(tc.content), 0666); err != nil {
				t.Fatal(err)
			}
			if _, err := LoadUserStore(dir).VerifyToken("t"); err == nil {
				t.Fatal("invalid users file must fail closed")
			}
		})
	}

	// Missing file also fails closed.
	if _, err := LoadUserStore(t.TempDir()).Users(); err == nil {
		t.Fatal("missing users file must fail closed")
	}
}

func TestUserStoreMalformedHashFailsClosedPerUser(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "users.yaml")
	content := "users:\n" +
		"  - name: broken\n" +
		"    token_hash: not-hex\n" +
		"    permissions:\n      - surface: logs\n        mode: read\n" +
		"  - name: ok\n" +
		"    token_hash: " + HashToken("ok-token") + "\n" +
		"    permissions:\n      - surface: logs\n        mode: read\n"
	if err := os.WriteFile(path, []byte(content), 0666); err != nil {
		t.Fatal(err)
	}

	store := LoadUserStore(dir)
	if _, err := store.VerifyToken("ok-token"); err != nil {
		t.Fatalf("valid user must still verify: %v", err)
	}
	// A token whose hash coincidentally matches nothing (including the broken
	// entry, which is skipped) must not authenticate.
	if _, err := store.VerifyToken("not-hex"); err == nil {
		t.Fatal("malformed-hash user must fail closed")
	}
}

func TestHashToken(t *testing.T) {
	// Deterministic, 64 hex chars, differs per input.
	h1 := HashToken("a")
	if h1 != HashToken("a") || len(h1) != 64 {
		t.Fatalf("unexpected hash %q", h1)
	}
	if h1 == HashToken("b") {
		t.Fatal("hashes must differ")
	}
}
