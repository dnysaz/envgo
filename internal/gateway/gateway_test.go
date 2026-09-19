package gateway

import (
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"envbridge/internal/history"
	"envbridge/internal/logger"
	"envbridge/internal/proxy"
)

func newTestGateway(t *testing.T, cfg *Config, vars proxy.MapVars) (*Gateway, *httptest.Server) {
	t.Helper()
	upstream := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{
			"query":   r.URL.RawQuery,
			"headers": r.Header,
			"body":    string(body),
			"method":  r.Method,
		})
	}))
	t.Cleanup(upstream.Close)

	// rewrite targets to the test server
	for i := range cfg.Routes {
		if cfg.Routes[i].Target == "https://upstream.test/api" {
			cfg.Routes[i].Target = upstream.URL + "/api"
		}
	}

	log := logger.New(false)
	log.RegisterSecretsFrom(vars)
	g := New(cfg, vars, log, history.New(20))
	g.client = upstream.Client()
	return g, upstream
}

func post(t *testing.T, g *Gateway, path, body string) *httptest.ResponseRecorder {
	t.Helper()
	req := httptest.NewRequest(http.MethodPost, path, strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	rr := httptest.NewRecorder()
	g.ServeHTTP(rr, req)
	return rr
}

func baseConfig() *Config {
	return &Config{
		ScrubResponse: false, // tests inspect raw values; scrub has its own test
		Routes: []Route{{
			Name:   "chat",
			Method: "POST",
			Target: "https://upstream.test/api",
			Vars:   []string{"GEMINI_KEY"},
			Inject: InjectConfig{
				Query:   map[string]string{"key": "{GEMINI_KEY}"},
				Headers: map[string]string{"X-From": "envgo"},
			},
		}},
	}
}

func TestConfigLoadAndValidate(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "envgo.json")
	content := `{
	  "default_rate_limit": "60/min",
	  "scrub_response": true,
	  "routes": [
	    {"name":"chat","method":"POST","target":"https://api.example.com/v1",
	     "vars":["GEMINI_KEY"],"inject":{"query":{"key":"{GEMINI_KEY}"}}}
	  ]
	}`
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
	cfg, err := LoadConfig(path)
	if err != nil {
		t.Fatalf("LoadConfig: %v", err)
	}
	if len(cfg.Routes) != 1 || cfg.Routes[0].Name != "chat" {
		t.Fatalf("unexpected config: %+v", cfg)
	}
}

func TestConfigRejectsHTTP(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "bad.json")
	_ = os.WriteFile(path, []byte(`{"routes":[{"name":"x","target":"http://insecure"}]}`), 0o644)
	if _, err := LoadConfig(path); err == nil {
		t.Fatal("expected error for http target")
	}
}

func TestInjectsDeclaredVarAndDefaultMethod(t *testing.T) {
	g, _ := newTestGateway(t, baseConfig(), proxy.MapVars{"GEMINI_KEY": "secret-key-123"})
	rr := post(t, g, "/api/chat", `{"prompt":"hi"}`)
	if rr.Code != 200 {
		t.Fatalf("status = %d, body=%s", rr.Code, rr.Body.String())
	}
	if !strings.Contains(rr.Body.String(), "key=secret-key-123") {
		t.Fatalf("query not injected: %s", rr.Body.String())
	}
	if !strings.Contains(rr.Body.String(), "X-From") {
		t.Fatalf("header not injected: %s", rr.Body.String())
	}
	if !strings.Contains(rr.Body.String(), `\"prompt\":\"hi\"`) {
		t.Fatalf("body not forwarded: %s", rr.Body.String())
	}
}

func TestClientPlaceholderUsesOnlyAllowedVars(t *testing.T) {
	g, _ := newTestGateway(t, baseConfig(), proxy.MapVars{
		"GEMINI_KEY": "secret-key-123",
		"ADMIN_KEY":  "should-never-leak",
	})
	// allowed placeholder in client body
	rr := post(t, g, "/api/chat", `{"prompt":"use {GEMINI_KEY}"}`)
	if rr.Code != 200 {
		t.Fatalf("status = %d, body=%s", rr.Code, rr.Body.String())
	}
	if !strings.Contains(rr.Body.String(), "secret-key-123") {
		t.Fatalf("allowed var not substituted: %s", rr.Body.String())
	}
	// disallowed placeholder must be rejected
	rr = post(t, g, "/api/chat", `{"prompt":"use {ADMIN_KEY}"}`)
	if rr.Code != 400 {
		t.Fatalf("disallowed var status = %d, want 400", rr.Code)
	}
	if strings.Contains(rr.Body.String(), "should-never-leak") {
		t.Fatal("disallowed secret leaked")
	}
}

func TestTargetIsFixed(t *testing.T) {
	// A client cannot influence the upstream host: there is no target_url
	// field. If the request were routed to evil.example it would fail; a 200
	// proves it went to the configured upstream.
	g, _ := newTestGateway(t, baseConfig(), proxy.MapVars{"GEMINI_KEY": "secret-value-xyz"})
	rr := post(t, g, "/api/chat", `{"target_url":"https://evil.example/"}`)
	if rr.Code != 200 {
		t.Fatalf("status = %d, want 200 (target must stay fixed)", rr.Code)
	}
	if !strings.Contains(rr.Body.String(), "target_url") {
		t.Fatalf("client payload should be forwarded as-is: %s", rr.Body.String())
	}
}

func TestAuthRequired(t *testing.T) {
	cfg := baseConfig()
	cfg.Routes[0].Auth = &AuthConfig{Type: "bearer", Secret: "ACCESS_TOKEN"}
	g, _ := newTestGateway(t, cfg, proxy.MapVars{"GEMINI_KEY": "secret-value-xyz", "ACCESS_TOKEN": "letmein"})

	if rr := post(t, g, "/api/chat", `{}`); rr.Code != 401 {
		t.Fatalf("no auth status = %d, want 401", rr.Code)
	}

	req := httptest.NewRequest(http.MethodPost, "/api/chat", strings.NewReader(`{}`))
	req.Header.Set("Authorization", "Bearer letmein")
	rr := httptest.NewRecorder()
	g.ServeHTTP(rr, req)
	if rr.Code != 200 {
		t.Fatalf("valid auth status = %d, want 200 (body=%s)", rr.Code, rr.Body.String())
	}
}

func TestRateLimit(t *testing.T) {
	cfg := baseConfig()
	cfg.Routes[0].RateLimit = "2/min"
	g, _ := newTestGateway(t, cfg, proxy.MapVars{"GEMINI_KEY": "secret-value-xyz"})

	if rr := post(t, g, "/api/chat", `{}`); rr.Code != 200 {
		t.Fatalf("1st = %d", rr.Code)
	}
	if rr := post(t, g, "/api/chat", `{}`); rr.Code != 200 {
		t.Fatalf("2nd = %d", rr.Code)
	}
	if rr := post(t, g, "/api/chat", `{}`); rr.Code != 429 {
		t.Fatalf("3rd = %d, want 429", rr.Code)
	}
}

func TestScrubsEchoedSecret(t *testing.T) {
	upstream := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"echo":"secret-key-123"}`))
	}))
	defer upstream.Close()

	cfg := &Config{
		ScrubResponse: true,
		Routes: []Route{{
			Name:   "echo",
			Method: "POST",
			Target: upstream.URL + "/",
			Vars:   []string{"GEMINI_KEY"},
		}},
	}
	vars := proxy.MapVars{"GEMINI_KEY": "secret-key-123"}
	log := logger.New(false)
	log.RegisterSecretsFrom(vars)
	g := New(cfg, vars, log, history.New(5))
	g.client = upstream.Client()

	rr := post(t, g, "/api/echo", `{}`)
	if rr.Code != 200 {
		t.Fatalf("status=%d", rr.Code)
	}
	if strings.Contains(rr.Body.String(), "secret-key-123") {
		t.Fatalf("secret not scrubbed: %s", rr.Body.String())
	}
	if !strings.Contains(rr.Body.String(), "[REDACTED]") {
		t.Fatalf("expected redaction marker: %s", rr.Body.String())
	}
}

func TestUnknownRoute(t *testing.T) {
	g, _ := newTestGateway(t, baseConfig(), proxy.MapVars{"GEMINI_KEY": "secret-value-xyz"})
	if rr := post(t, g, "/api/nope", `{}`); rr.Code != 404 {
		t.Fatalf("status = %d, want 404", rr.Code)
	}
}

func TestMethodNotAllowed(t *testing.T) {
	g, _ := newTestGateway(t, baseConfig(), proxy.MapVars{"GEMINI_KEY": "secret-value-xyz"})
	req := httptest.NewRequest(http.MethodGet, "/api/chat", nil)
	rr := httptest.NewRecorder()
	g.ServeHTTP(rr, req)
	if rr.Code != 405 {
		t.Fatalf("status = %d, want 405", rr.Code)
	}
}
