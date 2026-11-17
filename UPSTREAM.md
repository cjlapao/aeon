# The framework dependency

Aeon **imports** `go-backend-scaffolding`. It does not fork it, vendor it, or edit it (B1).

| | |
| --- | --- |
| **Module** | `github.com/cjlapao/go-backend-scaffolding/framework` |
| **Pinned version** | `v0.10.1` |
| **Git tag** | `framework/v0.10.1` — note the prefix; the repository root is a *different* module |
| **Pinned on** | 2026-10-11 |
| **Requires** | Go 1.26.6 (the framework's `go.mod` floor; `GOTOOLCHAIN=auto` fetches it) |

## Why a dependency and not a fork

The original B1 said a fork was *forced*, because the scaffolding's forty-odd domains live under
`internal/` and `internal/` does not cross a module boundary.

**That reasoning was right about `internal/` and wrong about the conclusion.** The services Aeon needs
are published in the `framework/` module, so the fork was solving a problem that does not exist.

**The cost did not vanish — it moved, and it got sharper.** A fork's cost is paid deliberately, every
time, and nothing changes underneath you. A dependency's is the opposite shape: an upgrade is one
command, so it happens often, and **it can widen the daemon without anyone reading a diff.**

Two things answer that, and they matter more here than they would have under a fork:

- **B8** — registration is an allow-list, so a service the framework adds in a future version arrives
  **inert**.
- **B11** — a committed fixture of the registered set, compared by a test. The daemon's composition
  cannot change without a reviewable diff.

## `GOPRIVATE` is required

```sh
export GOPRIVATE='github.com/cjlapao/*'
```

The repository is public, but `sum.golang.org` cannot verify this module — its own fetch of the
repository fails authentication, so the lookup 404s and `go get` refuses the download it already
completed. `GOPRIVATE` skips the checksum database for this path.

The `Makefile` exports it, so `make` targets work with no setup. A bare `go build` in this directory
needs it in the environment.

**The failure, if you forget:**

```
verifying module: ... reading https://sum.golang.org/lookup/...: 404 Not Found
```

## What Aeon imports

Enumerated rather than summarised, so a future upgrade can be read against a list:

| Import | Used for |
| --- | --- |
| `framework/core/app/models` | `ApplicationInfo` passed to `system.Initialize` |
| `framework/core/appctx` | the application context; `ctx.Log()` |
| `framework/core/system` | `Initialize`, and the service orchestrator |
| `framework/core/system/interfaces` | `SystemServiceInterface`, `Service` — the registration contract |
| `framework/core/logging` | `logging.Errorf` on the shutdown path |
| `framework/localrpc` | **the local JSON-RPC endpoint — the daemon's transport** |
| `framework/api` | the HTTP server — **registered but pinned off** until S19 |

Nothing else. In particular **not** the `database` service — re-checked against v0.10.1, which added it:
its published contract is `*gorm.DB` and nothing in the framework references `sqlite-vec`, so S4's case
for owning the store survives the service existing.

## Upgrading

```sh
make framework-upgrade FRAMEWORK_VERSION=v0.11.0
```

That target does three things in order, and the third is the point:

1. `go get` the named version
2. `go mod tidy`
3. **run the registry fixture test** — proving the registered service set did not change

Then, in the same commit:

- record the new version in the table at the top of this file
- **record the reason you upgraded.** Not "upgrade framework" — *what you needed*. A version history
  with no reasons cannot tell you whether a future upgrade is safe to skip.

### The v0.10.1 breaking change, and what it teaches

**`aeon serve` stopped starting entirely.** `cursor.signing_key` became a required config value; without
it the config service **fails validation and is never registered**, so every service depending on it
fails behind `service "localrpc" requires "config" which is not registered` — an error naming config
rather than the cause.

Fixed by `internal/secrets.EnsureCursorSigningKey()`, which generates and persists one at
`<store dir>/cursor.key` with mode `0600`, before `system.Initialize`.

**The lesson is about the guard, not the key.** `make ci` stayed **green** throughout: building and
testing never boot the daemon, and the fixture test checks the registered *set*, not runtime config
requirements. So:

> **An upgrade is not verified until something has actually run `serve` on a clean machine.**
> `make framework-upgrade` is necessary and not sufficient.

Until there is an automated smoke test that boots the daemon, run this after any upgrade:

```sh
rm -rf /tmp/aeon-upgrade-check && mkdir -p /tmp/aeon-upgrade-check
XDG_STATE_HOME=/tmp/aeon-upgrade-check ./dist/aeon serve
# expect: an `endpoint` line, a `store` line, and ZERO errors
```

### If the service set changed

The test fails and names the service. **That is the guard working, not a problem with the test.**

Decide deliberately: either Aeon wants the new service — register it and update `services.golden` in the
same commit — or it does not, in which case nothing changes except that you now know it exists.

### Upgrade log

| Date | Version | Reason |
| --- | --- | --- |
| 2026-10-03 | `v0.10.0` | Initial pin. First release carrying `framework/localrpc`, which this project requested (see `docs/aeon/REQUEST-localrpc-service.md`) and which S2's transport depends on |
| 2026-10-11 | `v0.10.1` | **For `framework/api`** — a registerable HTTP server, which S19 previously had to build itself. Also brought `database`, `swagger`, `websocket`, `otp`, `analytics`, `consent`, `feedback`, `honeytrap` (all refused) and `system.WithForcedServices`, which is what lets `api` be registered and pinned off. **Breaking: `cursor.signing_key` became mandatory** — see below |
