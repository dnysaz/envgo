package hotreload

import (
	"context"
	"fmt"
	"io/fs"
	"net/http"
	"path/filepath"
	"strings"
	"sync"
	"time"
)

// watchedExts are source files whose modification triggers a browser reload.
var watchedExts = map[string]bool{
	".html": true, ".htm": true, ".css": true, ".js": true, ".php": true,
}

// ReloadScript is the client-side snippet injected into HTML in dev mode. It
// opens a Server-Sent Events stream at /__envgo/reload and reloads the page
// when the server broadcasts a change.
const ReloadScript = `<script>if(!window.__envgoHMR){window.__envgoHMR=1;var e=new EventSource("/__envgo/reload");e.onmessage=function(t){var d=t.data;if(d==="hardreload"){location.reload(true)}else if(d==="reload"){location.reload()}};e.onerror=function(){}}</script>`

// Broadcaster holds the SSE subscribers and fans out reload/hardreload events
// produced by the file watcher or the `envgo cache clear` command.
type Broadcaster struct {
	mu          sync.Mutex
	subscribers map[chan string]struct{}
	notify      chan string
	quit        chan struct{}
	ran         bool
}

// New creates a Broadcaster. The background fan-out goroutine starts on the
// first Subscribe call (so an unused Broadcaster is cheap).
func New() *Broadcaster {
	return &Broadcaster{
		subscribers: make(map[chan string]struct{}),
		notify:      make(chan string, 16),
		quit:        make(chan struct{}),
	}
}

// Reload broadcasts a soft reload to every connected browser.
func (b *Broadcaster) Reload() { b.signal("reload") }

// HardReload broadcasts a hard reload (cache bypass) to every connected browser.
func (b *Broadcaster) HardReload() { b.signal("hardreload") }

func (b *Broadcaster) signal(msg string) {
	select {
	case b.notify <- msg:
	default:
	}
}

// Subscribe returns a channel that receives broadcast messages. The caller
// must eventually Unsubscribe (or drain until close) to release the slot.
func (b *Broadcaster) Subscribe() chan string {
	ch := make(chan string, 8)
	b.mu.Lock()
	b.subscribers[ch] = struct{}{}
	managed := b.ran
	b.mu.Unlock()
	if !managed {
		b.mu.Lock()
		if !b.ran {
			b.ran = true
			go b.run()
		}
		managed = true
		b.mu.Unlock()
	}
	_ = managed
	return ch
}

// Unsubscribe removes a previously returned channel from the fan-out set.
func (b *Broadcaster) Unsubscribe(ch chan string) {
	b.mu.Lock()
	delete(b.subscribers, ch)
	b.mu.Unlock()
}

func (b *Broadcaster) run() {
	for {
		select {
		case msg := <-b.notify:
			b.mu.Lock()
			for ch := range b.subscribers {
				select {
				case ch <- msg:
				default:
				}
			}
			b.mu.Unlock()
		case <-b.quit:
			return
		}
	}
}

// Close stops the background goroutine. Safe to call; further signals are no-ops.
func (b *Broadcaster) Close() {
	select {
	case <-b.quit:
	default:
		close(b.quit)
	}
}

// ServeSSE serves the Server-Sent Events endpoint at /__envgo/reload.
func (b *Broadcaster) ServeSSE(w http.ResponseWriter, r *http.Request) {
	flusher, ok := w.(http.Flusher)
	if !ok {
		http.Error(w, "streaming not supported", http.StatusNotAcceptable)
		return
	}
	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("Connection", "keep-alive")
	w.WriteHeader(http.StatusOK)
	flusher.Flush()

	ch := b.Subscribe()
	defer b.Unsubscribe(ch)

	ticker := time.NewTicker(15 * time.Second)
	defer ticker.Stop()

	for {
		select {
		case msg, ok := <-ch:
			if !ok || r.Context().Err() != nil {
				return
			}
			fmt.Fprintf(w, "data: %s\n\n", msg)
			flusher.Flush()
		case <-ticker.C:
			// Comment-only heartbeat keeps proxies/ASGI servers from buffering.
			fmt.Fprintf(w, ": %s\n\n", "ping")
			flusher.Flush()
		case <-r.Context().Done():
			return
		}
	}
}

// WatchDir polls dir for changes to watched source files and broadcasts a
// soft reload when something changes. It returns when ctx is cancelled.
func (b *Broadcaster) WatchDir(ctx context.Context, dir string, interval time.Duration) {
	if interval <= 0 {
		interval = 500 * time.Millisecond
	}
	ticker := time.NewTicker(interval)
	defer ticker.Stop()
	var prev map[string]sig
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			cur, changed := scan(dir, prev)
			if changed {
				b.Reload()
			}
			prev = cur
		}
	}
}

type sig struct {
	size  int64
	mtime time.Time
}

// scan walks dir and returns the current signatures plus whether anything
// changed since prev. The first scan (prev == nil) establishes a baseline and
// never reports a change.
func scan(dir string, prev map[string]sig) (map[string]sig, bool) {
	cur := make(map[string]sig)
	_ = filepath.WalkDir(dir, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return nil
		}
		if d.IsDir() {
			if path != dir && strings.HasPrefix(d.Name(), ".") {
				return filepath.SkipDir
			}
			return nil
		}
		if !watchedExts[strings.ToLower(filepath.Ext(path))] {
			return nil
		}
		info, err := d.Info()
		if err != nil {
			return nil
		}
		cur[path] = sig{size: info.Size(), mtime: info.ModTime()}
		return nil
	})
	if prev == nil {
		return cur, false
	}
	if len(cur) != len(prev) {
		return cur, true
	}
	for p, s := range cur {
		old, ok := prev[p]
		if !ok || old != s {
			return cur, true
		}
	}
	return cur, false
}
