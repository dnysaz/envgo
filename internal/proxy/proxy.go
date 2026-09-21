package proxy

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"regexp"
	"sort"
	"strings"
	"time"

	"envbridge/internal/history"
)

var varRe = regexp.MustCompile(`\{([A-Za-z_][A-Za-z0-9_]*)\}`)

// VarSource looks up environment variables. *envstore.Store implements it.
type VarSource interface {
	Get(name string) (string, bool)
}

// MapVars adapts a plain map to VarSource (used by tests).
type MapVars map[string]string

func (m MapVars) Get(name string) (string, bool) {
	v, ok := m[name]
	return v, ok
}

// InjectVars replaces every {NAME} placeholder found in s. ok is false (and
// missing holds the first missing name) when a referenced variable is absent.
func InjectVars(s string, vars VarSource) (out string, ok bool, missing string) {
	out, _, missing, ok = inject(s, vars)
	return
}

func inject(s string, vars VarSource) (out string, used []string, missing string, ok bool) {
	seen := map[string]bool{}
	out = varRe.ReplaceAllStringFunc(s, func(m string) string {
		name := m[1 : len(m)-1]
		v, found := vars.Get(name)
		if !found {
			if missing == "" {
				missing = name
			}
			return m
		}
		if !seen[name] {
			seen[name] = true
			used = append(used, name)
		}
		return v
	})
	return out, used, missing, missing == ""
}

type loggerI interface {
	Info(format string, args ...any)
	Warn(format string, args ...any)
	Error(format string, args ...any)
	Debug(format string, args ...any)
}

// Handler proxies outbound requests after injecting env values.
type Handler struct {
	vars   VarSource
	log    loggerI
	hist   *history.History
	client *http.Client
	allow  []string
}

func New(vars VarSource, log loggerI, allow []string, hist *history.History) *Handler {
	h := &Handler{
		vars:  vars,
		log:   log,
		hist:  hist,
		allow: allow,
	}
	h.client = &http.Client{
		Timeout: 60 * time.Second,
		CheckRedirect: func(req *http.Request, via []*http.Request) error {
			if !strings.HasPrefix(req.URL.String(), "https://") {
				return fmt.Errorf("redirect to non-HTTPS blocked: %s", req.URL)
			}
			host := hostOnly(req.URL.String())
			if !hostAllowed(host, allow) {
				return fmt.Errorf("redirect host not in allowlist: %s", host)
			}
			return nil
		},
	}
	return h
}

type ProxyRequest struct {
	TargetURL string            `json:"target_url"`
	Method    string            `json:"method"`
	Headers   map[string]string `json:"headers"`
	Body      json.RawMessage   `json:"body"`
}

func (h *Handler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	start := time.Now()
	entry := history.Entry{Time: start}

	if r.Method != http.MethodPost {
		writeError(w, http.StatusMethodNotAllowed, "method not allowed")
		return
	}

	body, err := io.ReadAll(io.LimitReader(r.Body, 16<<20))
	if err != nil {
		writeError(w, http.StatusBadRequest, "read request: "+err.Error())
		return
	}
	r.Body.Close()

	var req ProxyRequest
	if err := json.Unmarshal(body, &req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid JSON: "+err.Error())
		return
	}

	used := map[string]bool{}
	collect := func(names []string) {
		for _, n := range names {
			used[n] = true
		}
	}

	target, names, missing, ok := inject(req.TargetURL, h.vars)
	collect(names)
	if !ok {
		h.finish(w, &entry, start, http.StatusBadRequest, "missing env variable: "+missing)
		return
	}
	if err := h.validateTarget(target); err != nil {
		h.finish(w, &entry, start, http.StatusForbidden, err.Error())
		return
	}

	method := strings.ToUpper(strings.TrimSpace(req.Method))
	if method == "" {
		method = http.MethodPost
	}
	entry.Method = method
	entry.Host = hostOnly(target)

	outReq, err := http.NewRequest(method, target, nil)
	if err != nil {
		h.finish(w, &entry, start, http.StatusBadRequest, "build request: "+err.Error())
		return
	}

	for k, v := range req.Headers {
		val, names, missing, ok := inject(v, h.vars)
		collect(names)
		if !ok {
			h.finish(w, &entry, start, http.StatusBadRequest, "missing env variable: "+missing)
			return
		}
		outReq.Header.Set(k, val)
	}

	if len(req.Body) > 0 && string(req.Body) != "null" {
		payload, names, missing, ok := h.injectBody(req.Body)
		collect(names)
		if !ok {
			h.finish(w, &entry, start, http.StatusBadRequest, "missing env variable: "+missing)
			return
		}
		outReq.Body = io.NopCloser(bytes.NewReader(payload))
	}

	resp, err := h.client.Do(outReq)
	if err != nil {
		h.log.Error("proxy %s %s: %v", method, target, err)
		h.finish(w, &entry, start, http.StatusBadGateway, err.Error())
		return
	}
	defer resp.Body.Close()

	copyResponseHeaders(w.Header(), resp.Header)
	w.Header().Set("Content-Length", "")
	w.WriteHeader(resp.StatusCode)

	flusher, _ := w.(http.Flusher)
	buf := make([]byte, 32*1024)
	for {
		n, err := resp.Body.Read(buf)
		if n > 0 {
			if _, werr := w.Write(buf[:n]); werr != nil {
				break
			}
			if flusher != nil {
				flusher.Flush()
			}
		}
		if err == io.EOF || err == io.ErrUnexpectedEOF {
			break
		}
		if err != nil {
			break
		}
	}

	entry.Variables = sortedKeys(used)
	entry.Status = resp.StatusCode
	h.record(entry, start)
	h.log.Info("proxy %s %s (%d)", method, entry.Host, resp.StatusCode)
}

// finish records an error response and writes it.
func (h *Handler) finish(w http.ResponseWriter, entry *history.Entry, start time.Time, status int, msg string) {
	entry.Status = status
	entry.Error = msg
	h.record(*entry, start)
	writeError(w, status, msg)
}

func (h *Handler) record(e history.Entry, start time.Time) {
	e.Ms = time.Since(start).Milliseconds()
	if h.hist != nil {
		h.hist.Add(e)
	}
}

func (h *Handler) injectBody(raw json.RawMessage) (out []byte, used []string, missing string, ok bool) {
	trimmed := bytes.TrimSpace(raw)
	if len(trimmed) > 0 && trimmed[0] == '"' {
		var s string
		if err := json.Unmarshal(raw, &s); err != nil {
			return raw, nil, "", true
		}
		inj, used, missing, ok := inject(s, h.vars)
		if !ok {
			return nil, used, missing, false
		}
		marshalled, err := json.Marshal(inj)
		if err != nil {
			return raw, used, "", true
		}
		return marshalled, used, "", true
	}
	inj, used, missing, ok := inject(string(trimmed), h.vars)
	if !ok {
		return nil, used, missing, false
	}
	return []byte(inj), used, "", true
}

func (h *Handler) validateTarget(target string) error {
	if !strings.HasPrefix(target, "https://") {
		return fmt.Errorf("only HTTPS targets are allowed")
	}
	host := hostOnly(target)
	if host == "" {
		return fmt.Errorf("invalid target URL")
	}
	if !hostAllowed(host, h.allow) {
		return fmt.Errorf("target host not in allowlist: %s", host)
	}
	return nil
}

func hostOnly(target string) string {
	rest := strings.TrimPrefix(target, "https://")
	if i := strings.IndexByte(rest, '/'); i >= 0 {
		rest = rest[:i]
	}
	if i := strings.IndexByte(rest, '?'); i >= 0 {
		rest = rest[:i]
	}
	if i := strings.LastIndexByte(rest, ':'); i >= 0 && strings.IndexByte(rest, ':') == i {
		rest = rest[:i]
	}
	return strings.ToLower(rest)
}

func hostAllowed(host string, allow []string) bool {
	if len(allow) == 0 {
		return false
	}
	for _, a := range allow {
		a = strings.ToLower(strings.TrimSpace(a))
		if a == "*" || a == host {
			return true
		}
		if strings.HasPrefix(a, ".") && strings.HasSuffix(host, a) {
			return true
		}
		if strings.HasSuffix(host, "."+a) {
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

var hopByHop = []string{
	"Connection", "Keep-Alive", "Proxy-Authenticate", "Proxy-Authorization",
	"Proxy-Connection", "TE", "Trailer", "Transfer-Encoding", "Upgrade",
	"Content-Length",
}

func copyResponseHeaders(dst, src http.Header) {
	for k, vv := range src {
		if contains(hopByHop, k) {
			continue
		}
		for _, v := range vv {
			dst.Add(k, v)
		}
	}
}

func contains(list []string, s string) bool {
	for _, item := range list {
		if http.CanonicalHeaderKey(item) == http.CanonicalHeaderKey(s) {
			return true
		}
	}
	return false
}

func writeError(w http.ResponseWriter, status int, msg string) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(map[string]string{"error": msg})
}
