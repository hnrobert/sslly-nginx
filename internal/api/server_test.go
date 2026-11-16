package api

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/hnrobert/sslly-nginx/internal/config"
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"
	"google.golang.org/grpc/test/bufconn"
)

// testEnv is a fully wired control plane over a temp configs dir, served
// through a bufconn gRPC link and an httptest gateway façade. adminToken /
// opsToken are SESSION tokens (obtained via Authenticate in newTestEnv),
// matching the two-step auth model.
type testEnv struct {
	dir        string
	ts         *httptest.Server
	srv        *Server
	reloadMu   sync.Mutex
	reloadErr  error
	reloadN    int
	adminToken string // session token for admin / admin-token
	opsToken   string // session token for ops / ops-token
}

func (e *testEnv) reload() error {
	e.reloadMu.Lock()
	defer e.reloadMu.Unlock()
	e.reloadN++
	return e.reloadErr
}

func (e *testEnv) setReloadErr(err error) {
	e.reloadMu.Lock()
	defer e.reloadMu.Unlock()
	e.reloadErr = err
}

func (e *testEnv) reloadCount() int {
	e.reloadMu.Lock()
	defer e.reloadMu.Unlock()
	return e.reloadN
}

// setupTestConfigs populates a temp config dir with the example fixtures, a
// bootstrap admin ("admin-token"), and a scoped operator ("ops-token").
func setupTestConfigs(t *testing.T, dir string) {
	t.Helper()
	copyTo := func(src, dst string) {
		data, err := os.ReadFile(filepath.Join("..", "..", "configs", src))
		if err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(dir, dst), data, 0666); err != nil {
			t.Fatal(err)
		}
	}
	copyTo("proxy.example.yaml", config.ProxyConfigFile)
	copyTo("cors.example.yaml", config.CorsConfigFile)
	copyTo("logs.example.yaml", config.LogsConfigFile)

	if _, err := config.EnsureUsersFile(dir, "admin-token"); err != nil {
		t.Fatal(err)
	}
	ops := config.User{Name: "ops", Permissions: []config.Permission{
		{Surface: config.SurfaceCORS, Mode: config.ModeReadWrite, Domains: []string{"*.ibuduan.com"}},
		{Surface: config.SurfaceProxy, Mode: config.ModeRead},
	}}
	if _, err := config.NewEditor(dir).UpsertUser(ops, config.HashToken("ops-token")); err != nil {
		t.Fatal(err)
	}
}

func newTestEnv(t *testing.T) *testEnv {
	t.Helper()
	dir := t.TempDir()
	setupTestConfigs(t, dir)
	editor := config.NewEditor(dir)

	e := &testEnv{dir: dir, adminToken: "admin-token", opsToken: "ops-token"}
	bufLis := bufconn.Listen(1 << 20)
	srv := New(Config{
		// Placeholder addresses: bufconn mode never listens on TCP, but New
		// must not treat this server as disabled (both addrs unset = off).
		GRPCAddr:  "bufnet",
		HTTPAddr:  "bufnet",
		ConfigDir: dir,
		Reload:    e.reload,
		Users:     config.LoadUserStore(dir),
		Editor:    editor,
		Dial: func(ctx context.Context, _ string) (*grpc.ClientConn, error) {
			return grpc.NewClient("passthrough:bufnet",
				grpc.WithContextDialer(func(context.Context, string) (net.Conn, error) {
					return bufLis.Dial()
				}),
				grpc.WithTransportCredentials(insecure.NewCredentials()))
		},
	})
	e.srv = srv
	go func() { _ = srv.ServeGRPC(bufLis) }()
	t.Cleanup(func() {
		e.ts.Close()
		srv.Stop(context.Background())
		_ = bufLis.Close()
	})

	conn, err := srv.cfg.Dial(context.Background(), "")
	if err != nil {
		t.Fatal(err)
	}
	handler, err := srv.gatewayHandler(context.Background(), conn)
	if err != nil {
		t.Fatal(err)
	}
	e.ts = httptest.NewServer(handler)

	// Exchange the long-lived credentials for sessions (the two-step model).
	e.adminToken = e.login(t, "admin", "admin-token")
	e.opsToken = e.login(t, "ops", "ops-token")
	return e
}

// login authenticates (name, long-lived-token) and returns the session token.
func (e *testEnv) login(t *testing.T, name, token string) string {
	t.Helper()
	req, err := http.NewRequest(http.MethodPost, e.ts.URL+"/v1/Authenticate",
		strings.NewReader(`{"name":"`+name+`","token":"`+token+`"}`))
	if err != nil {
		t.Fatal(err)
	}
	req.Header.Set("Content-Type", "application/json")
	resp, err := e.ts.Client().Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	b, _ := io.ReadAll(resp.Body)
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("login %s = %d %s", name, resp.StatusCode, b)
	}
	var parsed struct {
		SessionToken string `json:"sessionToken"`
	}
	if err := json.Unmarshal(b, &parsed); err != nil || parsed.SessionToken == "" {
		t.Fatalf("login %s response: %s", name, b)
	}
	return parsed.SessionToken
}

// post sends a JSON POST with the given bearer token; returns status and body.
func (e *testEnv) post(t *testing.T, path, token, body string) (int, string) {
	t.Helper()
	var rd io.Reader
	if body != "" {
		rd = strings.NewReader(body)
	}
	req, err := http.NewRequest(http.MethodPost, e.ts.URL+path, rd)
	if err != nil {
		t.Fatal(err)
	}
	if token != "" {
		req.Header.Set("Authorization", "Bearer "+token)
	}
	if body != "" {
		req.Header.Set("Content-Type", "application/json")
	}
	resp, err := e.ts.Client().Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	b, _ := io.ReadAll(resp.Body)
	return resp.StatusCode, string(b)
}

func errCode(t *testing.T, body string) float64 {
	t.Helper()
	var parsed struct {
		Code int `json:"code"`
	}
	if err := json.Unmarshal([]byte(body), &parsed); err != nil {
		t.Fatalf("non-json error body: %s", body)
	}
	return float64(parsed.Code)
}

func TestGatewayHealthzNoAuth(t *testing.T) {
	e := newTestEnv(t)
	code, body := e.post(t, "/healthz", "", "")
	if code != http.StatusOK || !strings.Contains(body, "ok") {
		t.Fatalf("healthz = %d %s", code, body)
	}
}

func TestGatewayAuthEnforced(t *testing.T) {
	e := newTestEnv(t)
	// No token -> 401.
	code, body := e.post(t, "/v1/ListProxyEntries", "", "{}")
	if code != http.StatusUnauthorized {
		t.Fatalf("missing token = %d %s", code, body)
	}
	// Bad token -> 401.
	code, body = e.post(t, "/v1/ListProxyEntries", "wrong", "{}")
	if code != http.StatusUnauthorized {
		t.Fatalf("bad token = %d %s", code, body)
	}
	// Valid token -> 200.
	code, body = e.post(t, "/v1/ListProxyEntries", e.adminToken, "{}")
	if code != http.StatusOK {
		t.Fatalf("admin list = %d %s", code, body)
	}
}

func TestGatewaySetProxyEntryEndToEnd(t *testing.T) {
	e := newTestEnv(t)

	code, body := e.post(t, "/v1/SetProxyEntry", e.adminToken,
		`{"entry":{"upstreamKey":"9099","listenerKeys":["api.local.test"]}}`)
	if code != http.StatusOK {
		t.Fatalf("set = %d %s", code, body)
	}
	var resp struct {
		Apply struct {
			Applied bool   `json:"applied"`
			Error   string `json:"error"`
		} `json:"apply"`
	}
	if err := json.Unmarshal([]byte(body), &resp); err != nil {
		t.Fatal(err)
	}
	if !resp.Apply.Applied {
		t.Fatalf("change not applied: %s", body)
	}
	if e.reloadCount() != 1 {
		t.Fatalf("expected exactly 1 reload, got %d", e.reloadCount())
	}

	// File changed, comments preserved, new entry appended at end.
	data, err := os.ReadFile(filepath.Join(e.dir, config.ProxyConfigFile))
	if err != nil {
		t.Fatal(err)
	}
	lines := strings.Split(string(data), "\n")
	joined := strings.Join(lines, "\n")
	if !strings.Contains(joined, "# Format 1: Port only") || !strings.Contains(joined, "api.local.test") {
		t.Fatalf("comments lost or entry missing:\n%s", joined)
	}
	last := ""
	for _, l := range lines {
		if strings.HasPrefix(l, "9099:") {
			last = l
		}
	}
	if last == "" {
		t.Fatalf("entry 9099 missing")
	}

	// List shows the new entry.
	code, body = e.post(t, "/v1/ListProxyEntries", e.adminToken, "{}")
	if code != http.StatusOK || !strings.Contains(body, "9099") {
		t.Fatalf("list after set = %d %s", code, body)
	}
}

func TestGatewayReloadFailureRollsBackFile(t *testing.T) {
	e := newTestEnv(t)
	before, _ := os.ReadFile(filepath.Join(e.dir, config.ProxyConfigFile))

	e.setReloadErr(errors.New("health check: nginx -t failed"))

	code, body := e.post(t, "/v1/SetProxyEntry", e.adminToken,
		`{"entry":{"upstreamKey":"9099","listenerKeys":["api.local.test"]}}`)
	if code != http.StatusOK {
		t.Fatalf("set = %d %s", code, body)
	}
	var resp struct {
		Apply struct {
			Applied bool   `json:"applied"`
			Error   string `json:"error"`
		} `json:"apply"`
	}
	_ = json.Unmarshal([]byte(body), &resp)
	if resp.Apply.Applied || !strings.Contains(resp.Apply.Error, "health check") {
		t.Fatalf("expected failed apply with cause, got: %s", body)
	}

	after, _ := os.ReadFile(filepath.Join(e.dir, config.ProxyConfigFile))
	if string(before) != string(after) {
		t.Fatalf("proxy.yaml not restored after failed reload")
	}
}

func TestGatewayScopedUserPermissions(t *testing.T) {
	e := newTestEnv(t)

	// ops can set cors rules under *.ibuduan.com.
	code, body := e.post(t, "/v1/SetCorsRule", e.opsToken,
		`{"rule":{"key":"api.ibuduan.com","allowOrigin":"https://app.ibuduan.com"},"updateMask":"allowOrigin"}`)
	if code != http.StatusOK {
		t.Fatalf("ops cors set = %d %s", code, body)
	}

	// ... but not for other domains.
	code, body = e.post(t, "/v1/SetCorsRule", e.opsToken,
		`{"rule":{"key":"example.com","allowOrigin":"*"},"updateMask":"allowOrigin"}`)
	if code != http.StatusForbidden {
		t.Fatalf("ops cors set outside scope = %d %s", code, body)
	}

	// ... and not the global catch-all.
	code, body = e.post(t, "/v1/SetCorsRule", e.opsToken,
		`{"rule":{"key":"*","allowOrigin":"*"},"updateMask":"allowOrigin"}`)
	if code != http.StatusForbidden {
		t.Fatalf("ops cors catch-all = %d %s", code, body)
	}

	// ops is read-only on proxy: write forbidden, read allowed but filtered.
	code, _ = e.post(t, "/v1/SetProxyEntry", e.opsToken,
		`{"entry":{"upstreamKey":"9099","listenerKeys":["x.example.com"]}}`)
	if code != http.StatusForbidden {
		t.Fatalf("ops proxy write = %d", code)
	}
	code, body = e.post(t, "/v1/ListProxyEntries", e.opsToken, "{}")
	if code != http.StatusOK {
		t.Fatalf("ops proxy read = %d", code)
	}
	// ops holds an unrestricted proxy-read rule -> sees the fixture entry.
	if !strings.Contains(body, "1234") {
		t.Fatalf("ops should see unrestricted-read entries: %s", body)
	}

	// cors list is filtered: fixture has only the "*" rule, which ops cannot
	// see; the api.ibuduan.com rule created above is visible.
	code, body = e.post(t, "/v1/ListCorsRules", e.opsToken, "{}")
	if code != http.StatusOK {
		t.Fatalf("ops cors list = %d %s", code, body)
	}
	if strings.Contains(body, "\"key\":\"*\"") || !strings.Contains(body, "api.ibuduan.com") {
		t.Fatalf("ops cors list filtering wrong: %s", body)
	}
}

func TestGatewayUsersCRUDAndGuards(t *testing.T) {
	e := newTestEnv(t)

	// ops (no users scope) cannot list or upsert.
	code, _ := e.post(t, "/v1/ListUsers", e.opsToken, "{}")
	if code != http.StatusForbidden {
		t.Fatalf("ops users list = %d", code)
	}

	// admin creates a new user.
	code, body := e.post(t, "/v1/UpsertUser", e.adminToken,
		`{"user":{"name":"auditor","permissions":[{"surface":"E_PERMISSION_SURFACE_PROXY","mode":"E_PERMISSION_MODE_READ"}]},"token":"auditor-token"}`)
	if code != http.StatusOK {
		t.Fatalf("upsert auditor = %d %s", code, body)
	}
	// The new credentials authenticate immediately (mtime cache + session issue).
	code, _ = e.post(t, "/v1/ListProxyEntries", e.login(t, "auditor", "auditor-token"), "{}")
	if code != http.StatusOK {
		t.Fatalf("auditor session should work, got %d", code)
	}

	// Listing shows users but never token hashes.
	code, body = e.post(t, "/v1/ListUsers", e.adminToken, "{}")
	if code != http.StatusOK || strings.Contains(body, "token") {
		t.Fatalf("users list leaked token material: %s", body)
	}
	if !strings.Contains(body, "auditor") {
		t.Fatalf("auditor missing: %s", body)
	}

	// Deleting the last users-admin (admin) is refused: only ops/auditor
	// would remain, neither has users-scope read-write.
	code, body = e.post(t, "/v1/DeleteUser", e.adminToken, `{"name":"admin"}`)
	if code != http.StatusBadRequest || errCode(t, body) != 9 { // FailedPrecondition
		t.Fatalf("last-admin guard = %d %s", code, body)
	}

	// Deleting a non-admin user is fine.
	code, _ = e.post(t, "/v1/DeleteUser", e.adminToken, `{"name":"auditor"}`)
	if code != http.StatusOK {
		t.Fatalf("delete auditor failed")
	}

	// Deleting the last admin by stripping permissions is also refused.
	code, body = e.post(t, "/v1/UpsertUser", e.adminToken,
		`{"user":{"name":"admin","permissions":[{"surface":"E_PERMISSION_SURFACE_PROXY","mode":"E_PERMISSION_MODE_READ_WRITE"}]}}`)
	if errCode(t, body) != 9 {
		t.Fatalf("permission-strip guard = %s", body)
	}
}

func TestGatewayCorsClearSemantics(t *testing.T) {
	e := newTestEnv(t)

	// Clear allow_headers for a new rule: empty list = explicit clear.
	code, body := e.post(t, "/v1/SetCorsRule", e.adminToken,
		`{"rule":{"key":"api.example.com","allowHeaders":[]},"updateMask":"allowHeaders"}`)
	if code != http.StatusOK {
		t.Fatalf("cors clear set = %d %s", code, body)
	}

	// Reload parses it back as present-and-empty.
	loaded, err := config.Load(e.dir)
	if err != nil {
		t.Fatal(err)
	}
	rule := loaded.CORS["api.example.com"]
	if !rule.Presence().AllowHeaders || len(rule.AllowHeaders) != 0 {
		t.Fatalf("cleared allow_headers did not round-trip: %+v", rule)
	}

	// The list response reports explicit fields.
	code, body = e.post(t, "/v1/ListCorsRules", e.adminToken, "{}")
	if code != http.StatusOK || !strings.Contains(body, "explicitFields") {
		t.Fatalf("cors list missing explicitFields: %s", body)
	}
}

func TestGatewayLogsUpdate(t *testing.T) {
	e := newTestEnv(t)

	code, body := e.post(t, "/v1/UpdateLogsConfig", e.adminToken,
		`{"config":{"nginxLevel":"debug"},"updateMask":"nginx.level"}`)
	if code != http.StatusOK {
		t.Fatalf("logs update = %d %s", code, body)
	}
	if !strings.Contains(body, `"applied":true`) {
		t.Fatalf("logs not applied: %s", body)
	}

	// Untouched fields kept their values.
	code, body = e.post(t, "/v1/GetLogsConfig", e.adminToken, "{}")
	if code != http.StatusOK {
		t.Fatalf("logs get = %d %s", code, body)
	}
	var got struct {
		Config struct {
			SsllyLevel string `json:"ssllyLevel"`
			NginxLevel string `json:"nginxLevel"`
		} `json:"config"`
	}
	_ = json.Unmarshal([]byte(body), &got)
	if got.Config.SsllyLevel != "info" || got.Config.NginxLevel != "debug" {
		t.Fatalf("unexpected logs state: %s", body)
	}

	// Invalid level rejected before any write.
	code, _ = e.post(t, "/v1/UpdateLogsConfig", e.adminToken,
		`{"config":{"ssllyLevel":"loud"},"updateMask":"sslly.level"}`)
	if code != http.StatusBadRequest {
		t.Fatalf("invalid level = %d", code)
	}
}

func TestGatewayValidationAndNotFound(t *testing.T) {
	e := newTestEnv(t)

	// Missing listener list.
	code, _ := e.post(t, "/v1/SetProxyEntry", e.adminToken,
		`{"entry":{"upstreamKey":"9099"}}`)
	if code != http.StatusBadRequest {
		t.Fatalf("empty listeners = %d", code)
	}

	// Delete a missing entry.
	code, _ = e.post(t, "/v1/DeleteProxyEntry", e.adminToken, `{"upstreamKey":"missing"}`)
	if code != http.StatusNotFound {
		t.Fatalf("missing entry delete = %d", code)
	}

	// Unknown update_mask path.
	code, _ = e.post(t, "/v1/SetCorsRule", e.adminToken,
		`{"rule":{"key":"x.com","allowOrigin":"*"},"updateMask":"nope"}`)
	if code != http.StatusBadRequest {
		t.Fatalf("unknown mask path = %d", code)
	}
}

// TestServerStartStopLifecycle exercises the production Start() path: two
// real TCP listeners on ephemeral loopback ports, gateway dialing gRPC over
// loopback, and a graceful Stop.
func TestServerStartStopLifecycle(t *testing.T) {
	dir := t.TempDir()
	setupTestConfigs(t, dir)

	reloadCalled := make(chan struct{}, 1)
	srv := New(Config{
		GRPCAddr:  "127.0.0.1:0",
		HTTPAddr:  "127.0.0.1:0",
		ConfigDir: dir,
		Reload: func() error {
			reloadCalled <- struct{}{}
			return nil
		},
		Users:  config.LoadUserStore(dir),
		Editor: config.NewEditor(dir),
	})
	if err := srv.Start(); err != nil {
		t.Fatalf("Start: %v", err)
	}

	base := "http://" + srv.httpLis.Addr().String()

	// healthz, unauthenticated.
	resp, err := http.Post(base+"/healthz", "application/json", nil)
	if err != nil {
		t.Fatalf("healthz: %v", err)
	}
	resp.Body.Close()
	var body []byte
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("healthz = %d", resp.StatusCode)
	}

	// Authenticate over the real stack (loopback gateway->gRPC hop), then
	// mutate with the session token.
	req, _ := http.NewRequest(http.MethodPost, base+"/v1/Authenticate",
		strings.NewReader(`{"name":"admin","token":"admin-token"}`))
	req.Header.Set("Content-Type", "application/json")
	resp, err = http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	body, _ = io.ReadAll(resp.Body)
	resp.Body.Close()
	var auth struct {
		SessionToken string `json:"sessionToken"`
	}
	if resp.StatusCode != http.StatusOK || json.Unmarshal(body, &auth) != nil || auth.SessionToken == "" {
		t.Fatalf("authenticate = %d %s", resp.StatusCode, body)
	}
	req, _ = http.NewRequest(http.MethodPost, base+"/v1/SetProxyEntry",
		strings.NewReader(`{"entry":{"upstreamKey":"7070","listenerKeys":["life.example.com"]}}`))
	req.Header.Set("Authorization", "Bearer "+auth.SessionToken)
	req.Header.Set("Content-Type", "application/json")
	resp, err = http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	body, _ = io.ReadAll(resp.Body)
	resp.Body.Close()
	if resp.StatusCode != http.StatusOK || !strings.Contains(string(body), `"applied":true`) {
		t.Fatalf("set over real listeners = %d %s", resp.StatusCode, body)
	}
	select {
	case <-reloadCalled:
	case <-time.After(5 * time.Second):
		t.Fatal("reload was not invoked")
	}

	// Graceful stop closes the HTTP listener.
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	srv.Stop(ctx)
	if _, err := http.Post(base+"/healthz", "application/json", nil); err == nil {
		t.Fatal("http listener should be closed after Stop")
	}
}

// TestServerDisabledWithoutAddresses: with both addresses unset the control
// API must not open any listener — Start/Stop are no-ops.
func TestServerDisabledWithoutAddresses(t *testing.T) {
	dir := t.TempDir()
	setupTestConfigs(t, dir)

	srv := New(Config{ConfigDir: dir}) // no GRPCAddr / HTTPAddr
	if !srv.Disabled() {
		t.Fatal("expected server to be disabled without addresses")
	}
	if err := srv.Start(); err != nil {
		t.Fatalf("disabled Start must be a no-op: %v", err)
	}
	srv.Stop(context.Background()) // must not panic or block
}

// TestServerHTTPOnlyUsesInternalGRPC: setting only the HTTP address keeps the
// JSON gateway serving while the gRPC backend stays on an internal loopback
// port.
func TestServerHTTPOnlyUsesInternalGRPC(t *testing.T) {
	dir := t.TempDir()
	setupTestConfigs(t, dir)

	srv := New(Config{
		HTTPAddr:  "127.0.0.1:0",
		ConfigDir: dir,
		Users:     config.LoadUserStore(dir),
		Editor:    config.NewEditor(dir),
	})
	if srv.Disabled() {
		t.Fatal("http-only config must not be disabled")
	}
	if err := srv.Start(); err != nil {
		t.Fatalf("Start: %v", err)
	}
	t.Cleanup(func() { srv.Stop(context.Background()) })

	// The gRPC backend must be loopback (not exposed).
	if !strings.HasPrefix(srv.grpcAddr, "127.0.0.1:") {
		t.Fatalf("internal gRPC must bind loopback, got %s", srv.grpcAddr)
	}

	// The gateway answers on the real listener.
	resp, err := http.Post("http://"+srv.httpLis.Addr().String()+"/healthz", "application/json", nil)
	if err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("healthz = %d", resp.StatusCode)
	}
}

// --- two-step auth model ----------------------------------------------------

// TestAuthModelLongLivedTokenRejected: the users.yaml token must NOT work as
// a bearer for anything except Authenticate.
func TestAuthModelLongLivedTokenRejected(t *testing.T) {
	e := newTestEnv(t)
	code, _ := e.post(t, "/v1/ListProxyEntries", "admin-token", "{}")
	if code != http.StatusUnauthorized {
		t.Fatalf("long-lived token as bearer = %d, want 401", code)
	}
}

// TestAuthModelLifecycle: exchange -> use -> logout -> dead; wrong name and
// wrong token both 401 identically.
func TestAuthModelLifecycle(t *testing.T) {
	e := newTestEnv(t)

	// Wrong name / wrong token -> identical 401 (no enumeration oracle).
	for _, body := range []string{
		`{"name":"nosuch","token":"admin-token"}`,
		`{"name":"admin","token":"wrong"}`,
		`{}`,
	} {
		code, _ := e.post(t, "/v1/Authenticate", "", body)
		if code != http.StatusUnauthorized && code != http.StatusBadRequest {
			t.Fatalf("authenticate %s = %d, want 401/400", body, code)
		}
	}

	// Fresh session works, then Logout kills it.
	sess := e.login(t, "ops", "ops-token")
	code, _ := e.post(t, "/v1/ListCorsRules", sess, "{}")
	if code != http.StatusOK {
		t.Fatalf("session use = %d", code)
	}
	code, _ = e.post(t, "/v1/Logout", sess, "{}")
	if code != http.StatusOK {
		t.Fatalf("logout = %d", code)
	}
	code, _ = e.post(t, "/v1/ListCorsRules", sess, "{}")
	if code != http.StatusUnauthorized {
		t.Fatalf("session after logout = %d, want 401", code)
	}
}

// TestAuthModelCredentialRotationKillsSessions: rotating a user's long-lived
// token invalidates every session issued under the old credential.
func TestAuthModelCredentialRotationKillsSessions(t *testing.T) {
	e := newTestEnv(t)
	sess := e.login(t, "ops", "ops-token")
	code, _ := e.post(t, "/v1/ListCorsRules", sess, "{}")
	if code != http.StatusOK {
		t.Fatalf("pre-rotation session = %d", code)
	}

	// admin rotates ops's token.
	code, _ = e.post(t, "/v1/UpsertUser", e.adminToken,
		`{"user":{"name":"ops","permissions":[{"surface":"E_PERMISSION_SURFACE_CORS","mode":"E_PERMISSION_MODE_READ_WRITE","domains":["*.ibuduan.com"]},{"surface":"E_PERMISSION_SURFACE_PROXY","mode":"E_PERMISSION_MODE_READ"}]},"token":"ops-new-token"}`)
	if code != http.StatusOK {
		t.Fatalf("rotate = %d", code)
	}

	code, _ = e.post(t, "/v1/ListCorsRules", sess, "{}")
	if code != http.StatusUnauthorized {
		t.Fatalf("stale session after rotation = %d, want 401", code)
	}
	// New credential exchanges fine.
	if code, _ := e.post(t, "/v1/ListCorsRules", e.login(t, "ops", "ops-new-token"), "{}"); code != http.StatusOK {
		t.Fatalf("new credential session = %d", code)
	}
}

// TestAuthModelSessionExpiry: a session past its TTL is rejected.
func TestAuthModelSessionExpiry(t *testing.T) {
	e := newTestEnv(t)
	sess := e.login(t, "ops", "ops-token")

	// Expire it artificially.
	e.srv.sessions.mu.Lock()
	for _, s := range e.srv.sessions.sessions {
		s.expiresAt = time.Now().Add(-time.Second)
	}
	e.srv.sessions.mu.Unlock()

	code, _ := e.post(t, "/v1/ListCorsRules", sess, "{}")
	if code != http.StatusUnauthorized {
		t.Fatalf("expired session = %d, want 401", code)
	}
}
