# Plan — S34 Trust bench

## What is settled before planning

Everything in `brief.md` (§3's four methods, §4's deletion property and three-state rule, §6's arbiters,
§8's risks) and `design.md`'s TB1–TB12. In particular: daemon methods are the deliverable and the pages are
thin clients (P34, TB1); `Fact<T>` three states are a type, not a convention (TB2); the four buckets of
`tools.grants` are returned and rendered separately (TB7); the probe is one `mcp/client.go` path shared with
the CLI, click-triggered, single-flight, timeout-bounded, cache-with-timestamp, structurally incapable of
`tools/call` (TB8/TB9).

## How we will do it

Daemon first, pages second — the order is the doctrine (brief §0): T1–T4 land the four methods with their
fixtures before T6/T7 render a single fact, so the pages are written *against* a surface that already
answers, and every page fact has a method to blame. The probe (T4) is the only genuinely new daemon
behavior and gets the most test mass; everything else is exposure of already-proven code, which is why the
conformance fixtures are load-bearing rather than ceremony — they are what "the app can now ask" means.

`mcp.status` (T3) lands before `mcp.check` (T4) on purpose: the page first shows the config truth the CLI
already shows, and only then gains the one thing the CLI has that no door had before the probe — and after.

## Open questions this plan answers

- **Q1** (does the CLI's dial path refactor cleanly into `Probe`?) — T4's first step, before new code:
  extract, re-run `aeon mcp list --agent`'s existing tests green. If the extraction fights, the probe ships
  as a thin wrapper calling the same internal functions rather than a rewrite — decided by T4's diff, not
  in advance.
- **Q2** (what does `never_arrived` mean exactly in `plugin_state.go`'s terms?) — T1 reads the real state
  machine and records it in the fixture; the UI copies the fixture's vocabulary, not this plan's guess.
- **Q3** (which of the twelve existing bench pages already carries a third-state pattern to reuse?) —
  T5 inventories Status/agent-chat's disconnected handling and *reuses* it; a third bespoke empty is a
  defect (brief §4).

## Task sequence

### T1 — `agent.list`: names, provenance, state ([`tasks/T1.md`](tasks/T1.md))

Add `MethodAgentList` to `protocol/method.go` beside the `policy.*` block; handler in a new
`protocol/agents.go` reading exactly what `aeon agent list --provenance --all` reads
(`store/plugin_state.go` + the first-party set). Answers Q2 with the fixture as the record. Conformance
fixture `routes/agent-list.json`. Test: state vocabulary matches the store's, never collapses to on/off.

### T2 — `tools.grants {agent}`: the four buckets ([`tasks/T2.md`](tasks/T2.md))

Join the S11 evaluator's inputs (`internal/policy/evaluate.go`) with `internal/mcp/grants.go` into
`design.md` §2.2's shape — declared / granted / registered-ungranted / unregistered as separate fields
(TB7). Unknown agent is a method-level error naming the valid agents (test map #4). Fixture
`routes/tools-grants.json`. **Depends: T1** (shares the roster read).

### T3 — `mcp.status`: config truth, refusal-included, probes as cache ([`tasks/T3.md`](tasks/T3.md))

Expose `mcp.LoadConfigFile` + `classify.go` + config-validation refusals per `design.md` §2.3; `present:
false` + `config_path` for the MC11 empty (test map #3). No dialing anywhere in this task. Fixture
`routes/mcp-status.json`. **Depends: nothing** — parallel-safe with T1/T2.

### T4 — `Probe(server)` in `internal/mcp`, and `mcp.check` ([`tasks/T4.md`](tasks/T4.md))

First the Q1 extraction: pull the dial/`initialize`/`tools/list` path in `mcp/client.go` into
`Probe(server)` and make `aeon mcp list --agent` call it — its existing tests are the gate. Then the method:
single-flight per name, 5 s default timeout (config's own timeout field when present), result cache with
`checked_at`, surfaced by `mcp.status.probes` (T3's field). **The probe-never-calls test (fake transport
fails on `tools/call`), timeout, single-flight and cache tests all land in this task** — test map #1, TB8.
Fixture `routes/mcp-check.json`. **Depends: T3** (shares the cache surface).

### T5 — Menu rows, dispatch branches, third state ([`tasks/T5.md`](tasks/T5.md))

`routes.ts`: `agents` and `mcp` rows immediately after `policy` (TB11); `TestBench.tsx`: two dispatch
branches with the boundary wrapper; both pages exist as honest empties carrying the disconnected state
(TB12, Q3's reuse — no third bespoke empty). `product.test.tsx` nav pins updated. **Depends: nothing
daemon-side; safe to start beside T1–T3.**

### T6 — AgentsPage: the readiness view ([`tasks/T6.md`](tasks/T6.md))

The four strips of `design.md` §3 on `agent.list` + `tools.grants` + `mcp.status.declared_by` +
`policy.inForce`/`policy.why`; every cell a `Fact<T>` (TB2), every strip's CLI twin as text (TB10), the
four buckets visually distinct and never merged (TB7). Render test against an idle controller (test map #5).
**Depends: T1, T2, T5.**

### T7 — McpPage: table, chips, Check button ([`tasks/T7.md`](tasks/T7.md))

`design.md` §4 verbatim: the chip vocabulary with its two numberless states, per-server shared in-flight
button (TB8's UI half), refusal rows without a Check button, MC11 empty quoting the path. Render +
interaction tests (test map #5, #6). **Depends: T3, T4, T5.**

### T8 — Close-out: the deletion drill, the P34 grep, the docs ([`tasks/T8.md`](tasks/T8.md))

Scratch-branch deletion drill (test map #7) proving brief §4 in git, not prose; the P34 grep (brief §6.1)
recorded in the task notes; `STATUS.md`/`ROADMAP.md` rows flipped to built. **Depends: T1–T7.**

## Sequencing summary

```
T1 ──► T2 ──┐
T3 ──► T4 ──┼──► T6 (agents)   T7 (mcp)   ──► T8
T5 ─────────┘
```

T1/T3/T5 are parallel; the daemon tier (T1–T4) completes before either page tier reads a fact (T6/T7); T8
closes. Eight tasks, one new daemon behavior (T4), everything else exposure of already-proven code.
