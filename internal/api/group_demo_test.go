package api

// A readable walkthrough of the group feature: each step prints what the
// file / API / generator produced. Run with:
//
//	go test ./internal/api/ -run TestGroupDemo -v
//
// It doubles as a regression test (assertions inline with the narrative).
import (
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/hnrobert/sslly-nginx/internal/config"
)

func TestGroupDemo(t *testing.T) {
	e := newTestEnv(t)

	// Hand-write a grouped proxy.yaml (nested form + flat dotted form mixed).
	demoYAML := `# proxy.yaml with groups
1234:
  - legacy.example.com

web:
  front:
    8080:
      - front.example.com
  api:
    7001:
      - api.example.com

media.streams:
  9000:
    - hls.example.com
`
	if err := os.WriteFile(filepath.Join(e.dir, config.ProxyConfigFile), []byte(demoYAML), 0666); err != nil {
		t.Fatal(err)
	}

	cfg, err := config.Load(e.dir)
	if err != nil {
		t.Fatal(err)
	}
	t.Logf("flattened OrderedPorts: %v", cfg.OrderedPorts)
	t.Logf("EntryGroups: %+v", cfg.EntryGroups)

	// --- 1. API write into a NEW nested group -----------------------------
	code, body := e.post(t, "/v1/SetProxyEntry", e.adminToken,
		`{"entry":{"upstreamKey":"9099","listenerKeys":["new.example.com"],"group":"web.admin"}}`)
	if code != http.StatusOK {
		t.Fatalf("set = %d %s", code, body)
	}
	out, _ := os.ReadFile(filepath.Join(e.dir, config.ProxyConfigFile))
	t.Logf("proxy.yaml after SetProxyEntry(group=web.admin):\n%s", out)

	// --- 2. List expands per (group, key) ----------------------------------
	code, body = e.post(t, "/v1/ListProxyEntries", e.adminToken, "{}")
	if code != http.StatusOK {
		t.Fatalf("list = %d %s", code, body)
	}
	t.Logf("ListProxyEntries: %s", body)

	// --- 3. RBAC with group selectors --------------------------------------
	// A user granted `web/*` can edit inside group web.* but not top level.
	code, _ = e.post(t, "/v1/UpsertUser", e.adminToken,
		`{"user":{"name":"webdev","permissions":[{"surface":"E_PERMISSION_SURFACE_PROXY","mode":"E_PERMISSION_MODE_READ_WRITE","upstreams":["web/*"]}]},"token":"webdev-token"}`)
	if code != http.StatusOK {
		t.Fatalf("upsert webdev = %d", code)
	}
	code, _ = e.post(t, "/v1/SetProxyEntry", "webdev-token",
		`{"entry":{"upstreamKey":"8080","listenerKeys":["front.example.com","front2.example.com"],"group":"web.front"}}`)
	t.Logf("webdev edits web.front (web/* grant): %d (want 200)", code)
	code, _ = e.post(t, "/v1/SetProxyEntry", "webdev-token",
		`{"entry":{"upstreamKey":"1234","listenerKeys":["legacy.example.com"]}}`)
	t.Logf("webdev edits TOP-LEVEL 1234:          %d (want 403)", code)

	// List as webdev: only the web-group occurrences are visible.
	code, body = e.post(t, "/v1/ListProxyEntries", "webdev-token", "{}")
	if code != http.StatusOK {
		t.Fatalf("webdev list = %d", code)
	}
	t.Logf("webdev's filtered list: %s", body)
	if strings.Contains(body, "legacy.example.com") || !strings.Contains(body, "\"group\":\"web.front\"") {
		t.Fatalf("group-scoped filtering wrong: %s", body)
	}

	// --- 4. Delete targets (group, key) ------------------------------------
	code, _ = e.post(t, "/v1/DeleteProxyEntry", e.adminToken, `{"upstreamKey":"9099"}`)
	t.Logf("delete 9099 WITHOUT group: %d (want 404 — only the grouped one exists)", code)
	code, body = e.post(t, "/v1/DeleteProxyEntry", e.adminToken, `{"upstreamKey":"9099","group":"web.admin"}`)
	if code != http.StatusOK {
		t.Fatalf("group delete = %d %s", code, body)
	}
	out, _ = os.ReadFile(filepath.Join(e.dir, config.ProxyConfigFile))
	t.Logf("proxy.yaml after group delete (empty web.admin pruned):\n%s", out)
	if strings.Contains(string(out), "9099") || strings.Contains(string(out), "admin:") {
		t.Fatalf("group entry or empty group not pruned:\n%s", out)
	}

	// --- 5. Groups are transparent to nginx --------------------------------
	conf := e.generatedNginxConf(t)
	for _, domain := range []string{"legacy.example.com", "front2.example.com", "api.example.com", "hls.example.com"} {
		if !strings.Contains(conf, "server_name "+domain) {
			t.Fatalf("nginx conf missing %s:\n%s", domain, conf)
		}
	}
	t.Logf("nginx server blocks generated for all domains regardless of grouping ✓")
}
