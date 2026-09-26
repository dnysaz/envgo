package server

import (
	"context"
	"crypto/subtle"
	"encoding/json"
	"net"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"
	"time"

	"envbridge/internal/gateway"
	"envbridge/internal/history"
	"envbridge/internal/logger"
	"envbridge/internal/proxy"
)

type Options struct {
	Addr          string
	Dir           string
	Token         string
	Vars          proxy.VarSource
	EnvPath       string
	EnvNames      func() []string
	AllowHost     []string
	Log           *logger.Logger
	History       *history.History
	Gateway       *gateway.Gateway
	ShowDashboard bool
	ConfigPath    string
}

// Server wires the static file handler, the /proxy engine and the
// handshake endpoint behind a single set of security guards.
type Server struct {
	opts    Options
	proxy   *proxy.Handler
	hosts   []string
	origins []string
}

func New(o Options) *Server {
	return &Server{
		opts:  o,
		proxy: proxy.New(o.Vars, o.Log, o.AllowHost, o.History),
		hosts: allowedHosts(o.Addr),
		origins: []string{
			"http://localhost" + hostPortOnly(o.Addr),
			"http://" + o.Addr,
		},
	}
}

func allowedHosts(addr string) []string {
	host, port, err := net.SplitHostPort(addr)
	if err != nil {
		return []string{addr}
	}
	return []string{
		addr,
		net.JoinHostPort("localhost", port),
		net.JoinHostPort(host, port),
	}
}

func hostPortOnly(addr string) string {
	_, port, err := net.SplitHostPort(addr)
	if err != nil {
		return ""
	}
	return ":" + port
}

func (s *Server) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	if s.opts.Gateway != nil {
		switch {
		case strings.HasPrefix(r.URL.Path, "/api/"):
			s.opts.Gateway.ServeHTTP(w, r)
			return
		case r.URL.Path == "/proxy", r.URL.Path == "/__envgo_token", strings.HasPrefix(r.URL.Path, "/__env/"):
			http.NotFound(w, r)
			return
		}
	}
	switch {
	case r.URL.Path == "/proxy":
		s.serveProxy(w, r)
	case r.URL.Path == "/__envgo_token":
		s.serveToken(w, r)
	case r.URL.Path == "/__env.js", r.URL.Path == "/env.js":
		s.serveEnvJS(w, r)
	case r.URL.Path == "/history":
		s.serveHistory(w, r)
	case r.URL.Path == "/__envgo_dashboard", r.URL.Path == "/__envgo_dashboard/data":
		if !s.opts.ShowDashboard {
			http.NotFound(w, r)
			return
		}
		if r.URL.Path == "/__envgo_dashboard" {
			s.serveDashboard(w, r)
		} else {
			s.serveDashboardData(w, r)
		}
	default:
		s.serveStatic(w, r)
	}
}

func (s *Server) serveProxy(w http.ResponseWriter, r *http.Request) {
	if r.Method == http.MethodOptions {
		s.handlePreflight(w, r)
		return
	}
	if !s.authorized(r) {
		http.Error(w, `{"error":"unauthorized"}`, http.StatusUnauthorized)
		return
	}
	s.proxy.ServeHTTP(w, r)
}

func (s *Server) serveToken(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	if !s.hostAllowed(r.Host) {
		http.Error(w, "forbidden", http.StatusForbidden)
		return
	}
	if !s.originAllowed(r.Header.Get("Origin")) {
		http.Error(w, "forbidden", http.StatusForbidden)
		return
	}
	w.Header().Set("Content-Type", "text/plain")
	w.Header().Set("Cache-Control", "no-store")
	_, _ = w.Write([]byte(s.opts.Token))
}

func (s *Server) serveHistory(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	if !s.authorized(r) {
		http.Error(w, `{"error":"unauthorized"}`, http.StatusUnauthorized)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("Cache-Control", "no-store")
	var reqs []history.Entry
	if s.opts.History != nil {
		reqs = s.opts.History.List()
	}
	if reqs == nil {
		reqs = []history.Entry{}
	}
	_ = json.NewEncoder(w).Encode(reqs)
}

func (s *Server) serveEnvJS(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	if !s.hostAllowed(r.Host) {
		http.Error(w, "forbidden", http.StatusForbidden)
		return
	}
	w.Header().Set("Content-Type", "application/javascript; charset=utf-8")
	w.Header().Set("Cache-Control", "no-store")
	js := `window.EnvLoaded={};
function lev(a,b){const m=[];for(let i=0;i<=b.length;i++)m[i]=[i];for(let j=0;j<=a.length;j++)m[0][j]=j;for(let i=1;i<=b.length;i++)for(let j=1;j<=a.length;j++)m[i][j]=b[i-1]===a[j-1]?m[i-1][j-1]:Math.min(m[i-1][j]+1,m[i][j-1]+1,m[i-1][j-1]+1);return m[b.length][a.length];}
function suggest(name, vars){let best=null,dist=9;for(const v of vars){const d=lev(name,v);if(d<dist){dist=d;best=v;}}return dist<=3?best:null;}
(async()=>{
try{
const d=await fetch("/__envgo_dashboard/data").then(r=>r.json());
const vars=d.vars||[];const set=new.Set(vars);
const missing=[];
for(const e of document.querySelectorAll("[id]")){
const n=e.id;if(!n||n==="out"||n==="chatBtn"||n==="btn"||n==="prompt"||n==="askBtn")continue;
if(set.has(n)){
window.EnvLoaded[n]=true;
e.textContent="✓ "+n+" — Success (env exists, value hidden)";
e.className="border border-emerald-200 bg-emerald-50 text-emerald-700 rounded-xl p-4 text-sm";
}else{
if(n.trim()!=="")missing.push(n);
e.textContent="✗ "+n+" — ID not match Key in .env";
e.className="border border-red-200 bg-red-50 text-red-700 rounded-xl p-4 text-sm";
}
}
if(missing.length){
const s=suggest(missing[0], vars);
const msg=s?"Did you mean '"+s+"' ? Check .env for '"+s+"' (available: "+vars.join(", ")+")":"Available Keys in .env: "+(vars.join(", ")||"(none)")+" — add '"+missing[0]+"=value' to .env";
const b=document.createElement("div");
b.style.cssText="position:fixed;top:0;left:0;right:0;background:#fee2e2;color:#991b1b;padding:12px 16px;text-align:center;z-index:9999;font:12px monospace;border-bottom:1px solid #fecaca";
b.textContent="envGo — ID not match Key: '"+missing.join(", ")+"' not found in .env. "+msg;
document.body.prepend(b);
document.body.style.paddingTop="44px";
}
}catch{}
})();
`
	_, _ = w.Write([]byte(js))
}

// authorized enforces the three-guard handshake for /proxy:
// Host header (anti DNS-rebinding), Origin, and the session token.
func (s *Server) authorized(r *http.Request) bool {
	if !s.hostAllowed(r.Host) {
		return false
	}
	if !s.originAllowed(r.Header.Get("Origin")) {
		return false
	}
	tok := r.Header.Get("X-EnvGo-Token")
	const empty = ""
	return subtle.ConstantTimeCompare([]byte(tok), []byte(empty)) != 1 &&
		subtle.ConstantTimeCompare([]byte(tok), []byte(s.opts.Token)) == 1
}

func (s *Server) hostAllowed(host string) bool {
	for _, h := range s.hosts {
		if h == host {
			return true
		}
	}
	return false
}

func (s *Server) originAllowed(origin string) bool {
	if origin == "" {
		return true
	}
	for _, o := range s.origins {
		if o == origin {
			return true
		}
	}
	return false
}

func (s *Server) handlePreflight(w http.ResponseWriter, r *http.Request) {
	origin := r.Header.Get("Origin")
	if origin == "" || !s.originAllowed(origin) {
		http.Error(w, "forbidden", http.StatusForbidden)
		return
	}
	w.Header().Set("Access-Control-Allow-Origin", origin)
	w.Header().Set("Access-Control-Allow-Methods", "POST, OPTIONS")
	w.Header().Set("Access-Control-Allow-Headers", "Content-Type, X-EnvGo-Token")
	w.Header().Set("Access-Control-Max-Age", "600")
	w.WriteHeader(http.StatusNoContent)
}

func (s *Server) serveStatic(w http.ResponseWriter, r *http.Request) {
	if strings.Contains(r.URL.Path, "..") || strings.Contains(r.URL.Path, "\\") {
		http.NotFound(w, r)
		return
	}
	cleaned := strings.Trim(r.URL.Path, "/")
	if cleaned == "" {
		s.serveFileOrIndex(w, r, s.opts.Dir)
		return
	}
	full := filepath.Join(s.opts.Dir, filepath.FromSlash(cleaned))
	if s.opts.ConfigPath != "" {
		if full == s.opts.ConfigPath || filepath.Base(full) == filepath.Base(s.opts.ConfigPath) {
			http.NotFound(w, r)
			return
		}
		if relCfg, err := filepath.Rel(s.opts.Dir, s.opts.ConfigPath); err == nil && relCfg != "" && relCfg != "." && cleaned == filepath.ToSlash(relCfg) {
			http.NotFound(w, r)
			return
		}
	}
	base := filepath.Base(cleaned)
	if base == "envgo.routes.json" || base == "routes.json" {
		http.NotFound(w, r)
		return
	}
	rel, err := filepath.Rel(s.opts.Dir, full)
	if err != nil || rel == ".." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
		http.NotFound(w, r)
		return
	}
	segments := strings.Split(filepath.ToSlash(rel), "/")
	for _, seg := range segments {
		if strings.HasPrefix(seg, ".") {
			http.NotFound(w, r)
			return
		}
	}
	fi, err := os.Stat(full)
	if err != nil {
		http.NotFound(w, r)
		return
	}
	if fi.IsDir() {
		s.serveFileOrIndex(w, r, full)
		return
	}
	if strings.HasSuffix(strings.ToLower(full), ".php") {
		if s.servePHP(w, r, full) {
			return
		}
	}
	http.ServeFile(w, r, full)
}

// serveFileOrIndex serves index.html/index.php/index.htm when the target is a directory.
func (s *Server) serveFileOrIndex(w http.ResponseWriter, r *http.Request, dir string) {
	for _, name := range []string{"index.html", "index.php", "index.htm"} {
		idx := filepath.Join(dir, name)
		if fi, err := os.Stat(idx); err == nil && !fi.IsDir() {
			if strings.HasSuffix(name, ".php") {
				if s.servePHP(w, r, idx) {
					return
				}
			}
			http.ServeFile(w, r, idx)
			return
		}
	}
	http.NotFound(w, r)
}

// Serve PHP via exec.Command(php, path), inject .env vars, detect typos, inject error banner.
// Uses php-cgi if available for proper header() and HTTP status support.
func (s *Server) servePHP(w http.ResponseWriter, r *http.Request, path string) bool {
	phpPath, phpMode := findPHPMode()
	if phpPath == "" {
		return false
	}

	// Build CGI environment
	env := os.Environ()
	if s.opts.EnvNames != nil {
		for _, k := range s.opts.EnvNames() {
			if v, ok := s.opts.Vars.Get(k); ok {
				env = append(env, k+"="+v)
			}
		}
	}

	// Standard CGI variables
	absPath, _ := filepath.Abs(path)
	env = append(env,
		"REQUEST_METHOD="+r.Method,
		"QUERY_STRING="+r.URL.RawQuery,
		"REQUEST_URI="+r.URL.RequestURI(),
		"SCRIPT_FILENAME="+absPath,
		"SCRIPT_NAME="+filepath.ToSlash(r.URL.Path),
		"SERVER_PROTOCOL=HTTP/1.1",
		"GATEWAY_INTERFACE=CGI/1.1",
		"SERVER_NAME="+r.Host,
		"REMOTE_ADDR=127.0.0.1",
	)

	// Set CONTENT_TYPE and CONTENT_LENGTH for POST/PUT requests
	if r.Method == http.MethodPost || r.Method == http.MethodPut || r.Method == http.MethodPatch {
		if ct := r.Header.Get("Content-Type"); ct != "" {
			env = append(env, "CONTENT_TYPE="+ct)
		}
		if cl := r.Header.Get("Content-Length"); cl != "" {
			env = append(env, "CONTENT_LENGTH="+cl)
		}
	}

	var cmd *exec.Cmd
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	if phpMode == "cgi" {
		// php-cgi needs a stdin for POST bodies in CGI mode
		cmd = exec.CommandContext(ctx, phpPath, absPath)
	} else {
		// CLI mode: headers() won't produce HTTP headers, but still injects vars
		cmd = exec.CommandContext(ctx, phpPath, absPath)
	}
	cmd.Env = env

	// Pass the request body to PHP so php://input and $_POST work
	if r.Method == http.MethodPost || r.Method == http.MethodPut || r.Method == http.MethodPatch {
		cmd.Stdin = r.Body
		defer r.Body.Close()
	}

	out, err := cmd.CombinedOutput()
	if err != nil {
		return false
	}

	body := out
	if idx := findHeaderEnd(out); idx != -1 {
		hdrPart := string(out[:idx])
		body = out[idx:]
		for _, line := range strings.Split(hdrPart, "\n") {
			line = strings.TrimSpace(line)
			if line == "" {
				continue
			}
			// Skip CGI status lines like "Status: 200" or "HTTP/1.1 200 OK"
			if strings.HasPrefix(line, "Status:") {
				continue
			}
			if strings.HasPrefix(line, "HTTP/") {
				continue
			}
			if kv := strings.SplitN(line, ":", 2); len(kv) == 2 {
				w.Header().Set(strings.TrimSpace(kv[0]), strings.TrimSpace(kv[1]))
			}
		}
		if w.Header().Get("Content-Type") == "" {
			w.Header().Set("Content-Type", "text/html; charset=utf-8")
		}
	} else if phpMode == "cli" {
		// CLI mode doesn't output HTTP headers
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
	} else {
		// CGI mode without headers — output is the raw body
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
	}
	// Detect PHP typo: scan source for getenv/$_ENV/$_SERVER keys
	missing := detectPHPTypo(path, s.opts.EnvNames)
	if len(missing) > 0 {
		names := strings.Join(missing, ", ")
		suggestion := ""
		if s.opts.EnvNames != nil {
			vars := s.opts.EnvNames()
			if first := missing[0]; len(first) > 0 {
				if sv := closestMatch(first, vars); sv != "" {
					suggestion = " Did you mean '" + sv + "'?"
				}
			}
		}
		banner := `<div style="position:fixed;top:0;left:0;right:0;background:#fee2e2;color:#991b1b;padding:12px 16px;text-align:center;z-index:9999;font:13px monospace;border-bottom:2px solid #f87171">envGo — PHP env typo: undefined variable ` + names + ` not found in .env.` + suggestion + `</div>`
		body = append([]byte(banner), body...)
	}
	// Detect PHP security: warn if developer echoes secret values to browser
	secIssues := detectPHPSecurityIssue(path)
	if len(secIssues) > 0 {
		issueNames := strings.Join(secIssues, ", ")
		banner := `<div style="position:fixed;top:0;left:0;right:0;background:#fee2e2;color:#991b1b;padding:12px 16px;text-align:center;z-index:9999;font:13px monospace;border-bottom:2px solid #f87171">envGo — SECURITY WARNING: ` + issueNames + ` echoed to browser. Secrets exposed! Remove echo/print of getenv/$_ENV/$_SERVER.</div>`
		body = append([]byte(banner), body...)
	}
	_, _ = w.Write(body)
	return true
}

// detectPHPTypo scans PHP source for getenv("VAR") and $_ENV["VAR"] access
// and returns names that are not defined in EnvNames. $_SERVER["..."] access
// is intentionally excluded: PHP superglobals are never .env variables, so
// legitimate uses like $_SERVER["REQUEST_METHOD"] must not trigger false
// positives.
func detectPHPTypo(path string, namesFunc func() []string) []string {
	src, err := os.ReadFile(path)
	if err != nil {
		return nil
	}
	s := string(src)
	// Match getenv("KEY") and $_ENV["KEY"] only. $_SERVER["KEY"] is a PHP
	// superglobal and is not treated as an env-var reference here.
	re := regexp.MustCompile(`(?:getenv\s*\(|\$_ENV\s*\[)\s*['"]([^'"]+)['"]`)
	defined := make(map[string]bool)
	if namesFunc != nil {
		for _, n := range namesFunc() {
			defined[n] = true
		}
	}
	seen := make(map[string]bool)
	var missing []string
	for _, m := range re.FindAllStringSubmatch(s, -1) {
		if len(m) < 2 {
			continue
		}
		key := m[1]
		if seen[key] {
			continue
		}
		seen[key] = true
		if !defined[key] {
			missing = append(missing, key)
		}
	}
	return missing
}

// detectPHPSecurityIssue scans PHP source for lines that echo/print
// secret values (getenv, $_ENV, $_SERVER) to the browser.
// Returns the suspicious variable names found.
func detectPHPSecurityIssue(path string) []string {
	src, err := os.ReadFile(path)
	if err != nil {
		return nil
	}
	s := string(src)
	// Match echo/print/var_dump/print_r directly followed by getenv() or $_ENV[ or $_SERVER[
	secretEchoRe := regexp.MustCompile(`\b(?:echo|print|var_dump|print_r)\s+(?:getenv\s*\(|\$_ENV\s*\[|\$_SERVER\s*\[)\s*['"]`)
	seen := make(map[string]bool)
	var issues []string
	for _, line := range strings.Split(s, "\n") {
		if !secretEchoRe.MatchString(line) {
			continue
		}
		for _, name := range []string{"getenv", "$_ENV", "$_SERVER"} {
			if strings.Contains(line, name) && !seen[name] {
				seen[name] = true
				issues = append(issues, name)
			}
		}
	}
	return issues
}

// closestMatch returns closest string by Levenshtein distance <= 3.
func closestMatch(s string, candidates []string) string {
	best := ""
	dist := 999
	for _, c := range candidates {
		d := levenshtein(s, c)
		if d < dist {
			dist = d
			best = c
		}
	}
	if dist <= 3 {
		return best
	}
	return ""
}

func levenshtein(a, b string) int {
	m, n := len(a), len(b)
	dp := make([][]int, m+1)
	for i := range dp {
		dp[i] = make([]int, n+1)
		dp[i][0] = i
	}
	for j := 0; j <= n; j++ {
		dp[0][j] = j
	}
	for i := 1; i <= m; i++ {
		for j := 1; j <= n; j++ {
			if a[i-1] == b[j-1] {
				dp[i][j] = dp[i-1][j-1]
			} else {
				dp[i][j] = 1 + min(dp[i-1][j], dp[i][j-1], dp[i-1][j-1])
			}
		}
	}
	return dp[m][n]
}

func min(a, b, c int) int {
	if a <= b && a <= c {
		return a
	}
	if b <= a && b <= c {
		return b
	}
	return c
}

func findPHP() (string, error) {
	for _, name := range []string{"php-cgi", "php", "php8", "php81", "php8.1", "php82", "php8.2", "php83", "php8.3", "php84", "php8.4", "php7", "php74", "php7.4"} {
		if p, err := exec.LookPath(name); err == nil {
			return p, nil
		}
	}
	return "", os.ErrNotExist
}

func findPHPMode() (string, string) {
	if path, err := exec.LookPath("php-cgi"); err == nil {
		return path, "cgi"
	}
	for _, name := range []string{"php", "php8", "php81", "php8.2", "php82", "php8.3", "php8.3", "php84", "php8.4", "php7", "php74", "php7.4"} {
		if path, err := exec.LookPath(name); err == nil {
			return path, "cli"
		}
	}
	return "", ""
}

func findHeaderEnd(out []byte) int {
	if idx := findBytes(out, []byte("\r\n\r\n")); idx != -1 {
		return idx + 4
	}
	if idx := findBytes(out, []byte("\n\n")); idx != -1 {
		return idx + 2
	}
	return -1
}

func findBytes(b, sep []byte) int {
	for i := 0; i+len(sep) <= len(b); i++ {
		match := true
		for j := range sep {
			if b[i+j] != sep[j] {
				match = false
				break
			}
		}
		if match {
			return i
		}
	}
	return -1
}
