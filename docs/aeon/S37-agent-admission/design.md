# Design — S37 Agent admission

Fills in `brief.md`: the schema widening (§1), the validator entry (§2), the commit dispatch (§3), the
bench (§4), the test map (§5). Rules are numbered **AD1–AD6** and are cited from `plan.md` tasks the way
S35's TD1–TD12 are cited from its own plan.

## AD1 — The schema change is one migration, widening one CHECK

```sql
-- new migration, numbered after whatever S35's own migration landed as —
-- never an edit to S35's shipped CREATE TABLE (AGENTS.md §3's forward-only rule)
CREATE TABLE proposal_new (
  id            INTEGER PRIMARY KEY,
  kind          TEXT NOT NULL CHECK (kind IN
                  ('policy_add','policy_remove','seed_corpus',
                   'mcp_server_add','mcp_server_remove',
                   'agent_import')),                       -- the one addition
  payload       TEXT NOT NULL,
  status        TEXT NOT NULL DEFAULT 'proposed'
                  CHECK (status IN ('proposed','committed','rejected','discarded')),
  created_at    TEXT NOT NULL, created_by TEXT NOT NULL,
  decided_at    TEXT, decided_by TEXT, decide_reason TEXT,
  applied_error TEXT
);
-- copy, drop, rename — the 0016 rebuild pattern S10/S35 already used for a CHECK widening
```

Nothing else about the table changes: `decide`'s guarded-UPDATE-from-`proposed` pattern, discard-only
socket-reachable transition, immutable `committed`/`rejected` rows — all exactly as S35 built them. **This
migration's whole job is six characters: `,'agent_import'`.**

## AD2 — The payload shape

```jsonc
{ "pkgPath": "/home/operator/agents/reviewer-plus" }
```

Deliberately the smallest payload of any kind in the registry (contrast `mcp_server_add`'s full
`ServerConfig`) — per brief AA2, the daemon re-derives everything else by reading the path, both at create
and at commit. `pkgPath` is resolved to an **absolute** path at the moment `proposal.create` receives it
(Q3, resolved): a relative path is rejected with a named reason (*"refused: pkgPath must be absolute — a
relative path resolves against whoever's working directory is running at the time, and create and commit
are not guaranteed to be the same process"*) rather than silently resolved against the daemon's own
working directory, which nobody watching the desk would expect. **One ordering exception (amended
2026-10-27):** a path containing `://` is passed to the plugin layer unchanged, so a URL gets S15's own AP10
refusal text, byte-identical to the CLI's (criterion 2, N1) — a URL is not absolute, and the relative-path
refusal would otherwise shadow it.

## AD3 — The validator registry entry: three existing read-only calls, in order

```go
// internal/protocol or wherever S35's registry already lives — ONE map entry, not a new function
// that reimplements what ImportChecked already does in its own first half.
"agent_import": func(payload json.RawMessage) (preview any, err *Reject) {
    var p struct{ PkgPath string `json:"pkgPath"` }
    if err := json.Unmarshal(payload, &p); err != nil { return nil, refuse(...) }
    a, rj := plugin.ReadPackagePreview(p.PkgPath)   // AD6 — the one new export, see below
    if rj != nil { return nil, rj }
    carried, rj := plugin.ReadCarriedPolicies(p.PkgPath)
    if rj != nil { return nil, rj }
    ps, err := currentPolicySet(ctx)                // the same read agentCtx.set() makes
    if err != nil { return nil, refuse(...) }
    if rj := plugin.CheckRequests(a, ps, time.Now().UTC()); rj != nil { return nil, rj }
    return plugin.RenderConsent(a, ps), nil          // AA4: this IS the preview text, nothing else
}
```

**Answers Q1.** `CheckRequests`, `ReadCarriedPolicies` and `RenderConsent` are already exported in a
read-only shape. The one gap: `readPackage` is lower-case (package-private) in
`internal/plugin/import.go:194`. **AD6** below is the one new export this slice adds — not new logic, a
new name for logic that already exists, because `internal/protocol` cannot call an unexported function
across the package boundary and duplicating its body is exactly the "second import implementation" brief
acceptance criterion 5 forbids.

**Amended 2026-10-27 (found while writing the task files): where this entry's body lives.** The sketch above
calls `plugin.*` from `internal/protocol`, but `TestNoUnconditionalImportOfPlugin`
(`internal/plugin/absent_test.go:81`) fails if any package other than `cmd/aeon` and `internal/plugin` imports
`internal/plugin`. So the three calls run in a `cmd/aeon` closure (`agentImportPreviewFor`, tasks T3) handed to
the protocol layer as a plain function field (`Table.AgentImportPreview`, T4) — the same seam shape S15 already
uses for `Table.Plugins` (`protocol/method.go:436`, wired at `agent_services.go`). The registry entry in
`internal/protocol` only decodes the payload, applies the AD2 absolute-path rule and calls the seam; the
`preview` it returns is typed (`AgentImportPreview`) so `make protogen` gives the app a real type.
`absent_test.go` stays zero-diff. If S35's registry itself lives in `cmd/aeon`, the seam field is unnecessary
(T4 says so).

## AD4 — Commit dispatch: the write half of `ImportChecked`, called once, through the human door

```go
// cmd/aeon's proposal-commit dispatch, one more `case` beside S35's four:
case "agent_import":
    var p struct{ PkgPath string `json:"pkgPath"` }
    _ = json.Unmarshal([]byte(row.Payload), &p)
    existing, err := c.set()                         // same read cmdAgentImport already makes
    if err != nil { return err }
    a, rj := plugin.ImportChecked(p.PkgPath, existing, c.at)   // the EXACT CLI function, unchanged
    if rj != nil { return fmt.Errorf("%s", rj.Error()) }       // row stays `proposed` (AA3)
    for _, pol := range a.Carried {
        StoreCarriedPluginDenial(w, c.boundary, pol, c.at)     // the EXACT CLI call, unchanged
    }
    fmt.Fprint(w, plugin.RenderConsent(a, existing).String()+"\n")  // AA4: re-rendered, fresh
    // ... then the proposal row flips to committed, same as S35's other four kinds
```

**This is `cmdAgentImport`'s body, called from `commit` instead of from `import`** — realised in T5 by
extracting that body, unchanged, as `importAgentPackage(w, pkgPath)`, so both entry points call one function
(the `case` above is then a payload decode plus that call; `c.set()`/`openAgentCtx` live inside it). No second
implementation exists; the two entry points (`aeon agent import <path>` directly, and `aeon proposal
commit <id>` whose kind is `agent_import`) both end at `ImportChecked` + the same `StoreCarriedPluginDenial`
loop. **The direct `aeon agent import` verb is not removed** — it stays the fast path for someone at a
terminal who does not want the propose/commit ceremony for their own local work; the proposal path exists
for the app, which has no terminal.

## AD5 — AA3's refusal is a `Reject`, and the row's `applied_error` records it without flipping

If `ImportChecked` refuses at commit (package changed, card no longer verifies, a new `Allow` request
appeared), the commit CLI:

1. does **not** flip the row to `committed`;
2. writes the refusal's reason into `applied_error` (the column S35's own `TD1` already defined for
   exactly this shape — "the effect failed, not the daemon");
3. leaves the row `proposed`, so `aeon proposal list` shows it with the reason, and a person can either
   fix the package and re-commit, or discard.

**Crash-retry does not carry over (amended 2026-10-27).** S35's TD4 recovery for its four kinds is "effect already
present → complete the flip". For `agent_import` that test cannot be made safe: a live `plugin_agent` row of the
same name may have come from the CLI, and criterion 4 requires that case to *refuse* with the existing text. So a
re-commit after a lost flip fails with *"already has a live record"* and the human runs `aeon proposal reject`;
the per-kind recovery hook (if S35 has one) answers "not applied" for this kind. The window is one crash between
the import and the flip; the cost is a refusal, never a double import.

This is the one place this slice's test map must diverge from S35's own crash-retry test (S35's TD4):
S35's retry case is "the effect succeeded and the flip was lost" (idempotent, re-running completes it);
this slice's AA3 case is "the effect itself now fails, on a re-run that was never interrupted." Both are
real, and they are not the same test.

## AD6 — The one new export, and why it is the slice's entire `internal/plugin` footprint

```go
// ReadPackagePreview is readPackage, exported — the read-only half of ImportChecked's first step,
// with no change to its body. Named Preview, not Read, so a caller reaching for it understands it
// performs no registration and no store write (mirroring Rehydrate's own naming discipline: a
// read that is not an import says so in its name).
func ReadPackagePreview(pkgPath string) (*Agent, *Reject) { return readPackage(pkgPath) }
```

One function, one line of real logic (a rename-and-export), reviewed as its own commit so the diff against
`internal/plugin` is exactly this and nothing else — the smallest possible edit to a package this slice
otherwise promised not to touch (brief §5).

## 2. The method shapes (S35's methods, unchanged; this slice's kind only)

```jsonc
// proposal.create
{ "kind": "agent_import", "payload": { "pkgPath": "/home/operator/agents/reviewer-plus" } }
// → { "id": 12, "kind": "agent_import", "status": "proposed",
//     "created_at": "2026-10-26T16:00:00Z", "created_by": "cjlapao",
//     "preview": { "agent": "reviewer-plus",
//                  "consent": "reviewer-plus requests: ToolAccess(read, grep) ...",
//                  "carriedDeny": [ { "id": "...", "target": {...} } ] },
//     "command": "aeon proposal commit 12" }
//   or, refused at create:
// → { "ok": false, "error": "refused: card signature does not verify for reviewer-plus · ..." }

// proposal.list / proposal.discard — S35's methods, no change; rows of this kind carry the same
// `preview` shape as the create response

// aeon proposal commit 12   — re-reads pkgPath, re-runs the three checks, then recordImport +
//                              StoreCarriedPluginDenial per carried Deny, then flips the row
// aeon proposal reject 12 [--reason "..."]   — S35's verb, unchanged
```

## 3. The bench — one toggle, one existing page

- **AgentsPage** (S34's strips, S35's enable/disable/revoke controls already on it) gains **Propose
  import**: a path field, a submit button that calls `proposal.create {kind:"agent_import", ...}`, and a
  toast rendering S35's exact pattern — *"Proposed · commit with `aeon proposal commit 12`"*. No commit
  affordance anywhere (S35's TD7, inherited, not re-argued).
- **ProposalsPage — the desk** (S35's, unchanged code) renders `agent_import` rows using the same
  payload-vs-nothing-yet treatment it gives the other four kinds; the `preview.consent` field is shown as
  the proposal's payload-equivalent, the same way `mcp_server_add`'s candidate `Config` is shown today.
  **No new column, no new component** — the desk is generic over `kind` already (S35's own deletion-safe
  design); this slice proves that genericity rather than testing a new one.

## 4. Where each fact and effect comes from (P34 check)

| Surface fact/effect | Route |
| --- | --- |
| a package's declared skills, its carried policies, its consent text | `proposal.create`'s response (`preview`) |
| whether a package is importable at all | `proposal.create`'s refusal, or lack of one |
| open `agent_import` proposals | `proposal.list` (S35, unchanged) |
| withdrawing a proposed import before commit | `proposal.discard` (S35, unchanged) |
| actually importing the agent | **CLI only** — `aeon proposal commit <id>` (deliberately not in this table's socket column, same as S35's own promotion row) |
| the agent's live roster entry, once committed | `agent.plugins` (S34, unchanged — this slice writes a row that read already surfaces) |

## 5. The test map

1. **The slice test** (brief criterion 1): propose a known-good fixture package over the socket →
   `agent.plugins` shows no new row → commit via the CLI → `agent.plugins` now lists it `disabled`,
   matching a direct `aeon agent import` against the same fixture byte-for-byte in every field that is not
   a timestamp.
2. **Validator parity** (criterion 2): a fixture that `aeon agent import` refuses (bad card, requested
   `Allow`, unreadable manifest) refuses `proposal.create` with the identical reason string — table-driven
   over each of `readPackage`'s and `CheckRequests`' refusal shapes.
3. **AA3, the filesystem-moved case** (criterion 3, AD5): propose against a fixture, mutate the fixture
   directory (add a tool request `CheckRequests` would refuse) before committing, assert the commit fails
   with the fresh reason, `applied_error` is set, the row stays `proposed`, and a subsequent `discard`
   still works on it.
4. **`plugin_agent_live` through the new door** (criterion 4): import the same fixture directly via CLI
   first, then propose-and-commit the identical `pkgPath` as a second attempt — the commit fails with the
   existing *"already has a live record"* refusal, unchanged text.
5. **Import-boundary grep** (criterion 5): `internal/protocol/` and `internal/daemon/` do not import
   `internal/plugin` at all (the absent test already requires it), and nothing under `internal/protocol/` or `internal/daemon/`
   contains the literal sequence that `readPackage` performs (an `os.ReadDir`/manifest-parse shape) outside
   of calling `plugin.ReadPackagePreview` or `plugin.ImportChecked` — asserted by grep over the diff, not
   by code review alone.
6. **Thin-client render test** (criterion 6): the Agents page's propose-import form, driven against a fake
   socket, never reads a filesystem path itself — every string it shows came from a `proposal.create` or
   `proposal.list` response field.
7. **PL15 untouched** (criterion 7): `TestNoPolicyAuthoringReachesTheNarrowSurface`'s whitelist needs no
   new assertion — `agent_import`'s `proposal.create` handler writes only the `proposal` table, exactly
   like the four kinds already covered; the existing whitelist test is re-run, not edited, and passes.
