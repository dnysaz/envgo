package hotreload

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func writeFile(t *testing.T, dir, name, content string) {
	t.Helper()
	path := filepath.Join(dir, name)
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
}

func TestScanFirstRunNeverReportsChange(t *testing.T) {
	dir := t.TempDir()
	writeFile(t, dir, "index.html", "x")
	cur, changed := scan(dir, nil)
	if changed {
		t.Fatal("first scan should not report a change")
	}
	if len(cur) != 1 {
		t.Fatalf("expected 1 watched file, got %d", len(cur))
	}
}

func TestScanDetectsModification(t *testing.T) {
	dir := t.TempDir()
	writeFile(t, dir, "app.js", "small")
	prev, _ := scan(dir, nil)
	writeFile(t, dir, "app.js", "much-bigger-content")
	if _, changed := scan(dir, prev); !changed {
		t.Fatal("expected a change after rewriting app.js")
	}
}

func TestScanDetectsAddAndRemove(t *testing.T) {
	dir := t.TempDir()
	writeFile(t, dir, "a.html", "a")
	writeFile(t, dir, "b.css", "b")
	prev, _ := scan(dir, nil)

	// adding a file triggers a reload
	writeFile(t, dir, "c.php", "c")
	if _, changed := scan(dir, prev); !changed {
		t.Fatal("expected change after adding c.php")
	}
	prev, _ = scan(dir, prev)

	// removing a file triggers a reload
	if err := os.Remove(filepath.Join(dir, "c.php")); err != nil {
		t.Fatal(err)
	}
	if _, changed := scan(dir, prev); !changed {
		t.Fatal("expected change after removing c.php")
	}
}

func TestScanIgnoresNonWatched(t *testing.T) {
	dir := t.TempDir()
	writeFile(t, dir, "page.html", "x")
	writeFile(t, dir, "data.json", "{}")
	prev, _ := scan(dir, nil)
	writeFile(t, dir, "data.json", "{changed:true}")
	if _, changed := scan(dir, prev); changed {
		t.Fatal("changes to non-watched files must not trigger a reload")
	}
}

func TestScanIgnoresDotdirs(t *testing.T) {
	dir := t.TempDir()
	writeFile(t, dir, "page.html", "x")
	writeFile(t, dir, ".hidden/secret.html", "x")
	prev, _ := scan(dir, nil)
	if len(prev) != 1 {
		t.Fatalf("expected only 1 watched file (dotdirs skipped), got %d", len(prev))
	}
}

func TestReloadScriptFormat(t *testing.T) {
	if !strings.Contains(ReloadScript, "EventSource") {
		t.Fatal("ReloadScript should open an EventSource")
	}
	if !strings.Contains(ReloadScript, "/__envgo/reload") {
		t.Fatal("ReloadScript should connect to /__envgo/reload")
	}
}
