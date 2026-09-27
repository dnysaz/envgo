package main

import (
	"crypto/tls"
	"crypto/x509"
	"net"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
)

func TestSplitAllow(t *testing.T) {
	cases := []struct {
		input string
		want  []string
	}{
		{"", []string{}},
		{"openai.com", []string{"openai.com"}},
		{"openai.com,httpbin.org", []string{"openai.com", "httpbin.org"}},
		{"openai.com, httpbin.org ,  google.com", []string{"openai.com", "httpbin.org", "google.com"}},
		{",,,", []string{}},
		{"  spaced  ,  host  ", []string{"spaced", "host"}},
	}
	for _, c := range cases {
		got := splitAllow(c.input)
		if len(got) != len(c.want) {
			t.Errorf("splitAllow(%q) = %v (len %d), want %v (len %d)", c.input, got, len(got), c.want, len(c.want))
			continue
		}
		for i := range got {
			if got[i] != c.want[i] {
				t.Errorf("splitAllow(%q)[%d] = %q, want %q", c.input, i, got[i], c.want[i])
			}
		}
	}
}

func TestParseEnvKeys(t *testing.T) {
	t.Run("valid env file", func(t *testing.T) {
		dir := t.TempDir()
		path := filepath.Join(dir, ".env")
		content := "# comment\nKEY1=value1\nKEY2=value2\n=invalid\nKEY3=val=ue=3\n\n   \n"
		if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
			t.Fatal(err)
		}
		keys := parseEnvKeys(path)
		if len(keys) != 3 {
			t.Fatalf("parseEnvKeys returned %d keys, want 3", len(keys))
		}
		if !keys["KEY1"] || !keys["KEY2"] || !keys["KEY3"] {
			t.Errorf("expected KEY1, KEY2, KEY3 — got %v", keys)
		}
		if keys["invalid"] {
			t.Error("=invalid should not be parsed as a key")
		}
	})

	t.Run("missing file returns nil", func(t *testing.T) {
		if keys := parseEnvKeys("/nonexistent/path/.env"); keys != nil {
			t.Errorf("expected nil for missing file, got %v", keys)
		}
	})
}

func TestCheckEnvExampleSync(t *testing.T) {
	t.Run("keys in sync — no warnings", func(t *testing.T) {
		dir := t.TempDir()
		envPath := filepath.Join(dir, ".env")
		examplePath := filepath.Join(dir, ".env.example")

		if err := os.WriteFile(envPath, []byte("# comment\nMY_KEY=secret\nPORT=8080\n"), 0o600); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(examplePath, []byte("MY_KEY=placeholder\nPORT=8080\n"), 0o600); err != nil {
			t.Fatal(err)
		}

		checkEnvExampleSync(envPath)
	})

	t.Run("key in .env but not .env.example — warning", func(t *testing.T) {
		dir := t.TempDir()
		envPath := filepath.Join(dir, ".env")
		examplePath := filepath.Join(dir, ".env.example")

		if err := os.WriteFile(envPath, []byte("MY_KEY=secret\nEXTRA_KEY=extra\n"), 0o600); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(examplePath, []byte("MY_KEY=placeholder\n"), 0o600); err != nil {
			t.Fatal(err)
		}

		// checkEnvExampleSync prints to stderr, so we just verify it doesn't panic
		checkEnvExampleSync(envPath)
	})

	t.Run("no .env.example — no-op", func(t *testing.T) {
		dir := t.TempDir()
		envPath := filepath.Join(dir, ".env")

		if err := os.WriteFile(envPath, []byte("MY_KEY=secret\n"), 0o600); err != nil {
			t.Fatal(err)
		}

		checkEnvExampleSync(envPath)
	})

	t.Run("infra keys ignored", func(t *testing.T) {
		dir := t.TempDir()
		envPath := filepath.Join(dir, ".env")
		examplePath := filepath.Join(dir, ".env.example")

		if err := os.WriteFile(envPath, []byte("MODE_PUBLIC=true\nCONFIG=routes.json\nMY_KEY=secret\n"), 0o600); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(examplePath, []byte("MY_KEY=placeholder\n"), 0o600); err != nil {
			t.Fatal(err)
		}

		// Should not warn about MODE_PUBLIC or CONFIG
		checkEnvExampleSync(envPath)
	})
}

func TestDoInit(t *testing.T) {
	t.Run("creates files in named subdirectory", func(t *testing.T) {
		cwd, _ := os.Getwd()
		defer os.Chdir(cwd)

		tmpDir := t.TempDir()
		os.Chdir(tmpDir)

		doInit("myapp")

		projectDir := filepath.Join(tmpDir, "myapp")
		expectedFiles := []string{".env", ".env.example", "index.html", ".gitignore", "README.md"}
		for _, f := range expectedFiles {
			path := filepath.Join(projectDir, f)
			if _, err := os.Stat(path); os.IsNotExist(err) {
				t.Errorf("expected file %s to exist", path)
			}
		}

		// .env should have MY_SECRET
		envContent, _ := os.ReadFile(filepath.Join(projectDir, ".env"))
		if !contains(string(envContent), "MY_SECRET=") {
			t.Error(".env should contain MY_SECRET")
		}
		if !contains(string(envContent), "MODE_PUBLIC") {
			t.Error(".env should contain MODE_PUBLIC comment")
		}

		// .gitignore should ignore .env
		gitContent, _ := os.ReadFile(filepath.Join(projectDir, ".gitignore"))
		if !contains(string(gitContent), ".env") {
			t.Error(".gitignore should ignore .env")
		}

		// index.html should reference MY_SECRET and __env.js
		htmlContent, _ := os.ReadFile(filepath.Join(projectDir, "index.html"))
		if !contains(string(htmlContent), "MY_SECRET") {
			t.Error("index.html should reference MY_SECRET")
		}
		if !contains(string(htmlContent), "/__env.js") {
			t.Error("index.html should include __env.js")
		}
	})

	t.Run("refuses to overwrite an existing directory", func(t *testing.T) {
		cwd, _ := os.Getwd()
		defer os.Chdir(cwd)

		tmpDir := t.TempDir()
		os.Chdir(tmpDir)

		if err := os.Mkdir("conflict", 0o755); err != nil {
			t.Fatal(err)
		}
		// A file with the same name must be refused too, not just a directory.
		if err := os.WriteFile("clash", []byte("hi"), 0o644); err != nil {
			t.Fatal(err)
		}

		if _, err := initProjectDir("conflict"); err == nil {
			t.Fatal("expected an error for an existing directory, got nil")
		} else if !contains(err.Error(), "already exists") {
			t.Errorf("expected an 'already exists' error, got %v", err)
		}
		if _, err := initProjectDir("clash"); err == nil {
			t.Fatal("expected an error for an existing file, got nil")
		}

		// The refusal must leave the existing directory untouched.
		entries, err := os.ReadDir("conflict")
		if err != nil {
			t.Fatal(err)
		}
		if len(entries) != 0 {
			t.Errorf("expected the existing directory to be left empty, found %d entries", len(entries))
		}
	})

	t.Run("empty name scaffolds in place without creating a subdirectory", func(t *testing.T) {
		cwd, _ := os.Getwd()
		defer os.Chdir(cwd)

		tmpDir := t.TempDir()
		os.Chdir(tmpDir)

		dir, err := initProjectDir("")
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		resolved, _ := filepath.EvalSymlinks(tmpDir)
		got, _ := filepath.EvalSymlinks(dir)
		if got != resolved {
			t.Errorf("expected %s, got %s", resolved, got)
		}
		// A name matching the current directory's base must behave the same way.
		if dir, err := initProjectDir(filepath.Base(tmpDir)); err != nil {
			t.Errorf("unexpected error for matching name: %v", err)
		} else if got, _ := filepath.EvalSymlinks(dir); got != resolved {
			t.Errorf("expected %s, got %s", resolved, got)
		}
	})
}

func TestDoDeploy(t *testing.T) {
	cwd, _ := os.Getwd()
	defer os.Chdir(cwd)

	tmpDir := t.TempDir()

	// Use a temp output dir
	outputDir := filepath.Join(tmpDir, "deploy-output")
	doDeploy(outputDir)

	// Check all files exist
	expectedFiles := []string{
		filepath.Join(outputDir, "Caddyfile"),
		filepath.Join(outputDir, "nginx.conf"),
		filepath.Join(outputDir, "Dockerfile"),
	}
	for _, f := range expectedFiles {
		if _, err := os.Stat(f); os.IsNotExist(err) {
			t.Errorf("expected file %s to exist", f)
		}
	}

	// Check Caddyfile content
	caddyfile, _ := os.ReadFile(filepath.Join(outputDir, "Caddyfile"))
	cf := string(caddyfile)
	if !contains(cf, "/api/*") {
		t.Error("Caddyfile should proxy /api/*")
	}
	if !contains(cf, "reverse_proxy") {
		t.Error("Caddyfile should have reverse_proxy directive")
	}

	// Check nginx.conf content
	nginxConf, _ := os.ReadFile(filepath.Join(outputDir, "nginx.conf"))
	nc := string(nginxConf)
	if !contains(nc, "server_name") {
		t.Error("nginx.conf should have server_name")
	}
	if !contains(nc, "/api/") {
		t.Error("nginx.conf should proxy /api/")
	}

	// Check Dockerfile content
	dockerfile, _ := os.ReadFile(filepath.Join(outputDir, "Dockerfile"))
	df := string(dockerfile)
	if !contains(df, "FROM alpine") {
		t.Error("Dockerfile should use alpine base")
	}
	if !contains(df, "caddy") {
		t.Error("Dockerfile should install caddy")
	}
}

func TestListenOn(t *testing.T) {
	t.Run("binds to available port", func(t *testing.T) {
		ln, addr, err := listenOn("127.0.0.1", 0, 1)
		if err != nil {
			t.Fatalf("listenOn failed: %v", err)
		}
		defer ln.Close()

		// When port=0, OS assigns a real port — get it from the listener
		listenAddr := ln.Addr().(*net.TCPAddr)
		actualPort := listenAddr.Port
		if actualPort == 0 {
			t.Error("should bind to a real port, not 0")
		}
		if addr.IP == nil {
			t.Error("addr should have an IP")
		}
	})

	t.Run("increments port when in use", func(t *testing.T) {
		ln1, _, err := listenOn("127.0.0.1", 0, 1)
		if err != nil {
			t.Fatalf("listenOn failed: %v", err)
		}
		defer ln1.Close()

		// Get the actual port the OS assigned
		actualPort := ln1.Addr().(*net.TCPAddr).Port

		// Port actualPort is now in use, try same port — should get actualPort+1
		ln2, addr2, err := listenOn("127.0.0.1", actualPort, 10)
		if err != nil {
			t.Fatalf("second listenOn failed: %v", err)
		}
		defer ln2.Close()

		if addr2.Port != actualPort+1 {
			t.Errorf("expected port %d, got %d", actualPort+1, addr2.Port)
		}
	})
}

func TestGenerateSelfSignedCert(t *testing.T) {
	// A LAN address is passed in so the certificate covers the IP a phone
	// actually dials after scanning the QR code.
	certPEM, keyPEM := generateSelfSignedCert("192.168.1.20")

	if len(certPEM) == 0 {
		t.Error("certPEM should not be empty")
	}
	if len(keyPEM) == 0 {
		t.Error("keyPEM should not be empty")
	}

	// Verify they are PEM blocks
	if !contains(string(certPEM), "BEGIN CERTIFICATE") {
		t.Error("certPEM should contain BEGIN CERTIFICATE")
	}
	if !contains(string(keyPEM), "BEGIN RSA PRIVATE KEY") {
		t.Error("keyPEM should contain BEGIN RSA PRIVATE KEY")
	}

	// Verify cert parses
	tlsCert, err := tls.X509KeyPair(certPEM, keyPEM)
	if err != nil {
		t.Fatalf("X509KeyPair failed: %v", err)
	}
	if len(tlsCert.Certificate) == 0 {
		t.Error("certificate chain should not be empty")
	}

	leaf, err := x509.ParseCertificate(tlsCert.Certificate[0])
	if err != nil {
		t.Fatalf("ParseCertificate failed: %v", err)
	}
	if err := leaf.VerifyHostname("192.168.1.20"); err != nil {
		t.Errorf("certificate should be valid for the LAN address: %v", err)
	}
	if err := leaf.VerifyHostname("127.0.0.1"); err != nil {
		t.Errorf("certificate should be valid for loopback: %v", err)
	}
}

func TestGenerateSelfSignedCertWithoutLAN(t *testing.T) {
	certPEM, keyPEM := generateSelfSignedCert("")
	tlsCert, err := tls.X509KeyPair(certPEM, keyPEM)
	if err != nil {
		t.Fatalf("X509KeyPair failed: %v", err)
	}
	leaf, err := x509.ParseCertificate(tlsCert.Certificate[0])
	if err != nil {
		t.Fatalf("ParseCertificate failed: %v", err)
	}
	// A wildcard or garbage LAN value must not end up as a bogus SAN.
	for _, ip := range leaf.IPAddresses {
		if ip.IsUnspecified() {
			t.Errorf("certificate should not contain the unspecified address, got %v", ip)
		}
	}
	if err := leaf.VerifyHostname("localhost"); err != nil {
		t.Errorf("certificate should be valid for localhost: %v", err)
	}
}

func TestHostForDisplay(t *testing.T) {
	tests := []struct {
		name string
		ip   string
		want string
	}{
		{"loopback is left alone", "127.0.0.1", "127.0.0.1"},
		{"wildcard has no dialable URL", "0.0.0.0", "127.0.0.1"},
		{"wildcard v6 has no dialable URL", "::", "127.0.0.1"},
		{"lan address is shown as-is", "192.168.1.20", "192.168.1.20"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := hostForDisplay(net.ParseIP(tt.ip)); got != tt.want {
				t.Errorf("hostForDisplay(%s) = %s, want %s", tt.ip, got, tt.want)
			}
		})
	}
	if got := hostForDisplay(nil); got != "127.0.0.1" {
		t.Errorf("hostForDisplay(nil) = %s, want 127.0.0.1", got)
	}
}

func TestLoopbackHost(t *testing.T) {
	tests := []struct {
		host string
		want bool
	}{
		{"", true},
		{"127.0.0.1", true},
		{"localhost", true},
		{"LOCALHOST", true},
		{"127.0.0.1:8080", true},
		{"0.0.0.0", true},
		{"::1", true},
		{"0.0.0.0:3000", true},
		{"192.168.1.20", false},
		{"10.0.0.5:8080", false},
		{"example.com", false},
	}
	for _, tt := range tests {
		if got := loopbackHost(tt.host); got != tt.want {
			t.Errorf("loopbackHost(%q) = %v, want %v", tt.host, got, tt.want)
		}
	}
}

func TestQRFileName(t *testing.T) {
	got := qrFileName("http://192.168.1.20:8080", 8080)
	if got != "qrcode_192.168.1.20_8080.png" {
		t.Errorf("unexpected filename: %s", got)
	}
	// Path separators and colons must not survive into the filename.
	if got := qrFileName("http://[fe80::1]:8080", 8080); strings.ContainsAny(got, "/:\\") {
		t.Errorf("filename %s contains unsafe characters", got)
	}
	if got := qrFileName("not a url", 1234); strings.ContainsAny(got, " /\\") {
		t.Errorf("filename %s contains unsafe characters", got)
	}
}

func contains(s, substr string) bool {
	return regexp.MustCompile(regexp.QuoteMeta(substr)).MatchString(s)
}
