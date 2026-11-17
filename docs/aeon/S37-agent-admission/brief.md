# Brief — S37 Agent admission

**Implements:** a new slice, designed in the 2026-10-26 conversation that swept S18–S33 and the already-built
S1–S17(+S20/S21) against **P34** ([`ARCHITECTURE.md`](../ARCHITECTURE.md)) — the fourth of P34's children,
after [`S34-trust-bench`](../S34-trust-bench/brief.md), [`S35-trust-desk`](../S35-trust-desk/brief.md) and
[`S36-mcp-dispatch`](../S36-mcp-dispatch/brief.md). `design.md` and `plan.md` are written alongside this
brief. **Stage:** 5 · Fleet. **Depends on:** S4 (the store), S9 (the app + test bench), S10/S11 (the policy
boundary and evaluator), S13 (the roster), **S15** (`internal/plugin`, built — this slice edits none of its
files, §5), and **S35** (the proposal table and `proposal.create/list/discard` + `aeon proposal commit` —
this slice adds exactly one new `kind` to S35's existing registry; it seeds **after** S35's T1–T5 land, the
same ordering constraint S35 itself had onto S34). **Repo:** Aeon lives at `aeon/` in this repository (P24) ·
Go 1.26 · `cgo` enabled (B3)

---

## 0. Before anything else: this is the gap S35 named and deliberately left open

**S35's own brief, §3, verbatim:** *"The imported agent already passed blocklist + consent at import
(S15); enabling it starts something already permitted."* S35 staged three things (policy add/remove/
seed-corpus, MCP server add/remove) and left three things as plain writes (`agent.enable/disable/revoke`)
— **it never mentions import**, because at authoring time import was treated as a precondition, not as
one more knob. It is not: import is the one place in this whole domain where a stranger's code gains
capability for the **first time**, which is exactly P34 + PL15's collision all over again, one layer up
from where S35 drew its line.

**Confirmed against the real code, not assumed** (`S15-agent-plugins/brief.md`'s own retrofit note, written
in the same sweep): `internal/protocol/agent.go`'s one real socket method, `agent.plugins`, says so in its
own `Cannot` field — *"it cannot import, enable, disable or revoke — the four state verbs live ONLY in the
CLI."* `internal/protocol/method.go` has no `agent.import`. This slice is that gap, closed the way S35
closed its three.

## 1. What this is

**`aeon agent import` becomes reachable from the app — as a staged proposal, never a plain write —
through the exact mechanism S35 already built.** Nothing new is invented: this slice adds one `kind` to
S35's `proposal` table's closed enum, one entry to its validator registry, and one bench affordance on
S34's existing Agents page. The desk, the human-only commit door, the no-commit-button rule, the
three-state rendering — all S35's, untouched.

**The one-line version:** import is the one widening action in S15 that S35 didn't cover; this slice is
S35's pattern, applied to the one case it was always going to need.

## 2. What exists today, measured — the reuse surface, file by file

Verified 2026-10-26 against `aeon/internal/plugin/` and `aeon/cmd/aeon/agent.go`:

- **`plugin.ImportChecked(pkgPath, ps, at) (*Agent, *Reject)`** (`import.go:112`) is the one real import
  door: read the package and verify its card (`readPackage`), read its carried policies
  (`ReadCarriedPolicies`), run the policy check (`CheckRequests`) — all three **read-only, no side effect**
  — then, only on success, `recordImport` (writes the `plugin_agent` row) and the in-memory registration.
  **The three read-only steps are already separable from the one write step**, which is exactly the
  propose/commit split this slice needs and did not have to invent.
- **`cmdAgentImport`** (`agent.go:98`) does two things beyond calling `ImportChecked`: it prints
  `plugin.RenderConsent(a, existing).String()` — the consent text verbatim, the thing an operator decides
  against — and it calls `StoreCarriedPluginDenial` for every carried `Deny` policy, which is
  **`policy.Store.Add` itself** (`policy.go:753-759`), the identical function `aeon policy add` and S35's
  `policy_add` proposal kind both call. **Import already reaches into the policy-authoring boundary as a
  side effect.** This slice's commit step must do the same, through the human door, never a new one.
- **`plugin_agent_live` is a partial unique index** (`store/schema.go:203`, migration `0023`) — one live
  row per name, revoked rows excluded. A second import of a name with a live record already refuses today
  (*"already has a live record · remedy: revoke it first"*) — so two proposals racing on the same name is
  not a new failure mode this slice has to guard; the existing index refuses the second commit exactly as
  it refuses the second CLI run.
- **S35's registry is built to take a fifth kind without new mechanism.** `design.md` TD3: *"A registry
  `map[kind]func(payload) error`, each entry calling the code that already exists."* `agent_import`'s entry
  calls the three read-only `ImportChecked` steps directly — not a parallel validator, not a guess at what
  they'd return.

## 3. The daemon surface — the deliverable (shapes in `design.md` §2)

**No new socket method.** `proposal.create {kind: "agent_import", payload: {pkgPath}}` /
`proposal.list` / `proposal.discard` are S35's three methods, unchanged in code, widened in enum. The one
new daemon behavior is the validator entry itself:

| Step | What runs | Reuses | New code |
| --- | --- | --- | --- |
| `proposal.create {kind:"agent_import", payload:{pkgPath}}` | `readPackage` → `ReadCarriedPolicies` → `CheckRequests` against the policy set in force **now** | the three read-only `ImportChecked` steps, called directly, never through a path that also writes | one validator registry entry |
| `proposal.list` | unchanged | S35 as built | none |
| `proposal.discard {id}` | unchanged | S35 as built | none |
| `aeon proposal commit <id>` (human door, unchanged CLI verb) | re-reads the package **fresh** (AA3), re-runs the three checks, then `recordImport` + `StoreCarriedPluginDenial` per carried `Deny` — **`ImportChecked`'s own write half, called once** | `ImportChecked`, `policy.Store.Add` — the exact functions `cmdAgentImport` already calls | the commit-dispatch entry for this one kind |

A bad package (unreadable, unsigned card, requested `Allow`) is refused **at `proposal.create`**, with
`CheckRequests`'/`readPackage`'s own reason verbatim — never carried inert to commit (matching S35's
validator-parity rule, criterion 4 there).

## 4. Locked decisions (`AA`)

- **AA1 — Import is staged, never a plain write.** It is the one action in this domain that grants a
  stranger's code capability for the first time; S35's own line (*"anything that widens what the daemon may
  ever do is staged"*) places it on the staged side without ambiguity. `agent.enable/disable/revoke` are
  unchanged and untouched by this slice — they already move state within authority import already granted.
- **AA2 — The payload is a path, not a snapshot.** `{pkgPath}` only. The daemon re-reads the directory at
  both create and commit time; nothing about the package (its declared skills, its carried policies, its
  card) is copied into the proposal row. A path is small, auditable in the desk as plain text, and — unlike
  a JSON snapshot of an arbitrary package — cannot itself smuggle a trojan-shaped field past review (R3 in
  S35's own risk list, reasoned the same way here).
- **AA3 — Commit re-reads the disk; it does not trust the create-time read.** Between propose and commit
  the package on disk may have changed or vanished — the same real possibility `Rehydrate`'s own comment
  already names for a different path (*"the check runs again on the re-read... a package that grew a tool
  request after install is caught here"*). A commit that finds a **different** package than the one
  previewed fails with the fresh refusal; the proposal row stays `proposed`, never silently flips on a
  stale preview. This is the one genuine difference from S35's four kinds, whose whole payload travels
  inline and never touches a filesystem between create and commit — named here so the task that builds it
  does not assume S35's crash-retry story covers this case too (it covers the crash; it does not cover the
  disk moving under the row).
- **AA4 — The consent text at propose time is advisory; the consent text at commit time is authoritative.**
  `proposal.create`'s response carries `RenderConsent`'s text so the desk can show it before anyone commits
  — but `aeon proposal commit` renders it **again**, from the fresh read, and that is the text an operator
  actually sees at the moment of decision (matching `cmdAgentImport`'s existing discipline exactly — the
  consent is never summarized, S15's own T4 words). If the two renders differ, the commit succeeds or fails
  on the fresh one regardless; the stale one was always a preview.
- **AA5 — No new write-only method, no second import implementation.** This slice's whole backend footprint
  is one validator-registry entry (create-time) and one commit-dispatch entry (commit-time), both calling
  functions `internal/plugin` and `cmd/aeon/policy.go` already export. `rm -r internal/plugin` still leaves
  this slice's own files compiling against an interface, the same deletability shape S36 gave MCP dispatch
  — named as a goal here, resolved concretely in `design.md`.

## 5. The seams

**Downward — S15 (`internal/plugin`), unchanged.** Not one file in `import.go`, `check.go`, `consent.go`,
`state.go` is edited. This slice calls what is already exported; if it needs something not exported, that
is a finding for `design.md`, not a reason to reach around the package.

**Downward — S35's proposal mechanism, unchanged.** No edit to the `proposal` table's shape beyond widening
one `CHECK (kind IN (...))` constant list (a new migration, per the schema rule — §3 of `AGENTS.md`, never
an edit to a shipped one); no edit to `proposal.create/list/discard`'s handler code; no edit to
`aeon proposal commit/reject/list`'s CLI code beyond a `switch kind` arm.

**Sideways — S34's Agents page.** The propose-import form lands there, not a new bench page — S34's own
brief deferred the Agents page's write affordances to S35 (*"the read companion to what S35's toggles will
write"*); this slice is one more toggle on the same page, following S35's exact pattern (`proposal.create`
+ toast, never a live write).

**Sideways — S35's desk (`ProposalsPage.tsx`).** Lists `agent_import` proposals exactly like the other
four kinds; no new column, no new action. The desk's "no commit button, ever" rule (TD7) covers this kind
for free because it is a property of the page, not of the kind.

## 6. Out of scope

- **N1 — No fetching, ever.** AP10 is untouched: `proposal.create`'s payload is a local path, and a URL is
  refused by `internal/plugin` before any disk is touched, by the same check the CLI already has. This
  slice adds no HTTP client anywhere.
- **N2 — No change to `agent.enable/disable/revoke`.** S35's territory, already built to its own spec,
  untouched here.
- **N3 — No narrow-surface exposure.** `agent.plugins`'s own `Cannot` field keeps saying import lives
  outside the narrow surface — this slice adds a socket *kind*, not a narrow-surface method, and R6's
  reasoning (a caller handed the means to install the thing that bypasses the guards) applies to the narrow
  surface exactly as before.
- **N4 — No preview of the package's full file tree or its source.** The consent text (AA4) is the
  existing, deliberately-scoped disclosure S15 already settled on; this slice does not add a code viewer to
  the desk.
- **N5 — No batch import, no approval chains.** One path, one proposal, one human decision — S35's own N2,
  inherited rather than re-argued.

## 7. Questions to answer before building

| # | Question | Where |
| --- | --- | --- |
| Q1 | Does the validator-registry entry need `internal/plugin` to export anything it does not already (e.g. a combined "preview" call bundling the three read-only steps), or do the three existing calls compose cleanly from `internal/protocol`? | `design.md`, resolved in T2/T3 (one export, composed in `cmd/aeon`) |
| Q2 | What does the desk show for a proposal whose package has since vanished — refuse the *display* at `proposal.list` time (a fourth read of the disk, on every list call), or only discover it at commit? | `design.md` — leaning toward: never re-read at `list`, only at `commit` (AA3 already covers correctness; re-reading on every list call is an unbounded disk-read cost for a page that may be left open) |
| Q3 | Does `pkgPath` need to be resolved to an absolute path at create time, so a relative path typed by one operator does not resolve against a different working directory at commit time (run by the daemon, not a shell)? | `design.md` — almost certainly yes; named so the task that builds it (T4) does not discover it by a confusing bug report |

## 8. Acceptance criteria — the arbiters

1. **The slice is one test**, in S35's own shape: propose an `agent_import` for a known-good fixture
   package over the socket → `agent.plugins` unchanged (nothing imported yet) → commit via the CLI →
   `agent.plugins` now lists it, `disabled`, matching exactly what a direct `aeon agent import` would have
   produced on the same fixture.
2. **Validator parity.** A fixture package that `aeon agent import` refuses today refuses `proposal.create`
   with the identical reason string — asserted by test, not by inspection.
3. **AA3, proven, not claimed.** A fixture whose package directory is mutated between create and commit
   (an `Allow` request added) refuses at commit with the fresh reason; the proposal row stays `proposed`.
4. **Store teeth survive the new kind.** A second `agent_import` proposal for the same name, committed after
   the first succeeded, fails on `plugin_agent_live`'s existing unique index — the same refusal text a
   second CLI import already produces, not a new one.
5. **No second import path.** A grep/import-boundary test: only `cmd/aeon` imports `internal/plugin` (the
   existing absent test already requires it; the preview composes there behind a function seam) and nothing
   performs a `readPackage`-shaped read anywhere in `internal/protocol` or `internal/daemon`.
6. **The desk and the toggle are thin clients.** Every fact the Agents page's import form shows traces to
   `proposal.create`'s response or `proposal.list`; no renderer reads `pkgPath` off disk itself.
7. **PL15 untouched.** `TestNoPolicyAuthoringReachesTheNarrowSurface`'s whitelist (S35's TD10) needs no
   second amendment — `agent_import` writes only the `proposal` table at create time, exactly like S35's
   four kinds; verified, not re-argued.

## 9. Risks, named where they live

- **R1 — AA3 gets skipped because S35's crash-retry story looks like it already covers this.** It covers a
  *daemon* crash between effect and flip; it says nothing about the *filesystem* changing between propose
  and commit, which is a new failure shape only this kind has. **The guard is AA3 and acceptance criterion
  3**, which must exercise a real mutation, not a mocked one.
- **R2 — The consent text drifts between preview and commit without anyone noticing.** A desk that shows a
  stale consent as if it were final would be exactly the failure S15's own T4 refused (*"a CLI that
  prettified it would be editing the thing the decision rests on"*), one layer removed. **The guard is AA4**
  — commit always re-renders, and the desk's own copy is labelled a preview, not a promise.
- **R3 — A relative `pkgPath` resolves differently for the human running `commit` than for whoever proposed
  it.** Named in Q3 rather than guessed at; the fix (resolve to absolute at create) is cheap and the bug if
  skipped is confusing precisely because it would work on most machines most of the time.
- **R4 — This slice quietly becomes a second place that knows how to import an agent.** Every function this
  slice calls already exists; the discipline is calling them, not re-deriving what they do. Acceptance
  criterion 5 is the guard, same as S36's import-boundary test guarded MC11.
