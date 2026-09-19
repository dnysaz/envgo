package ratelimit

import (
	"fmt"
	"strconv"
	"strings"
	"sync"
	"time"
)

// Limiter is a fixed-window rate limiter keyed by an arbitrary string
// (e.g. route name + client IP). In-memory only, which fits the single-binary
// deployment model.
type Limiter struct {
	mu      sync.Mutex
	windows map[string]*window
}

type window struct {
	count int
	reset time.Time
}

func New() *Limiter {
	return &Limiter{windows: make(map[string]*window)}
}

// Allow reports whether one more request is permitted for key. A limit <= 0
// means unlimited.
func (l *Limiter) Allow(key string, limit int, period time.Duration) bool {
	if limit <= 0 || period <= 0 {
		return true
	}
	now := time.Now()
	l.mu.Lock()
	defer l.mu.Unlock()

	if len(l.windows) > 10000 {
		l.gcLocked(now)
	}

	w := l.windows[key]
	if w == nil || now.After(w.reset) {
		l.windows[key] = &window{count: 1, reset: now.Add(period)}
		return true
	}
	if w.count >= limit {
		return false
	}
	w.count++
	return true
}

func (l *Limiter) gcLocked(now time.Time) {
	for k, w := range l.windows {
		if now.After(w.reset) {
			delete(l.windows, k)
		}
	}
}

// Parse converts "20/min", "100/hour", "5/sec" or "20 per min" into a limit
// and a period. An empty or "0" spec means unlimited.
func Parse(spec string) (int, time.Duration, error) {
	s := strings.ToLower(strings.TrimSpace(spec))
	if s == "" || s == "0" {
		return 0, 0, nil
	}
	s = strings.ReplaceAll(s, " per ", "/")
	parts := strings.Split(s, "/")
	if len(parts) != 2 {
		return 0, 0, fmt.Errorf("invalid rate limit %q (expected e.g. 20/min)", spec)
	}
	limit, err := strconv.Atoi(strings.TrimSpace(parts[0]))
	if err != nil || limit < 0 {
		return 0, 0, fmt.Errorf("invalid rate limit number in %q", spec)
	}
	var period time.Duration
	switch strings.TrimSpace(parts[1]) {
	case "s", "sec", "second", "seconds":
		period = time.Second
	case "m", "min", "minute", "minutes":
		period = time.Minute
	case "h", "hour", "hours":
		period = time.Hour
	default:
		return 0, 0, fmt.Errorf("invalid rate limit unit in %q", spec)
	}
	return limit, period, nil
}
