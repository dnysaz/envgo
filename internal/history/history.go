package history

import (
	"sync"
	"time"
)

// Entry is one proxied request. It never stores request bodies, header values,
// or secret values — only metadata safe to show on the dashboard.
type Entry struct {
	Time      time.Time `json:"time"`
	Method    string    `json:"method"`
	Host      string    `json:"host"`
	Status    int       `json:"status"`
	Ms        int64     `json:"ms"`
	Variables []string  `json:"variables,omitempty"`
	Error     string    `json:"error,omitempty"`
}

// History is a fixed-size, concurrency-safe ring buffer of recent requests.
type History struct {
	mu      sync.Mutex
	entries []Entry
	max     int
}

func New(max int) *History {
	if max <= 0 {
		max = 200
	}
	return &History{max: max}
}

func (h *History) Add(e Entry) {
	h.mu.Lock()
	defer h.mu.Unlock()
	h.entries = append(h.entries, e)
	if len(h.entries) > h.max {
		h.entries = h.entries[len(h.entries)-h.max:]
	}
}

// List returns entries newest-first.
func (h *History) List() []Entry {
	h.mu.Lock()
	defer h.mu.Unlock()
	out := make([]Entry, len(h.entries))
	for i, e := range h.entries {
		out[len(h.entries)-1-i] = e
	}
	return out
}
