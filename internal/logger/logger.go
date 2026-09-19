package logger

import (
	"fmt"
	"log"
	"os"
	"sort"
	"strings"
	"sync"
)

// Logger writes leveled messages to stdout/stderr while never leaking
// the secret values it knows about. Any occurrence of a secret value in
// an output line is replaced with [REDACTED].
type Logger struct {
	mu      sync.Mutex
	secrets []string
	out     *log.Logger
	err     *log.Logger
	debug   bool
}

func New(debug bool) *Logger {
	return &Logger{
		out:   log.New(os.Stdout, "", 0),
		err:   log.New(os.Stderr, "", 0),
		debug: debug,
	}
}

// RegisterSecret teaches the logger to redact a value.
func (l *Logger) RegisterSecret(value string) {
	if value == "" {
		return
	}
	l.mu.Lock()
	defer l.mu.Unlock()
	l.secrets = append(l.secrets, value)
}

// RegisterSecretsFrom registers every non-empty value from a map.
func (l *Logger) RegisterSecretsFrom(m map[string]string) {
	for _, v := range m {
		l.RegisterSecret(v)
	}
}

// Redact replaces any known secret with [REDACTED].
func (l *Logger) Redact(s string) string {
	l.mu.Lock()
	defer l.mu.Unlock()
	// Longest first so overlapping values are replaced deterministically.
	sorted := make([]string, len(l.secrets))
	copy(sorted, l.secrets)
	sort.Slice(sorted, func(i, j int) bool { return len(sorted[i]) > len(sorted[j]) })
	for _, secret := range sorted {
		if strings.Contains(s, secret) {
			s = strings.ReplaceAll(s, secret, "[REDACTED]")
		}
	}
	return s
}

func (l *Logger) Info(format string, args ...any) {
	l.out.Print("[envGo] " + l.Redact(fmt.Sprintf(format, args...)))
}

func (l *Logger) Warn(format string, args ...any) {
	l.out.Print("[envGo] WARN " + l.Redact(fmt.Sprintf(format, args...)))
}

func (l *Logger) Error(format string, args ...any) {
	l.err.Print("[envGo] ERROR " + l.Redact(fmt.Sprintf(format, args...)))
}

func (l *Logger) Debug(format string, args ...any) {
	if l.debug {
		l.out.Print("[envGo] DEBUG " + l.Redact(fmt.Sprintf(format, args...)))
	}
}
