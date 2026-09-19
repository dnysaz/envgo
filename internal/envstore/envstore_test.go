package envstore

import (
	"context"
	"os"
	"path/filepath"
	"testing"
	"time"

	"envbridge/internal/logger"
)

func write(t *testing.T, path, content string, mod time.Time) {
	t.Helper()
	if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.Chtimes(path, mod, mod); err != nil {
		t.Fatal(err)
	}
}

func TestNewAndGet(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, ".env")
	write(t, path, "A=1\nB=2\n", time.Now())

	s, err := New(path, logger.New(false))
	if err != nil {
		t.Fatal(err)
	}
	if s.Len() != 2 {
		t.Fatalf("Len = %d, want 2", s.Len())
	}
	if v, ok := s.Get("A"); !ok || v != "1" {
		t.Fatalf("Get(A) = %q, %v", v, ok)
	}
	if _, ok := s.Get("MISSING"); ok {
		t.Fatal("MISSING should not exist")
	}
}

func TestHotReload(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, ".env")
	base := time.Now()
	write(t, path, "A=old\n", base)

	s, err := New(path, logger.New(false))
	if err != nil {
		t.Fatal(err)
	}

	// touch same mtime -> no change detected
	if changed, _ := s.Reload(); changed {
		t.Fatal("expected no change for identical file")
	}

	write(t, path, "A=new\n", base.Add(2*time.Second))
	changed, err := s.Reload()
	if err != nil {
		t.Fatal(err)
	}
	if !changed {
		t.Fatal("expected change to be detected")
	}
	if v, _ := s.Get("A"); v != "new" {
		t.Fatalf("A = %q, want new", v)
	}
}

func TestMissingFileThenAppears(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, ".env")

	s, err := New(path, logger.New(false))
	if err != nil {
		t.Fatalf("New on missing file: %v", err)
	}
	if s.Len() != 0 {
		t.Fatalf("Len = %d, want 0", s.Len())
	}

	write(t, path, "LATE=yes\n", time.Now())
	changed, err := s.Reload()
	if err != nil {
		t.Fatal(err)
	}
	if !changed {
		t.Fatal("expected new file to be picked up")
	}
	if v, _ := s.Get("LATE"); v != "yes" {
		t.Fatalf("LATE = %q", v)
	}
}

func TestNamesSorted(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, ".env")
	write(t, path, "Z=1\nA=2\nM=3\n", time.Now())
	s, _ := New(path, logger.New(false))
	names := s.Names()
	if len(names) != 3 || names[0] != "A" || names[1] != "M" || names[2] != "Z" {
		t.Fatalf("Names = %v", names)
	}
}

func TestWatch(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, ".env")
	write(t, path, "A=1\n", time.Now())
	s, _ := New(path, logger.New(false))

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	go s.Watch(ctx, 20*time.Millisecond)

	write(t, path, "A=2\n", time.Now().Add(2*time.Second))
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		if v, _ := s.Get("A"); v == "2" {
			return
		}
		time.Sleep(20 * time.Millisecond)
	}
	t.Fatal("watcher did not pick up the change")
}
