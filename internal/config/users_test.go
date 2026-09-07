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

func TestVerifyTokenPlaintextFieldPreferred(t *testing.T) {
	dir := t.TempDir()
	content := "users:\n" +
		"  - name: pw\n" +
		"    token: plaintext-secret\n" +
		"    token_hash: " + HashToken("stale-hash-credential") + "\n" + // stale: must be ignored
		"    permissions:\n      - surface: logs\n        mode: read\n" +
		"  - name: hashonly\n" +
		"    token_hash: " + HashToken("hash-credential") + "\n" +
		"    permissions:\n      - surface: logs\n        mode: read\n" +
		"  - name: nocreds\n" +
		"    permissions:\n      - surface: logs\n        mode: read\n"
	if err := os.WriteFile(UsersFilePath(dir), []byte(content), 0666); err != nil {
		t.Fatal(err)
	}

	store := LoadUserStore(dir)

	// token field is authoritative: its plaintext authenticates.
	if u, err := store.VerifyToken("plaintext-secret"); err != nil || u.Name != "pw" {
		t.Fatalf("token-field login failed: %v %v", u, err)
	}
	// the stale hash next to it must NOT authenticate.
	if _, err := store.VerifyToken("stale-hash-credential"); err == nil {
		t.Fatal("stale token_hash must be ignored while token field exists")
	}
	// users without a token field still go through token_hash.
	if u, err := store.VerifyToken("hash-credential"); err != nil || u.Name != "hashonly" {
		t.Fatalf("hash login failed: %v %v", u, err)
	}
	// a user with neither credential fails closed.
	if _, err := store.VerifyToken("anything"); err == nil {
		t.Fatal("credential-less user must never authenticate")
	}
}

func TestMigrateTokensToHashes(t *testing.T) {
	dir := t.TempDir()
	path := UsersFilePath(dir)
	content := "# users\n" +
		"users:\n" +
		"  - name: alice  # keep me\n" +
		"    token: alice-pw\n" +
		"    permissions:\n      - surface: logs\n        mode: read\n" +
		"  - name: bob\n" +
		"    token_hash: " + HashToken("bob-token") + "\n" +
		"    permissions:\n      - surface: logs\n        mode: read\n" +
		"  - name: carol\n" +
		"    token: carol-pw\n" +
		"    token_hash: " + HashToken("old") + "\n" + // stale: the token field overwrites it
		"    permissions:\n      - surface: logs\n        mode: read\n"
	if err := os.WriteFile(path, []byte(content), 0666); err != nil {
		t.Fatal(err)
	}

	ed := NewEditor(dir)
	n, err := ed.MigrateTokensToHashes()
	if err != nil || n != 2 {
		t.Fatalf("MigrateTokensToHashes = %d, %v; want 2, nil", n, err)
	}

	out, _ := os.ReadFile(path)
	s := string(out)
	if strings.Contains(s, "alice-pw") || strings.Contains(s, "carol-pw") || strings.Contains(s, "token:") {
		t.Fatalf("plaintext tokens must be stripped:\n%s", s)
	}
	if !strings.Contains(s, "# keep me") {
		t.Fatalf("existing comments must survive:\n%s", s)
	}
	if !strings.Contains(s, "converted from token at startup") {
		t.Fatalf("conversion note missing:\n%s", s)
	}

	// Semantics: converted users authenticate with the old plaintext.
	users, err := LoadUserStore(dir).Users()
	if err != nil {
		t.Fatal(err)
	}
	byName := map[string]User{}
	for _, u := range users {
		byName[u.Name] = u
	}
	if byName["alice"].TokenHash != HashToken("alice-pw") {
		t.Fatalf("alice hash wrong: %s", byName["alice"].TokenHash)
	}
	if byName["carol"].TokenHash != HashToken("carol-pw") {
		t.Fatalf("carol's stale hash must be overwritten by her token: %s", byName["carol"].TokenHash)
	}
	if byName["bob"].TokenHash != HashToken("bob-token") {
		t.Fatalf("untouched user's hash changed: %s", byName["bob"].TokenHash)
	}

	// Idempotent: a second pass finds nothing and writes nothing.
	before, _ := os.ReadFile(path)
	if n, err := ed.MigrateTokensToHashes(); err != nil || n != 0 {
		t.Fatalf("second pass = %d, %v; want 0, nil", n, err)
	}
	after, _ := os.ReadFile(path)
	if string(before) != string(after) {
		t.Fatalf("second pass must not rewrite the file")
	}
}
