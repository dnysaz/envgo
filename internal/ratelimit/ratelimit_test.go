package ratelimit

import (
	"testing"
	"time"
)

func TestParse(t *testing.T) {
	cases := map[string]struct {
		limit  int
		period time.Duration
	}{
		"":           {0, 0},
		"20/min":     {20, time.Minute},
		"100/hour":   {100, time.Hour},
		"5/sec":      {5, time.Second},
		"10 per min": {10, time.Minute},
	}
	for spec, want := range cases {
		limit, period, err := Parse(spec)
		if err != nil {
			t.Fatalf("Parse(%q): %v", spec, err)
		}
		if limit != want.limit || period != want.period {
			t.Errorf("Parse(%q) = (%d,%v), want (%d,%v)", spec, limit, period, want.limit, want.period)
		}
	}
	for _, bad := range []string{"20", "x/min", "20/fortnight"} {
		if _, _, err := Parse(bad); err == nil {
			t.Errorf("Parse(%q) expected error", bad)
		}
	}
}

func TestAllow(t *testing.T) {
	l := New()
	for i := 0; i < 3; i++ {
		if !l.Allow("k", 3, time.Minute) {
			t.Fatalf("request %d should be allowed", i+1)
		}
	}
	if l.Allow("k", 3, time.Minute) {
		t.Fatal("4th request should be denied")
	}
	if !l.Allow("other", 3, time.Minute) {
		t.Fatal("different key should be allowed")
	}
}

func TestAllowUnlimited(t *testing.T) {
	l := New()
	for i := 0; i < 100; i++ {
		if !l.Allow("k", 0, 0) {
			t.Fatal("limit 0 must be unlimited")
		}
	}
}

func TestWindowResets(t *testing.T) {
	l := New()
	if !l.Allow("k", 1, 30*time.Millisecond) {
		t.Fatal("first should pass")
	}
	if l.Allow("k", 1, 30*time.Millisecond) {
		t.Fatal("second should be denied")
	}
	time.Sleep(40 * time.Millisecond)
	if !l.Allow("k", 1, 30*time.Millisecond) {
		t.Fatal("after window should pass")
	}
}
