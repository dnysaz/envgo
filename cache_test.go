package main

import (
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
)

func TestDevServerAddrDefaults(t *testing.T) {
	dir := t.TempDir()
	host, port := devServerAddr(filepath.Join(dir, "missing.env"))
	if host != "127.0.0.1" {
		t.Fatalf("expected default host 127.0.0.1, got %s", host)
	}
	if port != 8080 {
		t.Fatalf("expected default port 8080, got %d", port)
	}
}

func TestDevServerAddrFromEnv(t *testing.T) {
	dir := t.TempDir()
	envPath := filepath.Join(dir, ".env")
	content := []byte("# comment\nHOST=0.0.0.0\nPORT=9090\nfoo=bar\n")
	if err := os.WriteFile(envPath, content, 0o644); err != nil {
		t.Fatal(err)
	}
	host, port := devServerAddr(envPath)
	if host != "0.0.0.0" {
		t.Fatalf("expected host 0.0.0.0, got %s", host)
	}
	if port != 9090 {
		t.Fatalf("expected port 9090, got %d", port)
	}
}

func TestDevServerAddrInvalidPort(t *testing.T) {
	dir := t.TempDir()
	envPath := filepath.Join(dir, ".env")
	content := []byte("HOST=localhost\nPORT=not-a-number\n")
	if err := os.WriteFile(envPath, content, 0o644); err != nil {
		t.Fatal(err)
	}
	if host, port := devServerAddr(envPath); host != "localhost" || port != 8080 {
		t.Fatalf("expected localhost:8080 with invalid port, got %s:%d", host, port)
	}
}

func TestDoCacheClearSuccess(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			t.Errorf("expected POST, got %s", r.Method)
		}
		if r.URL.Path != "/__envgo/admin/clear-cache" {
			t.Errorf("unexpected path: %s", r.URL.Path)
		}
		w.Header().Set("Content-Type", "text/plain")
		_, _ = io.WriteString(w, "Cache cleared")
	}))
	defer srv.Close()

	u, err := url.Parse(srv.URL)
	if err != nil {
		t.Fatal(err)
	}
	port, err := strconv.Atoi(strings.TrimPrefix(u.Port(), ""))
	if err != nil {
		t.Fatal(err)
	}

	dir := t.TempDir()
	envPath := filepath.Join(dir, ".env")
	content := []byte("HOST=" + u.Hostname() + "\nPORT=" + strconv.Itoa(port) + "\n")
	if err := os.WriteFile(envPath, content, 0o644); err != nil {
		t.Fatal(err)
	}

	out := captureStdout(t, func() { doCacheClear(envPath) })
	if !strings.Contains(out, "Cache cleared") {
		t.Fatalf("expected output to contain 'Cache cleared', got %q", out)
	}
}

func captureStdout(t *testing.T, fn func()) string {
	t.Helper()
	old := os.Stdout
	r, w, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	os.Stdout = w
	fn()
	_ = w.Close()
	os.Stdout = old
	b, err := io.ReadAll(r)
	if err != nil {
		t.Fatal(err)
	}
	return string(b)
}
