package main

import (
	"bufio"
	"bytes"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"strconv"
	"strings"
	"time"
)

// doCacheClear signals a running dev server to drop its caches and broadcast a
// hard-reload to every connected browser, so freshly saved files are fetched
// uncached. It is a dev-only convenience; in production there is no dev server
// to talk to.
func doCacheClear(envPath string) {
	host, port := devServerAddr(envPath)
	target := fmt.Sprintf("http://%s/__envgo/admin/clear-cache", url.PathEscape(host))
	if port > 0 {
		target = fmt.Sprintf("http://%s:%d/__envgo/admin/clear-cache", host, port)
	}
	client := &http.Client{Timeout: 5 * time.Second}
	req, err := http.NewRequest(http.MethodPost, target, nil)
	if err != nil {
		fmt.Fprintf(os.Stderr, "envgo: cannot build request: %v\n", err)
		os.Exit(1)
	}
	req.Header.Set("User-Agent", "envgo/"+version)
	res, err := client.Do(req)
	if err != nil {
		fmt.Fprintf(os.Stderr, "envgo: dev server not reachable at %s\n", target)
		fmt.Fprintln(os.Stderr, "hint: start a dev server first with 'envgo run dev'")
		os.Exit(1)
	}
	defer res.Body.Close()
	body, _ := io.ReadAll(res.Body)
	if res.StatusCode != http.StatusOK {
		fmt.Fprintf(os.Stderr, "envgo: cache clear failed (HTTP %s): %s\n", res.Status, strings.TrimSpace(string(body)))
		os.Exit(1)
	}
	fmt.Printf("envgo: %s\n", strings.TrimSpace(string(body)))
}

// devServerAddr reads HOST/PORT from the .env file (default 127.0.0.1:8080).
// Only non-empty, parseable values are used.
func devServerAddr(envPath string) (string, int) {
	host := "127.0.0.1"
	port := 8080
	f, err := os.Open(envPath)
	if err == nil {
		defer f.Close()
		scanner := bufio.NewScanner(f)
		for scanner.Scan() {
			line := bytes.TrimSpace(scanner.Bytes())
			if len(line) == 0 || bytes.HasPrefix(line, []byte("#")) {
				continue
			}
			idx := bytes.IndexByte(line, '=')
			if idx <= 0 {
				continue
			}
			k := string(bytes.TrimSpace(line[:idx]))
			v := string(bytes.TrimSpace(line[idx+1:]))
			switch k {
			case "HOST", "host":
				if v != "" {
					host = v
				}
			case "PORT", "port":
				if p, err := strconv.Atoi(v); err == nil && p > 0 {
					port = p
				}
			}
		}
	}
	return host, port
}
