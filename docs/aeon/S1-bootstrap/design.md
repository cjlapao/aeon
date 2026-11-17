# Design — S1 Bootstrap (Stage 0 · Garage)

**Date:** 2026-09-06
**Status:** approved (design); not yet implemented
**Stage:** 0 · Garage — *you can build it and it starts*
**Depends on:** nothing. It is the first slice and it blocks all twenty-two others.

> Decision ids are prefixed **B**. See the
> [prefix registry](../ARCHITECTURE.md#11-decision-prefix-registry).

---

## Context

This is the slice most often skipped and the one everything rests on. It produces no Aeon behaviour at
all — no model call, no loop, no protocol method. What it produces is **the ability to demonstrate any of
that**: a module that builds, a daemon that starts, an app that opens, a Makefile that runs both, and CI
that ships six binaries.

One question had to be settled before the design could be written:

**Aeon imports `go-backend-scaffolding` as a pinned module dependency. It is never forked, never vendored,
never edited.** `aeon/` holds Aeon's code and nothing upstream.

> **This reverses the original B1**, which said a fork was *forced* because the scaffolding's forty-odd
> domains live under `internal/` and `internal/` does not cross a module boundary. That reasoning was sound
> about `internal/`; it was wrong about the conclusion, because **the services Aeon needs are reachable
> through the framework's public API.** The fork was solving a problem that turned out not to exist.

**The cost does not vanish — it moves, and it gets sharper.** A fork's cost is that upstream fixes are ported
by hand; you pay it every time, deliberately, and nothing changes underneath you. A dependency's cost is the
opposite shape: **an upgrade is one command, so it happens often, and it can widen the daemon without anyone
reading a diff.** B8 and B11 are the whole answer to that, and they matter *more* here than they did under a
fork.

Three properties of the scaffolding make it a good dependency, all verified rather than assumed:

- **Its service packages carry zero `net/http` or `gorilla/mux` imports** — `user/service`, `auth`,
  `api_keys/service`, `session` are all clean. Transport and service are already separated, which is what
  makes a socket dispatcher cheap.
- **`core.GlobalTenantID` exists** and `privilege_guard.go` branches on it, so single-user degenerates
  cleanly rather than being fought.
- **`bootstrap.Register(svc)` is a plugin registry with per-service config gates**, so removing what Aeon
  does not need is a shorter registration list and a config file, not surgery.

## Goal

`make dev` starts the daemon and opens the app, both reporting a version. `make build` produces binaries
for six platforms. `make test` is green. Nothing else.

## Decisions

| # | Decision | Why |
| --- | --- | --- |
| B1 | Aeon **imports** `go-backend-scaffolding` as a pinned module dependency at `aeon/go.mod`. **Never forked, never vendored, never edited** | The services Aeon needs are reachable through the framework's public API, so the fork this decision originally forced is unnecessary. An upgrade becomes `go get`, not a hand-port — and **`aeon/` owning no copy of anything upstream is what keeps the later extraction a `git subtree split`** (P24) rather than a licence-and-provenance argument. Supersedes the fork there |
| B2 | The dependency is pinned to an **exact version**, and an upgrade is a deliberate, reviewed act with its reason recorded in `aeon/UPSTREAM.md` | Replaces the base-commit record, because the divergence problem is gone. What replaces it is worse if unmanaged: `go get -u` is one command and a floating version means the daemon's composition changes when nobody asked. Was: a fork whose divergence point is unrecorded cannot take an upstream fix; this is the whole maintenance cost of B1 and it is paid by writing one line |
| B3 | **`cgo` is enabled from the start**, with `mattn/go-sqlite3` and `sqlite-vec-go-bindings/cgo` as the settled SQLite path | It is the only verified path serving both the store (S4) and vectors (S22). Deciding late means rebuilding CI, and a driver swap mid-plan is the risk the v1 plan named |
| B4 | One binary: `aeon`. `aeon serve` is the daemon; every other verb is a client | The single binary is what makes the editor-extension path real (P30) |
| B5 | The Tauri app lives at `aeon/ui/`, with `ui/src/` (TypeScript) and `ui/src-tauri/` (Rust) | Tauri's own convention, and it matches the architecture's `ui/` entry |
| B6 | The Makefile is the only entry point a contributor needs | `make dev`, `build`, `test`, `lint`, `fmt`, `clean`. A README that lists eight commands is a README nobody follows |
| B7 | `make dev` starts daemon **and** app together, and stopping it stops both | Two terminals is friction that compounds seventeen times |
| B8 | Registration is an **explicit allow-list in `aeon/`** — Aeon names the services it wants and registers those. It never subtracts from the framework's defaults | You cannot delete from a module you do not own — **and the allow-list is the better discipline anyway.** A deny-list fails open on every upstream addition; an allow-list makes a new upstream service **inert by default**, so it fails safe on every future upgrade rather than only the ones somebody remembered to audit. An unregistered service cannot be switched on by a stray config key, and it shrinks the binary and the audit surface |
| B9 | CI builds all six targets on every commit to the default branch, and **fails on any target** | A cross-compilation break found at release is found at the worst time. `cgo` (B3) makes this the slice's main risk, so it is exercised from day one |
| B10 | The daemon's socket path and store path are **printed on startup** | Every later slice's demo begins by finding them, and a daemon that hides where it lives costs one question per demo |
| B11 | **A dependency upgrade must not change the registered service set**, and the committed golden file is what proves it. CI runs that check | Under a fork, upstream arrived only when somebody deliberately merged it. Under a dependency, `go get -u` is one command — **making upgrades cheap makes this test more valuable, not less.** It is the only thing standing between a routine version bump and a daemon quietly wider than anyone decided, in a program that runs shell commands |

## Architecture

```
aeon/
  go.mod                     module github.com/cjlapao/aeon
  Makefile                   the only entry point (B6)
  cmd/aeon/                  the one binary (B4)
    main.go                  verb dispatch: serve | version | …
  internal/                  AEON'S OWN CODE ONLY — nothing upstream lives here
    bootstrap/               the explicit allow-list registration (B8) + its golden file
    … Aeon's packages arrive in S2 onward
                             the framework's config, logging, database, core,
                             encryption, validation, health, api_keys, role,
                             claim and ratelimit are USED, not copied (B1)
  ui/                        the Tauri app (B5)
    src/                     TypeScript — vite, React, Tailwind
    src-tauri/               Rust shell; talks to the socket
  .github/workflows/         CI: six targets (B9)
```

### What is registered, and why it is an allow-list rather than a subtraction

**Aeon names what it wants.** Everything else is simply never registered — not disabled, not removed, never
named. These are the services the original design listed as *dropped*, kept here as the record of what was
considered and refused (B8): `oauth`, `webauthn`, `otp`, `session`, `sms`, `email`,
`notification`, `object_storage`, `media`, `redis`, `jobs`, `flipt`, `honeytrap`, `ipban`, `bruteforce`,
`tracing`, `profiling`, `metrics`, `swagger`, `websocket`, `device`, `activity`, `user`, `tenant`,
`api` (its HTTP handler tree), `webauthn`, `vaults`.

Most of those defend or instrument a **public multi-tenant endpoint**, and Aeon does not have one — its
first-party transport is a socket whose access control is filesystem permissions (P26). Several others
(`redis`, `jobs`, `object_storage`) require an external service, against P10's "one install, nothing to
provision".

Kept because Aeon genuinely uses them: `config`, `logging`, `core`, `database`, `encryption`,
`validation`, `osutils`, `health`, `idempotency`, `events`, `update`, `app`, `appctx`, `bootstrap`. And
kept **for S19 only**: `api_keys`, `role`, `claim`, `ratelimit` — surplus while the only transport was a
loopback port with no auth, and exactly right once HTTP is a deliberate integration surface.

**Removal, not a config flag**, because a disabled service is one a stray key re-enables, and because
every registered service is audit surface in a program that runs shell commands.

### The tenant question, answered once

`core.GlobalTenantID` becomes the only tenant. Aeon does not expose tenancy, does not accept a tenant in
any request, and creates no second tenant. The scaffolding's tenant-scoped queries keep working because
they all resolve to the global id — which is why single-user degenerates rather than needing 177 files
edited.

### `cgo`, and the risk it introduces

B3 settles SQLite on `mattn/go-sqlite3` plus `sqlite-vec-go-bindings/cgo`, which vendors `sqlite-vec.c`
and registers it through `sqlite3_auto_extension()`. The user's machine needs nothing; the *build* needs a
C toolchain per target.

That is this slice's main risk and the reason B9 exercises all six targets immediately. Two viable
approaches, and the slice picks one and records why: native CI runners per OS, or `zig cc` as a universal
cross-compiler. A third — deferring `cgo` by starting on a pure-Go driver — is rejected: it moves a known
problem to S22 and makes it a driver swap.

## The demo

See [`DEMO.md`](DEMO.md). In short: `make dev` starts both processes and the app shows a version;
`make build` yields six binaries; `make test` is green; `./aeon serve` prints its socket and store paths.

## Testing

| What | How |
| --- | --- |
| The module builds | `make build` for the host, then all six targets |
| The daemon starts | spawn `aeon serve`, assert it logs a socket path and a store path, then exits cleanly on SIGTERM |
| The socket is created and accepts | connect to the logged path; assert a connection is accepted (the *protocol* is S2) |
| Only expected services register | assert the registered set equals a committed list — a service added by an upstream merge fails this |
| The global tenant is the only one | assert no code path creates a second tenant, and that a tenant-scoped read resolves to `GlobalTenantID` |
| `cgo` builds cross-platform | CI, six targets, every commit |
| The app opens | Tauri build succeeds; a smoke test asserts the window renders the version string |
| `make dev` starts and stops both | spawn it, assert two processes, send SIGINT, assert both gone |

## The socket service is the scaffolding's, and there is a request for it

This slice pins `go-backend-scaffolding` and registers only the services Aeon named. **It does not build the socket
listener** — that is the scaffolding's to add, and
[`../REQUEST-localrpc-service.md`](../REQUEST-localrpc-service.md) is the specification handed to it.

**Read that request before starting**, because it settles what Aeon may assume: one API over a unix socket and a named
pipe, path resolution rather than blind configuration, mode `0600` plus peer credentials as the whole access-control
model, JSON-RPC 2.0 framing with a server→client notification channel, and disconnect cancellation reaching handlers
through their `context.Context`.

**Two things in it constrain this slice directly.** The service satisfies the existing `Service` + `Runnable` contract
with no change to it — bind in `Init`, accept from `Serve`, return immediately — so B8's removal discipline applies to it
like any other plugin. And its `HealthCheck` reports both that the listener is accepting **and** that the endpoint still
has the mode it was created with, which is what `doctor` (S23) surfaces.

**If the service is not available when this slice runs**, the honest path is a minimal local implementation behind the
same interface the request specifies — and **that is a finding to raise, not a fork of the design.** Two implementations
of a socket listener is the outcome the request exists to avoid.

## Out of scope

- **The Aeon protocol.** S2. This slice proves a socket is accepted, nothing about what is spoken over it.
- **The socket transport itself.** Built in `go-backend-scaffolding` (§1a). If it is not there yet, S1
  still lands — S2 is what blocks on it.
- **Any model call, store schema, tool or loop.** S3 onward.
- **`aeon init`, presets, `doctor`.** S23.
- **The HTTP surface.** S19. `api_keys` and friends are kept but unmounted.
- **Code signing and notarisation** for the app. Real work, and it belongs with S23's distribution.

## Consequences for later slices

| Slice | Consequence |
| --- | --- |
| S2 | Needs the scaffolding's socket transport to exist. Its first task should assert that, not assume it |
| S4 | The SQLite driver is already chosen (B3); S4 implements the schema rather than picking a driver |
| S22 | `sqlite-vec` needs no new build machinery — B3 paid for it |
| S19 | `api_keys`, `role`, `claim`, `ratelimit` are present and unmounted, so S19 mounts rather than adds |
| all | `make test` must stay green. A slice that leaves it red has not landed |

## Open questions

None. The `cgo` cross-compilation approach (native runners versus `zig cc`) is a choice the implementing
task makes and records, not a design question — both work, and the plan requires the decision to be
written down rather than made twice.
