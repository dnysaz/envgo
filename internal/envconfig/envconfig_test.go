package envconfig

import (
	"os"
	"path/filepath"
	"testing"
)

func TestLoadBasic(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, ".env")
	content := "# comment\nFOO=bar\nBAZ =  hello  \nQUOTED=\"a b\"\nSINGLE='c d'\nEMPTY=\n"
	if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
		t.Fatal(err)
	}
	vars, err := Load(path)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	want := map[string]string{
		"FOO":    "bar",
		"BAZ":    "hello",
		"QUOTED": "a b",
		"SINGLE": "c d",
		"EMPTY":  "",
	}
	for k, v := range want {
		if vars[k] != v {
			t.Errorf("vars[%q] = %q, want %q", k, vars[k], v)
		}
	}
}

func TestLoadExportAndExpand(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, ".env")
	content := "export BASE=https://api.example.com\nURL=${BASE}/v1\n"
	if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
		t.Fatal(err)
	}
	vars, err := Load(path)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if vars["BASE"] != "https://api.example.com" {
		t.Errorf("BASE = %q", vars["BASE"])
	}
	if vars["URL"] != "https://api.example.com/v1" {
		t.Errorf("URL = %q, want expansion", vars["URL"])
	}
}

func TestLoadMissingFile(t *testing.T) {
	if _, err := Load(filepath.Join(t.TempDir(), "nope.env")); err == nil {
		t.Fatal("expected error for missing file")
	}
}

func TestLoadMalformedLine(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, ".env")
	if err := os.WriteFile(path, []byte("NOEQUALS\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := Load(path); err == nil {
		t.Fatal("expected error for malformed line")
	}
}
