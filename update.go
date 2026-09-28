package main

import (
	"bufio"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"time"
)

const (
	githubRepo = "dnysaz/envgo"
)

// githubAPI is a variable so tests can point it at a stub. githubMeta returning
// nil is what sends doUpdate down the fallback path, so its failure modes are
// worth covering directly.
var githubAPI = "https://api.github.com/repos/" + githubRepo + "/releases/latest"

type ghRelease struct {
	TagName string    `json:"tag_name"`
	Assets  []ghAsset `json:"assets"`
}

type ghAsset struct {
	Name               string `json:"name"`
	BrowserDownloadURL string `json:"browser_download_url"`
}

// platformAsset returns the release asset name for the given OS/arch pair,
// matching the convention used in dist/ and scripts/install.sh:
//
//	envgo-<os>-<arch>      (darwin, linux)
//	envgo-<os>-<arch>.exe  (windows)
func platformAsset(osName, archName string) string {
	name := "envgo-" + osName + "-" + archName
	if osName == "windows" {
		name += ".exe"
	}
	return name
}

// assetName returns the release asset name for the current platform.
func assetName() string {
	return platformAsset(runtime.GOOS, runtime.GOARCH)
}

func doUpdate(skip bool) {
	fmt.Print("Update envgo to latest version? Y/n ")
	ask := bufio.NewReader(os.Stdin)
	line, err := ask.ReadString('\n')
	if err != nil && err != io.EOF {
		fmt.Fprintf(os.Stderr, "envgo: cannot read input: %v\n", err)
		os.Exit(1)
	}
	ans := strings.ToLower(strings.TrimSpace(line))
	if ans != "" && ans != "y" && ans != "yes" {
		fmt.Println("Update cancelled.")
		return
	}

	client := &http.Client{
		Timeout: 30 * time.Second,
		CheckRedirect: func(req *http.Request, via []*http.Request) error {
			return nil
		},
	}
	meta := githubMeta(client)
	if meta == nil {
		// githubMeta printed an error; fall back to a direct download.
		fallbackDownload(client, skip)
		return
	}
	if meta.TagName == "" {
		fmt.Fprintln(os.Stderr, "envgo: GitHub release has no tag name")
		os.Exit(1)
	}
	if meta.TagName == "v"+version || meta.TagName == version {
		fmt.Printf("envgo %s is already the latest version (%s).\n", version, meta.TagName)
		return
	}
	fmt.Printf("Latest: %s (current: v%s)\n", meta.TagName, version)

	asset := assetName()
	url := findAssetURL(meta, asset)
	if url == "" {
		// Fall back to GitHub's /latest/download redirect.
		url = fmt.Sprintf("https://github.com/%s/releases/latest/download/%s", githubRepo, asset)
	}
	fmt.Printf("Downloading %s ...\n", asset)

	target, err := os.Executable()
	if err != nil {
		fmt.Fprintf(os.Stderr, "envgo: cannot locate running executable: %v\n", err)
		os.Exit(1)
	}
	if target, err = filepath.Abs(target); err != nil {
		fmt.Fprintf(os.Stderr, "envgo: cannot resolve executable path: %v\n", err)
		os.Exit(1)
	}
	tmp := filepath.Join(filepath.Dir(target), ".envgo.update."+asset)
	if err := downloadToFile(client, url, tmp); err != nil {
		os.Remove(tmp)
		fmt.Fprintf(os.Stderr, "envgo: download failed: %v\n", err)
		os.Exit(1)
	}
	defer os.Remove(tmp)

	if runtime.GOOS != "windows" {
		if err := os.Chmod(tmp, 0o755); err != nil {
			fmt.Fprintf(os.Stderr, "envgo: chmod failed: %v\n", err)
			os.Exit(1)
		}
	}

	// Integrity gate: refuse to install anything we cannot prove is the
	// released binary. --skip-checksum is the only way past an unverifiable
	// manifest, and it never covers an actual digest mismatch.
	if err := verifyDownload(client, tmp, meta, asset, skip); err != nil {
		return
	}

	// Startup self-check: ensure the downloaded binary is valid.
	if err := exec.Command(tmp, "-h").Run(); err != nil {
		fmt.Fprintf(os.Stderr, "envgo: downloaded binary failed its startup check (-h): %v\n", err)
		os.Exit(1)
	}

	if err := installBinary(tmp, target); err != nil {
		fmt.Fprintf(os.Stderr, "envgo: install failed: %v\n", err)
		fmt.Fprintf(os.Stderr, "envgo: the new binary is at %s — replace %s manually after exit.\n", tmp, target)
		os.Exit(1)
	}
	fmt.Println("Congrats to new version of envgo!")
}

// githubMeta queries the GitHub latest-release API and returns the parsed
// release. It returns nil (and prints an error) if the API cannot be reached,
// so the caller can fall back to a direct download path.
func githubMeta(client *http.Client) *ghRelease {
	req, err := http.NewRequest(http.MethodGet, githubAPI, nil)
	if err != nil {
		fmt.Fprintf(os.Stderr, "envgo: cannot build request: %v\n", err)
		return nil
	}
	req.Header.Set("User-Agent", "envgo/"+version)
	req.Header.Set("Accept", "application/vnd.github+json")
	res, err := client.Do(req)
	if err != nil {
		fmt.Fprintf(os.Stderr, "envgo: cannot reach GitHub: %v\n", err)
		return nil
	}
	defer res.Body.Close()
	if res.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(res.Body)
		fmt.Fprintf(os.Stderr, "envgo: GitHub API %s: %s\n", res.Status, strings.TrimSpace(string(body)))
		return nil
	}
	var rel ghRelease
	if err := json.NewDecoder(res.Body).Decode(&rel); err != nil {
		fmt.Fprintf(os.Stderr, "envgo: cannot parse GitHub response: %v\n", err)
		return nil
	}
	return &rel
}

// findAssetURL returns the browser_download_url for the platform asset, or ""
// if the release has no matching asset. A nil rel is treated as "no metadata",
// which is the case on the API-fallback download path.
func findAssetURL(rel *ghRelease, want string) string {
	if rel == nil {
		return ""
	}
	for _, a := range rel.Assets {
		if a.Name == want {
			return a.BrowserDownloadURL
		}
	}
	return ""
}

// latestManifestURL returns the fallback manifest location, used when the
// release metadata carries no SHA256SUMS asset. It is a function value rather
// than an inline format so tests can point it at a stub instead of reaching
// out to GitHub.
var latestManifestURL = func() string {
	return fmt.Sprintf("https://github.com/%s/releases/latest/download/SHA256SUMS", githubRepo)
}

// verifyChecksum checks the downloaded file against the SHA256SUMS manifest for
// the release. It returns nil only when the digest was proven to match.
//
// Every other outcome is an error, including the ones that are merely
// unverifiable: an unreachable manifest, an unreadable manifest, or a manifest
// that does not list this asset all mean "we cannot prove these are our
// bytes". Callers must treat any error as a hard failure unless the user
// explicitly opted out via --skip-checksum.
func verifyChecksum(client *http.Client, path string, rel *ghRelease, asset string) error {
	sumURL := findAssetURL(rel, "SHA256SUMS")
	if sumURL == "" {
		sumURL = latestManifestURL()
	}
	req, err := http.NewRequest(http.MethodGet, sumURL, nil)
	if err != nil {
		return fmt.Errorf("%w: cannot build manifest request: %v", errChecksumUnverified, err)
	}
	req.Header.Set("User-Agent", "envgo/"+version)
	res, err := client.Do(req)
	if err != nil {
		return fmt.Errorf("%w: cannot fetch SHA256SUMS: %v", errChecksumUnverified, err)
	}
	defer res.Body.Close()
	if res.StatusCode != http.StatusOK {
		return fmt.Errorf("%w: SHA256SUMS returned HTTP %s", errChecksumUnverified, res.Status)
	}
	body, err := io.ReadAll(res.Body)
	if err != nil {
		return fmt.Errorf("%w: cannot read SHA256SUMS: %v", errChecksumUnverified, err)
	}
	want := matchChecksum(string(body), asset)
	if want == "" {
		return fmt.Errorf("%w: SHA256SUMS does not list %s", errChecksumUnverified, asset)
	}
	got, err := sha256File(path)
	if err != nil {
		return fmt.Errorf("%w: cannot hash the download: %v", errChecksumUnverified, err)
	}
	if got != want {
		return fmt.Errorf("%w for %s (got %s, want %s)", errChecksumMismatch, asset, got, want)
	}
	return nil
}

// errChecksumMismatch marks a digest that disagrees with the published
// manifest. It is never bypassable by --skip-checksum: the downloaded bytes
// are not the bytes we published, and only the release author can explain why.
var errChecksumMismatch = errors.New("checksum mismatch")

// errChecksumUnverified marks a download we could not prove either way, because
// the manifest was unreachable, unreadable, or did not list the asset.
var errChecksumUnverified = errors.New("checksum could not be verified")

// enforceChecksum decides whether an update may continue after a verification
// attempt. It returns nil when installing is safe, or an error explaining why
// the update must be aborted.
//
// A mismatch always aborts. Unverifiability aborts too, unless the user passed
// --skip-checksum, which exists as the escape hatch for corporate proxies and
// air-gapped mirrors that cannot reach the release manifest.
func enforceChecksum(err error, skip bool) error {
	if err == nil {
		return nil
	}
	if errors.Is(err, errChecksumMismatch) {
		return err
	}
	if !skip {
		return err
	}
	fmt.Fprintln(os.Stderr, "  WARNING: --skip-checksum was set, so this download is UNVERIFIED.")
	fmt.Fprintln(os.Stderr, "  A proxy or mirror may have altered it. Only proceed if you trust your network.")
	return nil
}

// verifyDownload is the single gate both the normal and the API-fallback update
// paths go through, so neither can install an unverified binary by accident.
func verifyDownload(client *http.Client, path string, rel *ghRelease, asset string, skip bool) error {
	if err := enforceChecksum(verifyChecksum(client, path, rel, asset), skip); err != nil {
		if errors.Is(err, errChecksumMismatch) {
			fmt.Fprintf(os.Stderr, "envgo: %v\n", err)
			fmt.Fprintln(os.Stderr, "envgo: refusing to install. Re-run with a clean network, or download and verify manually from the release page.")
		} else {
			fmt.Fprintf(os.Stderr, "envgo: %v\n", err)
			fmt.Fprintln(os.Stderr, "envgo: refusing to install an unverified binary. If your network cannot reach the release manifest, re-run with --skip-checksum.")
		}
		return err
	}
	if !skip {
		fmt.Printf("  checksum OK (%s)\n", asset)
	}
	return nil
}

// matchChecksum finds the sha256 hex whose recorded path ends with `asset`.
func matchChecksum(sums, asset string) string {
	for _, line := range strings.Split(sums, "\n") {
		fields := strings.Fields(line)
		if len(fields) != 2 {
			continue
		}
		path := fields[1]
		// sha256sum prints "<hash>  <name>" or "<hash> *<name>".
		path = strings.TrimPrefix(path, "*")
		if filepath.Base(path) == asset || path == "dist/"+asset {
			return fields[0]
		}
	}
	return ""
}

// fallbackDownload downloads the platform asset straight from GitHub's
// /releases/latest/download redirect when the API is unavailable.
func fallbackDownload(client *http.Client, skip bool) {
	asset := assetName()
	url := fmt.Sprintf("https://github.com/%s/releases/latest/download/%s", githubRepo, asset)
	fmt.Printf("GitHub API unreachable; trying direct download of %s\n", asset)
	target, err := os.Executable()
	if err != nil {
		fmt.Fprintf(os.Stderr, "envgo: cannot locate running executable: %v\n", err)
		os.Exit(1)
	}
	if target, err = filepath.Abs(target); err != nil {
		fmt.Fprintf(os.Stderr, "envgo: cannot resolve executable path: %v\n", err)
		os.Exit(1)
	}
	tmp := filepath.Join(filepath.Dir(target), ".envgo.update."+asset)
	if err := downloadToFile(client, url, tmp); err != nil {
		os.Remove(tmp)
		fmt.Fprintf(os.Stderr, "envgo: download failed: %v\n", err)
		os.Exit(1)
	}
	defer os.Remove(tmp)
	if runtime.GOOS != "windows" {
		_ = os.Chmod(tmp, 0o755)
	}
	// The fallback path used to skip verification entirely. It now goes through
	// the same gate; rel is nil, so verifyChecksum resolves the manifest from
	// the /releases/latest/download redirect.
	if err := verifyDownload(client, tmp, nil, asset, skip); err != nil {
		return
	}
	if err := exec.Command(tmp, "-h").Run(); err != nil {
		fmt.Fprintf(os.Stderr, "envgo: downloaded binary failed its startup check (-h): %v\n", err)
		os.Exit(1)
	}
	if err := installBinary(tmp, target); err != nil {
		fmt.Fprintf(os.Stderr, "envgo: install failed: %v\n", err)
		os.Exit(1)
	}
	fmt.Println("Congrats to new version of envgo!")
}

// downloadToFile streams url into path, printing a compact progress indicator.
func downloadToFile(client *http.Client, url, path string) error {
	req, err := http.NewRequest(http.MethodGet, url, nil)
	if err != nil {
		return err
	}
	req.Header.Set("User-Agent", "envgo/"+version)
	res, err := client.Do(req)
	if err != nil {
		return err
	}
	defer res.Body.Close()
	if res.StatusCode != http.StatusOK {
		return fmt.Errorf("HTTP %s", res.Status)
	}
	out, err := os.Create(path)
	if err != nil {
		return err
	}
	defer out.Close()
	if res.ContentLength > 0 {
		pr := newProgressBar(res.ContentLength)
		if _, err := io.Copy(out, io.TeeReader(res.Body, pr)); err != nil {
			return err
		}
		pr.finish()
		return out.Sync()
	}
	if _, err := io.Copy(out, res.Body); err != nil {
		return err
	}
	return out.Sync()
}

// sha256File computes the hex sha256 of a file (used for checksum verification).
func sha256File(path string) (string, error) {
	f, err := os.Open(path)
	if err != nil {
		return "", err
	}
	defer f.Close()
	h := sha256.New()
	if _, err := io.Copy(h, f); err != nil {
		return "", err
	}
	return hex.EncodeToString(h.Sum(nil)), nil
}

// progressBar prints a compact "downloading N/M bytes (p%)" indicator.
type progressBar struct {
	total   int64
	written int64
	last    time.Time
}

func newProgressBar(total int64) *progressBar {
	return &progressBar{total: total}
}

func (p *progressBar) Write(b []byte) (int, error) {
	n := len(b)
	p.written += int64(n)
	now := time.Now()
	if now.Sub(p.last) >= 200*time.Millisecond || p.written == p.total {
		p.last = now
		fmt.Printf("\r  downloading %d/%d bytes (%.1f%%)", p.written, p.total, pct(p.written, p.total))
		if p.written == p.total {
			fmt.Println()
		}
	}
	return n, nil
}

func (p *progressBar) finish() {
	if p.written == p.total {
		return
	}
	fmt.Println()
}

func pct(n, d int64) float64 {
	if d == 0 {
		return 0
	}
	return float64(n) * 100 / float64(d)
}
