# Security Policy

envGo exists to keep secrets out of the browser, so a flaw that defeats that is
taken seriously. Please report security issues privately rather than in a public
issue.

## Reporting a vulnerability

Use GitHub's private vulnerability reporting:

**https://github.com/dnysaz/envgo/security/advisories/new**

That form is visible only to you and the maintainer. If you cannot use it, open
a minimal public issue that says only that you have a security report and would
like a private channel — do not include details in the issue itself.

Please include:

- a description of the issue and the impact you believe it has
- steps to reproduce, ideally a minimal configuration or request
- the version you tested (`envgo -v`), plus OS and architecture
- any suggested fix or mitigation you have in mind

Expect an initial response within a few days. Please allow 90 days for a fix
before public disclosure, and coordinate with the maintainer if you would like a
different timeline. Credit is offered unless you prefer to stay anonymous.

## Supported versions

Fixes are released against the latest release only. If you are running an older
build, please confirm the issue still reproduces on the newest release before
reporting.

## Scope

In scope — anything that lets a party who should not have a secret obtain it:

- secret values reaching the browser through the DOM, network responses, the
  dashboard, the request history, or log output
- bypassing the `/proxy` guards (session token, `Host` header, `Origin` header)
- bypassing the outbound allowlist, or reaching a host that is not allowlisted
- a public-mode route reaching a variable outside its `vars` allow-set
- escaping the web root, or serving a file the static handler should refuse
  (dotfiles, config files, `.env`)
- command or path injection through the PHP handler
- anything that makes `--allow` or `rate_limit` ineffective

Out of scope — these are known, documented limitations rather than defects:

- **Streaming responses are not scrubbed.** SSE passthrough deliberately skips
  redaction. If an upstream echoes a secret inside a stream, the browser sees it.
- **Variable names are visible on the dashboard.** That is by design; the
  dashboard is meant for local debugging.
- **The session token is not user authentication.** It is per process, has no
  expiry, and any page on your own origin can fetch it.
- **A failed PHP execution serves the `.php` source.** Documented, with the
  prerequisite spelled out in the docs.
- **Empty rate limit means unlimited.** Deny-by-default is not applied to rate
  limiting; it must be configured.
- **Rate limiting is per process.** Multiple instances each enforce their own.
- **`trust_proxy: true` on a directly exposed instance** lets clients forge their
  own rate-limit bucket. That is operator misconfiguration, and the docs warn
  about it.
- **A self-signed certificate from `--tls`.** Expected for development only.

See the [threat model](https://dnysaz.github.io/envgo-site/reference/threat-model/)
for the full list, including what envGo explicitly does not protect against.

## Hardening checklist for operators

- Keep `.env` outside the served web root, mode `600`, and out of git
- Bind to `127.0.0.1` and put a reverse proxy in front for TLS
- Set `default_rate_limit` or a per-route `rate_limit` — empty means unlimited
- Enable `trust_proxy` only when a proxy you control is actually in front
- Do not enable the dashboard in public mode
- Keep `vars` per route as narrow as possible
- Do not deploy `.php` files without a PHP interpreter present
