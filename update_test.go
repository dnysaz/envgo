package main

import (
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"
)

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

// TestMatchChecksumBareFilename covers the "release checksum self-check" layout,
// where the manifest records bare names rather than dist/-prefixed paths.
func TestMatchChecksumBareFilename(t *testing.T) {
	sums := "aaaabbbb  envgo-darwin-arm64\n"
	if h := matchChecksum(sums, "envgo-darwin-arm64"); h != "aaaabbbb" {
		t.Fatalf("bare filename hash = %q, want aaaabbbb", h)
	}
}

// TestMatchChecksumIgnoresMalformedLines makes sure a truncated or blank line
// cannot be mistaken for a manifest entry.
func TestMatchChecksumIgnoresMalformedLines(t *testing.T) {
	sums := "\n   \nonlyonefield\ndeadbeef\n"
	if h := matchChecksum(sums, "envgo-darwin-arm64"); h != "" {
		t.Fatalf("malformed manifest matched %q, want no match", h)
	}
}

// writeAsset creates a temp file with the given contents and returns its path.
func writeAsset(t *testing.T, body string) string {
	t.Helper()
	p := filepath.Join(t.TempDir(), "envgo-darwin-arm64")
	if err := os.WriteFile(p, []byte(body), 0o644); err != nil {
		t.Fatalf("write asset: %v", err)
	}
	return p
}

// manifestSrv serves a SHA256SUMS body and returns a release pointing at it.
func manifestSrv(t *testing.T, body string) (*ghRelease, *http.Client) {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/SHA256SUMS" {
			fmt.Fprint(w, body)
			return
		}
		http.NotFound(w, r)
	}))
	t.Cleanup(srv.Close)
	rel := &ghRelease{Assets: []ghAsset{{
		Name:               "SHA256SUMS",
		BrowserDownloadURL: srv.URL + "/SHA256SUMS",
	}}}
	return rel, srv.Client()
}

// The happy path: the manifest lists the asset and the digests agree.
func TestVerifyChecksumAcceptsMatchingDigest(t *testing.T) {
	const body = "the real binary"
	path := writeAsset(t, body)
	got, err := sha256File(path)
	if err != nil {
		t.Fatalf("hash: %v", err)
	}
	rel, client := manifestSrv(t, fmt.Sprintf("%s  dist/envgo-darwin-arm64\n", got))

	if err := verifyChecksum(client, path, rel, "envgo-darwin-arm64"); err != nil {
		t.Fatalf("expected the download to verify, got %v", err)
	}
}

// A digest that disagrees with the manifest must be reported as a mismatch, and
// must never be downgraded to "unverified".
func TestVerifyChecksumDetectsMismatch(t *testing.T) {
	path := writeAsset(t, "tampered binary")
	rel, client := manifestSrv(t, "0000000000000000000000000000000000000000000000000000000000000000  dist/envgo-darwin-arm64\n")

	err := verifyChecksum(client, path, rel, "envgo-darwin-arm64")
	if !errors.Is(err, errChecksumMismatch) {
		t.Fatalf("want errChecksumMismatch, got %v", err)
	}
	if errors.Is(err, errChecksumUnverified) {
		t.Fatal("a mismatch must not be reported as merely unverified")
	}
}

// A manifest that does not list the asset is unverifiable, not a mismatch.
func TestVerifyChecksumDetectsUnlistedAsset(t *testing.T) {
	path := writeAsset(t, "whatever")
	rel, client := manifestSrv(t, "aaaabbbb  dist/envgo-darwin-amd64\n")

	err := verifyChecksum(client, path, rel, "envgo-darwin-arm64")
	if !errors.Is(err, errChecksumUnverified) {
		t.Fatalf("want errChecksumUnverified for an unlisted asset, got %v", err)
	}
	if errors.Is(err, errChecksumMismatch) {
		t.Fatal("an unlisted asset is not a mismatch")
	}
}

// An unreachable manifest must be unverifiable, not a silent pass.
func TestVerifyChecksumHandlesUnreachableManifest(t *testing.T) {
	path := writeAsset(t, "whatever")
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, "gone", http.StatusNotFound)
	}))
	rel := &ghRelease{Assets: []ghAsset{{
		Name:               "SHA256SUMS",
		BrowserDownloadURL: srv.URL + "/SHA256SUMS",
	}}}

	err := verifyChecksum(srv.Client(), path, rel, "envgo-darwin-arm64")
	srv.Close()
	if !errors.Is(err, errChecksumUnverified) {
		t.Fatalf("want errChecksumUnverified on HTTP 404, got %v", err)
	}
}

// A dead host must be unverifiable rather than an error the updater ignores.
func TestVerifyChecksumHandlesDeadHost(t *testing.T) {
	path := writeAsset(t, "whatever")
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {}))
	url := srv.URL + "/SHA256SUMS"
	srv.Close() // nothing is listening now

	rel := &ghRelease{Assets: []ghAsset{{Name: "SHA256SUMS", BrowserDownloadURL: url}}}
	err := verifyChecksum(&http.Client{}, path, rel, "envgo-darwin-arm64")
	if !errors.Is(err, errChecksumUnverified) {
		t.Fatalf("want errChecksumUnverified on a dead host, got %v", err)
	}
}

// A release that carries no manifest at all must not read as "verified": the
// updater falls back to the /releases/latest/download URL, and whatever that
// yields has to be proven or the install is blocked.
func TestVerifyChecksumFallsBackToLatestDownloadURL(t *testing.T) {
	path := writeAsset(t, "whatever")
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		fmt.Fprint(w, "aaaabbbb  dist/envgo-darwin-arm64\n")
	}))
	defer srv.Close()

	orig := latestManifestURL
	latestManifestURL = func() string { return srv.URL + "/SHA256SUMS" }
	defer func() { latestManifestURL = orig }()

	rel := &ghRelease{} // no assets: verifyChecksum must use the fallback URL
	err := verifyChecksum(srv.Client(), path, rel, "envgo-darwin-arm64")
	if err == nil {
		t.Fatal("a digest that does not match the fallback manifest must not report success")
	}
	if !errors.Is(err, errChecksumMismatch) {
		t.Fatalf("want errChecksumMismatch, got %v", err)
	}
}

// The fallback URL is used only when the release metadata has no manifest.
func TestVerifyChecksumPrefersReleaseAssetURL(t *testing.T) {
	const body = "the real binary"
	path := writeAsset(t, body)
	got, err := sha256File(path)
	if err != nil {
		t.Fatalf("hash: %v", err)
	}
	rel, client := manifestSrv(t, fmt.Sprintf("%s  dist/envgo-darwin-arm64\n", got))

	// Point the fallback at a host that would fail loudly, so a regression that
	// ignores the release asset URL shows up as an error instead of a pass.
	orig := latestManifestURL
	latestManifestURL = func() string { return "http://127.0.0.1:1/SHA256SUMS" }
	defer func() { latestManifestURL = orig }()

	if err := verifyChecksum(client, path, rel, "envgo-darwin-arm64"); err != nil {
		t.Fatalf("the release asset URL should have been used, got %v", err)
	}
}

// The policy that decides whether an update may continue.
func TestEnforceChecksum(t *testing.T) {
	cases := []struct {
		name      string
		err       error
		skip      bool
		wantAbort bool
	}{
		{"verified continues", nil, false, false},
		{"unverified aborts by default", errChecksumUnverified, false, true},
		{"unverified aborts even when skip is on but flagged mismatch", errChecksumMismatch, true, true},
		{"mismatch aborts", errChecksumMismatch, false, true},
		{"unverified continues with explicit skip", errChecksumUnverified, true, false},
		{"wrapped mismatch stays a mismatch", fmt.Errorf("outer: %w", errChecksumMismatch), true, true},
		{"wrapped unverified stays unverified", fmt.Errorf("outer: %w", errChecksumUnverified), false, true},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			got := enforceChecksum(c.err, c.skip)
			if c.wantAbort && got == nil {
				t.Fatalf("expected the update to be blocked, but it was allowed")
			}
			if !c.wantAbort && got != nil {
				t.Fatalf("expected the update to be allowed, got %v", got)
			}
		})
	}
}

// A mismatch must abort the install even when the user passed --skip-checksum.
func TestVerifyDownloadBlocksMismatchDespiteSkip(t *testing.T) {
	const body = "tampered binary"
	path := writeAsset(t, body)
	rel, client := manifestSrv(t, "0000000000000000000000000000000000000000000000000000000000000000  dist/envgo-darwin-arm64\n")

	if err := verifyDownload(client, path, rel, "envgo-darwin-arm64", true); err == nil {
		t.Fatal("--skip-checksum must not wave through a digest mismatch")
	}
}

// An unverifiable download aborts by default...
func TestVerifyDownloadBlocksUnverifiedByDefault(t *testing.T) {
	path := writeAsset(t, "whatever")
	rel, client := manifestSrv(t, "aaaabbbb  dist/envgo-darwin-amd64\n")

	if err := verifyDownload(client, path, rel, "envgo-darwin-arm64", false); err == nil {
		t.Fatal("an unverified download must not be installed by default")
	}
}

// ...but the documented escape hatch still works for air-gapped mirrors.
func TestVerifyDownloadAllowsSkipForUnverified(t *testing.T) {
	path := writeAsset(t, "whatever")
	rel, client := manifestSrv(t, "aaaabbbb  dist/envgo-darwin-amd64\n")

	if err := verifyDownload(client, path, rel, "envgo-darwin-arm64", true); err != nil {
		t.Fatalf("--skip-checksum should allow an unverifiable download, got %v", err)
	}
}

// A verified download passes the gate.
func TestVerifyDownloadAllowsVerified(t *testing.T) {
	const body = "the real binary"
	path := writeAsset(t, body)
	got, err := sha256File(path)
	if err != nil {
		t.Fatalf("hash: %v", err)
	}
	rel, client := manifestSrv(t, fmt.Sprintf("%s  dist/envgo-darwin-arm64\n", got))

	if err := verifyDownload(client, path, rel, "envgo-darwin-arm64", false); err != nil {
		t.Fatalf("a verified download must pass, got %v", err)
	}
}
