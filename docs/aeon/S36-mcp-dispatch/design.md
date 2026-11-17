# Design — S36 MCP dispatch

**One sentence:** MCP tools enter a step inside `BuildTools`, behind `runtime.ToolCaller`, through a
function seam injected by `cmd/aeon` — so `internal/daemon` never imports `internal/mcp`,
`absent_test.go` is never amended, and a step cannot tell which half of its tools came from a server.

Everything below is settled before the first line of code; `plan.md` sequences it, nothing decides it.

## MD1 — The seam: a function value, typed in `daemon`, implemented in `cmd/aeon`

```go
// internal/daemon — no import of internal/mcp; stdlib + model + runtime only.
type MCPCall      func(ctx context.Context, qualified string, args json.RawMessage) (string, error)
type MCPWithheld struct{ Qualified, Sentence string }
type MCPBundle struct {
    Specs    []model.ToolSpec // offered tools ONLY, with the server's LIVE schema
    Call     MCPCall          // dispatch for exactly those names
    Withheld []MCPWithheld    // every non-offered candidate + grants.go's sentence
    Close    func() error     // close cached server children; idempotent
}
// daemon.Options gains:
StepMCP func(ctx context.Context, agent string, frozen runtime.FrozenManifest, at time.Time) (*MCPBundle, error)
```

`BuildTools` calls the seam (nil ⇒ no bundle, step unaffected — the *unwired* honest state, `daemon.go:99`
in spirit), and returns a **composite caller**: names ∈ `Bundle.Specs` route to `Bundle.Call`, everything
else routes to the first-party `toolCaller` unchanged. The seam's *implementation* lives in `cmd/aeon`
(the sanctioned importer, beside `mcp.go`), wired at the one `serve.go:154` site. **A function value
carries no import** — `absent_test`'s banned list, `internal/daemon` included, survives with **zero diff
lines** (criterion 5).

## MD2 — The offer algorithm (inside the `cmd/aeon` implementation) — zero new verdict logic

```
cfg      := mcp.LoadConfigFile()                          // missing file = empty, MC8/MD8
reg      := mcp.NewRegistry(cfg)
views    := reg.GrantView(agent, frozenEntries, at)        // THE double gate, S12's own — called, not copied
offered  := views where State == GrantAvailable            // §3's ruling == grants.go's existing filter
asked    := views where State == GrantAsk                  // carried as Withheld with their sentence
// per server S holding ≥1 offered tool, in parallel with MD4's budget:
c        := mcp.Dial(ctx, S, getenv)                       // budget ⇒ timeout ⇒ withhold the server
live     := c.ListTools(ctx)
specs    := for each offered tool t on S: t ∈ live ? spec(t.Qualified, liveInputSchema) : withheld("no longer listed")
```

- **`GrantView` is called, never re-implemented** — visibility × authority in one function, refusal
  sentences already formatted (MD9). The offer filter `State == GrantAvailable` *is* ruling §3; a second
  intersection would be a second truth, and MC10's sentence already exists (grants.go:99).
- **Specs carry the live schema.** No synthesized schemas, ever: a tool the server stopped listing is
  withheld, not promised from stale config. **The disagreement always costs the call, never gains one**
  (step.go's own comment, applied one layer down).
- Core tools: excluded from the bundle (`TierCore` exempt — "S12 does not re-decide core").

## MD3 — Dispatch: the existing gate stays the only decider

`runTool` asks the policy gate with `Tool: use.Name` (loop.go:619 — handler-stamped, never tool input);
a qualified `server.tool` name is just a name to this machinery. The T4 obligation is **proving**, not
building: (a) an `ask` (by row or L22) suspends on the **existing** `pendingPermission` path —
`approve` resumes, unattended denies at once (S11 verbatim); (b) a corpus flip between build and dispatch
is caught because dispatch decides on the **frozen** set (D12), the same set the offer used; (c) the
composite's `Call` is reached only for offered names — a forged name (model inventing `srv.evil`) lands in
the first-party path's unknown-tool error, which is the correct refusal.

## MD4 — The budgets, named where they live

One pair of constants in the `cmd/aeon` implementation, each pinned by a test that measures it:

| Constant | Default | Fails as |
| --- | --- | --- |
| `mcpDialBudget` (build, per server, parallel) | **3 s** | withheld entry + step starts anyway |
| `mcpCallDeadline` (dispatch, per call) | **60 s** | error-text `tool_result`, step continues |

The degradation law (brief §4): **degrades, never hangs, never promises.** `mcp/client.go` is already
ctx-first (`Dial/ListTools/CallTool` all take ctx) — the budgets are ctx deadlines, no client edits.
Numbers are defaults to be *measured* in T3/T4 notes, not folklore.

## MD5 — Lifecycle: connections are children, steps are their lifetime

The bundle caches **lazily-dialed per-server connections** for exactly one step run; `Close` (client's
own — kill + reap) is wired at **all four** `BuildTools` callers via an `io.Closer` check on the returned
composite (`protocol/step.go:380`, `daemon/recover.go:188`, `protocol/step_stop.go:189`,
`protocol/answer.go:79`). **Resume re-dials cold** — P2 says a suspended step has no process, and MCP
connections are processes: after an `ask` park or a crash, the new adapter's connections are new. A test
counts server children before/after a full step incl. stop; the kill-`-9`-mid-call test proves the text
degrade (criterion 4). Daemon shutdown: children ride the daemon ctx (already `newStdioTransport`'s
contract), so nothing outlives the process.

## MD6 — No `AllowToken`, and why said out loud

Tokens guard **model-composed command strings** (`token.go`'s one mint → `spawn.go`'s first statement).
MCP dispatch composes nothing model-authored: command line = operator config (S12-validated, addable only
via S35's proposal + human commit, spawn-verified only after an S34 operator's Check click). The grant
check (§3/MD2) is the authority and already ran through the one gate. A reviewer's "where's the token?"
gets this paragraph, not a new abstraction.

## MD7 — Honesty: withheld is a fact, not a silence

Each withheld entry surfaces once at step start as a structured line on the step's log/events —
`withheld: reddit.search · unavailable: no ToolAccess allow rule grants reddit.search (MC10)` — reusing
the **existing** event channel (additive fields only; the UI ignores unknowns, TB12's old-daemon
harmlessness in reverse). No page changes; whoever wants a column adds one to S34's page later,
one-row-sized, as a leftover. **The empty case renders empty, not zero**: a first-party-only step has no
MCP section at all — no `0` masquerading as an answer (three-state rule).

## MD8 — MC11 is the byte-identity bar, not a vibe

No config file → `LoadConfigFile` empty → `GrantView` core-only → bundle has zero specs, zero dial
attempts → `BuildTools`'s two returns are **byte-identical to today's**. Enforced by: the existing suite
passing untouched, a test that dials **nothing** when no server holds an offered tool (spy on the dial
seam), and criterion 5's zero-diff `absent_test` note. The daemon must stay as MCP-blind as it is now —
that is what makes this slice deletable (MD12).

## MD9 — One sentence per refusal, across all three doors

`mc10Sentence` / `effectAskSentence` have **exactly one definition** (grants.go:96-106's own comment:
*"a refusal whose text depends on which door you asked through is two truths"*). The bundle's withheld
sentences **come from `GrantView` values**, never re-typed. If a genuinely new refusal is needed
(dial-timeout, no-longer-listed), it is **one named string in the `cmd/aeon` implementation**, tested for
exactness — never a paraphrase of MC10 at the call site.

## MD10 — The dot is the router, pinned both sides

`splitQualified` (`config.go:340`) splits on one dot; first-party names are ten dot-less words
(`TestRegistryExactNames`). Composite routing: name contains `.` ⇒ bundle side; else first-party. **Two
new pins:** the registry's names stay dot-free (extend the existing pin's spirit), and a bundle may never
publish a dot-free spec name (seam returns an error if it does — a violation, not a routing accident).

## MD11 — `absent_test` untouched; S34 §5 amended to point here

This slice's CI note carries `git diff --stat` proving **zero lines** changed in
`internal/mcp/absent_test.go`, and re-runs MC11's deletability drill (drop `internal/mcp` + the seam →
daemon builds). **S34's design §5** ("`Probe` wraps `mcp/client.go`" — written before the seam existed,
would have made the daemon import the layer) receives the one-line amendment: the daemon's `mcp.*`
handlers reach the probe through the **same `cmd/aeon`-injected seam pattern** defined here. One pattern,
both slices, banned list intact.

## MD12 — Deletion: three files and one wiring line

Added: seam types + composite (1 file in `daemon`), the `cmd/aeon` bundle implementation (1 file), the
`serve.go` wiring line (1 line). Removed in one commit ⇒ `BuildTools` returns the first-party caller
exactly as today, suites byte-identical. Nothing else in the slice is load-bearing; the tests ride along
with the files. If the slice grows a fourth file, that is a design event, not tidiness.

## Test map — the arbiters, as rows

| # | Row | Proves |
| --- | --- | --- |
| 1 | fake server + config + allow row → real loop step offered `server.tool` w/ live schema → calls → `tool_result` in transcript | brief criterion 1 (the slice is one test) |
| 2 | delete row → not in specs; step record carries mc10 sentence **byte-exact** | criterion 2, MD9 |
| 3 | row `ask` / effect `outward` under allow → `pendingPermission` park → approve → call; unattended → deny at once | criterion 3, S11 verbatim |
| 4 | kill `-9` mid-call → error-text result, step completes < budget; unreachable at build → withheld, step starts | criterion 4, MD4 |
| 5 | zero config → dial spy counts **0**; `BuildTools` outputs identical; `absent_test` zero diff; layer-deletion builds | criterion 5, MC11, MD8 |
| 6 | corpus flip post-build → dispatch still decides frozen set (deny stays denied, allow stays allowed) | criterion 6, D12, MD3 |
| 7 | `write_file`-shaped row vs `server.tool` → canonicalizer identity, no alias confusion | §R4/SEC-059 |
| 8 | child count before/after full step incl. stop + crash-path rebuilds | §R1, MD5 |
| 9 | forged `srv.evil` tool_use (not offered) → first-party unknown-tool refusal, bundle `Call` never reached | MD3's third clause |
