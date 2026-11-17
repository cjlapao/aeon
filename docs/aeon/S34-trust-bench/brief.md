# Brief — S34 Trust bench

**Implements:** a new slice, designed in the 2026-10-26 conversation that decided the app-vs-CLI ordering
(**P34**, [`ARCHITECTURE.md`](../ARCHITECTURE.md)) — the first slice written *after* that doctrine and the
first built under it. `design.md` and `plan.md` are written alongside this brief, not "the next pass" the way
S33's are. **Stage:** 5 · Fleet. **Depends on:** S4 (the store), S9 (the app + test bench), S11 (the policy
evaluator), S12 (MCP config + the `aeon mcp` CLI), S13 (agent roster via plugin state) — **all five are
built**, so unlike every "free to start" candidate in [`PLAN.md`](../PLAN.md) this slice has no unbuilt
dependency and no runtime edit; its collision surface is named in §8. **Repo:** Aeon lives at `aeon/` in this
repository (P24) · Go 1.26 · `cgo` enabled (B3)

---

## 0. Before anything else: this slice exists because of P34

**P34**: *"The daemon surface is the primary interface — the CLI is secondary… A capability that is not
reachable through the daemon surface is not finished and is not accepted as finished."* This slice is that
sentence applied to the one area where the inversion was still true: **knowing who an agent is, what it may
do, and whether its MCP servers actually work.** Today every one of those facts is reachable *only* by
running a CLI on a terminal. The bench — the project's own place for "test it where you consume it" — cannot
ask any of them, because the daemon has never been asked to answer them.

So the build order here is deliberate and is the acceptance test itself: **the daemon methods are the
deliverable; the two bench pages are thin clients of them** (§3 before §4, T1–T4 before T5–T7 in
`plan.md`). A page fact that cannot be got from a daemon method is not allowed on the page — not from the
CLI, not from the renderer reading config files directly.

## 1. What this is

Two bench sections, each its own side-menu item and its own page (the bench left tabs for the joined
side-menu in `PageLayout` during 2026-10-26; both new sections slot into that shape, §4):

- **Agents — the readiness view.** Per agent: identity and provenance (first-party / imported), its state
  (enabled · blocked · never-arrived), and what it *can* do — declared vs granted vs registered-not-granted
  tools side by side, its MCP declares, and its guard verdicts. The read companion to what S35's toggles
  will write.
- **MCP — the config-and-check view.** The configured servers exactly as the daemon's own MCP layer sees
  them (tiers, per-agent declares, refused entries with the reason), plus an operator-triggered **Check**
  per server: does this server in fact answer `initialize` and `tools/list`, and with which tools.

**The one-line version:** the bench grows the two pages that answer *"who can do what, and does this MCP
server actually work?"* — answered by four new daemon methods, with the pages as thin clients (P34).

## 2. What exists today, measured — the gap is real, not assumed

Verified 2026-10-26, file by file, so nobody re-derives it at seeding:

- **The method table has none of it.** `aeon/internal/protocol/method.go` carries the full surface; the
  closest reads are `policy.list` / `policy.why` / `policy.inForce` / `policy.events`
  (`method.go:132-135`) — the guard's verdicts only. There is **no `mcp.*` method, no `agent.list`, no
  tool-registry read anywhere in the table**. `grep -n '"mcp\.\|"agent\.' method.go` is the one-line proof.
- **Yet every underlying fact already exists, twice over** —
  - the roster: `aeon/internal/store/plugin_state.go` + the first-party set, read by `aeon agent list
    --provenance --all`;
  - the MCP config: `aeon/internal/mcp/config.go`'s `LoadConfigFile` at `paths.MCPConfigFile()`
    (`AEON_MCP_CONFIG` override, else XDG; absent is a legitimate state — **MC11**), tiers in
    `internal/mcp/classify.go`, per-agent grants in `internal/mcp/grants.go`;
  - the live probe: `aeon/internal/mcp/client.go` dials configured servers — `aeon mcp list --agent NAME`
    (`aeon/cmd/aeon/mcp.go:72`) spawns/dials, runs `initialize` + `tools/list`, and reports live classes and
    connection state. **The mechanism this slice needs is already written and already trusted; it lives one
    door away from the daemon.**
- **The bench has neither page.** `aeon/ui/src/app/routes.ts:32` `BENCH_PAGES` is the menu *and* the route
  validator (`isBenchPage`); `aeon/ui/src/features/test-bench/pages/` holds the twelve existing pages,
  `PolicyPage.tsx` among them. The panel reads `policy.*` — the verdicts — but never the subjects the
  verdicts are about.
- **The policy store already speaks `mcp` as a target kind** (`store/schema.go:375`: `target_kind IN
  ('all-agents','agent','loop','lane-type','mcp')`) — so an agent's and a server's guard story is already
  answerable from `policy.why` once the *subjects* have pages to hang the verdicts on.

## 3. The daemon surface — the deliverable (locked shape; schemas in `design.md` §2)

Four read methods, added the same way the `policy.*` reads were (names in `protocol/method.go`, handlers in
a `protocol/` handler file per subject, conformance fixtures under `protocol/conformance/routes/`):

| Method | Answers | Reuses (no re-implementation) | New code |
| --- | --- | --- | --- |
| `agent.list` | roster + identity + enabled/blocked/never-arrived | `store/plugin_state.go` + first-party set — exactly what `aeon agent list` reads | exposure only |
| `tools.grants {agent}` | declared vs granted vs registered-not-granted, per agent | the S11 evaluator's own inputs (`internal/policy/evaluate.go`) + `mcp/grants.go` | exposure only |
| `mcp.status` | configured servers, tiers, per-agent declares, refused entries + reasons | `mcp.LoadConfigFile` + `classify.go` + config validation | exposure only |
| `mcp.check {server}` | one probe: spawn/dial → `initialize` → `tools/list`; never `tools/call` | `mcp/client.go` — the same dial path `mcp list --agent` uses | **the one genuinely new daemon behavior** |

`mcp.check` rules (locked here, testable per `design.md` §5): operator-triggered only (a page open spawns
nothing); bounded timeout; one in-flight probe per server; result cached with its timestamp so the page can
show *"checked 14:02"* honestly; **`tools/call` appears nowhere in its code path, asserted by a test against
a fake transport**, not asserted in prose. Same socket, same peer-credential gate as every other method
(P25's split unchanged); PL15 is untouched — this slice changes no policy state, so the human-only door
still has nothing to open.

## 4. The bench — thin clients, and the deletion property

- `BENCH_PAGES` gains `{ id: "agents" }` and `{ id: "mcp" }` **immediately after `policy`** — the
  authorization story then reads in order: Policy → Agents → MCP. One list is the side-menu (rendered by
  `PageLayout.tsx` when the rail section is `bench`) and the URL validator, so a row *is* the menu item plus
  its route; no other wiring.
- **Own file per page** (`pages/AgentsPage.tsx`, `pages/McpPage.tsx`) and **own dispatch branch** in
  `TestBench.tsx` (which since 2026-10-26 is a route-driven dispatcher, not a tab strip). This is the
  **deletion property**: when a section's coverage is proven, it deletes in one commit — its `BENCH_PAGES`
  row, its `pages/` file, its dispatch branch, its tests. Nothing shared to untangle. This is why neither
  section merges into an existing page.
- **The three-state rule** (acceptance criterion, §7): every fact on both pages renders one of **value /
  `not wired` / `error`** — a page open against an unwired or old daemon says so in the fact's own cell,
  never renders silence as zero. Both pages also carry the whole-page third state when disconnected, like
  Status and agent-chat do today.
- **Every row shows its CLI twin** (the `aeon …` command for the same fact) — secondary door (P34), but the
  bench's job is comparison, and a mismatch between the two doors is a bug report.

## 5. What this is not

- **No writes, provably.** No enable, no grant, no add-server, no policy commit — S35 owns every toggle and
  the staged-authoring pattern for policy commits. The only state this slice mutates in the whole system is
  `mcp.check`'s probe cache and the probe process itself, which exits.
- **Not the authoring story.** S35 (agent/MCP authoring from the bench) hangs its toggles on the readiness
  view this brief describes; building that view's columns now is what lets S35 be only about writes.
- **No menu regrouping yet.** `SideMenuItem` supports `type: 'group'` (the rail uses it); the bench panel
  starts flat and groups by concern when S35 lands, not before — two new rows next to twelve is not yet a
  list that needs structure.

## 6. Acceptance criteria — the arbiters

1. **P34 test.** Every rendered fact on both pages traces to one of the four methods or the existing
   `policy.*` reads. A page reading `paths.MCPConfigFile()` or `plugin_state` by any other route fails the
   build review; the renderer's data path is greppable in one pass.
2. **The probe never calls.** A test drives `mcp.check` against a fake MCP transport that fails the test if
   it ever sees `tools/call`. Timeout, single-flight-per-server, and cache-with-timestamp are each their own
   case.
3. **Three-state honesty.** A render test per page with an idle/disconnected controller: no cell shows a
   plausible zero where the truth is *unwired*; the `unreachable: exit 127` and `not checked` shapes render.
4. **Absent config is a state, not an error (MC11).** `mcp.status` against no config file renders the
   legitimate empty, quoting the config path it looked at.
5. **Deletion property holds.** Removing one section is one commit touching exactly: its `BENCH_PAGES` row,
   its `pages/` file, its dispatch branch, its tests. Verified by doing it to a scratch branch during T8.
6. **Conformance.** Each new method ships a fixture under `protocol/conformance/routes/`, same pattern as
   `models-list.json`.

## 7. Out of scope, on purpose

S35: all writes/toggles, policy commit staging, menu grouping. S33: anything token/cost shaped — this slice
reads *who may act*, never *what it cost*. The CLI verbs stay as they are (secondary doors, P34) — this
slice adds no new verb, and `aeon mcp list --agent` remains the reference implementation `mcp.check` is
tested against.

## 8. Risks, named where they live

- **R1 — the probe spawns processes from the daemon.** Today only a human running a CLI does this; after
  this slice a peer-credentialed socket call can, on operator click. Mitigated by §3's locks (no auto-probe,
  never `tools/call`, single-flight, bounded timeout) and by keeping the dial path literally shared with the
  CLI, so there is one implementation to audit, not two.
- **R2 — `method.go` is a shared file.** Every future slice adding reads touches the same table and the same
  dispatcher struct (the `PolicyStore` pattern, `method.go:382`). Seeding should run the same
  `files_expected` collision check S24's seeding used, against any slice in flight on daemon reads.
- **R3 — old-daemon bench.** `BENCH_PAGES` rows appear before an old daemon can serve the methods; the
  three-state rule (§4) is the whole mitigation — a new page must be harmless against a daemon that answers
  "unknown method", which is why it is an acceptance criterion, not a style note.
