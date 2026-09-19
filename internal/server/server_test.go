package server

import (
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"envbridge/internal/gateway"
	"envbridge/internal/history"
	"envbridge/internal/logger"
	"envbridge/internal/proxy"
)

func testServer(t *testing.T, dir string) *Server {
	t.Helper()
	s := New(Options{
		Addr:          "127.0.0.1:8080",
		Dir:           dir,
		Token:         "test-token",
		Vars:          proxy.MapVars{"OPENAI_API_KEY": "sk-secret"},
		EnvPath:       ".env",
		EnvNames:      func() []string { return []string{"OPENAI_API_KEY"} },
		Log:           logger.New(false),
		History:       history.New(10),
		ShowDashboard: true,
	})
	return s
}

func do(t *testing.T, s *Server, req *http.Request) *httptest.ResponseRecorder {
	t.Helper()
	req.Host = "127.0.0.1:8080"
	rr := httptest.NewRecorder()
	s.ServeHTTP(rr, req)
	return rr
}

func req(method, path string) *http.Request {
	r := httptest.NewRequest(method, path, nil)
	r.Host = "127.0.0.1:8080"
	return r
}

func TestStaticServesIndex(t *testing.T) {
	dir := t.TempDir()
	os.WriteFile(filepath.Join(dir, "index.html"), []byte("<h1>hi</h1>"), 0o644)
	s := testServer(t, dir)
	rr := do(t, s, req(http.MethodGet, "/"))
	if rr.Code != 200 || !strings.Contains(rr.Body.String(), "hi") {
		t.Fatalf("got %d %q", rr.Code, rr.Body.String())
	}
}

func TestStaticServesJs(t *testing.T) {
	dir := t.TempDir()
	os.WriteFile(filepath.Join(dir, "app.js"), []byte("console.log(1)"), 0o644)
	s := testServer(t, dir)
	rr := do(t, s, req(http.MethodGet, "/app.js"))
	if rr.Code != 200 {
		t.Fatalf("got %d", rr.Code)
	}
}

func TestStaticDeniesDotfiles(t *testing.T) {
	dir := t.TempDir()
	os.WriteFile(filepath.Join(dir, "index.html"), []byte("x"), 0o644)
	os.WriteFile(filepath.Join(dir, ".env"), []byte("SECRET=leak"), 0o644)
	os.WriteFile(filepath.Join(dir, ".env.local"), []byte("SECRET=leak2"), 0o644)
	s := testServer(t, dir)
	for _, p := range []string{"/.env", "/.env.local"} {
		if rr := do(t, s, req(http.MethodGet, p)); rr.Code != 404 {
			t.Errorf("%s = %d, want 404", p, rr.Code)
		}
	}
}

func TestStaticDeniesTraversal(t *testing.T) {
	dir := t.TempDir()
	os.WriteFile(filepath.Join(dir, "index.html"), []byte("x"), 0o644)
	secret := filepath.Join(dir, "../../../etc/hostname")
	s := testServer(t, dir)
	rr := do(t, s, req(http.MethodGet, "/../"+secret))
	_ = secret
	if rr.Code == 200 {
		t.Fatal("traversal must not succeed")
	}
}

func TestTokenEndpoint(t *testing.T) {
	dir := t.TempDir()
	s := testServer(t, dir)

	// correct host + no origin -> allowed (token only leaks via our own origin)
	rr := do(t, s, req(http.MethodGet, "/__envgo_token"))
	if rr.Code != 200 || rr.Body.String() != "test-token" {
		t.Fatalf("got %d %q", rr.Code, rr.Body.String())
	}

	// evil host -> forbidden
	r := req(http.MethodGet, "/__envgo_token")
	r.Host = "evil.com"
	rr = httptest.NewRecorder()
	s.ServeHTTP(rr, r)
	if rr.Code != 403 {
		t.Fatalf("evil host got %d, want 403", rr.Code)
	}

	// cross-origin -> forbidden
	r = req(http.MethodGet, "/__envgo_token")
	r.Header.Set("Origin", "http://evil.com")
	rr = httptest.NewRecorder()
	s.ServeHTTP(rr, r)
	if rr.Code != 403 {
		t.Fatalf("evil origin got %d, want 403", rr.Code)
	}
}

func TestProxyRequiresToken(t *testing.T) {
	dir := t.TempDir()
	os.WriteFile(filepath.Join(dir, "index.html"), []byte("x"), 0o644)
	s := testServer(t, dir)

	r := req(http.MethodPost, "/proxy")
	r.Header.Set("Content-Type", "application/json")
	r.Body = http.NoBody
	if rr := do(t, s, r); rr.Code != 401 {
		t.Fatalf("no token got %d, want 401", rr.Code)
	}

	r = req(http.MethodPost, "/proxy")
	r.Header.Set("X-EnvGo-Token", "wrong-token")
	r.Header.Set("Content-Type", "application/json")
	r.Body = http.NoBody
	if rr := do(t, s, r); rr.Code != 401 {
		t.Fatalf("wrong token got %d, want 401", rr.Code)
	}
}

func TestProxyHostGuard(t *testing.T) {
	dir := t.TempDir()
	os.WriteFile(filepath.Join(dir, "index.html"), []byte("x"), 0o644)
	s := testServer(t, dir)

	r := req(http.MethodPost, "/proxy")
	r.Host = "evil.com"
	r.Header.Set("X-EnvGo-Token", "test-token")
	r.Header.Set("Content-Type", "application/json")
	r.Body = http.NoBody
	rr := httptest.NewRecorder()
	s.ServeHTTP(rr, r)
	if rr.Code != 401 {
		t.Fatalf("evil host got %d, want 401", rr.Code)
	}
}

func TestDashboard(t *testing.T) {
	dir := t.TempDir()
	s := testServer(t, dir)

	rr := do(t, s, req(http.MethodGet, "/__envgo_dashboard"))
	if rr.Code != 200 || !strings.Contains(rr.Body.String(), "envGo Dashboard") {
		t.Fatalf("dashboard page: %d %q", rr.Code, rr.Body.String())
	}
	if rr.Header().Get("X-Frame-Options") != "DENY" {
		t.Fatal("dashboard must set X-Frame-Options: DENY")
	}

	rr = do(t, s, req(http.MethodGet, "/__envgo_dashboard/data"))
	if rr.Code != 200 {
		t.Fatalf("dashboard data: %d", rr.Code)
	}
	body := rr.Body.String()
	if !strings.Contains(body, "OPENAI_API_KEY") || !strings.Contains(body, `"requests"`) {
		t.Fatalf("unexpected dashboard data: %s", body)
	}
	if strings.Contains(body, "sk-secret") {
		t.Fatal("dashboard data must not contain secret values")
	}
}

func TestDashboardRejectsEvilHost(t *testing.T) {
	dir := t.TempDir()
	s := testServer(t, dir)
	r := req(http.MethodGet, "/__envgo_dashboard")
	r.Host = "evil.com"
	rr := httptest.NewRecorder()
	s.ServeHTTP(rr, r)
	if rr.Code != 403 {
		t.Fatalf("evil host got %d, want 403", rr.Code)
	}
}

func TestPublicModeDisablesLocalEndpoints(t *testing.T) {
	dir := t.TempDir()
	os.WriteFile(filepath.Join(dir, "index.html"), []byte("x"), 0o644)

	gw := gateway.New(&gateway.Config{
		Routes: []gateway.Route{{Name: "chat", Method: "POST", Target: "https://example.com"}},
	}, proxy.MapVars{"K": "v"}, logger.New(false), history.New(5))

	s := New(Options{
		Addr:          "127.0.0.1:8080",
		Dir:           dir,
		Token:         "test-token",
		Vars:          proxy.MapVars{"K": "v"},
		EnvPath:       ".env",
		Log:           logger.New(false),
		History:       history.New(5),
		Gateway:       gw,
		ShowDashboard: false,
	})

	// local-only endpoints must be gone (both new and legacy paths)
	for _, p := range []string{"/proxy", "/__envgo_token", "/__envgo_dashboard"} {
		if rr := do(t, s, req(http.MethodGet, p)); rr.Code != 404 {
			t.Errorf("%s in public mode = %d, want 404", p, rr.Code)
		}
	}
	// gateway namespace is served (unknown route -> 404 from gateway)
	if rr := do(t, s, req(http.MethodPost, "/api/unknown")); rr.Code != 404 {
		t.Errorf("/api/unknown = %d, want 404", rr.Code)
	}
	// static still works
	if rr := do(t, s, req(http.MethodGet, "/")); rr.Code != 200 {
		t.Errorf("static = %d, want 200", rr.Code)
	}
}
