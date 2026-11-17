# Plan — S12 MCP client

> **Source material:** [`design.md`](design.md) · the roadmap's [§5.3](../ROADMAP.md) ·
> N1–N4 and N7–N11 in [`docs/_archive/aeon-v1-horizontal/12-mcp-adapters`](../../_archive/aeon-v1-horizontal/12-mcp-adapters/design.md).

**Implements:** [`design.md`](design.md) · **Demo:** [`DEMO.md`](DEMO.md)
**Stage:** 3 · Car, third of eight. Depends on **S10**, **S11**. **Blocks S13–S15.**

---

## What is settled before planning

**A grant is a `ToolAccess` policy** (MC4). S10 built the evaluator, the authorship rule and the record, so
this slice adds **no ACL, no allow-list file and no per-server permission list.** That is the single
largest thing this plan does not contain.

**Classes already mean something.** S11 defines re-run and effect classes; this slice *requires* them and
invents nothing.

**The transport is already understood.** MCP is JSON-RPC 2.0 and S2 speaks it — so Q1 is a dependency
question, not a protocol one.

## How we will do it

**The registry first** (T1), because MC2 is a property of its shape rather than of a check. `ToolsFor(agent)`
returns what that agent may see and **there is no "all tools" function to filter later** — build it that
way before any transport exists, so a shared list never has a chance to appear.

**Then the client** (T2), then **classification** (T3) — and classification before any tool is callable,
because MC6 refuses the unclassified and a tool that was callable first is a tool somebody will default.

**Then the two untrusted paths** (T4). Descriptions and results together, in one task, because they are one
rule and splitting them is how MC7 gets satisfied for payloads and forgotten for prose.

**Then the tiers** (T5, T6), **the policy wiring** (T7), **degradation** (T8), **deletability** (T9), the
walk (T10).

**The ordering that matters:** T1 before T2. A registry designed around an existing client acquires the
client's shape — and the client's natural shape is "here are all the tools from all the servers", which is
exactly what MC2 forbids.

## Open questions this plan answers

| # | Question | How |
| --- | --- | --- |
| Q1 | SDK or hand-rolled | T2, with the dependency tree in front of you |
| Q2 | The per-call timeout | T8, a number with reasoning |
| Q3 | Public config shape | T1, chosen so a mis-tiered server is hard to write |
| Q4 | **Classification from our config or the server?** | T3 — **and the security answer is our config** |

---

## Task sequence

| # | Task | Gate |
| --- | --- | --- |
| **T1** | `registry.go` — the three tiers, and no list of everything | B cannot see A's private tool; no all-tools function exists |
| **T2** | `client.go` — stdio and HTTP, configured | A configured server's tools appear; nothing is discovered |
| **T3** | `classify.go` — both classes, required | An unclassified tool is refused, naming it |
| **T4** | `untrusted.go` — descriptions **and** results | A description is labelled in the assembled prompt |
| **T5** | Private namespaces | `unknown tool`, never `denied` |
| **T6** | The public registry | Two agents, one tool, both callable |
| **T7** | Grants are `ToolAccess` policies | Revoking the policy removes the tool |
| **T8** | Timeouts and degradation | A hanging server does not hang the step |
| **T9** | The level-0 invariant | The suite passes with no MCP configuration |
| **T10** | Walk the demo, write the QA suite | `DEMO.md` runs as written |

### T1 — `registry.go`: the three tiers
- engineer: golang-engineer-loop
- tech: go
- depends_on: []
- est_changed_lines: ~300
- detail: tasks/T1.md
- success_criteria:
  - `ToolsFor(agent)` returns core-granted, public-declared and private-configured tools
  - **no function returns every tool across agents** — asserted by grep
  - unit test `agent B's list does not contain agent A's private tool` passes
  - unit test `a tool's tier comes from its configuration, not the caller` passes
  - Q3 answered: the config shape, chosen so a mis-tiered server is hard to write
- files_expected: `aeon/internal/mcp/registry.go`, `aeon/internal/mcp/config.go`, `aeon/internal/mcp/registry_test.go`

### T2 — `client.go`: stdio and HTTP
- engineer: golang-engineer-loop
- tech: go
- depends_on: [T1]
- est_changed_lines: ~320
- detail: tasks/T2.md
- success_criteria:
  - stdio and HTTP transports (N7); a configured server's tools are listed
  - unit test `nothing is discovered` passes, asserted by grep — no scan, no auto-registration
  - unit test `two servers offering search are both reachable` passes (MC8)
  - Q1 answered in the notes: SDK or hand-rolled, with the reason
  - **tests run against a fake server, never a real one**
- files_expected: `aeon/internal/mcp/client.go`, `aeon/internal/mcp/client_test.go`, `aeon/internal/mcp/testdata/fakeserver/**`

### T3 — `classify.go`: both classes, required
- engineer: golang-engineer-loop
- tech: go
- depends_on: [T2]
- est_changed_lines: ~220
- detail: tasks/T3.md
- success_criteria:
  - every tool carries a re-run class and an effect class before it is callable
  - unit test `an unclassified tool is refused, naming the tool` passes
  - unit test `no default classification exists` passes, asserted by grep for a `default:` arm
  - **Q4 answered: classification comes from our config; a server's own claim may not lower a class**
- files_expected: `aeon/internal/mcp/classify.go`, `aeon/internal/mcp/classify_test.go`

### T4 — `untrusted.go`: descriptions and results
- engineer: golang-engineer-loop
- tech: go
- depends_on: [T2]
- est_changed_lines: ~240
- detail: tasks/T4.md
- success_criteria:
  - unit test `a tool description is labelled untrusted in the assembled prompt` passes
  - unit test `a tool result is labelled untrusted` passes
  - unit test `the label survives a description containing the label text` passes
  - **S11's `W23` is amended to cover descriptions, in this task**
- files_expected: `aeon/internal/mcp/untrusted.go`, `aeon/internal/mcp/untrusted_test.go`, `docs/aeon/S11-permissions/design.md`

### T5 — Private namespaces
- engineer: golang-engineer-loop
- tech: go
- depends_on: [T3, T4]
- est_changed_lines: ~240
- detail: tasks/T5.md
- success_criteria:
  - unit test `a peer calling a private tool gets unknown, not denied` passes
  - unit test `a private tool never appears in a peer's prompt` passes
  - unit test `the refusal does not reveal that the tool exists` passes
- files_expected: `aeon/internal/mcp/registry.go`, `aeon/internal/mcp/private_test.go`

### T6 — The public registry
- engineer: golang-engineer-loop
- tech: go
- depends_on: [T5]
- est_changed_lines: ~200
- detail: tasks/T6.md
- success_criteria:
  - unit test `two agents declaring one public tool can both call it` passes
  - unit test `an all-agents ToolAccess deny closes the public door` passes — with no code change
  - unit test `an outward public tool still asks at use time` passes
- files_expected: `aeon/internal/mcp/registry.go`, `aeon/internal/mcp/public_test.go`

### T7 — Grants are `ToolAccess` policies
- engineer: golang-engineer-loop
- tech: go
- depends_on: [T6]
- est_changed_lines: ~220
- detail: tasks/T7.md
- success_criteria:
  - the registry asks S10's evaluator; **no ACL exists in this package** — asserted by grep
  - unit test `revoking the ToolAccess policy removes the tool` passes
  - unit test `there is exactly one way to revoke` passes
  - unit test `the client grants nothing the policy did not` passes (MC10)
- files_expected: `aeon/internal/mcp/registry.go`, `aeon/internal/mcp/grant_test.go`

### T8 — Timeouts and degradation
- engineer: golang-engineer-loop
- tech: go
- depends_on: [T2]
- est_changed_lines: ~220
- detail: tasks/T8.md
- success_criteria:
  - unit test `a hanging server does not hang the step` passes
  - unit test `a timeout is distinguishable from an empty result` passes
  - unit test `a dead server degrades the tool list, not the step` passes
  - Q2 answered: the timeout as a number, with reasoning
- files_expected: `aeon/internal/mcp/degrade.go`, `aeon/internal/mcp/degrade_test.go`

### T9 — The level-0 invariant
- engineer: golang-engineer-loop
- tech: go
- depends_on: [T7, T8]
- est_changed_lines: ~160
- detail: tasks/T9.md
- success_criteria:
  - **the whole suite passes with no MCP configuration at all** (MC11)
  - unit test `an agent with no tools runs a step` passes
  - unit test `no package outside internal/mcp imports it unconditionally` passes
- files_expected: `aeon/internal/mcp/absent_test.go`, `aeon/internal/runtime/loop.go`

### T10 — Walk the demo, write the QA suite
- engineer: technical-writer-loop
- tech: docs
- depends_on: [T9]
- est_changed_lines: ~200
- detail: tasks/T10.md
- success_criteria:
  - `DEMO.md` runs end to end against the fake server; anything undocumented is added
  - `docs/test_suites/aeon-mcp.md` is **amended**, not replaced — it exists for v1's adapters
  - the suite covers invisibility, the unknown-vs-denied distinction, untrusted descriptions, and deletability
- files_expected: `docs/aeon/S12-mcp-client/DEMO.md`, `docs/test_suites/aeon-mcp.md`

---

## Files

| File | Task | Notes |
| --- | --- | --- |
| `internal/mcp/registry.go` | T1, T5, T6, T7 | **four tasks; strictly in that order** |
| `internal/mcp/config.go` | T1 | — |
| `internal/mcp/client.go` | T2 | — |
| `internal/mcp/classify.go` | T3 | — |
| `internal/mcp/untrusted.go` | T4 | — |
| `internal/mcp/degrade.go` | T8 | — |
| `internal/runtime/loop.go` | T9 | the optional seam |
| `docs/aeon/S11-permissions/design.md` | T4 | the `W23` amendment |

**`registry.go` is the serialization point**, and it is touched by four tasks because it *is* the security
object. T1 establishes the shape, T5 and T6 add tiers, T7 replaces its grant source with S10's evaluator.
**No two run concurrently.**

## Ordering rationale

**T1 before T2.** A registry designed around an existing client acquires the client's shape — and a
client's natural shape is *"all the tools from all the servers"*, which is exactly what MC2 forbids. Build
the thing with no all-tools function before anything can offer one.

**T3 before T5.** A tool must be unclassifiable-and-refused before it is private-and-callable; otherwise
the first unclassified tool arrives when private visibility is also in flight and the failure is two
things at once.

**T4 with both halves in one task.** Descriptions and results are one rule. Split, MC7 gets satisfied for
payloads and forgotten for prose — which is the exact failure the design calls the largest cost of this
slice.

**T9 near the end, deliberately.** Deletability is only meaningful once there is something to delete.

## What this slice does not do

No MCP server (S19). No cards, skills or Tasks (S14). No authoring UI. No credential storage design. No
sampling, roots or elicitation. No retrieval over tools (S22, dispatch closed by A13).
