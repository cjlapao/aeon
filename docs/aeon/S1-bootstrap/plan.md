# Plan — S1 Bootstrap

> **For agentic workers:** written to the `dev-loop-protocol` `plan.md` + `tasks/{taskid}.md` schema.
> Each task's full spec is at `tasks/T{n}.md`.
>
> **Spec:** [`design.md`](design.md) · **Brief:** [`brief.md`](brief.md) · **Demo:** [`DEMO.md`](DEMO.md)
> **Architecture:** [`../ARCHITECTURE.md`](../ARCHITECTURE.md) — §1a and §7 are binding.

## Understanding

### What we understood

Sixteen slices need something to run. This one builds it and ships **no Aeon behaviour at all** — no
protocol method, no model call, no store schema. What it ships is the ability to demonstrate any of those.

Two questions were settled before planning, one by the language and one to avoid a known trap.

**Aeon imports `go-backend-scaffolding` as a pinned dependency — never forks it, never vendors it, never
edits it.** `aeon/` holds Aeon's code and nothing upstream, which is what keeps the later extraction a
`git subtree split` (P24) rather than a provenance argument.

**The cost moves rather than disappearing.** A fork's cost is paid deliberately, every time, and nothing
changes underneath you. A dependency's is the opposite shape: **an upgrade is one command, so it happens
often, and it can widen the daemon without anyone reading a diff.** B8's allow-list and B11's golden file
are the whole answer, and they carry more weight here than they would have under a fork.

**`aeon/` is created by hand before this slice starts** — module, `cmd/`, `internal/`, and a `main.go` that
starts the API. T1 does not build it; T1 establishes what it is.

**`cgo` is on from the first commit**, with `mattn/go-sqlite3` and `sqlite-vec-go-bindings/cgo`. It is the
only verified path serving both the store (S4) and vectors (S22): pure-Go `modernc.org/sqlite` exposes no
extension loading at all, and the WASM route does not currently compile against the maintained driver.
Starting pure-Go and switching later converts a known build problem into a **driver swap fifteen slices
deep**, which is exactly the risk the earlier plan named and then walked into.

Three properties of the scaffolding make it a good dependency, all verified rather than assumed: its
service packages carry **zero** `net/http` imports, so transport and service are already separated;
`core.GlobalTenantID` exists so single-user degenerates cleanly; and registration is a plugin registry with
config gates, so naming an allow-list is a short list rather than surgery.

### Probes to run and record before decomposing

| # | Probe |
| --- | --- |
| Q1 | **Does the scaffolding have the socket transport yet?** S1 lands either way, but **S2 blocks on it** — say so explicitly rather than letting the plan imply it exists |
| Q2 | Enumerate what `bootstrap.Register` registers today. This becomes D5's committed fixture |
| Q3 | Which registered services **fail to start** without Redis, Postgres or SMTP? Run `serve` with none available. Any that block startup must be removed, not merely unused |
| Q4 | What does the scaffolding need at minimum to boot? A daemon that needs a config file before printing `version` is wrong |
| Q5 | Does the kept set assume a Postgres DSN, or fall back to SQLite? Aeon is SQLite-only |
| Q6 | Does `mattn/go-sqlite3` build on all six targets at the scaffolding's Go version? **Try it** |

> Q3, Q5 and Q6 must be **run**, not read. A service that boots fine on a machine with Redis installed is
> the failure this slice exists to prevent.

### How we will do it

**The handshake first, alone** (R4). `aeon/` arrives made by hand and the loop cannot see it being made, so
T1 enumerates what is actually there and pins the framework. Six tasks follow and all of them edit this
module — a handshake that costs twenty minutes is the difference between *"T3 failed"* and *"T3 failed
because `main.go` was not what T3 believed"*.

Then explicit registration, ending in a **committed fixture** of the registered set. That fixture turns "the
daemon runs only what we decided" from a claim into a test, and it is what catches a service arriving from a
routine `go get -u`.

Then the binary, then `cgo` and the six-target build **as its own task**, because it is the slice's real
risk and it must fail visibly rather than inside somebody else's task. Then the Tauri app, separately —
three toolchains in one task is three ways to be blocked.

The Makefile is a serialization point (R11) and lands once. The final task **walks `DEMO.md` end to end on a
clean checkout** and fixes whatever does not match, because a demo nobody has followed is a demo that does
not work.

### What you will have at the end

`make dev` starts the daemon and opens the app, both reporting a version read over the socket. `make build`
produces six binaries that each run. `make test` and `make lint` are green, and stay a standing condition
for the next sixteen slices.

## Objective

Produce a repository that builds, a daemon that starts and says where it lives, an app that opens, and a
Makefile that runs all of it.

## Tech tags in use

go, react-ts, docs

## Test suite

aeon-bootstrap

## Tasks

### T1 — Verify the starting state, and pin the framework
- engineer: golang-engineer-loop
- tech: go
- depends_on: []
- est_changed_lines: ~180
- detail: tasks/T1.md
- success_criteria:
  - this task's notes **enumerate** the module path, Go version, every direct requirement with its version, and every package under `aeon/internal/`
  - `aeon/go.mod` pins the framework to an **exact version** — not `latest`, not a branch
  - `aeon/UPSTREAM.md` records the module path, the pinned version, the packages Aeon imports, and the upgrade procedure
  - unit test `aeon/internal contains no copy of the framework` passes — a hit is **reported as a finding**, not deleted
  - `go build ./...` succeeds, the binary starts, and the API stays up
  - nothing outside `aeon/` is created or modified
  - `go-backend-scaffolding` is untouched — asserted by `git status` in that repository
- files_expected: `aeon/go.mod`, `aeon/go.sum`, `aeon/UPSTREAM.md`, `aeon/cmd/aeon/main.go`

### T2 — Explicit registration, and the fixture that guards it
- engineer: golang-engineer-loop
- tech: go
- depends_on: [T1]
- est_changed_lines: ~260
- detail: tasks/T2.md
- success_criteria:
  - unit test `the registered service set equals the committed fixture` passes
  - unit test `no removed service is referenced in the registration path` passes, asserted by grep
  - unit test `the daemon starts with no Redis, no Postgres and no SMTP available` passes
  - unit test `GlobalTenantID is the only tenant and no path creates a second` passes
  - `aeon/internal/bootstrap/services.golden` is committed and is the test's input
- files_expected: `aeon/internal/bootstrap/bootstrap.go`, `aeon/internal/bootstrap/services.golden`, `aeon/internal/bootstrap/registry_test.go`

### T3 — `cmd/aeon`: one binary, `version` and `serve`
- engineer: golang-engineer-loop
- tech: go
- depends_on: [T2]
- est_changed_lines: ~290
- detail: tasks/T3.md
- success_criteria:
  - unit test `aeon version prints version, commit and Go version with no config file present` passes
  - unit test `aeon serve prints the socket path then the store path on the first two lines` passes
  - unit test `the socket is created 0600` passes, asserted by stat
  - unit test `a connection to the socket is accepted` passes
  - unit test `SIGTERM shuts down cleanly and unlinks the socket` passes
  - unit test `a second serve refuses, naming the socket and the holding pid` passes
  - unit test `--print-services matches the fixture` passes
  - there is exactly one `cmd/` directory
- files_expected: `aeon/cmd/aeon/main.go`, `aeon/cmd/aeon/serve.go`, `aeon/cmd/aeon/version.go`, `aeon/cmd/aeon/main_test.go`

### T4 — `cgo` and the six-target build
- engineer: golang-engineer-loop
- tech: go
- depends_on: [T3]
- est_changed_lines: ~200
- detail: tasks/T4.md
- success_criteria:
  - all six binaries are **built and executed** — `linux/amd64`, `linux/arm64`, `darwin/amd64`, `darwin/arm64`, `windows/amd64`, `windows/arm64`
  - each runs `aeon version` successfully on its own platform, or under emulation where available; any target that cannot be executed is **named** as unverified rather than assumed
  - the cross-compilation approach is recorded in `aeon/docs/BUILD.md` with the reason
  - `sqlite-vec-go-bindings/cgo` is wired and a smoke test proves `vec_version()` answers
  - unit test `the sqlite driver opens a file store and applies no schema` passes
- files_expected: `aeon/docs/BUILD.md`, `aeon/internal/database/driver.go`, `aeon/internal/database/driver_test.go`, `Makefile`

### T5 — The Tauri app skeleton
- engineer: react-typescript-engineer-loop
- tech: react-ts
- depends_on: [T3]
- est_changed_lines: ~280
- detail: tasks/T5.md
- success_criteria:
  - `aeon/ui/` builds: `ui/src/` (React 19, Tailwind 4, Vite 7) and `ui/src-tauri/` (Rust shell)
  - the window renders the **daemon's** version, read over the socket — not a constant
  - unit test `the app renders a disconnected state when no daemon is running` passes
  - unit test `the app renders the version returned by the socket` passes, against a fake
  - the Rust shell is the only place that touches the socket
- files_expected: `aeon/ui/package.json`, `aeon/ui/vite.config.ts`, `aeon/ui/src/**`, `aeon/ui/src-tauri/**`, `aeon/ui/test/**`

### T6 — The Makefile and CI
- engineer: golang-engineer-loop
- tech: go
- depends_on: [T4, T5]
- est_changed_lines: ~240
- detail: tasks/T6.md
- success_criteria:
  - `make dev` starts daemon and app; `Ctrl-C` stops **both**, asserted by process count before and after
  - `make dev` refuses with a **named** missing prerequisite when Go, Node or Rust is absent
  - `make build`, `make test`, `make lint`, `make fmt`, `make clean` all work
  - `make fmt` then `git status --porcelain` prints nothing
  - CI runs `make build` for all six targets plus `make test` and `make lint` on every push, and **fails on any target**
  - exactly one task edits the Makefile — this one
- files_expected: `Makefile`, `.github/workflows/ci.yml`

### T7 — Walk the demo, and the QA suite
- engineer: technical-writer-loop
- tech: docs
- depends_on: [T6]
- est_changed_lines: ~170
- detail: tasks/T7.md
- success_criteria:
  - `DEMO.md` is walked end to end **on a clean checkout** and every step's stated output matches
  - any mismatch is fixed in the code or the demo, and which was wrong is recorded
  - `docs/test_suites/aeon-bootstrap.md` exists, `draft`, ids `QAB-01` upward
  - the suite covers the six-target build, a real `make dev`, socket permissions, a second `serve`, the service fixture, and a boot with no external services available
- files_expected: `docs/aeon/S1-bootstrap/DEMO.md`, `docs/test_suites/aeon-bootstrap.md`

## Priority order

**T2 and T4 are the deliverable**, which is a strange thing to say about a slice whose visible output is a
window with a version string.

T2 because the committed fixture is what makes the allow-list a **property rather than an intention**.
Twenty slices from now someone runs `go get -u` and the framework registers something new — and the only
thing standing between that and a daemon quietly wider than anyone decided is a golden file and a test.
**Cheap upgrades are what make that guard load-bearing** (B11), not optional. It is also the check Q3 feeds:
a service that boots fine because Redis happens to be installed locally is the failure this catches.

T4 because `cgo` cross-compilation is this slice's real risk and the only one that gets **more** expensive
with delay. `sqlite-vec` needs cgo; the pure-Go alternative exposes no extension loading at all; the WASM
route does not compile against the maintained driver. So the decision is forced, and the choice is between
finding the toolchain gaps now — while nothing depends on the build — or at S22, with fifteen slices of code
sitting on top. T4 also refuses to accept a Makefile that *would* build six targets: all six must exist and
run.

T1 is first and alone because the loop did not create `aeon/` and cannot see what is in it. Everything after
it edits this module, and a task that fails against an assumed starting state fails in a way nobody can
attribute.

T5 is separate because three toolchains in one task is three ways to be blocked, and because its one real
requirement is easy to fake: the window must show the **daemon's** version read over the socket, not its own
constant. A constant would pass a naive check while the socket was broken, and the whole point of building
the app now is that its integration is never a surprise later.

T7 exists because a demo nobody has followed does not work. It is also where the slice's honesty is
enforced: `DEMO.md` must say what this slice does **not** show — no protocol, no model, no store, no UI
capability — so nobody mistakes the garage for the skateboard.

A plan ordered by visible progress would have started with the app, because a window is the only thing here
a person can see. It would then have discovered the cross-compilation problem at S22.
