package main

import (
	"bufio"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
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
	githubAPI  = "https://api.github.com/repos/" + githubRepo + "/releases/latest"
)

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

func doUpdate() {
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
		fallbackDownload(client)
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

	// Best-effort checksum verification against the release SHA256SUMS asset.
	if !verifyChecksum(tmp, meta, asset) {
		fmt.Println("  (checksum not verified: no SHA256SUMS asset found — relying on startup check)")
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
// if the release has no matching asset.
func findAssetURL(rel *ghRelease, want string) string {
	for _, a := range rel.Assets {
		if a.Name == want {
			return a.BrowserDownloadURL
		}
	}
	return ""
}

// verifyChecksum checks the downloaded file against the SHA256SUMS asset in the
// release when present. It returns false (and skips) when no such asset exists.
func verifyChecksum(path string, rel *ghRelease, asset string) bool {
	sumURL := findAssetURL(rel, "SHA256SUMS")
	if sumURL == "" {
		sumURL = fmt.Sprintf("https://github.com/%s/releases/latest/download/SHA256SUMS", githubRepo)
	}
	client := &http.Client{Timeout: 30 * time.Second}
	req, _ := http.NewRequest(http.MethodGet, sumURL, nil)
	req.Header.Set("User-Agent", "envgo/"+version)
	res, err := client.Do(req)
	if err != nil {
		return false
	}
	defer res.Body.Close()
	if res.StatusCode != http.StatusOK {
		return false
	}
	got, err := sha256File(path)
	if err != nil {
		return false
	}
	body, err := io.ReadAll(res.Body)
	if err != nil {
		return false
	}
	want := matchChecksum(string(body), asset)
	if want == "" {
		return false
	}
	if got != want {
		fmt.Fprintf(os.Stderr, "envgo: checksum mismatch for %s (got %s, want %s)\n", asset, got, want)
		os.Exit(1)
	}
	fmt.Printf("  checksum OK (%s)\n", asset)
	return true
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
func fallbackDownload(client *http.Client) {
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
