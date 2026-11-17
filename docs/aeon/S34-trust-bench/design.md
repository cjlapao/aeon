# Design — S34 Trust bench

Fills in `brief.md`: the four method shapes (§2), the two pages (§3–§4), the probe (§5), the test map (§6).
Rules are numbered **TB1–TB12** and are cited from `plan.md` tasks the way S30's CG1–CG9 are.

## TB1 — One door per fact

Every number, name and verdict on both pages arrives through exactly one daemon method (§2), and the
renderer never touches `plugin_state`, `LoadConfigFile`, or the CLI to get it (brief §6.1). The CLI command
is displayed as *text* for comparison, never executed or parsed.

## TB2 — Three states, no invented zeros

Every fact field is a three-state union at the type level, not by convention:

```ts
type Fact<T> = { state: "ok"; value: T } | { state: "unwired" } | { state: "error"; message: string };
```

`unwired` is what a method-not-found or an idle controller yields; `error` carries the daemon's message
verbatim. "There are 0 servers" and "we could not ask" are different pixels (brief §4); only
`{ state: "ok", value: [] }` may render the empty.

## 2. The four daemon methods

All four are reads on the existing socket, public-window like `policy.*` (a read, like the S15 plugins
reads at `method.go:541`'s comment). Names join the table in `protocol/method.go` after the `policy.*`
block; handlers live beside `protocol/policy.go`'s precedent, one file per subject; each ships a
conformance fixture (brief §6.6).

### 2.1 `agent.list` — the roster as identity

```jsonc
{ "agents": [
  { "name": "reviewer", "provenance": "first_party",   // | "imported"
    "state": "enabled",                                 // | "blocked" | "never_arrived"
    "version": "1.4.0", "capabilities": ["code_review"] } ] }
```

Same source the CLI's `aeon agent list --provenance --all` reads (`store/plugin_state.go` + the first-party
set), same semantics for `never_arrived` (known but never checked in). No cost, no task rows — that is
S33/S14 territory (brief §7).

### 2.2 `tools.grants {agent}` — Registered is not granted, in one read

```jsonc
{ "agent": "reviewer",
  "declared":  ["read_file", "mcp.github.list_issues"],
  "granted":   ["read_file"],
  "registered_ungranted": ["search_code"],               // registered by the tool layer, not granted here
  "unregistered": ["mcp.github.list_issues"] }           // declared but nothing answers for it
```

The four buckets are the S11 evaluator's own inputs (`internal/policy/evaluate.go`) joined with
`internal/mcp/grants.go` — the page renders exactly these buckets (TB7); the *distinction* is the product.
Unknown agent → method-level error with the roster's names, never an empty-everything answer (TB2 again,
daemon-side this time).

### 2.3 `mcp.status` — the config truth, refusal-included

```jsonc
{ "config_path": "/home/u/.config/aeon/mcp.json", "present": true,   // absent is a state (MC11)
  "servers": [
    { "name": "github", "tier": "trusted",                            // classify.go's tiers
      "transport": "stdio", "command": "gh-mcp", "declared_by": ["reviewer"],
      "refused": null },
    { "name": "scratch", "tier": "untrusted", "transport": "http",
      "url": "127.0.0.1:9100", "declared_by": [], "refused": "not on the loopback tier (config validation)" } ],
  "probes": { "github": { "ok": true, "tools": 12, "ms": 480, "checked_at": "2026-10-26T14:02:11Z" } } }
```

Pure `LoadConfigFile` + `classify.go` + the validation that produces refusals — no dialing here; `probes`
is the cache §5 maintains, so a page open shows last-known truth with its age, never a fake live badge.

### 2.4 `mcp.check {server}` — one probe, one answer

```jsonc
{ "server": "github", "ok": true, "tools": [ {"name": "list_issues", "class": "read"} ],
  "ms": 480, "checked_at": "2026-10-26T14:02:11Z" }
// or the honest failure shape, same fields:
{ "server": "scratch", "ok": false, "error": "unreachable: exit 127", "ms": 3, "checked_at": "…" }
```

## 3. AgentsPage — the readiness view

One card per agent, composed of four strips, each with its TB2 states and its CLI twin as quiet text:

1. **Identity** — name, provenance badge (`first-party` / `imported`), state (`enabled` / `blocked` /
   `never-arrived`, never rendered as merely "off"), version.
2. **Tools** — the four buckets from `tools.grants` (TB7): `granted` solid, `registered not granted`
   outline, `declared not registered` dashed with the refusal reason from config validation where one
   exists, `declared-mcp` tagged by tier. Never a merged list — the merging is exactly what S11 forbids.
3. **MCP declares** — the servers naming this agent in `mcp.status.declared_by`, each with its last probe
   chip (§4).
4. **Guard verdicts** — `policy.inForce` scoped to the agent subject, plus per-decision `policy.why` on
   expand; this is why the row sits *after* Policy in the menu (brief §4).

Header: roster count by state; a `Refresh` that refetches the reads (refetch, not dial — TB9).

## 4. McpPage — config truth + the Check button

A table of configured servers (rows = `mcp.status.servers`): name, tier badge, transport + target, who
declares it, last probe chip, **Check** button.

- **Chip vocabulary, verbatim:** `ok · 12 tools · 480ms · checked 14:02` / `unreachable: exit 127 · checked
  14:02` / `timed out after 5s · checked 14:02` / `not checked` / `checking…` — the last two with no number
  where a number would be invented (TB2).
- **Check** calls `mcp.check {server}`; while in flight, the button and chip are shared per server across
  the page (TB8's single-flight surfaces in the UI too). Results expand to the cached tool list with classes.
- **Refused rows** render inline and explained — the refusal string comes from `mcp.status`, quoting config
  validation; a refused server has no Check button (there is nothing the daemon will dial).
- **Absent config (MC11)**: the legitimate empty with the exact `config_path` looked at — this row is the
  difference between a slice that understands S12 and one that fights it.

## 5. The probe (TB8, TB9) — `internal/mcp` gains `Probe(server)`

- **One implementation.** `Probe` wraps the same dial path `mcp list --agent` uses in `mcp/client.go`; the
  CLI is refactored to call it too, so reference and app door cannot drift (brief §7). It performs
  `initialize` + `tools/list` and *structurally cannot* issue `tools/call`: the probe builds no call request
  (asserted against a fake transport, brief §6.2).
- **Amended 2026-10-26, before seeding (the S36 seam ruling).** The daemon's `mcp.*` handlers do **not**
  import `internal/mcp` — `absent_test.go`'s banned list names `internal/daemon` explicitly, and it survives
  unamended. `Probe` reaches the daemon through a **function seam in `daemon.Options` implemented in
  `cmd/aeon`** (the sanctioned importer), exactly the pattern
  [`S36-mcp-dispatch/design.md`](../S36-mcp-dispatch/design.md) MD1 defines for step dispatch; one pattern,
  both slices. A function value carries no import; MC11 keeps its exact edge.
- **Single-flight per server name**: a second `mcp.check` while one is in flight returns the in-flight
  result, not a second process (brief §6.2).
- **Bounded timeout** (5 s default, from the config's own timeout field when present), timeout recorded as
  the honest error shape (2.4).
- **Cache**: last result per server with `checked_at`, in daemon memory, surfaced by `mcp.status.probes`.
  Restarting the daemon returns `not checked`, never a stale lie.
- **Nothing else triggers a probe** — not `mcp.status`, not page open, not refresh (brief §3; TB9).

## 6. Test map

| # | Test | Arbiter for |
| --- | --- | --- |
| 1 | fake-transport probe: fails on any `tools/call`; timeout; single-flight; cache+timestamp | brief §6.2, TB8/9 |
| 2 | conformance fixtures ×4 under `protocol/conformance/routes/` | brief §6.6 |
| 3 | `mcp.status` vs absent config file → `present:false` + path (MC11) | brief §6.4 |
| 4 | `tools.grants` unknown agent → error naming valid agents, never empty-everything | TB2 daemon-side |
| 5 | AgentsPage/McpPage render vs idle controller: no plausible zeros; `not checked`/`unreachable` shapes | brief §6.3 |
| 6 | probe-cache chip renders `checked_at` age; restart clears to `not checked` | TB9 |
| 7 | deletion drill: remove McpPage section on a scratch branch → one commit, suite green | brief §6.5 |
| 8 | `product.test.tsx`: both menu items navigate; bench nav pins updated | bench regression |

## TB7 — The four buckets are the product
`tools.grants` returns declared / granted / registered-ungranted / unregistered as *separate* fields and
AgentsPage renders them separately; any UI that merges them into one list fails review. Registered is not
granted is S11's rule; the page exists to make people see it.

## TB8 — Probe discipline
Operator-click only; per-server single-flight; bounded timeout; cached with timestamp; never `tools/call`;
same `mcp/client.go` code path as the CLI. Each clause is a test row above, not an intention.

## TB9 — No probe side effects
Page open, refresh, `mcp.status`, and daemon startup never spawn a probe. The only spawn trigger is the
Check button's method call.

## TB10 — CLI twin as text
Each fact shows its `aeon …` equivalent as copyable text (drift between doors is a bug report, TB1's
comparison mandate); the app never shells out (P34 inversion is permanent).

## TB11 — Menu position is load-bearing
`agents` and `mcp` sit immediately after `policy` in `BENCH_PAGES`; the order *is* the authorization story
(Policy → Agents → MCP), and the deletion property (brief §4) depends on each section owning exactly one
row, one page file, one dispatch branch.

## TB12 — Old-daemon harmlessness
With `BENCH_PAGES` rows present and a daemon lacking the methods, both pages render TB2's `unwired` states
and the disconnected third state — shipping the rows before every daemon updates must be boring.
