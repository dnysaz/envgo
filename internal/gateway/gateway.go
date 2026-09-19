package gateway

import (
	"bytes"
	"crypto/subtle"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"time"

	"envbridge/internal/history"
	"envbridge/internal/proxy"
	"envbridge/internal/ratelimit"
)

var varRe = regexp.MustCompile(`\{([A-Za-z_][A-Za-z0-9_]*)\}`)

// Gateway is the public-mode request handler. Unlike the local proxy, the
// target URL comes from the config (trusted) and the browser only picks a
// route name plus payload.
type Gateway struct {
	cfg     *Config
	vars    proxy.VarSource
	log     Logger
	client  *http.Client
	limiter *ratelimit.Limiter
	hist    *history.History
	routes  map[string]*compiled
}

// Logger is the subset of the redacting logger the gateway needs.
type Logger interface {
	Info(format string, args ...any)
	Warn(format string, args ...any)
	Error(format string, args ...any)
	Debug(format string, args ...any)
	Redact(s string) string
}

type compiled struct {
	cfg    *Route
	limit  int
	period time.Duration
	vars   map[string]bool
}

func New(cfg *Config, vars proxy.VarSource, log Logger, hist *history.History) *Gateway {
	defLimit, defPeriod, _ := ratelimit.Parse(cfg.DefaultRateLimit)
	g := &Gateway{
		cfg:     cfg,
		vars:    vars,
		log:     log,
		client:  &http.Client{Timeout: 60 * time.Second},
		limiter: ratelimit.New(),
		hist:    hist,
		routes:  make(map[string]*compiled, len(cfg.Routes)),
	}
	for i := range cfg.Routes {
		r := &cfg.Routes[i]
		limit, period, _ := ratelimit.Parse(r.RateLimit)
		if strings.TrimSpace(r.RateLimit) == "" {
			limit, period = defLimit, defPeriod
		}
		g.routes[strings.ToLower(r.Name)] = &compiled{
			cfg:    r,
			limit:  limit,
			period: period,
			vars:   r.AllowedVars(),
		}
	}
	return g
}

func (g *Gateway) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	start := time.Now()
	name := strings.TrimPrefix(r.URL.Path, "/api/")
	if name == "" || strings.Contains(name, "/") {
		http.NotFound(w, r)
		return
	}
	rt := g.routes[strings.ToLower(name)]
	if rt == nil {
		http.NotFound(w, r)
		return
	}
	if !rt.cfg.MethodAllowed(r.Method) {
		w.Header().Set("Allow", routeAllowHeader(rt.cfg))
		writeJSONError(w, http.StatusMethodNotAllowed, "method not allowed for route")
		return
	}

	entry := history.Entry{Time: start, Method: strings.ToUpper(r.Method), Host: rt.cfg.Name}

	ip := g.clientIP(r)
	if rt.cfg.Auth != nil && !g.authOK(r, rt.cfg.Auth) {
		entry.Status = http.StatusUnauthorized
		entry.Error = "unauthorized"
		g.record(entry, start)
		writeJSONError(w, http.StatusUnauthorized, "unauthorized")
		return
	}
	if !g.limiter.Allow(rt.cfg.Name+":"+ip, rt.limit, rt.period) {
		entry.Status = http.StatusTooManyRequests
		entry.Error = "rate limit exceeded"
		g.record(entry, start)
		w.Header().Set("Retry-After", "60")
		writeJSONError(w, http.StatusTooManyRequests, "rate limit exceeded")
		return
	}

	g.forward(w, r, rt, entry, start)
}

func (g *Gateway) forward(w http.ResponseWriter, r *http.Request, rt *compiled, entry history.Entry, start time.Time) {
	used := map[string]bool{}

	clientBody, err := io.ReadAll(io.LimitReader(r.Body, 16<<20))
	if err != nil {
		g.fail(w, entry, start, http.StatusBadRequest, "read body: "+err.Error())
		return
	}
	r.Body.Close()

	target, err := url.Parse(rt.cfg.Target)
	if err != nil {
		g.fail(w, entry, start, http.StatusInternalServerError, "invalid target")
		return
	}

	q := target.Query()
	for k, vs := range r.URL.Query() {
		for _, v := range vs {
			iv, err := g.inject(v, rt.vars, used)
			if err != nil {
				g.fail(w, entry, start, http.StatusBadRequest, err.Error())
				return
			}
			q.Add(k, iv)
		}
	}
	for k, v := range rt.cfg.Inject.Query {
		iv, err := g.inject(v, rt.vars, used)
		if err != nil {
			g.fail(w, entry, start, http.StatusInternalServerError, err.Error())
			return
		}
		q.Set(k, iv)
	}
	target.RawQuery = q.Encode()

	method := strings.ToUpper(r.Method)

	var bodyBytes []byte
	if len(rt.cfg.Inject.Body) > 0 {
		merged, err := mergeJSON(clientBody, rt.cfg.Inject.Body)
		if err != nil {
			g.fail(w, entry, start, http.StatusBadRequest, "invalid JSON body: "+err.Error())
			return
		}
		bodyBytes = merged
	} else {
		bodyBytes = clientBody
	}
	if len(bodyBytes) > 0 {
		injected, err := g.inject(string(bodyBytes), rt.vars, used)
		if err != nil {
			g.fail(w, entry, start, http.StatusBadRequest, err.Error())
			return
		}
		bodyBytes = []byte(injected)
	}

	var reader io.Reader
	if len(bodyBytes) > 0 {
		reader = bytes.NewReader(bodyBytes)
	}
	outReq, err := http.NewRequestWithContext(r.Context(), method, target.String(), reader)
	if err != nil {
		g.fail(w, entry, start, http.StatusBadRequest, "build request: "+err.Error())
		return
	}

	for k, vv := range r.Header {
		if reservedHeader(k) {
			continue
		}
		for _, v := range vv {
			iv, err := g.inject(v, rt.vars, used)
			if err != nil {
				g.fail(w, entry, start, http.StatusBadRequest, err.Error())
				return
			}
			outReq.Header.Add(k, iv)
		}
	}
	for k, v := range rt.cfg.Inject.Headers {
		iv, err := g.inject(v, rt.vars, used)
		if err != nil {
			g.fail(w, entry, start, http.StatusInternalServerError, err.Error())
			return
		}
		outReq.Header.Set(k, iv)
	}

	resp, err := g.client.Do(outReq)
	if err != nil {
		g.log.Error("gateway %s -> %s: %v", rt.cfg.Name, target.Host, err)
		g.fail(w, entry, start, http.StatusBadGateway, "upstream error: "+err.Error())
		return
	}
	defer resp.Body.Close()

	copyResponseHeaders(w.Header(), resp.Header)
	w.Header().Del("Content-Length")

	if isStreaming(resp) || !g.cfg.ScrubResponse {
		w.WriteHeader(resp.StatusCode)
		flushCopy(w, resp.Body)
	} else {
		data, err := io.ReadAll(io.LimitReader(resp.Body, 32<<20))
		if err != nil {
			g.fail(w, entry, start, http.StatusBadGateway, "read upstream: "+err.Error())
			return
		}
		if g.log != nil {
			data = []byte(g.log.Redact(string(data)))
		}
		w.Header().Set("Content-Length", strconv.Itoa(len(data)))
		w.WriteHeader(resp.StatusCode)
		_, _ = w.Write(data)
	}

	entry.Variables = sortedKeys(used)
	entry.Status = resp.StatusCode
	g.record(entry, start)
	g.log.Info("gateway %s (%d) %dms", rt.cfg.Name, resp.StatusCode, time.Since(start).Milliseconds())
}

func (g *Gateway) fail(w http.ResponseWriter, entry history.Entry, start time.Time, status int, msg string) {
	entry.Status = status
	entry.Error = msg
	g.record(entry, start)
	writeJSONError(w, status, msg)
}

func (g *Gateway) record(e history.Entry, start time.Time) {
	e.Ms = time.Since(start).Milliseconds()
	if g.hist != nil {
		g.hist.Add(e)
	}
}

// inject substitutes {NAME} placeholders, but only for names in the route's
// allow-set. Anything else is an error, so a route can never leak a var it was
// not declared to use.
func (g *Gateway) inject(s string, allowed, used map[string]bool) (string, error) {
	var missing string
	out := varRe.ReplaceAllStringFunc(s, func(m string) string {
		name := m[1 : len(m)-1]
		if !allowed[name] {
			if missing == "" {
				missing = name
			}
			return m
		}
		v, ok := g.vars.Get(name)
		if !ok {
			if missing == "" {
				missing = name
			}
			return m
		}
		used[name] = true
		return v
	})
	if missing != "" {
		return "", fmt.Errorf("variable not available for this route")
	}
	return out, nil
}

func (g *Gateway) authOK(r *http.Request, a *AuthConfig) bool {
	want, ok := g.vars.Get(a.Secret)
	if !ok || want == "" {
		return false
	}
	got := strings.TrimSpace(strings.TrimPrefix(r.Header.Get("Authorization"), "Bearer "))
	if got == "" {
		return false
	}
	return subtle.ConstantTimeCompare([]byte(got), []byte(want)) == 1
}

func (g *Gateway) clientIP(r *http.Request) string {
	if g.cfg.TrustProxy {
		if xff := r.Header.Get("X-Forwarded-For"); xff != "" {
			parts := strings.Split(xff, ",")
			return strings.TrimSpace(parts[0])
		}
	}
	host, _, err := net.SplitHostPort(r.RemoteAddr)
	if err != nil {
		return r.RemoteAddr
	}
	return host
}

func parseRate(spec string) (int, time.Duration, error) {
	return ratelimit.Parse(spec)
}

func mergeJSON(clientBody []byte, add map[string]any) ([]byte, error) {
	obj := map[string]any{}
	trimmed := bytes.TrimSpace(clientBody)
	if len(trimmed) > 0 {
		if err := json.Unmarshal(trimmed, &obj); err != nil {
			return nil, err
		}
	}
	for k, v := range add {
		obj[k] = v
	}
	return json.Marshal(obj)
}

func routeAllowHeader(r *Route) string {
	if strings.TrimSpace(r.Method) == "" {
		return "POST"
	}
	return strings.ToUpper(strings.ReplaceAll(r.Method, " ", ""))
}

func isStreaming(resp *http.Response) bool {
	ct := strings.ToLower(resp.Header.Get("Content-Type"))
	return strings.HasPrefix(ct, "text/event-stream") || strings.Contains(ct, "stream")
}

func flushCopy(w http.ResponseWriter, r io.Reader) {
	flusher, _ := w.(http.Flusher)
	buf := make([]byte, 32*1024)
	for {
		n, err := r.Read(buf)
		if n > 0 {
			if _, werr := w.Write(buf[:n]); werr != nil {
				return
			}
			if flusher != nil {
				flusher.Flush()
			}
		}
		if err == io.EOF || err == io.ErrUnexpectedEOF {
			return
		}
		if err != nil {
			return
		}
	}
}

func writeJSONError(w http.ResponseWriter, status int, msg string) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(map[string]string{"error": msg})
}

var reserved = map[string]bool{
	"host":                true,
	"connection":          true,
	"keep-alive":          true,
	"proxy-authenticate":  true,
	"proxy-authorization": true,
	"proxy-connection":    true,
	"te":                  true,
	"trailer":             true,
	"transfer-encoding":   true,
	"upgrade":             true,
	"content-length":      true,
	"accept-encoding":     true,
	"authorization":       true,
	"x-forwarded-for":     true,
	"x-forwarded-host":    true,
	"x-forwarded-proto":   true,
}

func reservedHeader(k string) bool {
	return reserved[strings.ToLower(k)]
}

var hopByHop = []string{
	"Connection", "Keep-Alive", "Proxy-Authenticate", "Proxy-Authorization",
	"Proxy-Connection", "TE", "Trailer", "Transfer-Encoding", "Upgrade",
	"Content-Length",
}

func copyResponseHeaders(dst, src http.Header) {
	for k, vv := range src {
		if containsFold(hopByHop, k) {
			continue
		}
		for _, v := range vv {
			dst.Add(k, v)
		}
	}
}

func containsFold(list []string, s string) bool {
	for _, item := range list {
		if http.CanonicalHeaderKey(item) == http.CanonicalHeaderKey(s) {
			return true
		}
	}
	return false
}

func sortedKeys(m map[string]bool) []string {
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	sort.Strings(out)
	return out
}
