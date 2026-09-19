package main

import (
	"context"
	"crypto/rand"
	"crypto/rsa"
	"crypto/tls"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/pem"
	"flag"
	"fmt"
	"math/big"
	"net"
	"net/http"
	"os"
	"os/exec"
	"os/signal"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"syscall"
	"time"

	"envbridge/internal/envstore"
	"envbridge/internal/gateway"
	"envbridge/internal/history"
	"envbridge/internal/logger"
	"envbridge/internal/server"
	"envbridge/internal/token"
)

var version = "dev"

func printBanner() {
	fmt.Print("\033[36m")
	fmt.Println(" ____        ")
	fmt.Println("   ___ _ ____   __/ ___| ___  ")
	fmt.Println("  / _ \\ '_ \\ \\ / / |  _ / _ \\ ")
	fmt.Println(" |  __/ | | \\ V /| |_| | (_) |")
	fmt.Println("  \\___|_| |_|\\_/  \\____|\\___/ ")
	fmt.Print("\033[0m")
	fmt.Printf("  envGo v%s — Zero-dependency micro-runtime for HTML/Vanilla JS\n", version)
	fmt.Println("  Secure .env injection — secrets never reach the browser")
	fmt.Println()
}

func main() {
	runMode := false
	isDev := false
	initMode := false
	deployMode := false
	if len(os.Args) > 1 {
		switch os.Args[1] {
		case "run":
			runMode = true
			if len(os.Args) > 2 && os.Args[2] == "dev" {
				isDev = true
				os.Args = append([]string{os.Args[0]}, os.Args[3:]...)
			} else {
				os.Args = append([]string{os.Args[0]}, os.Args[2:]...)
			}
		case "dev":
			runMode = true
			isDev = true
			os.Args = append([]string{os.Args[0]}, os.Args[2:]...)
		case "init":
			initMode = true
			os.Args = append([]string{os.Args[0]}, os.Args[2:]...)
		case "deploy":
			deployMode = true
			os.Args = append([]string{os.Args[0]}, os.Args[2:]...)
		}
	}

	var (
		portVal         int
		hostVal         string
		dirVal          string
		envPathVal      string
		allowListVal    string
		configPathVal   string
		dashboardVal    bool
		debugVal        bool
		showVerVal      bool
		showHelpVal     bool
		openBrowserVal  bool
		tlsVal          bool
		initNameVal     string
		deployOutputVal string
	)

	flag.IntVar(&portVal, "port", 8080, "port to listen on")
	flag.IntVar(&portVal, "p", 8080, "port to listen on (shorthand)")
	flag.StringVar(&hostVal, "host", "127.0.0.1", "interface to bind (keep 127.0.0.1 for local)")
	flag.StringVar(&dirVal, "dir", ".", "web root directory to serve")
	flag.StringVar(&dirVal, "d", ".", "web root directory to serve (shorthand)")
	flag.StringVar(&envPathVal, "env", ".env", "path to the .env file")
	flag.StringVar(&envPathVal, "e", ".env", "path to the .env file (shorthand)")
	flag.StringVar(&allowListVal, "allow", "", "comma-separated outbound host allowlist")
	flag.StringVar(&allowListVal, "a", "", "comma-separated outbound host allowlist (shorthand)")
	flag.StringVar(&configPathVal, "config", "", "path to public-mode routes JSON (enables the API gateway)")
	flag.StringVar(&configPathVal, "c", "", "path to public-mode routes JSON (shorthand)")
	flag.BoolVar(&dashboardVal, "dashboard", false, "enable the metadata dashboard")
	flag.BoolVar(&dashboardVal, "D", false, "enable the metadata dashboard (shorthand)")
	flag.BoolVar(&debugVal, "debug", false, "verbose logging")
	flag.BoolVar(&showVerVal, "version", false, "print version and exit")
	flag.BoolVar(&showVerVal, "v", false, "print version and exit (shorthand)")
	flag.BoolVar(&showHelpVal, "help", false, "show help")
	flag.BoolVar(&showHelpVal, "h", false, "show help (shorthand)")
	flag.BoolVar(&openBrowserVal, "browser", false, "open browser automatically")
	flag.BoolVar(&openBrowserVal, "b", false, "open browser automatically (shorthand)")
	flag.BoolVar(&tlsVal, "tls", false, "enable HTTPS with auto-generated self-signed certificate")
	flag.StringVar(&initNameVal, "name", "", "project name for envgo init (empty = current directory)")
	flag.StringVar(&deployOutputVal, "o", "", "output directory for deploy configs (default: ./deploy)")

	flag.Usage = func() {
		printBanner()
		fmt.Println("Usage:")
		fmt.Println("  envgo [options]                              — start server")
		fmt.Println("  envgo run [dev] [options]                    — start dev server")
		fmt.Println("  envgo init [options]                         — create new project template")
		fmt.Println("  envgo deploy [options]                       — generate Caddyfile/nginx config")
		fmt.Println()
		fmt.Println("Options:")
		flag.PrintDefaults()
		fmt.Println()
		fmt.Println("Examples:")
		fmt.Println("  envgo --dir . --env .env --allow httpbin.org")
		fmt.Println("  envgo run dev                                # reads HOST/PORT from .env")
		fmt.Println("  envgo init -name myapp                       # create project template")
		fmt.Println("  envgo deploy -o ./deploy                     # generate Caddyfile + nginx.conf")
		fmt.Println("  envgo --tls --dir public -b                  # HTTPS with auto-cert")
		fmt.Println("  envgo -v                                     # print version")
		fmt.Println("  envgo -h                                     # show help")
	}

	flag.Parse()

	if isDev {
		openBrowserVal = true
	}

	if showHelpVal {
		flag.Usage()
		return
	}
	if showVerVal {
		printBanner()
		fmt.Println("envGo " + version)
		return
	}
	if initMode {
		doInit(initNameVal)
		return
	}
	if deployMode {
		doDeploy(deployOutputVal)
		return
	}

	log := logger.New(debugVal)

	store, err := envstore.New(envPathVal, log)
	if err != nil {
		log.Error("cannot load %s: %v", envPathVal, err)
		os.Exit(1)
	}
	if store.Len() > 0 {
		log.Info("loaded %d variables from %s", store.Len(), envPathVal)
	} else {
		log.Warn("%s has no variables (or does not exist yet)", envPathVal)
		log.Warn("hint: create .env or run with -e /path/to/.env  |  example: envgo -e /path/to/.env -a httpbin.org -b")
		log.Warn("      help: envgo -h")
	}

	if runMode {
		if h, ok := store.Get("HOST"); ok && h != "" && hostVal == "127.0.0.1" {
			hostVal = strings.TrimSpace(h)
			log.Info("using HOST from .env: %s", hostVal)
		} else if h, ok := store.Get("host"); ok && h != "" && hostVal == "127.0.0.1" {
			hostVal = strings.TrimSpace(h)
			log.Info("using HOST from .env: %s", hostVal)
		}
		if pStr, ok := store.Get("PORT"); ok && pStr != "" && portVal == 8080 {
			pStr = strings.TrimSpace(strings.TrimPrefix(pStr, ":"))
			if p, err := strconv.Atoi(pStr); err == nil {
				portVal = p
				log.Info("using PORT from .env: %d", portVal)
			}
		} else if pStr, ok := store.Get("port"); ok && pStr != "" && portVal == 8080 {
			pStr = strings.TrimSpace(strings.TrimPrefix(pStr, ":"))
			if p, err := strconv.Atoi(pStr); err == nil {
				portVal = p
				log.Info("using PORT from .env: %d", portVal)
			}
		}
	}

	checkEnvExampleSync(envPathVal)

	watchCtx, stopWatch := context.WithCancel(context.Background())
	defer stopWatch()
	go store.Watch(watchCtx, 1500*time.Millisecond)

	sessToken, err := token.New()
	if err != nil {
		log.Error("%v", err)
		os.Exit(1)
	}

	ln, addr, err := listenOn(hostVal, portVal, 20)
	if err != nil {
		log.Error("cannot bind %s:%d", hostVal, portVal)
		os.Exit(1)
	}

	allow := splitAllow(allowListVal)
	if len(allow) == 0 && configPathVal == "" {
		log.Warn("no --allow set: proxy is DISABLED for security. Use -a/--allow host1,host2 (e.g. openai.com,httpbin.org) to enable.")
	}

	hist := history.New(200)

	var gw *gateway.Gateway
	if configPathVal != "" {
		cfg, err := gateway.LoadConfig(configPathVal)
		if err != nil {
			log.Error("cannot load config: %v", err)
			os.Exit(1)
		}
		gw = gateway.New(cfg, store, log, hist)
		log.Info("PUBLIC MODE: %d route(s) from %s (/proxy and token endpoints disabled)", len(cfg.Routes), configPathVal)
		for _, r := range cfg.Routes {
			for _, v := range r.Vars {
				if _, ok := store.Get(v); !ok {
					log.Warn("route %q uses env var %q which is not defined yet", r.Name, v)
				}
			}
			log.Info("  /api/%s -> %s", r.Name, r.Target)
		}
	}

	srv := server.New(server.Options{
		Addr:          addr.String(),
		Dir:           dirVal,
		Token:         sessToken,
		Vars:          store,
		EnvPath:       envPathVal,
		EnvNames:      store.Names,
		AllowHost:     allow,
		Log:           log,
		History:       hist,
		Gateway:       gw,
		ShowDashboard: gw == nil || dashboardVal,
		ConfigPath:    configPathVal,
	})

	var httpSrv *http.Server
	if tlsVal {
		cert, key := generateSelfSignedCert()
		_ = ln.Close()
		tlsCert, err := tls.X509KeyPair(cert, key)
		if err != nil {
			log.Error("cannot create TLS cert: %v", err)
			os.Exit(1)
		}
		tlsConfig := &tls.Config{Certificates: []tls.Certificate{tlsCert}, MinVersion: tls.VersionTLS12}
		tlsLn, err := tls.Listen("tcp", addr.String(), tlsConfig)
		if err != nil {
			log.Error("tls listen on %s: %v", addr, err)
			os.Exit(1)
		}
		log.Info("envGo HTTPS running -> https://%s/", addr)
		httpSrv = &http.Server{Addr: addr.String(), Handler: srv}
		go func() {
			if err := httpSrv.Serve(tlsLn); err != nil && err != http.ErrServerClosed {
				log.Error("server error: %v", err)
				os.Exit(1)
			}
		}()
	} else {
		httpSrv = &http.Server{Addr: addr.String(), Handler: srv}
		log.Info("envGo running -> http://%s/", addr)
		go func() {
			if err := httpSrv.Serve(ln); err != nil && err != http.ErrServerClosed {
				log.Error("server error: %v", err)
				os.Exit(1)
			}
		}()
	}

	if openBrowserVal {
		go func() {
			time.Sleep(300 * time.Millisecond)
			scheme := "https"; if !tlsVal { scheme = "http" }
			_ = openBrowser(scheme + "://" + addr.String() + "/")
		}()
	}

	stop := make(chan os.Signal, 1)
	signal.Notify(stop, os.Interrupt, syscall.SIGTERM)
	<-stop

	log.Info("shutting down...")
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	_ = httpSrv.Shutdown(ctx)
}

func doInit(name string) {
	cwd, _ := os.Getwd()
	dir := cwd
	if name != "" && name != filepath.Base(cwd) {
		dir = filepath.Join(cwd, name)
		if _, err := os.Stat(dir); err == nil {
			fmt.Fprintf(os.Stderr, "Error: directory %s already exists\n", dir)
			os.Exit(1)
		}
		if err := os.MkdirAll(dir, 0755); err != nil {
			fmt.Fprintf(os.Stderr, "Error creating dir: %v\n", err)
			os.Exit(1)
		}
	}

	envContent := `# envGo project
# Copy to .env and fill in real values. NEVER commit the real .env.

MY_SECRET=change-me
HOST=127.0.0.1
PORT=8080
`
	envExampleContent := `# envGo project
# Copy to .env and fill in real values. NEVER commit the real .env.

MY_SECRET=change-me
HOST=127.0.0.1
PORT=8080
`
	if err := os.WriteFile(filepath.Join(dir, ".env.example"), []byte(envExampleContent), 0644); err != nil {
		fmt.Fprintf(os.Stderr, "Error writing .env.example: %v\n", err)
		os.Exit(1)
	}
	if err := os.WriteFile(filepath.Join(dir, ".env"), []byte(envContent), 0600); err != nil {
		fmt.Fprintf(os.Stderr, "Error writing .env: %v\n", err)
		os.Exit(1)
	}

	title := name
	if title == "" {
		title = ""
	}
	if title != "" {
		title = title + " — "
	}
	htmlContent := "<!DOCTYPE html>\n<html>\n<head><title>" + title + "envGo</title></head>\n<body>\n<h1>Hello from envGo!</h1>\n<div id=\"MY_SECRET\"></div>\n<script src=\"/__env.js\"></script>\n</body>\n</html>"
	if err := os.WriteFile(filepath.Join(dir, "index.html"), []byte(htmlContent), 0644); err != nil {
		fmt.Fprintf(os.Stderr, "Error writing index.html: %v\n", err)
		os.Exit(1)
	}

	gitignore := ".env\n*.key\n*.secret\n"
	if err := os.WriteFile(filepath.Join(dir, ".gitignore"), []byte(gitignore), 0644); err != nil {
		fmt.Fprintf(os.Stderr, "Error writing .gitignore: %v\n", err)
		os.Exit(1)
	}

	readmeQuickStart := "envgo run dev"
	if name != "" && name != filepath.Base(cwd) {
		readmeQuickStart = "cd " + name + " && envgo run dev"
	}
	readmeContent := "# envGo\n\n" +
		"envGo is a zero-dependency, single-binary runtime that lets HTML/Vanilla JS use `.env` secrets safely.\n\n" +
		"## How It Works\n" +
		"- Secrets stay on the server — never reach the browser\n" +
		"- `__env.js` is Go-served and auto-injects variables into `<div id=\"KEY\">` elements\n" +
		"- Only variable **names** are sent (boolean flag), never values\n" +
		"- PHP files are executed server-side with `.env` vars injected\n\n" +
		"## Quick Start\n" +
		"```bash\n" +
		readmeQuickStart + "\n" +
		"# → http://127.0.0.1:8080/\n" +
		"```\n\n" +
		"## Commands\n" +
		"```bash\n" +
		"envgo run dev                    # start dev server (reads HOST/PORT from .env)\n" +
		"envgo -h                         # show help\n" +
		"envgo -v                         # print version\n" +
		"envgo --tls                      # start with HTTPS (auto-generated cert)\n" +
		"envgo --config routes.json -b    # public mode with gateway\n" +
		"envgo deploy -o ./deploy         # generate Caddyfile + nginx.conf + Dockerfile\n" +
		"envgo init                       # create new project template\n" +
		"```\n\n" +
		"## Public Mode (API Gateway)\n" +
		"Define fixed routes in JSON. Browser calls `/api/<name>`, secrets injected server-side.\n" +
		"```bash\n" +
		"envgo --config envgo.routes.json --env .env --dir . -b\n" +
		"```\n\n" +
		"## Security\n" +
		"- Secrets never leave the server\n" +
		"- `GET /__env/KEY` returns 404\n" +
		"- `GET /__env.js` returns only variable names (boolean flags)\n" +
		"- Typo detection: red banner if `id` doesn't match `.env` key\n" +
		"- PHP security warning: red banner if `echo getenv()` is detected\n" +
		"- Config files (`envgo.routes.json`, `routes.json`) always blocked\n" +
		"- Rate limiting and response scrubbing per route\n\n" +
		"## Deploy\n" +
		"```bash\n" +
		"envgo deploy -o ./deploy\n" +
		"# Copy dist/envgo-* + deploy/Caddyfile to server\n" +
		"```\n\n" +
		"## Files\n" +
		"- `.env` — your secrets (never commit)\n" +
		"- `.env.example` — template with variable names\n" +
		"- `.gitignore` — ignores `.env`\n" +
		"- `index.html` — your website with `<div id=\"KEY\">` elements\n" +
		"- `index.php` — optional PHP files with server-side .env access\n"
if err := os.WriteFile(filepath.Join(dir, "README.md"), []byte(readmeContent), 0644); err != nil {
		fmt.Fprintf(os.Stderr, "Error writing README.md: %v\n", err)
		os.Exit(1)
	}

	fmt.Println("Project created at:", dir)
	fmt.Println("  .env.example")
	fmt.Println("  .env")
	fmt.Println("  .gitignore")
	fmt.Println("  index.html")
	fmt.Println("  README.md")
	fmt.Println()
	if name != "" && name != filepath.Base(cwd) {
		fmt.Println("cd", name, "&& envgo run dev")
	} else {
		fmt.Println("envgo run dev")
	}
}

func doDeploy(outputDir string) {
	if outputDir == "" {
		outputDir = "deploy"
	}
	if err := os.MkdirAll(outputDir, 0755); err != nil {
		fmt.Fprintf(os.Stderr, "Error creating deploy dir: %v\n", err)
		os.Exit(1)
	}

	caddyfile := `:443 {
    tls /etc/ssl/certs/envgo.crt /etc/ssl/private/envgo.key
    root * .
    file_server
    reverse_proxy /api/* localhost:8080
}

:80 {
    redir https://{host}{uri} permanent
}
`
	if err := os.WriteFile(filepath.Join(outputDir, "Caddyfile"), []byte(caddyfile), 0644); err != nil {
		fmt.Fprintf(os.Stderr, "Error writing Caddyfile: %v\n", err)
		os.Exit(1)
	}

	nginxConf := `server {
    listen 443 ssl;
    server_name _;
    ssl_certificate /etc/ssl/certs/envgo.crt;
    ssl_certificate_key /etc/ssl/private/envgo.key;
    root /var/www/envgo;
    index index.html;
    location / { try_files $uri $uri/ =404; }
    location /api/ { proxy_pass http://localhost:8080; }
}
server {
    listen 80;
    server_name _;
    return 301 https://$host$request_uri;
}
`
	if err := os.WriteFile(filepath.Join(outputDir, "nginx.conf"), []byte(nginxConf), 0644); err != nil {
		fmt.Fprintf(os.Stderr, "Error writing nginx.conf: %v\n", err)
		os.Exit(1)
	}

	dockerfile := `FROM alpine:latest
RUN apk --no-cache add caddy
COPY . /var/www/envgo
COPY Caddyfile /etc/caddy/Caddyfile
EXPOSE 80 443
CMD ["caddy", "run", "--config", "/etc/caddy/Caddyfile"]
`
	if err := os.WriteFile(filepath.Join(outputDir, "Dockerfile"), []byte(dockerfile), 0644); err != nil {
		fmt.Fprintf(os.Stderr, "Error writing Dockerfile: %v\n", err)
		os.Exit(1)
	}

	fmt.Println("Deploy configs generated at:", outputDir)
	fmt.Println("  Caddyfile")
	fmt.Println("  nginx.conf")
	fmt.Println("  Dockerfile")
	fmt.Println()
	fmt.Println("Copy envGo binary + deploy/ to your server")
}

func generateSelfSignedCert() ([]byte, []byte) {
	priv, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		fmt.Fprintf(os.Stderr, "generate RSA key: %v\n", err)
		os.Exit(1)
	}
	template := x509.Certificate{
		SerialNumber: big.NewInt(1),
		Subject: pkix.Name{Organization: []string{"envGo"}},
		NotBefore: time.Now(),
		NotAfter:  time.Now().Add(365 * 24 * time.Hour),
		KeyUsage:  x509.KeyUsageKeyEncipherment | x509.KeyUsageDigitalSignature,
		ExtKeyUsage: []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth},
		IPAddresses: []net.IP{net.ParseIP("127.0.0.1")},
		DNSNames:  []string{"localhost"},
	}
	certDER, err := x509.CreateCertificate(rand.Reader, &template, &template, &priv.PublicKey, priv)
	if err != nil {
		fmt.Fprintf(os.Stderr, "create certificate: %v\n", err)
		os.Exit(1)
	}
	certPEM := pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: certDER})
	keyPEM := pem.EncodeToMemory(&pem.Block{Type: "RSA PRIVATE KEY", Bytes: x509.MarshalPKCS1PrivateKey(priv)})
	return certPEM, keyPEM
}

func listenOn(host string, port, attempts int) (net.Listener, *net.TCPAddr, error) {
	var lastErr error
	for i := 0; i < attempts; i++ {
		p := port + i
		addr, err := net.ResolveTCPAddr("tcp", fmt.Sprintf("%s:%d", host, p))
		if err != nil {
			return nil, nil, err
		}
		ln, err := net.ListenTCP("tcp", addr)
		if err != nil {
			lastErr = err
			continue
		}
		return ln, addr, nil
	}
	return nil, nil, lastErr
}

func splitAllow(s string) []string {
	var out []string
	for _, part := range strings.Split(s, ",") {
		if p := strings.TrimSpace(part); p != "" {
			out = append(out, p)
		}
	}
	return out
}

func checkEnvExampleSync(envPath string) {
	dir := filepath.Dir(envPath)
	if dir == "." {
		dir = "."
	}
	examplePath := filepath.Join(dir, ".env.example")
	if _, err := os.Stat(examplePath); os.IsNotExist(err) {
		return
	}
	// Parse .env keys
	envKeys := parseEnvKeys(envPath)
	exampleKeys := parseEnvKeys(examplePath)
	for k := range envKeys {
		if _, ok := exampleKeys[k]; !ok {
			fmt.Fprintf(os.Stderr, "envGo: warning: %s in .env but not in .env.example\n", k)
		}
	}
	for k := range exampleKeys {
		if _, ok := envKeys[k]; !ok {
			fmt.Fprintf(os.Stderr, "envGo: hint: %s in .env.example but not in .env — add it\n", k)
		}
	}
}

func parseEnvKeys(path string) map[string]bool {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil
	}
	keys := make(map[string]bool)
	for _, line := range strings.Split(string(data), "\n") {
		line = strings.TrimSpace(line)
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		if idx := strings.Index(line, "="); idx > 0 {
			keys[strings.TrimSpace(line[:idx])] = true
		}
	}
	return keys
}

func openBrowser(url string) error {
	switch runtime.GOOS {
	case "darwin":
		return exec.Command("open", url).Start()
	case "windows":
		return exec.Command("rundll32", "url.dll,FileProtocolHandler", url).Start()
	default:
		return exec.Command("xdg-open", url).Start()
	}
}
