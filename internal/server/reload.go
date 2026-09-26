package server

import (
	"bytes"
	"net/http"
	"os"
	"path/filepath"
	"strings"

	"envbridge/internal/hotreload"
)

// dev returns true when the server is running in development mode, where the
// file watcher, SSE reload stream, and cache-busting headers are enabled.
func (s *Server) dev() bool {
	return s.opts.DevMode && s.opts.HotReload != nil
}

// setNoCacheHeaders tells the browser not to cache the response. Used in dev
// mode so that a reload always fetches fresh resources (clearing stale cache).
func setNoCacheHeaders(w http.ResponseWriter) {
	w.Header().Set("Cache-Control", "no-store, no-cache, must-revalidate, max-age=0")
	w.Header().Set("Pragma", "no-cache")
	w.Header().Set("Expires", "0")
}

// isHTMLPath reports whether full looks like an HTML document.
func isHTMLPath(full string) bool {
	ext := strings.ToLower(filepath.Ext(full))
	return ext == ".html" || ext == ".htm"
}

// injectReloadScript inserts the HMR client script before </head>, </body>, or
// at the end of the document if neither tag is present.
func injectReloadScript(body []byte) []byte {
	script := []byte(hotreload.ReloadScript)
	body = append([]byte(nil), body...) // defensive copy
	if i := indexCloseTag(body, "</head>"); i >= 0 {
		return insertAt(body, script, i)
	}
	if i := indexCloseTag(body, "</body>"); i >= 0 {
		return insertAt(body, script, i)
	}
	return append(body, script...)
}

func indexCloseTag(body []byte, tag string) int {
	return bytes.Index(bytes.ToLower(body), []byte(tag))
}

func insertAt(body, script []byte, i int) []byte {
	out := make([]byte, 0, len(body)+len(script))
	out = append(out, body[:i]...)
	out = append(out, script...)
	out = append(out, body[i:]...)
	return out
}

// serveHTMLDev serves an HTML file in dev mode with the HMR script injected
// and no-cache headers applied. Returns whether it handled the request.
func (s *Server) serveHTMLDev(w http.ResponseWriter, full string) bool {
	if !s.dev() || !isHTMLPath(full) {
		return false
	}
	b, err := os.ReadFile(full)
	if err != nil {
		return false
	}
	setNoCacheHeaders(w)
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	b = injectReloadScript(b)
	_, _ = w.Write(b)
	return true
}

func (s *Server) serveSSEReload(w http.ResponseWriter, r *http.Request) {
	if !s.dev() {
		http.NotFound(w, r)
		return
	}
	s.opts.HotReload.ServeSSE(w, r)
}

// adminClearCache handles `envgo cache clear`. Dev-only: it broadcasts a
// hard-reload to every connected browser so they fetch fresh, uncached bytes.
func (s *Server) adminClearCache(w http.ResponseWriter, r *http.Request) {
	if !s.dev() || r.Method != http.MethodPost {
		http.NotFound(w, r)
		return
	}
	s.opts.HotReload.HardReload()
	w.Header().Set("Content-Type", "text/plain; charset=utf-8")
	w.WriteHeader(http.StatusOK)
	_, _ = w.Write([]byte("Cache cleared — browsers will hard-reload with fresh resources."))
}
