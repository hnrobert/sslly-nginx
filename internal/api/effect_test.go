package api

import (
	"net/http"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/hnrobert/sslly-nginx/internal/config"
	"github.com/hnrobert/sslly-nginx/internal/nginx"
)

// generatedNginxConf runs the production generation path over the temp
// config dir — what the reload pipeline would hand to nginx after the API
// change is applied.
func (e *testEnv) generatedNginxConf(t *testing.T) string {
	t.Helper()
	cfg, err := config.Load(e.dir)
	if err != nil {
		t.Fatalf("load config: %v", err)
	}
	return nginx.GenerateConfig(cfg, nil)
}

// TestProxyChangesTakeEffectInGeneratedNginxConfig verifies the full chain
// API -> YAML -> Load -> nginx config: an entry added through the API shows
// up as a server block + proxy_pass, and deleting it removes them again.
func TestProxyChangesTakeEffectInGeneratedNginxConfig(t *testing.T) {
	e := newTestEnv(t)

	code, body := e.post(t, "/v1/SetProxyEntry", e.adminToken,
		`{"entry":{"upstreamKey":"7070","listenerKeys":["added.example.com"]}}`)
	if code != http.StatusOK || !strings.Contains(body, `"applied":true`) {
		t.Fatalf("SetProxyEntry = %d %s", code, body)
	}

	conf := e.generatedNginxConf(t)
	if !strings.Contains(conf, "server_name added.example.com") {
		t.Fatalf("added route missing from generated nginx.conf:\n%.500s", conf)
	}
	if !strings.Contains(conf, "proxy_pass http://127.0.0.1:7070") {
		t.Fatalf("upstream 7070 missing from generated nginx.conf:\n%.500s", conf)
	}

	// Delete it; the server block must disappear.
	code, body = e.post(t, "/v1/DeleteProxyEntry", e.adminToken, `{"upstreamKey":"7070"}`)
	if code != http.StatusOK || !strings.Contains(body, `"applied":true`) {
		t.Fatalf("DeleteProxyEntry = %d %s", code, body)
	}
	conf = e.generatedNginxConf(t)
	if strings.Contains(conf, "added.example.com") {
		t.Fatalf("deleted route still present in generated nginx.conf:\n%.500s", conf)
	}
}

// TestNoTrailingSlashChangeTakesEffect: by default a path listener gets a
// 301 redirect block; after SetNoTrailingSlash the redirect disappears while
// the location stays.
func TestNoTrailingSlashChangeTakesEffect(t *testing.T) {
	e := newTestEnv(t)

	code, body := e.post(t, "/v1/SetProxyEntry", e.adminToken,
		`{"entry":{"upstreamKey":"7070","listenerKeys":["route.example.com/api"]}}`)
	if code != http.StatusOK {
		t.Fatalf("SetProxyEntry = %d %s", code, body)
	}

	// Default: /api redirects to /api/.
	conf := e.generatedNginxConf(t)
	if !strings.Contains(conf, "location = /api") {
		t.Fatalf("default trailing-slash redirect missing:\n%.800s", conf)
	}

	code, body = e.post(t, "/v1/SetNoTrailingSlash", e.adminToken,
		`{"listenerKeys":["route.example.com/api"]}`)
	if code != http.StatusOK || !strings.Contains(body, `"applied":true`) {
		t.Fatalf("SetNoTrailingSlash = %d %s", code, body)
	}

	conf = e.generatedNginxConf(t)
	if strings.Contains(conf, "location = /api") {
		t.Fatalf("redirect should be gone after no_trailing_slash:\n%.800s", conf)
	}
	if !strings.Contains(conf, "location /api/") {
		t.Fatalf("proxy location /api/ must survive:\n%.800s", conf)
	}
}

// TestCorsChangesTakeEffectInGeneratedNginxConfig: a rule set through the
// API lands in the generated CORS headers (OPTIONS-only origin), a
// suffix-wildcard rule covers subdomains, and deleting the rule removes the
// headers.
func TestCorsChangesTakeEffectInGeneratedNginxConfig(t *testing.T) {
	e := newTestEnv(t)

	code, body := e.post(t, "/v1/SetProxyEntry", e.adminToken,
		`{"entry":{"upstreamKey":"7070","listenerKeys":["api.cors-test.example"]}}`)
	if code != http.StatusOK {
		t.Fatalf("SetProxyEntry = %d %s", code, body)
	}

	code, body = e.post(t, "/v1/SetCorsRule", e.adminToken,
		`{"rule":{"key":"*.cors-test.example","allowOrigin":"https://app.example.org"},"updateMask":"allowOrigin"}`)
	if code != http.StatusOK || !strings.Contains(body, `"applied":true`) {
		t.Fatalf("SetCorsRule = %d %s", code, body)
	}

	conf := e.generatedNginxConf(t)
	// Origin is emitted only inside the OPTIONS block (16-space indent).
	if !strings.Contains(conf, "add_header 'Access-Control-Allow-Origin' 'https://app.example.org' always;") {
		t.Fatalf("cors origin missing from generated nginx.conf:\n%.800s", conf)
	}
	// And never at the location level.
	for _, line := range strings.Split(conf, "\n") {
		if strings.HasPrefix(line, "            add_header 'Access-Control-Allow-Origin'") {
			t.Fatalf("origin must not appear at location level:\n%s", line)
		}
	}

	code, body = e.post(t, "/v1/DeleteCorsRule", e.adminToken, `{"key":"*.cors-test.example"}`)
	if code != http.StatusOK {
		t.Fatalf("DeleteCorsRule = %d %s", code, body)
	}
	conf = e.generatedNginxConf(t)
	if strings.Contains(conf, "https://app.example.org") {
		t.Fatalf("deleted cors rule still generated:\n%.800s", conf)
	}
}

// TestPermissionChangesTakeEffectImmediately: permission edits via the API
// apply to the very next request with the SAME token (mtime cache, no
// restart) — both granting and revoking.
func TestPermissionChangesTakeEffectImmediately(t *testing.T) {
	e := newTestEnv(t)

	// Create a scoped writer: cors rw under *.ibuduan.com.
	mk := func(mode string) string {
		return `{"user":{"name":"live","permissions":[{"surface":"E_PERMISSION_SURFACE_CORS","mode":"` + mode + `","domains":["*.ibuduan.com"]}]}},"token":"live-token"}`
	}
	code, body := e.post(t, "/v1/UpsertUser", e.adminToken,
		`{"user":{"name":"live","permissions":[{"surface":"E_PERMISSION_SURFACE_CORS","mode":"E_PERMISSION_MODE_READ_WRITE","domains":["*.ibuduan.com"]}]},"token":"live-token"}`)
	if code != http.StatusOK {
		t.Fatalf("UpsertUser = %d %s", code, body)
	}

	setRule := func(key string) int {
		c, _ := e.post(t, "/v1/SetCorsRule", "live-token",
			`{"rule":{"key":"`+key+`","allowOrigin":"https://x.test"},"updateMask":"allowOrigin"}`)
		return c
	}

	// Granted: subdomain write works.
	if c := setRule("api.ibuduan.com"); c != http.StatusOK {
		t.Fatalf("rw subdomain write = %d", c)
	}
	// Domain boundary enforced from the start: bare apex denied.
	if c := setRule("ibuduan.com"); c != http.StatusForbidden {
		t.Fatalf("bare apex write = %d, want 403", c)
	}

	// Downgrade to read-only: the SAME token must lose write access at once.
	code, body = e.post(t, "/v1/UpsertUser", e.adminToken, mk("E_PERMISSION_MODE_READ"))
	if code != http.StatusOK {
		t.Fatalf("downgrade UpsertUser = %d %s", code, body)
	}
	if c := setRule("api2.ibuduan.com"); c != http.StatusForbidden {
		t.Fatalf("write after downgrade = %d, want 403", c)
	}
	// ...but keeps read access.
	code, _ = e.post(t, "/v1/ListCorsRules", "live-token", "{}")
	if code != http.StatusOK {
		t.Fatalf("read after downgrade = %d", code)
	}

	// Upgrade back: write works again.
	code, body = e.post(t, "/v1/UpsertUser", e.adminToken, mk("E_PERMISSION_MODE_READ_WRITE"))
	if code != http.StatusOK {
		t.Fatalf("upgrade UpsertUser = %d %s", code, body)
	}
	if c := setRule("api3.ibuduan.com"); c != http.StatusOK {
		t.Fatalf("write after upgrade = %d", c)
	}
}

// TestHandEditedPermissionsTakeEffect: editing users.yaml by hand (no API,
// no restart) changes authorization on the next request — the mtime cache
// picks it up.
func TestHandEditedPermissionsTakeEffect(t *testing.T) {
	e := newTestEnv(t)

	code, _ := e.post(t, "/v1/UpsertUser", e.adminToken,
		`{"user":{"name":"hand","permissions":[{"surface":"E_PERMISSION_SURFACE_LOGS","mode":"E_PERMISSION_MODE_READ_WRITE"}]},"token":"hand-token"}`)
	if code != http.StatusOK {
		t.Fatalf("UpsertUser = %d", code)
	}
	code, _ = e.post(t, "/v1/GetLogsConfig", "hand-token", "{}")
	if code != http.StatusOK {
		t.Fatalf("hand token should read logs, got %d", code)
	}

	// Hand-edit the file: strip the logs permission entirely.
	if err := e.srv.cfg.Editor.RestoreFile(config.UsersConfigFile, []byte("users: []\n")); err != nil {
		t.Fatal(err)
	}
	code, _ = e.post(t, "/v1/GetLogsConfig", "hand-token", "{}")
	if code != http.StatusUnauthorized {
		t.Fatalf("removed user must stop authenticating, got %d", code)
	}
}

// TestUpstreamScopedWriteBoundary: a user granted proxy write on specific
// upstream keys can edit those entries and nothing else.
func TestUpstreamScopedWriteBoundary(t *testing.T) {
	e := newTestEnv(t)

	code, body := e.post(t, "/v1/UpsertUser", e.adminToken,
		`{"user":{"name":"portops","permissions":[{"surface":"E_PERMISSION_SURFACE_PROXY","mode":"E_PERMISSION_MODE_READ_WRITE","upstreams":["8080"]}]},"token":"portops-token"}`)
	if code != http.StatusOK {
		t.Fatalf("UpsertUser = %d %s", code, body)
	}

	set := func(upstream string) int {
		c, _ := e.post(t, "/v1/SetProxyEntry", "portops-token",
			`{"entry":{"upstreamKey":"`+upstream+`","listenerKeys":["x.example.com"]}}`)
		return c
	}

	if c := set("8080"); c != http.StatusOK {
		t.Fatalf("write within granted upstream = %d", c)
	}
	if c := set("9099"); c != http.StatusForbidden {
		t.Fatalf("write outside granted upstream = %d, want 403", c)
	}

	// Delete is scoped the same way: 8080 (created above) is deletable, the
	// fixture's 1234 is not.
	code, _ = e.post(t, "/v1/DeleteProxyEntry", "portops-token", `{"upstreamKey":"8080"}`)
	if code != http.StatusOK {
		t.Fatalf("delete within granted upstream = %d", code)
	}
	code, _ = e.post(t, "/v1/DeleteProxyEntry", "portops-token", `{"upstreamKey":"1234"}`)
	if code != http.StatusForbidden {
		t.Fatalf("delete outside granted upstream = %d, want 403", code)
	}
	// The fixture entry survives.
	conf := e.generatedNginxConf(t)
	if !strings.Contains(conf, "server_name yourdomain.com") {
		t.Fatalf("fixture entry must be untouched:\n%.500s", conf)
	}
}

// TestTokenFieldHotReloadAuth: a user added to users.yaml with a
// plaintext token while the service is RUNNING (hot reload — no
// cold-start migration) authenticates with that token through the
// gateway, while existing hash users keep working.
func TestTokenFieldHotReloadAuth(t *testing.T) {
	e := newTestEnv(t)

	// Simulate the operator hand-editing users.yaml at runtime: append a
	// token-style user next to the existing hash-style admin/ops.
	path := config.UsersFilePath(e.dir)
	if err := os.WriteFile(path, []byte(
		"users:\n"+
			"  - name: admin\n"+
			"    token_hash: "+config.HashToken(e.adminToken)+"\n"+
			"    permissions:\n      - surface: users\n        mode: read-write\n"+
			"  - name: pwuser\n"+
			"    token: live-plaintext\n"+
			"    permissions:\n      - surface: logs\n        mode: read-write\n"), 0666); err != nil {
		t.Fatal(err)
	}
	// Push mtime past the store's cache granularity.
	future := time.Now().Add(2 * time.Second)
	if err := os.Chtimes(path, future, future); err != nil {
		t.Fatal(err)
	}

	// Hash-style user still authenticates.
	code, _ := e.post(t, "/v1/ListUsers", e.adminToken, "{}")
	if code != http.StatusOK {
		t.Fatalf("hash-style admin = %d", code)
	}

	// token-style user authenticates with the plaintext as bearer.
	code, _ = e.post(t, "/v1/GetLogsConfig", "live-plaintext", "{}")
	if code != http.StatusOK {
		t.Fatalf("token-style user = %d", code)
	}

	// The file was NOT migrated by the hot reload (no conversion happened):
	// the plaintext must still be on disk.
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(data), "token: live-plaintext") ||
		strings.Contains(string(data), "converted from token") {
		t.Fatalf("hot reload must leave token fields untouched:\n%s", data)
	}
}
