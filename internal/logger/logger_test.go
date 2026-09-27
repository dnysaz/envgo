package logger

import (
	"bytes"
	"log"
	"strings"
	"sync"
	"testing"
)

// newTestLogger builds a logger writing into buffers so output can be asserted
// without touching the process-wide stdout/stderr.
func newTestLogger(debug bool) (*Logger, *bytes.Buffer, *bytes.Buffer) {
	var out, errOut bytes.Buffer
	l := New(debug)
	l.out = log.New(&out, "", 0)
	l.err = log.New(&errOut, "", 0)
	return l, &out, &errOut
}

func TestRegisterSecretIgnoresEmpty(t *testing.T) {
	l, _, _ := newTestLogger(false)
	l.RegisterSecret("")
	if got := l.Redact("nothing to redact"); got != "nothing to redact" {
		t.Fatalf("empty secret must not be registered, got %q", got)
	}
}

func TestRegisterSecretsFrom(t *testing.T) {
	l, _, _ := newTestLogger(false)
	l.RegisterSecretsFrom(map[string]string{
		"EMPTY": "",
		"A":     "sk-alpha",
		"B":     "sk-bravo",
	})
	got := l.Redact("a=sk-alpha b=sk-bravo")
	if strings.Contains(got, "sk-alpha") || strings.Contains(got, "sk-bravo") {
		t.Fatalf("secrets leaked: %q", got)
	}
	if strings.Count(got, "[REDACTED]") != 2 {
		t.Fatalf("expected 2 redactions, got %q", got)
	}
}

// TestRedactLongestFirst pins the ordering rule: when one registered secret is a
// prefix of another, the longer one has to win or the shorter replacement would
// slice the longer secret in half and leak its tail.
func TestRedactLongestFirst(t *testing.T) {
	l, _, _ := newTestLogger(false)
	// Deliberately register the shorter value first.
	l.RegisterSecret("sk-123")
	l.RegisterSecret("sk-1234")

	got := l.Redact("token=sk-1234 end")
	if strings.Contains(got, "1234") {
		t.Fatalf("longer secret was partially leaked: %q", got)
	}
	if got != "token=[REDACTED] end" {
		t.Fatalf("unexpected redaction: %q", got)
	}
}

func TestRedactIsRepeatableAndIdempotent(t *testing.T) {
	l, _, _ := newTestLogger(false)
	l.RegisterSecret("hunter2")
	first := l.Redact("pw=hunter2")
	second := l.Redact("pw=hunter2")
	if first != second {
		t.Fatalf("Redact is not repeatable: %q vs %q", first, second)
	}
	// Redaction must not corrupt the registered secret list.
	if got := l.Redact("pw=hunter2"); got != "pw=[REDACTED]" {
		t.Fatalf("unexpected redaction on repeat: %q", got)
	}
}

func TestLevelsUseTheRightStream(t *testing.T) {
	l, out, errOut := newTestLogger(false)
	l.Info("info %s", "line")
	l.Warn("warn %s", "line")
	l.Error("err %s", "line")
	l.Debug("debug %s", "line")

	if !strings.Contains(out.String(), "[envGo] info line") {
		t.Errorf("stdout missing info line: %q", out.String())
	}
	if !strings.Contains(out.String(), "[envGo] WARN warn line") {
		t.Errorf("stdout missing warn line: %q", out.String())
	}
	if !strings.Contains(errOut.String(), "[envGo] ERROR err line") {
		t.Errorf("stderr missing error line: %q", errOut.String())
	}
	if strings.Contains(out.String(), "debug") || strings.Contains(errOut.String(), "debug") {
		t.Error("debug output must be suppressed unless debug is enabled")
	}
}

func TestDebugEnabled(t *testing.T) {
	l, out, _ := newTestLogger(true)
	l.Debug("dbg %d", 7)
	if !strings.Contains(out.String(), "[envGo] DEBUG dbg 7") {
		t.Errorf("stdout missing debug line: %q", out.String())
	}
}

func TestLevelsRedactSecrets(t *testing.T) {
	l, out, errOut := newTestLogger(false)
	l.RegisterSecret("sk-topsecret")
	l.Info("using %s", "sk-topsecret")
	l.Warn("using %s", "sk-topsecret")
	l.Error("using %s", "sk-topsecret")
	l.Debug("using %s", "sk-topsecret")

	for name, buf := range map[string]*bytes.Buffer{"stdout": out, "stderr": errOut} {
		if strings.Contains(buf.String(), "sk-topsecret") {
			t.Errorf("%s leaked a secret: %q", name, buf.String())
		}
	}
}

func TestConcurrentUseIsSafe(t *testing.T) {
	l, _, _ := newTestLogger(false)
	const workers = 16
	var wg sync.WaitGroup
	for i := 0; i < workers; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			l.RegisterSecret("sk-secret-value")
			for j := 0; j < 50; j++ {
				if got := l.Redact("key=sk-secret-value"); strings.Contains(got, "sk-secret-value") {
					t.Errorf("secret leaked under concurrency: %q", got)
					return
				}
			}
		}(i)
	}
	wg.Wait()
}
