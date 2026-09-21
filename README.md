# envGo

Zero-dependency micro-runtime (Go, single binary) that lets plain HTML +
Vanilla JS pages use `.env` variables **without ever exposing the key to the
browser**. It serves your static site and proxies API calls, replacing
`{VAR_NAME}` placeholders with real values inside the Go process — invisible
to DevTools, the Network tab, and browser memory.

[![License: MIT](https://img.shields.io/badge/License-MIT-blue.svg)](LICENSE)
[![Latest release](https://img.shields.io/github/v/release/dnysaz/envgo)](https://github.com/dnysaz/envgo/releases/latest)
[![Go version](https://img.shields.io/github/go-mod/go-version/dnysaz/envgo)](go.mod)

**Free and open source under the MIT license.** No paid tier, no licence key, no
account, no telemetry. Download it, use it commercially, modify it, ship it —
see [License](#license).

Full documentation: **https://envgo.dev/**

## Why

- `.env` in a browser leaks secrets (readable via F12 / Network / memory).
- Traditional backends (Express, Django) add huge `node_modules` just to hide a key.
- envGo is a single binary (~2–12 MB). No Node, no host runtime, no dependencies.

## Features

- Static file server + secure proxy in a single binary.
- Hot-reload: edit `.env` and the running process picks it up (no restart).
- Dashboard at `/__envgo_dashboard` — recent requests + variable *names*, never values.
- SSE/streaming passthrough (e.g. OpenAI `stream: true`).
- **Public gateway mode** (`--config`): fixed routes at `/api/<name>` with per-route allowlist, rate limiting, optional bearer auth, and response scrubbing.
- **TLS support** (`--tls`): auto-generated self-signed certificate for HTTPS.
- **PHP support**: execute `.php` files server-side with `.env` vars injected.
- **Typo detection**: red banner if HTML `id` doesn't match `.env` key.
- Cross-platform: macOS, Linux, Windows.

---

## Install

### Prebuilt binaries

Download the archive for your platform from the
[latest release](https://github.com/dnysaz/envgo/releases/latest). Every release
includes a `SHA256SUMS` file you can use to verify the download.

| Platform | File |
|----------|------|
| macOS, Apple Silicon | `envGo-macOS-AppleSilicon.zip` |
| macOS, Intel | `envGo-macOS-Intel.zip` |
| Linux, amd64 | `envgo-linux-amd64` |
| Linux, arm64 | `envgo-linux-arm64` |
| Windows, amd64 | `envgo-windows-amd64.exe` |
| Windows, arm64 | `envgo-windows-arm64.exe` |

The steps below assume you already have the files (for example after running
`make release`). For platform-specific instructions, see the
[documentation](https://envgo.dev/download/).

### macOS

```bash
# Option 1: Manual
cp dist/envgo-darwin-arm64 ~/.local/bin/envgo   # Apple Silicon
cp dist/envgo-darwin-amd64 ~/.local/bin/envgo   # Intel

# Option 2: Double-click Install_envGo.command from the zip
```

### Linux (VPS / Server)

```bash
# AMD64 (most servers)
sudo cp dist/envgo-linux-amd64 /usr/local/bin/envgo
sudo chmod +x /usr/local/bin/envgo

# ARM64 (Raspberry Pi, Graviton, etc.)
sudo cp dist/envgo-linux-arm64 /usr/local/bin/envgo
sudo chmod +x /usr/local/bin/envgo
```

### Windows

```
Copy envgo-windows-amd64.exe to C:\envgo\envgo.exe
Add C:\envgo to PATH
```

### Verify

```bash
envgo -v    # print version
envgo -h    # show help
```

---

## Quick Start

### 1. Create a project

```bash
mkdir myapp && cd myapp
envgo init
```

This creates:
- `.env` — your secrets (MY_SECRET, HOST, PORT)
- `.env.example` — template (safe to commit)
- `index.html` — minimal starter page
- `.gitignore` — ignores `.env`
- `README.md` — project docs

### 2. Edit `.env`

```bash
# Add your real secrets
MY_SECRET=your-real-secret-here
HOST=127.0.0.1
PORT=8080
# MODE_PUBLIC=false  # true/public = public gateway (needs envgo.routes.json)
# CONFIG=envgo.routes.json  # explicit routes file (overrides MODE_PUBLIC)
```

### 3. Run

```bash
envgo run dev
# → http://127.0.0.1:8080/
# Browser opens automatically
```

### 4. Use in HTML

```html
<!DOCTYPE html>
<html>
<head><title>My App</title></head>
<body>
<h1>Hello from envGo!</h1>
<div id="MY_SECRET"></div>
<script src="/__env.js"></script>
</body>
</html>
```

The `<div id="MY_SECRET">` will show "✓ MY_SECRET — Success" if the key
exists in `.env`. The actual value **never** reaches the browser.

---

## Local Mode (Development)

Local mode uses `/proxy` endpoint with a session token. Best for development.

```bash
# Basic
envgo run dev

# With specific port and allowlist
envgo --port 3000 --dir . --env .env --allow api.openai.com,httpbin.org

# Open browser automatically
envgo run dev -b
```

### How it works

1. Browser fetches token: `GET /__envgo_token`
2. Browser sends request to `/proxy` with token:
```js
const token = await fetch("/__envgo_token").then(r => r.text());
const res = await fetch("/proxy", {
  method: "POST",
  headers: {
    "Content-Type": "application/json",
    "X-EnvGo-Token": token,
  },
  body: JSON.stringify({
    target_url: "https://api.openai.com/v1/chat/completions",
    method: "POST",
    headers: { "Authorization": "Bearer {OPENAI_API_KEY}" },
    body: { model: "gpt-4o", messages: [{ role: "user", content: "Hi" }] }
  })
});
```

3. Go server replaces `{OPENAI_API_KEY}` with real value, forwards to API.
4. Response returned to browser. **Secret never exposed.**

### Security

- Session token required for every `/proxy` call
- Host header validation (anti DNS-rebinding)
- Origin validation (anti cross-tab attacks)
- `--allow` restricts which hosts can be proxied

---

## Public Mode (Production Gateway)

Public mode uses fixed routes at `/api/<name>` — no token handshake needed.

### 1. Create route config

Create `envgo.routes.json`:

```json
{
  "trust_proxy": true,
  "default_rate_limit": "30/min",
  "scrub_response": true,
  "routes": [
    {
      "name": "chat",
      "method": "POST",
      "target": "https://api.openai.com/v1/chat/completions",
      "vars": ["OPENAI_API_KEY"],
      "inject": {
        "headers": { "Authorization": "Bearer {OPENAI_API_KEY}" }
      },
      "rate_limit": "20/min"
    },
    {
      "name": "gemini",
      "method": "POST",
      "target": "https://generativelanguage.googleapis.com/v1beta/models/gemini-2.0-flash:generateContent",
      "vars": ["GEMINI_API_KEY"],
      "inject": {
        "query": { "key": "{GEMINI_API_KEY}" }
      },
      "rate_limit": "20/min"
    }
  ]
}
```

### 2. Run

```bash
# via flag (classic)
envgo --config envgo.routes.json --env .env --dir . --host 127.0.0.1 --port 8080

# or via .env alone (new): set MODE_PUBLIC=true in .env then
envgo run dev
# same as above, reads MODE_PUBLIC + CONFIG from .env
```

### 3. Frontend calls

```js
// No token needed
const res = await fetch("/api/chat", {
  method: "POST",
  headers: { "Content-Type": "application/json" },
  body: JSON.stringify({
    model: "gpt-4o",
    messages: [{ role: "user", content: "Hello!" }]
  })
});
```

### Config reference

| Field | Type | Description |
|-------|------|-------------|
| `trust_proxy` | bool | Trust `X-Forwarded-For` when behind reverse proxy |
| `default_rate_limit` | string | e.g. `30/min`, `5/s`, `100/hour` |
| `scrub_response` | bool | Replace secrets in response with `[REDACTED]` |
| `routes[].name` | string | URL segment at `/api/<name>` |
| `routes[].target` | string | Upstream URL (HTTPS only) |
| `routes[].method` | string | Allowed method (default: POST) |
| `routes[].vars` | string[] | Env vars allowed for this route |
| `routes[].inject.query` | map | Query params added to upstream |
| `routes[].inject.headers` | map | Headers added to upstream |
| `routes[].inject.body` | map | JSON fields merged into body |
| `routes[].rate_limit` | string | Per-route limit (overrides default) |
| `routes[].auth` | object | `{ "type": "bearer", "secret": "{ENV_VAR}" }` |

---

## Deploy to VPS (Production)

### Option A: envgo directly (Simple)

**Step 1: Upload to VPS**

```bash
# From your local machine
scp dist/envgo-linux-amd64 user@your-vps:/tmp/envgo
scp -r . user@your-vps:/var/www/myapp

# SSH to VPS
ssh user@your-vps
```

**Step 2: Install on VPS**

```bash
sudo cp /tmp/envgo /usr/local/bin/envgo
sudo chmod +x /usr/local/bin/envgo
```

**Step 3: Configure**

```bash
cd /var/www/myapp

# Edit .env with real secrets
nano .env
```

**Step 4: Run with TLS**

```bash
# Option 1: Direct HTTPS (self-signed cert)
envgo --tls --dir . --env .env --allow api.openai.com

# Option 2: Public mode with routes
envgo --tls --config envgo.routes.json --env .env --dir .
```

**Step 5: Run as service (optional)**

Create `/etc/systemd/system/envgo.service`:

```ini
[Unit]
Description=envGo Server
After=network.target

[Service]
Type=simple
User=www-data
WorkingDirectory=/var/www/myapp
ExecStart=/usr/local/bin/envgo --dir /var/www/myapp --env /var/www/myapp/.env --allow api.openai.com --port 8080
Restart=always
RestartSec=5

[Install]
WantedBy=multi-user.target
```

```bash
sudo systemctl daemon-reload
sudo systemctl enable envgo
sudo systemctl start envgo
```

---

### Option B: Reverse Proxy (Recommended for Production)

envGo runs on localhost, Caddy/nginx handles TLS.

**Step 1: Run envGo on localhost**

```bash
cd /var/www/myapp

# Local mode
envgo --dir . --env .env --allow api.openai.com --port 8080

# Public mode
envgo --config envgo.routes.json --env .env --dir . --port 8080
```

**Step 2: Install Caddy**

```bash
# Debian/Ubuntu
sudo apt install -y caddy

# CentOS/RHEL
sudo yum install -y caddy
```

**Step 3: Configure Caddy**

Edit `/etc/caddy/Caddyfile`:

```
yourdomain.com {
    reverse_proxy 127.0.0.1:8080
}
```

**Step 4: Start Caddy**

```bash
sudo systemctl enable caddy
sudo systemctl start caddy
```

Caddy auto-generates TLS certificate via Let's Encrypt.

**Step 5: Update envgo routes config**

```json
{
  "trust_proxy": true,
  ...
}
```

---

### Option C: Docker

**Step 1: Create Dockerfile**

```bash
envgo deploy -o ./deploy
```

**Step 2: Build and run**

```bash
cd deploy
docker build -t envgo .
docker run -d -p 8080:8080 --name envgo envgo
```

---

## TLS (HTTPS)

### Self-signed (dev/testing)

```bash
envgo --tls --dir . --env .env
# → https://127.0.0.1:8080/
```

Auto-generates self-signed certificate. Browser will show warning — click
"Advanced" → "Proceed" to continue.

### Production (recommended)

Use Caddy/nginx as reverse proxy. They handle TLS termination with
Let's Encrypt certificates automatically.

```bash
# envGo runs plain HTTP on localhost
envgo --dir . --env .env --port 8080

# Caddy handles HTTPS
caddy reverse-proxy --from yourdomain.com --to localhost:8080
```

---

## PHP Support

envGo can execute PHP files server-side with `.env` vars injected.

### Example

`index.php`:
```php
<?php
$secret = getenv("MY_SECRET");
if (!$secret) {
    echo "<h1>Error: MY_SECRET not configured</h1>";
} else {
    echo "<h1>Secret validated (value hidden)</h1>";
}
?>
```

### Security features

- **Typo detection**: If PHP code uses `getenv("WRONG_KEY")`, red banner appears
- **Security warning**: If PHP code does `echo getenv("KEY")`, red banner warns about secret exposure
- **Timeout**: PHP scripts limited to 30 seconds execution time
- **Safe pattern**: `$secret = getenv("KEY"); echo "...hidden..."` — no warning
- **Unsafe pattern**: `echo getenv("KEY")` — triggers security banner

---

## Hot Reload

Edit `.env` while envGo is running — changes picked up within ~1.5 seconds.

```bash
# Terminal 1
envgo run dev

# Terminal 2
echo "NEW_SECRET=new-value" >> .env
# → [envGo] hot-reloaded 5 variables from .env
```

---

## Dashboard

Open `http://127.0.0.1:8080/__envgo_dashboard` to see:

- Loaded variable **names** (never values)
- Last 200 requests (method, host, status, duration, variables used)
- Errors

Enable with `--dashboard` flag.

---

## Commands Reference

| Command | Description |
|---------|-------------|
| `envgo run dev` | Start dev server, read HOST/PORT from .env, auto-open browser |
| `envgo init` | Create new project template in current directory |
| `envgo init -name myapp` | Create project in subdirectory `myapp/` |
| `envgo deploy -o ./deploy` | Generate Caddyfile, nginx.conf, Dockerfile |
| `envgo -v` | Print version |
| `envgo -h` | Show help |

### Flags

| Flag | Short | Description |
|------|-------|-------------|
| `--port` | `-p` | Port to listen on (default: 8080) |
| `--host` | | Interface to bind (default: 127.0.0.1) |
| `--dir` | `-d` | Web root directory (default: .) |
| `--env` | `-e` | Path to .env file (default: .env) |
| `--allow` | `-a` | Comma-separated outbound host allowlist |
| `--config` | `-c` | Path to routes JSON (enables public mode) |
| `--dashboard` | `-D` | Enable metadata dashboard |
| `--tls` | | Enable HTTPS with auto-generated cert |
| `--browser` | `-b` | Open browser automatically |
| `--debug` | | Verbose logging |
| `--version` | `-v` | Print version |
| `--help` | `-h` | Show help |

### `.env` overrides (no flag needed)

`envgo run` reads `HOST`/`PORT` from `.env`. Public mode can also be set from `.env` without flags:

```env
MODE_PUBLIC=true          # or false, 1/0, yes/no, public/local
# aliases: MODE, PUBLIC_MODE, ENVGO_MODE
CONFIG=envgo.routes.json  # or ENVGO_CONFIG, ROUTES — explicit path (wins over MODE_PUBLIC)
```
Flag `--config` always wins over `.env`. Example: `MODE_PUBLIC=true envgo run dev` starts public mode via `.env` alone.

---

## Security Checklist

Before going to production:

- [ ] `.env` is in `.gitignore` (never commit secrets)
- [ ] Using `--allow` to restrict proxy targets
- [ ] Running behind Caddy/nginx for TLS (not `--tls` direct)
- [ ] `trust_proxy: true` in routes config when behind reverse proxy
- [ ] envGo bound to `127.0.0.1` only (not `0.0.0.0`)
- [ ] Dashboard disabled in public mode (no `--dashboard`)
- [ ] `.env.example` has placeholder values only

---

## Repository Layout

```
internal/envconfig     .env parser
internal/envstore      hot-reloading store
internal/gateway       public-mode gateway
internal/token         per-session token
internal/server        static + guards + PHP
internal/proxy         injection + outbound engine
internal/ratelimit     fixed-window rate limiter
internal/history       in-memory request log
internal/logger        redacting logger
scripts/               install.sh
dist/                  local build output (gitignored; make release)
```

## Threat Model

| Threat | Mitigation |
|--------|------------|
| Inspect Element / F12 | Secrets never enter browser; JS holds only placeholders |
| Network tab sniffing | Browser talks to localhost; key injected server-side |
| Cross-tab / XSS | `--allow` + session token + Host/Origin checks |
| SSRF | Proxy denied when `--allow` is empty |
| Abuse of fixed routes | Per-route `vars` allow-set + `rate_limit` + optional auth |
| Upstream secret leak | `scrub_response` redacts secrets from API responses |
| LAN access | Bound to `127.0.0.1` only |
| TLS downgrade | MinVersion TLS 1.2 when using `--tls` |
| PHP code execution | 30s timeout, typo detection, security warning |

---

## Contributing and feedback

Feedback of every size is welcome — a confusing error message is as useful to
hear about as a bug. See [CONTRIBUTING.md](CONTRIBUTING.md) for the full guide.

| What you have | Where to put it |
|---------------|-----------------|
| A bug or crash | [Open an issue](https://github.com/dnysaz/envgo/issues/new/choose) |
| An idea or question | [Start a discussion](https://github.com/dnysaz/envgo/discussions) |
| A security concern | Follow [SECURITY.md](SECURITY.md) — privately, not in a public issue |
| A docs problem | [dnysaz/envgo-site](https://github.com/dnysaz/envgo-site/issues) |

Before contributing code, note the two constraints that matter most: **no
third-party dependencies** (the standard library is the whole toolkit) and
**secrets must never reach the client**.

```bash
make build    # build ./envgo
make test     # run the test suite
make vet      # run go vet
make release  # cross-compile every platform into dist/
```

## License

MIT — see [LICENSE](LICENSE).

Copyright (c) 2026 Ketut Dana

You may use, copy, modify, merge, publish, distribute, sublicense, and sell
copies of this software, including for commercial and closed-source projects.
The only requirement is that the copyright notice and licence text are included.
There is no fee, no registration, and no restriction on the number of users,
servers, or deployments.

The software is provided "as is", without warranty of any kind. Review
[the threat model](https://envgo.dev/reference/threat-model/)
for the behaviours you are responsible for configuring yourself — in particular
rate limiting, which is unlimited unless you set it.
