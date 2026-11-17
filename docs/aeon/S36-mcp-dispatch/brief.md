# Brief — S36 MCP dispatch

**Implements:** the third and load-bearing member of the trust trio — **S34 sees it · S35 authorizes it ·
S36 runs it**. Written 2026-10-26 as **P34's third child** ([`ARCHITECTURE.md`](../ARCHITECTURE.md)): the
slice where an MCP tool a human added, checked and granted finally *executes inside a loop step*.
`design.md` and `plan.md` are written alongside this brief.
**Stage:** 5 · Fleet. **Depends on:** S5 (the step runtime), S11 (the policy gate, `ask`, the frozen set),
S12 (the MCP client, the tier registry, `GrantView`), S13 (granted-list semantics) — **all built code**.
Unlike S35, it edits **no page**: its surfaces already exist (S34's columns predict exactly what this
slice makes true), so it is free to start. **Repo:** Aeon lives at `aeon/` (P24) · Go 1.26 · `cgo` (B3)

---

## 0. The seam discovery that changed this slice's shape

The conversation's first sketch said: *the MCP-backed caller lives in `internal/daemon`, and
`absent_test.go` gets a dated amendment (second instance of S35's grep discipline).* The sketch was wrong
in a way worth recording, because the correction is the slice's architecture:

[`internal/mcp/absent_test.go`](../../../internal/mcp/absent_test.go)'s banned list **names
`internal/daemon` explicitly** — *"explicitly forbidden territory"*, alongside `internal/runtime`,
`internal/tools`, `internal/policy`. Amending it would dull MC11's exact edge, and MC11 is the one
invariant here that must not move: **the layer stays optional; the suite passes with it absent.**

The amendment therefore evaporates. [`cmd/aeon/serve.go:154`](../../../cmd/aeon/serve.go) already
constructs `daemon.Options{}`, and `daemon.go` already lives by *"not wired rather than pretending
otherwise"* (nil seams → honest `ErrNotWired`, `daemon.go:99,106`). The adapter implementation lives in
**`cmd/aeon` — the already-sanctioned importer** — and reaches the daemon as a **function value** in
`Options`. A function value carries no import. `internal/daemon` never learns the word `mcp`;
`absent_test` is **never touched**, and this slice's close-out proves it by byte-diff.

This ruling also retro-fixes a latent collision in
[`S34-trust-bench/design.md`](../S34-trust-bench/design.md) §5, which said the daemon's `mcp.*` handlers
call `mcp.Probe` directly — seeded as written, S34 would have failed its own MC11 test. A one-line
amendment is made there pointing at this design (§MD1). The trio now shares **one** seam pattern.

## 1. What this is

**One sentence:** MCP tools enter a step exactly where first-party tools enter — inside `BuildTools`,
behind `runtime.ToolCaller` — and the loop never learns which half came from a server; but a tool runs
only under the **double gate**: the agent must *see* the server (S12's tier map) **and** a `ToolAccess`
allow row must name the exact qualified tool (S11), with **no row = never runs**.

- **Zero new socket methods.** S34's four reads stay the only `mcp.*`. This slice is engine work: the
  capability it ships is reached through `loop.start`, which already has its door.
- **Zero new pages.** S34's *registered-not-granted* column becomes load-bearing — it now predicts,
  exactly, what a step will be offered. S35's grant proposals become the only way to light one up.
- **Zero new refusal sentences.** Every withheld tool speaks `grants.go`'s own words (MD9), shared by
  definition with `aeon mcp list` — *"a refusal whose text depends on which door you asked through is
  two truths"* (grants.go header), now load-bearing across CLI, view and runtime.

## 2. What exists today, measured — so nobody re-derives it at seeding

- **One choke point, four callers.**
  `BuildTools(granted []string, workspaceRoot, agent string, frozen runtime.FrozenManifest) (runtime.ToolCaller, []model.ToolSpec, error)`
  (`internal/daemon/step.go:250`) is the only place a step's toolset is assembled. Its comment already
  states the two-list discipline — *the caller is what the loop calls, the specs are what the provider is
  told exists; "the derivation would be the place a granted tool stopped being offered"*. Callers:
  `protocol/step.go:380` (first run), `daemon/recover.go:188`, `protocol/step_stop.go:189`,
  `protocol/answer.go:79` (resume-after-answer). MCP enters here or nowhere, and all four callers get it
  for free *because* they share the function.
- **The gate is already generic over tool names.** `internal/runtime/loop.go:619` stamps
  `PolicyGateRequest{Tool: use.Name}` from the **handler side — never tool input** (SEC posture S2's
  comment guards); `ask` suspends through `pendingPermission` (`loop.go:76,663`). No MCP-aware branch
  needed; a qualified name is just a name to this machinery.
- **The double gate already exists — as a view.** S12's `grants.go` computes exactly ruling §3's
  intersection: `Registry.ToolsFor(agent)` (the CAPABILITY view — tier position: private servers nested
  under their agent, public servers require naming in `declares` (MC5), absent agent → core only) ×
  `policy.Evaluate(ToolAccess, {Agent, MCPServer, Tool: qualified})` (the AUTHORITY view). Four states:
  `available` / `unavailable` (MC10 — *"no ToolAccess allow rule grants"* / unclassified MC6) /
  `refused` (S10's sentence) / `ask` — **plus L22**: `outward`/`irreversible` effect class asks at use
  *even when allowed*. `GrantView` today feeds `aeon mcp list`; this slice makes the same verdict decide
  what a step is offered and what it dispatches.
- **Qualified form is `server.tool`** — `splitQualified` (`internal/mcp/config.go:340`) splits on a single
  **dot**; no first-party tool name contains one (`TestRegistryExactNames`: ten dot-less names). The dot is
  the routing signal (MD10).
- **The client is ctx-first and process-owning.** `Dial / ListTools / CallTool / Close`
  (`internal/mcp/client.go:118-227`); stdio transport spawns the child (`newStdioTransport:272`) and
  `Close` reaps it. A step's MCP connections **are child processes** — which makes lifecycle, not policy,
  this slice's real risk (§R1).
- **`grants.go` holds no state** (grep-pinned: `policy.Policy` appears only as a parameter), and **core
  tools are exempt** from re-decision — *"S12 does not re-decide core"*; their verdict stays S11's step
  gate's alone. Unchanged here.

## 3. The gate (the 2026-10-26 ruling): double gate, absent row never runs

A tool is **offered** to the model iff *both* hold at step-build, judged on the step's **frozen** policy
set (D12 — the same set the gate decides with):

1. **Visible:** in `ToolsFor(agent)` — server private to this agent, or public **and declared**; and
2. **Granted:** the `ToolAccess` verdict is **allow** — and an effect class of `outward`/`irreversible`
   downgrades even an allow to **ask** (L22, already S12's own rule, unchanged).

Everything else is **withheld, with a sentence**: `ask` (a tool a broad allow made "askable" is not
offered as callable-by-accident; it appears only through the ask path below), `unavailable` (MC10),
`refused` (S10), unclassified (MC6), server-unreachable-at-build (MD4). **Dispatch re-decides** through
the existing gate on the same frozen set — a row flipped to deny between build and dispatch is caught
there, and `ask` (by row or by L22) parks on the **existing** `pendingPermission` path; an attended loop
suspends, an unattended one denies at once (S11's rule verbatim, no MCP branch).

*Why not tier-trust-suffices (option 2) or a config flag (option 3):* both put a second source of
authority beside the corpus S11/S12/S35 all treat as the single truth, and both make S34's
*registered-not-granted* column a lie. **There is no second grant mechanism** (grants.go header) —
S36 keeps that literally true.

## 4. The degradation law — the slice's other load-bearing wall

> **A dead server degrades, never hangs, never promises.**
> Build: unreachable/timed-out server ⇒ its tools are withheld (step starts anyway, facts recorded).
> Dispatch: server dies mid-step ⇒ tool_result carries error text, the step continues.
> A tool that cannot be called was never promised: specs and caller come from the same enumeration.

The loop must never freeze because a child process died — no timeout today means no timeout at all
(`CallTool` inherits whatever ctx the caller forgot to bound). This slice owns those budgets (MD4).

## 5. Lifecycle — because MCP connections are processes

A suspended step has no process (P2) — MCP connections are processes, so **resume re-dials cold**: the
adapter's cached connections live exactly as long as a running step, and **all four `BuildTools` callers**
get a fresh adapter each. Teardown closes cached children (kill + reap via the client's own `Close`);
daemon shutdown reaps the rest with the daemon's ctx. Orphaned children are this slice's named bug
(§R1), and the kill-`-9` test is its answer.

## 6. Why no `AllowToken` here — said so reviewers stop looking

`bash` needs a token because the **model composes the command string** (`policy/token.go`'s one mint,
`tools/spawn.go`'s first statement). MCP dispatch composes **nothing model-authored**: the command line is
operator config (S12-validated at parse; addable only through S35's staged proposal + human commit;
spawn-verified only after an S34 operator clicked Check). The token guards *forged intents*; there is no
intent here to forge — the grant check (§3) is the authority, and it already ran through the one gate.

## Acceptance criteria — the arbiters

1. **The slice is one test.** Fake MCP server (S12's conformance fake pattern) + config entry + operator
   `ToolAccess` allow row → a real loop step is **offered** `server.tool` with the server's live schema,
   **calls** it, and the result lands as a `tool_result` in the transcript.
2. **MC10 is load-bearing.** Delete the row → the tool is **not in `specs`** (the model is never told it
   exists), and the step record carries `grants.go`'s mc10 sentence **verbatim**.
3. **Ask parks.** Row set to `ask` (or effect class `outward` under allow) → dispatch suspends on the
   **existing** `pendingPermission` question, `approve` resumes and the call proceeds; unattended loop
   denies at once. S11's machinery, zero MCP branches.
4. **Degrades, never hangs.** Kill the server child with `-9` between build and dispatch → the step
   completes within budget with error-text tool_result; unreachable-at-build → tools withheld, step
   starts. Timeouts are measured numbers in task notes, not intentions.
5. **MC11 sacred, unamended.** Zero MCP config → suites byte-identical; `absent_test.go` has **zero diff
   lines** (CI note proves it); deleting `internal/mcp` + the seam still builds — MC11's original
   deletability proof, re-run, still passes.
6. **Frozen-set parity.** Offer uses the step's frozen set; a corpus flip after build cannot resurrect a
   withheld tool at dispatch, and cannot save a revoked one mid-call beyond the frozen verdict (D12,
   same discipline bash's `CommandVerdict` closes over).
7. **Deletion property.** The seam type + composite caller + `cmd/aeon` bundle ≈ three files and one
   wiring site (`serve.go`); one commit removes them and the daemon returns to exactly today's behavior.

## What this is not

- **No socket methods, no UI edits** — S34/S35 own the surfaces; their columns become true, they don't
  grow. (If withheld-tools deserve a visible home later, that is a one-row addition to S34's page, filed
  as a leftover, not smuggled in here.)
- **Tools-only MCP.** No resources, no prompts, no sampling, no `notifications/*` — `ListTools` and
  `CallTool` are the entire vocabulary, as they already are for `aeon mcp`.
- **No blueprint/granted-list changes.** Blueprints and handler `Policy` fields keep naming first-party
  tools only (*"a declaration and never a grant"*, AGENTS.md §4); MCP names reach the step through the
  corpus, not through authored manifests. `granted []string` keeps its first-party-only meaning.
- **No grant-state store.** `grants.go`'s "no state here" grep-pin must stay green; offers are derived,
  never stored.

## 8. Risks, named where they live

- **R1 — orphaned children.** Every crash/stop path that builds an adapter must close it; a leak is
  invisible until `ps` fills with zombie servers. Mitigation: `Close` wired at all four callers with a
  test that counts children before/after (and a kill-`-9` survival test); stdio children die with the
  daemon's ctx regardless. This is where the slice's bugs will actually live.
- **R2 — step-start latency.** Build-time enumeration dials every visible server with ≥1 available tool.
  Mitigation: parallel dials with a per-server budget (MD4), measured numbers recorded; a step that
  takes 3 s to start is still better than one that hangs forever — but the number must be *said*.
- **R3 — offer/dispatch race.** Corpus or config flips between build and dispatch. Mitigation: dispatch
  re-decides on the **frozen** set (criterion 6), and the live `ListTools` cross-check means a server
  that *renamed* its tools mid-step withholds them rather than offering a schema the call would fail
  against — the disagreement always costs the call, never gains one (step.go's own words).
- **R4 — the canonicalizer meets the dot.** SEC-059's injected canonicalizer compares ToolAccess rows'
  `toolName`; a row spelled `write_file`-style against `server.tool` must canonicalize to identity
  (no alias confusion between the two name spaces). T5 tests it with an alias-shaped row.
