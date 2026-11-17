# Plan — S36 MCP dispatch

## What is settled before planning

Everything in `brief.md` (§3's double gate, §4's degradation law, §5's lifecycle, §6's token answer, the
seven arbiters, the four risks) and `design.md`'s MD1–MD12. In particular: the seam is a function value
typed in `daemon` and implemented in `cmd/aeon` (MD1); the offer **calls** `GrantView` and never copies
it (MD2); dispatch decides on the frozen set through the existing gate (MD3); the budgets are two named
constants (MD4); `absent_test.go` gets **zero diff lines** (MD11); the deletion is three files and one
wiring line (MD12).

## How we will do it

The seam before anything that uses it, the offer before the call, the call before the loop test, and the
loop test before the close-out. **Execution is strictly sequential (the operator's 2026-10-26 direction:
no parallel tasks)** — and linearity is honest here: T3 cannot dial what T2 hasn't made offerable, T4
cannot dispatch what T3 hasn't enumerated. Unlike S34/S35 this slice touches **no page** and adds **no
socket method**, so the whole run is Go + fixtures; the UI never wobbles. S34/S35 precedent holds: each
task lands with its tests and the suites green, and every measured number (budgets, child counts, dial
spies) goes into the task's Notes the day it is measured.

## Open questions this plan answers

- **Q1** (does `Registry` construction have a canonical home in `cmd/aeon` yet, or does each MCP verb
  build its own?) — T2's first step: read `mcp.go`'s verbs, reuse or extract; the seam implementation and
  the CLI verbs must read config through **one** path, or config-drift between doors becomes possible.
- **Q2** (`at` for `GrantView` — build-time `time.Now` with the frozen *set*, matching how
  `daemon/policy_gate.go` picks its instant?) — T2 reads the gate's own choice and uses the same; a step
  must not expire rules on a clock its gate isn't using. Recorded in Notes with the line reference.
- **Q3** (where do the withheld lines ride — `step.started` event's payload or a dedicated log line?) —
  T4 decides with the actual event struct in hand; MD7's additive-only rule constrains it either way, and
  old clients must shrug (TB12's harmlessness in reverse).

## Task sequence

### T1 — The seam and the composite (MD1, MD10, MD8's first half) ([`tasks/T1.md`](tasks/T1.md))

`daemon` gains the seam types (`MCPBundle`/`MCPCall`/`MCPWithheld`) and the `Options.StepMCP` field;
`BuildTools` consults it (nil ⇒ exactly today's behavior, byte-identical returns — test) and returns the
composite caller: dot-routes to `Bundle.Call`, else the first-party caller unchanged; a bundle publishing
a dot-free spec name is a construction **error** (MD10's pin #2). One file in `daemon` + tests; daemon
still imports no MCP vocabulary (vet the import graph in the task notes — `go list` output, not vibes).
**Depends: nothing (first).**

### T2 — The seam implementation: visibility ∩ authority, sentences intact (MD2, MD9) ([`tasks/T2.md`](tasks/T2.md))

First Q1 (one config-read path shared with the CLI verbs) and Q2 (the gate's instant). Then the bundle
factory in `cmd/aeon`: `LoadConfigFile` → `NewRegistry` → `GrantView(agent, frozenEntries, at)` →
offered = `GrantAvailable` only; `ask`/`unavailable`/`refused`/MC6 ride into `Withheld` with
`GrantView`'s sentences **byte-carried**, never re-typed. Unit-tested with config fixtures — no
subprocesses yet: test map #2's sentence-exactness lands here. **Depends: T1.**

### T3 — Live enumeration and the budgets (MD2's dial half, MD4) ([`tasks/T3.md`](tasks/T3.md))

Parallel dials with `mcpDialBudget` over servers holding ≥1 offered tool; live `ListTools` supplies every
schema (no synthesized specs); `!in live` or dial failure ⇒ withheld, with MD9's one named string; a
**dial spy** proves zero dial attempts when nothing is offered (test map #5's first half). Fake-server
tests (S12's conformance pattern) pin timeout → withheld → step starts anyway. **Depends: T2.**

### T4 — Dispatch, degradation, lifecycle (MD3, MD4's call half, MD5) ([`tasks/T4.md`](tasks/T4.md))

`Bundle.Call` → cached `Dial` per server → `CallTool` under `mcpCallDeadline`; every failure shape →
error-text result, step continues (kill-`-9` test, map #4); `Close` wired via `io.Closer` at **all four**
`BuildTools` callers; child-count test (map #8); Q3 decided and implemented (withheld lines ride);
frozen-set flip test (map #6) and forged-name refusal (map #9) land here. **Depends: T3.**

### T5 — The one test, and the edges (map #1, #3, #7) ([`tasks/T5.md`](tasks/T5.md))

End-to-end: fake server + config + operator allow row → **real loop step** offered `server.tool` with the
live schema → calls → `tool_result` in the transcript (map #1). The `ask` park on the **existing**
`pendingPermission` path, approve-and-proceed, unattended-denies-at-once (map #3 — asserting S11's
machinery reused, grepping that no MCP branch was added to `loop.go`). The SEC-059 alias-shaped row
against a qualified name (map #7). **Depends: T4.**

### T6 — Close-out: MC11's proof, the drill, the record ([`tasks/T6.md`](tasks/T6.md))

`absent_test.go` **zero-diff** proof + MC11's layer-deletion drill run and recorded (map #5's second
half); the MD12 deletion drill in a scratch branch (drop the three files + the wiring line → suites
byte-identical); the S34 §5 seam amendment landed (MD11) if not already; measured budgets and dial counts
into Notes (AGENTS.md §4a — measured, not recollected); `DEMO.md` section appended (AGENTS.md §4b — walked
with a §Findings table); `STATUS.md` / `ROADMAP.md` / `PLAN.md` rows flipped to built. **Depends: T1–T5.**

## Sequencing summary

```
T1 ► T2 ► T3 ► T4 ► T5 ► T6
```

Linear by ruling, and linear by nature: the seam, then what it offers, then how it's dialed, then how it
is called and closed, then the loop proves it, then the record. Six tasks; the slice adds **one** seam
type, **one** implementation file, **one** wiring line, **zero** socket methods, **zero** pages, **zero**
new verdict logic (the offer *calls* `GrantView`), and **zero** diff lines in `internal/mcp/absent_test.go`
— which is the whole design in one line: everything new is a door's hinge, nothing new is a wall.
