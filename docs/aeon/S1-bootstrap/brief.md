# Brief — S1 Bootstrap

> ## State — the ground floor is built
>
> **`aeon/` exists, builds, boots and is verified.** It was created by hand on 2026-10-03, outside the
> loop, together with the Makefile, the parked CI and `AGENTS.md`. Read [`../../../AGENTS.md`](../../../AGENTS.md)
> before this brief: it is the map of the folder you are working in.
>
> | Task | State |
> | --- | --- |
> | **T1** verify and pin | ✅ **landed** — framework pinned at **`v0.10.1`**, `UPSTREAM.md` with the upgrade log, the no-copy guard |
> | **T2** explicit registration | ✅ **landed** — allow-list, `services.golden`, **the pins**, 8 passing tests. **Q1 answered: `svc.Register()` is the seam** |
> | **T3** one binary | ✅ **landed** — `--print-services`, second-instance refusal with the holder's pid, stale-endpoint reclaim, 6 tests, split into `serve.go`/`version.go` |
> | **T4** cgo and six targets | ✅ **landed** — `sqlite-vec` statically linked and proven; `docs/BUILD.md` records the native-runner decision; **1 of 6 measured honestly** |
> | **T5** Tauri skeleton | ◐ **web layer landed** (9 tests, builds); `endpoint.rs`/`rpc.rs` verified against a live daemon; **`main.rs` uncompiled** — this machine lacks Tauri's Linux packages |
> | **T6** Makefile and CI | ✅ **landed** — `make dev` starts both with a trap, prerequisites checked before anything starts, UI folded into test/vet/clean |
> | **T7** walk the demo | ✅ **landed** — walked, **6 discrepancies and 1 real bug found**, `DEMO.md` rewritten, suite amended to 15 cases |
>
> **Two services are registered, and only one runs.** `localrpc` (`ForceOn` — a daemon with no endpoint
> can never be spoken to) and `api` (`ForceOff` — the framework defaults it to `:8080`, and S19 owns
> removing the pin). **`system.WithForcedServices` is new in v0.10.1** and is what makes "registered but
> not listening" expressible at all.
>
> **One thing the upgrade taught, which belongs in T7's QA suite.** `framework v0.10.1` made
> `cursor.signing_key` mandatory and **`aeon serve` stopped starting**, while `make ci` stayed green —
> because building and testing never boot the daemon. `internal/secrets` fixes the key; the lesson is that
> **an upgrade is not verified until something has run `serve` on a clean machine.** There is no automated
> smoke test for that yet.
>
> **The measurement, now settled.** `make build-all` passes **1 of 6** — and the number was wrong twice
> before it was right: *5 of 6* with no cgo in the tree, *3 of 6* with cgo in the tree but not in
> `./cmd/aeon`. **Two of those "passes" were Windows binaries containing no SQLite at all.** `build-all`
> now greps each artefact for `sqlite3_auto_extension`, because the exit code is not the measurement.


**Implements:** [`design.md`](design.md) · **Demo:** [`DEMO.md`](DEMO.md)
**Architecture:** [`../ARCHITECTURE.md`](../ARCHITECTURE.md) — §1a and §7 are binding.
**Stage:** 0 · Garage. First slice; blocks all twenty-two others.
**Repo:** Aeon lives at `aeon/` in this repository (P12) · Go 1.26 · `cgo` enabled (B3)

> **Template deviations from `proposal-brief-protocol`, stated rather than silent:**
> **(a)** Implements a design, not a proposal — §7 records where this brief corrects it.
> **(b)** There is a **§14, the demo contract**. Every slice in this plan has one, because a slice is not
> done when its tests pass — it is done when someone else can follow `DEMO.md` and watch it work.
> **(c)** §10's investigation reads a **different repository** (`go-backend-scaffolding`), which is a
> **dependency** — read, never forked and never changed.

---

## 1. How to use this brief

> **Constant. Copy verbatim.**

This is a **brief, not a plan.** It states what must be true when the work is done, what must not change
along the way, and where to look to find out the rest. It contains no task breakdown and no implementation
code — producing those is your job.

1. **Read** the design and the demo, then §7 for where this brief overrides the design.
2. **Investigate** — §10 names what to read. Do not plan before you have those answers.
3. **Plan and decompose** — §12 gives the rules your task sequence must obey.
4. **Self-check** — §13, against your finished plan, before dispatching anything.

Where this brief states a decision, it is settled. Where it poses a question, answer it from the code.

**On the self-check:** every answer needs an artifact — a command's output, an enumeration, a quoted line.

---

## 2. Definition of Done

| # | Check |
| --- | --- |
| **D1** | **`make build` produces six binaries** — linux/darwin/windows × amd64/arm64 — each of which runs `aeon version` and prints a version, a commit and a Go version. `cgo` is enabled, so this is the check that proves the cross-toolchain is wired. |
| **D2** | **`aeon serve` prints its socket path and its store path on the first two lines** (B10), then shuts down cleanly on SIGTERM leaving **no socket file behind**. |
| **D3** | **The socket is created with owner-only permissions** (`srw-------`). Asserted by `stat`, not by intent — this is Aeon's entire access-control story for first-party clients (P26). |
| **D4** | **A connection to the socket is accepted.** Nothing needs to be spoken over it; the protocol is S2. |
| **D5** | **`--print-services` lists exactly the decided set**, asserted against a committed list. Anything extra is a failure — including a service arriving from a routine `go get -u`, which is what makes this check earn its place (B11). |
| **D6** | **Registration is an explicit allow-list; refused services are never named** (B8). Asserted by grep: no reference to `redis`, `jobs`, `oauth`, `webauthn`, `otp`, `sms`, `email`, `object_storage`, `metrics`, `swagger`, `tracing`, `profiling` in the registration path. |
| **D7** | **`core.GlobalTenantID` is the only tenant.** No code path creates a second one, no request accepts a tenant, and a tenant-scoped read resolves to the global id. |
| **D8** | **`make dev` starts the daemon and the app, and `Ctrl-C` stops both** (B7). Asserted by process count before and after — an orphaned daemon is a failure. |
| **D9** | **The Tauri app builds and renders a version string.** Nothing more; it has no capability until S9. |
| **D10** | **The framework is pinned to an exact version** (B2), recorded in `UPSTREAM.md` with the packages Aeon imports and the upgrade procedure. A floating version means the daemon's composition changes when nobody asked. |
| **D11** | **The `cgo` cross-compilation approach is recorded** — native runners or `zig cc` — with the reason. Not the decision itself: the *record* of it, so it is not re-made at S22. |
| **D12** | **CI runs `make build` for all six targets, `make test` and `make lint` on every push, and fails on any target** (B9). |
| **D13** | **`make fmt` then `git status --porcelain` prints nothing**, and `make test && make lint` are clean. |

D1 and D5 are the two that matter. D1 because `cgo` cross-compilation is this slice's real risk and finding
it broken at S22 is finding it at the worst time. D5 because it is the check that keeps a **dependency
upgrade** from silently widening the daemon — and upgrades are one command now, so it will be asked to do
that job often.

**D1–D12 must each exist as a named test or a CI job.**

D13 is a floor, never a task's sole acceptance criterion (R1).

---

## 3. The problem

Sixteen slices need something to run. This one builds it, and it produces no Aeon behaviour at all.

One question shapes everything: **Aeon imports `go-backend-scaffolding` as a pinned dependency** (B1). It is
never forked, never vendored, never edited, and `aeon/` holds Aeon's code and nothing upstream — which is
what keeps the later extraction a `git subtree split` (P24).

**The cost moves rather than disappearing.** A fork's is paid deliberately and nothing changes underneath
you. A dependency's is the opposite: **an upgrade is one command, so it happens often and can widen the
daemon without anyone reading a diff.** B8's allow-list and B11's golden file are the answer.

**`aeon/` is created by hand before this slice runs** — module, `cmd/`, `internal/`, and a `main.go` that
starts the API. T1 does not build it; T1 establishes what it is.

The second settled question is `cgo`. SQLite is on `mattn/go-sqlite3` with `sqlite-vec-go-bindings/cgo`
from the first commit (B3) — the only verified path serving both the store (S4) and vectors (S22). The
alternative, starting pure-Go and switching at S22, converts a known build problem into a driver swap
fifteen slices deep, which is precisely the risk the earlier plan named and then walked into.

## 4. Goal

`make dev` starts the daemon and opens the app, both reporting a version. `make build` produces six
binaries. `make test` is green.

## 5. Shape and the seam

```
go-backend-scaffolding ──require──▶ aeon/        (pinned dependency, B1)
                                   cmd/aeon        one binary (B4)
                                   internal/       AEON'S OWN CODE ONLY (B1)
                                                   bootstrap/ = the allow-list (B8)
                                   ui/             Tauri: src/ + src-tauri/ (B5)
                                   Makefile        the only entry point (B6)
                                   .github/        six targets (B9)
```

### The seams

**Upward — the Makefile.** A contributor needs `make` and nothing else. Any capability reachable only by a
raw `go` or `cargo` invocation is a capability most people will not use.

**Sideways — the pinned dependency.** Exact version, recorded in `UPSTREAM.md` with the packages Aeon imports
(B2, D10). An upgrade is deliberate, reasoned in writing, and must leave the registered set unchanged (B11).

**Downward — the plugin registry.** Aeon's own packages register into `bootstrap.Register` from S2 onward.
This slice leaves that list **short and asserted** so a later addition is deliberate.

## 6. Locked decisions

- **B1** — **dependency, not fork.** Never copy framework source into `aeon/`; a copy found there is a **finding to report**, not something to tidy away.
- **B3** — `cgo` on, `mattn/go-sqlite3` + `sqlite-vec-go-bindings/cgo`. **Do not start on a pure-Go driver.**
- **B4** — one binary, `aeon`. `serve` is the daemon; every other verb is a client.
- **B7** — `make dev` starts and stops both.
- **B8** — **an explicit allow-list, not a subtraction and not a config flag.**
- **B11** — **a dependency upgrade must not change the registered set**, and CI proves it.
- **B10** — socket and store paths printed on startup.

## 7. Overrides to the design

| # | Design says | This brief requires |
| --- | --- | --- |
| O1 | The design lists the kept and removed services | Treat the **kept** list as the contract and the removed list as illustrative. Enumerate what the fork actually registers, commit that as D5's fixture, and reconcile any difference in this slice's notes. An inherited service nobody noticed is exactly what D5 exists to catch. |
| O2 | `cgo` cross-compilation is "a choice the task records" | Also **prove it**: D1 is not satisfied by a Makefile that *would* build six targets. All six must exist and run `version`, in CI, before this slice lands. |
| O3 | `make dev` starts both | It must also **fail loudly when a prerequisite is missing** — no Go, no Node, no Rust toolchain — naming which. A `make dev` that half-starts is worse than one that refuses. |
| O4 | The app "renders a version string" | It renders the **daemon's** version, read over the socket, not its own constant. A hardcoded string proves the window opened and nothing else, and it would pass D9 while the socket was broken. |
| O5 | The design does not say what happens on a second `aeon serve` | The second instance **refuses**, naming the existing socket and the pid holding it. One daemon per machine (§1a); two racing on one store is the failure this prevents. |

## 8. Non-goals

- **N1 — No Aeon protocol.** S2. Acceptance of a connection is the whole claim.
- **N2 — No socket transport implementation.** It is `go-backend-scaffolding`'s (§1a).
- **N3 — No model call, no store schema, no tool, no loop.** S3 onward.
- **N4 — No `aeon init`, presets or `doctor`.** S23.
- **N5 — No HTTP surface mounted.** S19. `api_keys` and friends are present and unmounted.
- **N6 — No code signing or notarisation.** S23's distribution work.
- **N7 — No UI capability.** A window and a version string.
- **N8 — No upstream changes.** `go-backend-scaffolding` is a dependency: read, never edited from here. A change it needs is **raised as a request**, the way `localrpc` was.

### Standing non-goals

- **Never write to `opencode/` or `packages/loop-state-*`.**
- **Never edit `go-backend-scaffolding`.** It is a different repository and this slice only reads it.
- **Never commit a credential.** The daemon reads none yet; keep it that way.
- **Never run `git clean`.**

## 9. Repo invariants

**I1 — Everything Aeon writes lives under `aeon/`** (P12). Nothing at the repository root, which already has
a `package.json` and a `README.md` that must not be overwritten.

**I2 — Nothing under `aeon/` imports from `packages/`** (P24). It is what keeps the later
`git subtree split --prefix=aeon` mechanical. Name the mechanism that catches a violation.

**I3 — The Makefile is the only documented entry point** (B6).

**I4 — One binary.** No second `cmd/`. A `cmd/aeon-daemon` alongside `cmd/aeon` is the split B4 exists to
prevent, and it would break the editor-extension story.

**I5 — The registered-service list is a committed fixture** (D5), not a runtime discovery. A test compares
the two.

**I6 — No test starts a real daemon on the real socket path.** Temp directories and temp socket paths, so a
test run never collides with a developer's running daemon.

**I7 — `make test` green is a standing condition** from this slice onward.

**I8 — Public API changes ship with their tests in the same task.**

---

## 10. Investigation brief

### 10.1 What to read

1. **`go-backend-scaffolding/internal/bootstrap/bootstrap.go`** in full — the `Register(svc)` list is what
   gets trimmed, and it is the single most important file in this slice.
2. **`internal/app/plugin.go`** and **`internal/appctx/`** — the plugin interface Aeon's own packages will
   implement from S2.
3. **`internal/config/`** — the layering, which S23's `C2` inherits. Read enough to know whether it needs
   changing later; change nothing now.
4. **`internal/core/`** — `GlobalTenantID` and what depends on it.
5. **`cmd/api/`** — the existing entry point, as the model for `cmd/aeon`.
6. **The scaffolding's own `Makefile` and CI**, if any — inherit what works.
7. **`docs/aeon/docs/_archive/aeon-v1-horizontal/13-cli/`** — `C1`–`C13` and the three-part error shape. Not implemented here,
   but `cmd/aeon`'s skeleton should not contradict them.

### 10.2 The patterns to copy

- **The plugin registry shape.** Aeon's packages are services in the same sense.
- **The scaffolding's config conventions**, so S23 has nothing to undo.
- **`pkg/diagnostics`** — the `Diagnostic`/`DiagnosticBag` pattern several superseded slices already assume.

### 10.3 Confirm each from the code

| # | Question | Where |
| --- | --- | --- |
| Q1 | **Does the scaffolding have the socket transport yet?** If not, S1 still lands — but say so, because S2 blocks on it. | The scaffolding. Grep for `net.Listen("unix"`, `winio`. |
| Q2 | What exactly does `bootstrap.Register` register today? Enumerate it. | `bootstrap.go`. This becomes D5's fixture. |
| Q3 | Which registered services fail to start without an external service — Redis, Postgres, an SMTP host? | Try `serve` with none available. Any that block startup must be removed, not merely unused. |
| Q4 | What does the scaffolding's config require at minimum to boot? | `config/` and the app lifecycle. A daemon that needs a config file before it can print `version` is wrong. |
| Q5 | Does anything in the kept set assume a Postgres DSN, or does it fall back to SQLite? | `internal/database/`. Aeon is SQLite-only. |
| Q6 | What Go version does the scaffolding require, and does `mattn/go-sqlite3` build against it on all six targets? | `go.mod`; then try. |

### 10.4 Project-specific questions

| # | Question |
| --- | --- |
| Q7 | Does the framework expose a **registration seam** a caller can use to choose services (B8)? If not, that is a finding to raise, not something to work around by copying its bootstrap. Name it, and name how an upstream fix is taken later. |
| Q8 | `cgo` cross-compilation: native CI runners or `zig cc`? Pick, prove, and record why (D11). |
| Q9 | How does `make dev` supervise two processes so `Ctrl-C` stops both (B7, D8)? Name the mechanism — a process group, `trap`, or a tiny supervisor. |
| Q10 | Where does the socket path come from, and what is it on macOS and Windows? `$XDG_RUNTIME_DIR` is not universal. |

---

## 11. Behavioural contract

- `aeon version` prints version, commit and Go version. It requires **no config file** (Q4).
- `aeon serve` prints the socket path then the store path (B10), creates the socket `0600` (D3), accepts
  connections (D4), and on SIGTERM shuts down cleanly and **unlinks the socket** (D2).
- A second `aeon serve` **refuses**, naming the socket and the holding pid (O5).
- `aeon serve --print-services` prints exactly the committed list (D5).
- `make dev` starts both, forwards `Ctrl-C` to both (D8), and refuses with a named prerequisite when a
  toolchain is missing (O3).
- The app displays the **daemon's** version, read over the socket (O4).

---

## 12. How to decompose

- **R1** — every task is gated by at least one **named test** or CI job.
- **R2** — a task that adds behaviour adds its test in the same task.
- **R3** — a file in more than one task's `files_expected` needs a stated reason.
- **R4** — **the handshake is the first task**, alone. `aeon/` arrives made by hand; everything else edits it,
  and a task failing against an assumed starting state fails in a way nobody can attribute.
- **R5** — explicit registration (B8) is its own task, and it ends with D5's committed fixture.
- **R6** — `cgo` and the six-target build is its own task, because it is the slice's real risk and it must
  fail visibly rather than inside someone else's task.
- **R7** — no task over roughly 300 changed lines, T1 included. The old exemption existed because T1 was a
  fork; it is now a handshake, so the rule applies to every task in the slice.
- **R8** — the Tauri app is its own task. Three toolchains in one task is three ways to be blocked.
- **R9** — task files contain no deliberation: no "hmm", "let me", "wait", "on reflection", "I think",
  "not sure", "FIXME". `TODO(engineer):` in a code skeleton is the one permitted form.
- **R10** — a task carries no open question.
- **R11** — the Makefile is edited by exactly one task; it is a serialization point.
- **R12** — the final task walks `DEMO.md` end to end and fixes what does not match.

## 13. Pre-dispatch self-check

### A. Mechanical

| # | Run | Pass condition |
| --- | --- | --- |
| A1 | `grep -rniE "\bhmm\b\|\blet me\b\|\bon reflection\b\|\bFIXME\b\|\bwait\b\|\bI think\b\|\bnot sure\b" tasks/` then `grep -rn "TODO" tasks/ \| grep -v "TODO(engineer)"` | Both empty (R9, R10) |
| A2 | List every task with `est_changed_lines` and file count | Nothing over ~300 lines (R7) |
| A3 | List every task with the test or CI job that gates it | Every task has one (R1) |
| A4 | List every file in more than one task's `files_expected` | Each justified (R3) |
| A5 | Paste the enumerated output of `bootstrap.Register` today (Q2) | The actual list, from the source — it becomes D5's fixture |
| A6 | Paste the answer to Q1 | **If the socket transport does not exist yet, say so explicitly** — S2 blocks on it and the plan must not imply otherwise |
| A7 | Paste the answers to Q3, Q5 and Q6 | Three answers, from running it, not from reading it |
| A8 | Name the registration seam and where the pinned version is recorded (Q7) | Both named. If there is no seam, the finding is raised rather than worked around. A floating version means the daemon changes when nobody asked, so it cannoake an upstream fix, which is the entire cost of B1 |
| A9 | `make fmt` then `git status --porcelain` | No output |

### B. Scope

| # | Check | Ref |
| --- | --- | --- |
| B1 | Confirm no task implements a protocol method | N1 |
| B2 | Confirm no task implements a socket listener | N2 |
| B3 | Confirm no task calls a model, creates a table, or adds a tool | N3 |
| B4 | Confirm no task adds `init`, presets or `doctor` | N4 |
| B5 | Confirm no task mounts HTTP | N5 |
| B6 | Confirm no task edits `go-backend-scaffolding` | N8 |
| B7 | Confirm no task writes outside `aeon/` | I1 |
| B8 | Quote the `out_of_scope` lines covering N1–N8 | §8 |

### C. Acceptance

| # | Check | Ref |
| --- | --- | --- |
| C1 | Map each of D1–D12 to a task and a named test or CI job | §2 |
| C2 | For D1, confirm all six binaries are **built and run** in CI, not merely configured | O2 |
| C3 | For D3, confirm the permission check is a `stat` assertion | D3 |
| C4 | For D5, name the committed fixture file | I5 |
| C5 | For D8, confirm the assertion is a process count before and after | D8 |
| C6 | For O4, confirm the app reads the version **over the socket** rather than from a constant | O4 |

### D. Ordering

| # | Check | Ref |
| --- | --- | --- |
| D1 | Confirm the handshake is the first task, alone | R4 |
| D2 | Confirm the trim is its own task ending in the fixture | R5 |
| D3 | Confirm the six-target build is its own task | R6 |
| D4 | Confirm the Tauri app is its own task | R8 |

### E. Structure

| # | Check | Ref |
| --- | --- | --- |
| E1 | Confirm exactly one task edits the Makefile | R11 |
| E2 | Confirm one `cmd/` | I4 |
| E3 | Confirm no test binds the real socket path | I6 |

### F. Project-specific

| # | Check |
| --- | --- |
| F1 | State the socket path on Linux, macOS and Windows (Q10). |
| F2 | State the `cgo` cross-compilation approach and where it is recorded (Q8, D11). |
| F3 | State how `make dev` stops both processes (Q9). |
| F4 | Confirm a second `aeon serve` refuses, naming the pid (O5). |
| F5 | Confirm `aeon version` works with no config file present (Q4). |

## 14. The demo contract

> **Present in every slice of this plan.** A slice is not done when its tests pass. It is done when someone
> who did not build it can follow `DEMO.md` and watch the thing work.

[`DEMO.md`](DEMO.md) is a deliverable of this slice, and the final task (R12) walks it end to end on a
clean checkout and fixes whatever does not match. Two rules:

- **Every step states what you should see**, concretely enough to be wrong. "It should start correctly" is
  not a demo step.
- **Every step states what would mean it failed**, where that is not obvious. D3's permission check is the
  example: everything appears to work with a world-writable socket, and the demo has to say so.

`DEMO.md` also states what the slice does **not** show. S1's says the protocol, the model, the store and
the UI's capability are all absent — so nobody reads the garage as the skateboard.
