# Contributing to envGo

Thanks for considering a contribution. Feedback of every size is welcome —
a confusing error message is as useful to hear about as a bug, and often more.

envGo is **free and open source under the MIT license**. There is no paid tier,
no licence key, no account to create, and nothing to unlock. It is meant to stay
that way.

## Ways to give feedback

| What you have | Where to put it |
|---------------|-----------------|
| A bug, crash, or wrong output | [Open an issue](https://github.com/dnysaz/envgo/issues/new/choose) |
| A feature idea or a design question | [Start a discussion](https://github.com/dnysaz/envgo/discussions) |
| A security concern | Follow [SECURITY.md](SECURITY.md) — please do **not** open a public issue |
| A documentation problem | An issue here, or a PR on [envgo-site](https://github.com/dnysaz/envgo-site) |

When reporting a bug, please include:

- the envGo version (`envgo -v`) and your OS/architecture
- the exact command you ran
- what you expected and what happened instead
- the relevant log output — and confirm first that it contains no secrets, since
  envGo redacts known secret values but cannot redact what you paste yourself

## Development

Requirements: Go 1.27.1 or newer.

```bash
git clone https://github.com/dnysaz/envgo.git
cd envgo

make build     # build ./envgo
make test      # run the test suite
make vet       # run go vet
make release   # rebuild verified Windows/Linux binaries into dist/
```

Before opening a pull request, please make sure `make vet` and `make test` pass.

### Trying your build

```bash
mkdir /tmp/demo && cd /tmp/demo
/path/to/envgo init
/path/to/envgo run dev
```

If PHP is not installed, `.php` files are served as static downloads by design —
see the PHP page in the docs before "fixing" that behaviour.

## Project constraints

Two rules matter more than style preferences here.

**1. Zero third-party dependencies.** `go.mod` lists no requires, and the
standard library is the whole toolkit. A pull request that adds a module needs a
strong justification, because the single-binary, no-runtime promise is the point
of the project. Hot reload uses mtime polling rather than a file-watcher library
for exactly this reason.

**2. Secrets must never reach the client.** Any change touching substitution,
logging, the dashboard, or response handling needs to be checked against that
rule. This includes new code paths that might echo a value.

## Code style

- Match the surrounding code. Run `gofmt` before committing.
- Keep comments about *why*, not *what*. If a behaviour is surprising, document
  the reason, since several behaviours here are deliberate (deny-by-default
  proxying, the PHP fall-through, empty rate limit meaning unlimited).
- Add a test for behaviour changes. The existing tests are table-driven and use
  `httptest`; follow those patterns.
- Prefer a small, focused change over a large one. It is easier to review a
  correct fix than to review a rewrite.

## Documentation

The documentation lives in a separate repository,
[dnysaz/envgo-site](https://github.com/dnysaz/envgo-site). It is not generated
from this code, so a behaviour change needs a matching docs change. If you would
rather not touch the docs, say so in your pull request and it can be handled
separately — a note about what changed is enough.

Please be precise about defaults and limits in the docs, and mark illustrative
examples as examples rather than required configuration.

## Pull requests

- Describe the problem first, then the fix.
- Note any user-visible behaviour change, especially for flags, config fields,
  status codes, or error messages.
- Mention what you tested and how.

## License

By contributing, you agree that your contribution is licensed under the MIT
license, the same terms as the project. See [LICENSE](LICENSE).
