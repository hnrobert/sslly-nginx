package config

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"gopkg.in/yaml.v3"
)

// copyFixture copies a file from repoRoot/configs into dir under the live
// config name (e.g. proxy.example.yaml -> proxy.yaml).
func copyFixture(t *testing.T, dir, srcName, dstName string) {
	t.Helper()
	src := filepath.Join("..", "..", "configs", srcName)
	data, err := os.ReadFile(src)
	if err != nil {
		t.Fatalf("read fixture %s: %v", srcName, err)
	}
	if err := os.WriteFile(filepath.Join(dir, dstName), data, 0666); err != nil {
		t.Fatal(err)
	}
}

func topLevelKeys(t *testing.T, path string) []string {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	keys, err := orderedTopLevelKeys(data)
	if err != nil {
		t.Fatal(err)
	}
	return keys
}

func TestEditorProxyReplacePreservesComments(t *testing.T) {
	dir := t.TempDir()
	copyFixture(t, dir, proxyExampleFile, proxyConfigFile)
	ed := NewEditor(dir)

	if _, err := ed.SetProxyEntry("1234", []string{"new.example.com", "alt.example.com"}); err != nil {
		t.Fatalf("SetProxyEntry: %v", err)
	}

	out, _ := os.ReadFile(filepath.Join(dir, proxyConfigFile))
	s := string(out)
	for _, want := range []string{
		"# Format 1: Port only (proxies to 127.0.0.1:port)", // head comment of the replaced key
		"# Format 2: IP:port (proxies to specific IP)",      // untouched neighbours
		"# TCP/UDP Stream Forwarding",
		"new.example.com",
	} {
		if !strings.Contains(s, want) {
			t.Fatalf("expected %q in output:\n%s", want, s)
		}
	}

	// Semantic check: the entry round-trips through Load.
	cfg, err := Load(dir)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if got := cfg.Ports["1234"]; len(got) != 2 || got[0] != "new.example.com" {
		t.Fatalf("unexpected listeners: %v", got)
	}
	// No .tmp-* leftovers from the atomic write.
	entries, _ := os.ReadDir(dir)
	for _, e := range entries {
		if strings.HasPrefix(e.Name(), ".tmp-") {
			t.Fatalf("temp file left behind: %s", e.Name())
		}
	}
}

func TestEditorProxyAppendKeepsOrder(t *testing.T) {
	dir := t.TempDir()
	copyFixture(t, dir, proxyExampleFile, proxyConfigFile)
	ed := NewEditor(dir)

	if _, err := ed.SetProxyEntry("9099", []string{"nine.example.com"}); err != nil {
		t.Fatal(err)
	}
	if _, err := ed.SetProxyEntry("4555", []string{"four.example.com"}); err != nil {
		t.Fatal(err)
	}

	keys := topLevelKeys(t, filepath.Join(dir, proxyConfigFile))
	want := []string{"1234", "9099", "4555"} // special keys absent; new keys appended in order
	if strings.Join(keys, ",") != strings.Join(want, ",") {
		t.Fatalf("key order = %v, want %v", keys, want)
	}

	// Deleting the middle entry leaves the others in place.
	if _, err := ed.DeleteProxyEntry("9099"); err != nil {
		t.Fatal(err)
	}
	keys = topLevelKeys(t, filepath.Join(dir, proxyConfigFile))
	if strings.Join(keys, ",") != "1234,4555" {
		t.Fatalf("key order after delete = %v", keys)
	}

	if _, err := ed.DeleteProxyEntry("9099"); !errors.Is(err, ErrEntryNotFound) {
		t.Fatalf("expected ErrEntryNotFound, got %v", err)
	}
}

func TestEditorSetNoTrailingSlash(t *testing.T) {
	dir := t.TempDir()
	copyFixture(t, dir, proxyExampleFile, proxyConfigFile)
	ed := NewEditor(dir)

	if _, err := ed.SetNoTrailingSlash([]string{"shared.example.com/api"}); err != nil {
		t.Fatal(err)
	}

	cfg, err := Load(dir)
	if err != nil {
		t.Fatal(err)
	}
	if len(cfg.NoTrailingSlash) != 1 || cfg.NoTrailingSlash[0] != "shared.example.com/api" {
		t.Fatalf("unexpected NoTrailingSlash: %v", cfg.NoTrailingSlash)
	}
	// OrderedPorts must not include no_trailing_slash.
	for _, k := range cfg.OrderedPorts {
		if k == "no_trailing_slash" {
			t.Fatalf("no_trailing_slash leaked into OrderedPorts: %v", cfg.OrderedPorts)
		}
	}

	// Replace with an empty list renders [] and round-trips as empty.
	if _, err := ed.SetNoTrailingSlash(nil); err != nil {
		t.Fatal(err)
	}
	out, _ := os.ReadFile(filepath.Join(dir, proxyConfigFile))
	if !strings.Contains(string(out), "no_trailing_slash: []") {
		t.Fatalf("empty list should render as [], got:\n%s", out)
	}
}

func TestEditorCorsUpsertPreservesComments(t *testing.T) {
	dir := t.TempDir()
	copyFixture(t, dir, corsExampleFile, corsConfigFile)
	ed := NewEditor(dir)

	// Partial update of the existing "*" rule: only max_age.
	cfg := CORSConfig{MaxAge: 86400}
	cfg.SetPresence(CORSFieldPresence{MaxAge: true})
	if _, err := ed.SetCorsRule("*", cfg, cfg.Presence()); err != nil {
		t.Fatal(err)
	}

	out, _ := os.ReadFile(filepath.Join(dir, corsConfigFile))
	s := string(out)
	for _, want := range []string{
		"# Wildcard applies to all domains",
		"# Origin to allow",   // comment of an untouched leaf
		"allow_origin: \"*\"", // untouched leaf value
		"max_age: 86400",      // replaced leaf
	} {
		if !strings.Contains(s, want) {
			t.Fatalf("expected %q in output:\n%s", want, s)
		}
	}

	// Append a new rule with a single field.
	fresh := CORSConfig{AllowOrigin: "https://app.example.com"}
	fresh.SetPresence(CORSFieldPresence{AllowOrigin: true})
	if _, err := ed.SetCorsRule("api.example.com", fresh, fresh.Presence()); err != nil {
		t.Fatal(err)
	}

	keys := topLevelKeys(t, filepath.Join(dir, corsConfigFile))
	if strings.Join(keys, ",") != "*,api.example.com" {
		t.Fatalf("cors key order = %v", keys)
	}
}

func TestEditorCorsClearSemanticsRoundTrip(t *testing.T) {
	dir := t.TempDir()
	copyFixture(t, dir, corsExampleFile, corsConfigFile)
	ed := NewEditor(dir)

	// Explicitly clear allow_headers ([]), set allow_origin to "" — both must
	// parse back as present-and-empty.
	cleared := CORSConfig{AllowOrigin: "", AllowHeaders: nil}
	cleared.SetPresence(CORSFieldPresence{AllowOrigin: true, AllowHeaders: true})
	if _, err := ed.SetCorsRule("*.example.com", cleared, cleared.Presence()); err != nil {
		t.Fatal(err)
	}

	out, _ := os.ReadFile(filepath.Join(dir, corsConfigFile))
	s := string(out)
	if !strings.Contains(s, "allow_origin: \"\"") {
		t.Fatalf("cleared origin should render \"\", got:\n%s", s)
	}
	if !strings.Contains(s, "allow_headers: []") {
		t.Fatalf("cleared headers should render [], got:\n%s", s)
	}

	// Parse back with the production presence-tracking path.
	var m map[string]CORSConfig
	if err := yaml.Unmarshal(out, &m); err != nil {
		t.Fatal(err)
	}
	rule := m["*.example.com"]
	p := rule.Presence()
	if !p.AllowOrigin || !p.AllowHeaders {
		t.Fatalf("cleared fields must be present, got %+v", p)
	}
	if rule.AllowOrigin != "" || len(rule.AllowHeaders) != 0 {
		t.Fatalf("cleared fields must be empty, got %+v", rule)
	}
}

func TestEditorCorsDelete(t *testing.T) {
	dir := t.TempDir()
	copyFixture(t, dir, corsExampleFile, corsConfigFile)
	ed := NewEditor(dir)

	if _, err := ed.DeleteCorsRule("*"); err != nil {
		t.Fatal(err)
	}
	if _, err := ed.DeleteCorsRule("*"); !errors.Is(err, ErrEntryNotFound) {
		t.Fatalf("expected ErrEntryNotFound, got %v", err)
	}

	// Deleting from an (now) effectively empty mapping is fine; deleting from
	// a completely empty file also works.
	empty := t.TempDir()
	if err := os.WriteFile(filepath.Join(empty, corsConfigFile), nil, 0666); err != nil {
		t.Fatal(err)
	}
	if _, err := NewEditor(empty).DeleteCorsRule("*"); !errors.Is(err, ErrEntryNotFound) {
		t.Fatalf("empty file delete should be NotFound, got %v", err)
	}
}

func TestEditorCorsOnEmptyFile(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, corsConfigFile), nil, 0666); err != nil {
		t.Fatal(err)
	}

	fresh := CORSConfig{AllowOrigin: "*"}
	fresh.SetPresence(CORSFieldPresence{AllowOrigin: true})
	if _, err := NewEditor(dir).SetCorsRule("example.com", fresh, fresh.Presence()); err != nil {
		t.Fatal(err)
	}

	keys := topLevelKeys(t, filepath.Join(dir, corsConfigFile))
	if strings.Join(keys, ",") != "example.com" {
		t.Fatalf("keys = %v", keys)
	}
}

func TestEditorLogsPartialUpdate(t *testing.T) {
	dir := t.TempDir()
	copyFixture(t, dir, logsExampleFile, logsConfigFile)
	ed := NewEditor(dir)

	lc := LogConfig{Nginx: NginxLogConfig{Level: "debug"}}
	if _, err := ed.UpdateLogsConfig(lc, LogFieldPresence{NginxLevel: true}); err != nil {
		t.Fatal(err)
	}

	out, _ := os.ReadFile(filepath.Join(dir, logsConfigFile))
	s := string(out)
	if !strings.Contains(s, "sslly:\n  level: info") {
		t.Fatalf("untouched sslly.level changed:\n%s", s)
	}
	if !strings.Contains(s, "level: debug") {
		t.Fatalf("nginx.level not updated:\n%s", s)
	}
	if !strings.Contains(s, "# SSLLY-NGINX component log level") {
		t.Fatalf("comment lost:\n%s", s)
	}
	if !strings.Contains(s, "stderr_as: error") {
		t.Fatalf("untouched stderr_as changed:\n%s", s)
	}
}

func TestEditorLogsOnEmptyFile(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, logsConfigFile), nil, 0666); err != nil {
		t.Fatal(err)
	}

	lc := LogConfig{SSLLY: LogLevelConfig{Level: "warn"}, Nginx: NginxLogConfig{Level: "error", StderrAs: "warn"}}
	mask := LogFieldPresence{SSLLYLevel: true, NginxLevel: true, NginxStderrAs: true}
	if _, err := NewEditor(dir).UpdateLogsConfig(lc, mask); err != nil {
		t.Fatal(err)
	}

	var loaded LogConfig
	out, _ := os.ReadFile(filepath.Join(dir, logsConfigFile))
	if err := yaml.Unmarshal(out, &loaded); err != nil {
		t.Fatal(err)
	}
	if loaded.SSLLY.Level != "warn" || loaded.Nginx.Level != "error" || loaded.Nginx.StderrAs != "warn" {
		t.Fatalf("unexpected logs: %+v", loaded)
	}
}

func TestEditorUsersUpsertDelete(t *testing.T) {
	dir := t.TempDir()
	if _, err := EnsureUsersFile(dir, "admin-token"); err != nil {
		t.Fatal(err)
	}
	ed := NewEditor(dir)
	store := LoadUserStore(dir)

	// Upsert with empty hash keeps the existing token.
	keep := User{Name: "admin", Permissions: []Permission{{Surface: SurfaceProxy, Mode: ModeReadWrite}}}
	if _, err := ed.UpsertUser(keep, ""); err != nil {
		t.Fatal(err)
	}
	if _, err := store.VerifyToken("admin-token"); err != nil {
		t.Fatalf("existing token must survive hash-less upsert: %v", err)
	}

	// Upsert with a new hash replaces the token.
	if _, err := ed.UpsertUser(keep, HashToken("rotated")); err != nil {
		t.Fatal(err)
	}
	if _, err := store.VerifyToken("rotated"); err != nil {
		t.Fatalf("rotated token must verify: %v", err)
	}
	if _, err := store.VerifyToken("admin-token"); err == nil {
		t.Fatal("old token must stop working")
	}

	// Add a second user with selectors; verify selectors round-trip.
	ops := User{Name: "ops", Permissions: []Permission{{
		Surface: SurfaceCORS, Mode: ModeReadWrite,
		Domains: []string{"*.ibuduan.com"}, Upstreams: []string{"8080"},
	}}}
	if _, err := ed.UpsertUser(ops, HashToken("ops-token")); err != nil {
		t.Fatal(err)
	}
	users, err := store.Users()
	if err != nil {
		t.Fatal(err)
	}
	if len(users) != 2 {
		t.Fatalf("expected 2 users, got %d", len(users))
	}
	var gotOps *User
	for i := range users {
		if users[i].Name == "ops" {
			gotOps = &users[i]
		}
	}
	if gotOps == nil || len(gotOps.Permissions) != 1 ||
		gotOps.Permissions[0].Domains[0] != "*.ibuduan.com" ||
		gotOps.Permissions[0].Upstreams[0] != "8080" {
		t.Fatalf("ops user did not round-trip: %+v", gotOps)
	}

	// Delete.
	if _, err := ed.DeleteUser("admin"); err != nil {
		t.Fatal(err)
	}
	users, _ = store.Users()
	if len(users) != 1 || users[0].Name != "ops" {
		t.Fatalf("unexpected users after delete: %+v", users)
	}
	if _, err := ed.DeleteUser("admin"); !errors.Is(err, ErrEntryNotFound) {
		t.Fatalf("expected ErrEntryNotFound, got %v", err)
	}
}

func TestEditorRestoreFile(t *testing.T) {
	dir := t.TempDir()
	copyFixture(t, dir, proxyExampleFile, proxyConfigFile)
	ed := NewEditor(dir)

	prev, err := ed.SetProxyEntry("1234", []string{"changed.example.com"})
	if err != nil {
		t.Fatal(err)
	}
	if err := ed.RestoreFile(proxyConfigFile, prev); err != nil {
		t.Fatal(err)
	}

	cfg, err := Load(dir)
	if err != nil {
		t.Fatal(err)
	}
	if got := cfg.Ports["1234"]; len(got) != 2 || got[0] != "yourdomain.com" {
		t.Fatalf("restore did not revert: %v", got)
	}
}
