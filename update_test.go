package main

import "testing"

func TestPlatformAsset(t *testing.T) {
	cases := []struct {
		osName, arch, want string
	}{
		{"darwin", "amd64", "envgo-darwin-amd64"},
		{"darwin", "arm64", "envgo-darwin-arm64"},
		{"linux", "amd64", "envgo-linux-amd64"},
		{"linux", "arm64", "envgo-linux-arm64"},
		{"windows", "amd64", "envgo-windows-amd64.exe"},
		{"windows", "arm64", "envgo-windows-arm64.exe"},
	}
	for _, c := range cases {
		got := platformAsset(c.osName, c.arch)
		if got != c.want {
			t.Errorf("platformAsset(%q,%q) = %q, want %q", c.osName, c.arch, got, c.want)
		}
	}
}

func TestMatchChecksum(t *testing.T) {
	sums := `77f7fb2ba56cc45ce547c087925cd157699d8feb5734800e2dcf7084c252c6fc  dist/envgo-darwin-amd64
2c4766d502d3be5b0464b044b3052fc7122741574108c9366de0a80b095b60a9  dist/envgo-darwin-arm64
abc123  dist/envgo-linux-arm64
def456 *dist/envgo-windows-amd64.exe
`
	if h := matchChecksum(sums, "envgo-darwin-arm64"); h != "2c4766d502d3be5b0464b044b3052fc7122741574108c9366de0a80b095b60a9" {
		t.Fatalf("arm64 hash = %q, want the 2c47... hash", h)
	}
	if h := matchChecksum(sums, "envgo-windows-amd64.exe"); h != "def456" {
		t.Fatalf("windows hash = %q, want def456", h)
	}
	if h := matchChecksum(sums, "envgo-unknown"); h != "" {
		t.Fatalf("expected empty for unknown asset, got %q", h)
	}
}
