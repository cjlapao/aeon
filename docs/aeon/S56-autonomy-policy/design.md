# Design — S56 Autonomy policy

Rules **AL1–AL17**, cited from `plan.md` and the task files. Shapes follow [`MANAGEMENT-PATTERNS.md`](../MANAGEMENT-PATTERNS.md) (P1–P11); a deviation is
recorded in the rule with its reason. Written 2026-10-30; T0 re-walks every fact below.

## Verified facts (2026-10-30, `aeon/` working tree)

| # | Fact | Where |
| --- | --- | --- |
| 1 | `EffectClass` is `local \| outward \| irreversible`; `classes` is a package-private map "the only definition, no setter, no configuration path (N7/I4)" | `internal/action/class.go:25-33,67-70` |
| 2 | Registry names: `git.createBranch`(local) `git.commitAndPush`(outward) `git.mergeBranch`(local) `forge.openIssue`(outward) `forge.openPR`(outward) `forge.postReview`(outward) `forge.merge`(**irreversible**) `forge.closeIssue`(outward) `audit`(local); `action.Names()` lists them | `class.go:52-62`; `action/registry.go:104` |
| 3 | `Resolve(name, attended, allowOutward)`: local → run; unattended outward → run iff `allowOutward`; unattended irreversible → deny, with a sentence that says `allowOutward does not grant it` when the flag was set; attended → ask. Sentences are quoted verbatim by tests | `class.go:157-180` |
| 4 | `TestClassesAreNotConfigurable` fails on any non-test file in `internal/action` containing `os.Getenv`, `os.LookupEnv`, `SetClass`, `OverrideClass`, `RegisterAction` or `yaml` (substring, comments included) | `action/class_test.go:195-215` |
| 5 | The decision site reads `LoopAttended` (NULL/error ⇒ false) and `LoopAllowOutward` (error ⇒ return, never run), calls `action.Resolve`, and on `Deny` blocks with reason kind `action-denied` and the sentence verbatim | `loop/execute.go:68-93` |
| 6 | `loop.allow_outward` is `INTEGER NOT NULL DEFAULT 0`, written once at creation, read by `LoopAllowOutward` | `schema.go:1517`; `store/loops.go:183-187,356-368` |
| 7 | The pinned-loop INSERT lists `attended,allow_outward`; `CreatePinnedLoop` begins one transaction for loop + blueprint pin | `store/blueprint.go:142-157` |
| 8 | `loop` PK is `id TEXT`; archive moves rows out of `loop` (no FK may point at it without breaking archive) | `schema.go:775-799`; `store/archive.go` |
| 9 | Schedule/webhook start: `attended := false`, `LoopStartParams` carries no `AllowOutward`; then `SetLoopProvenance` | `daemon/trigger_fire.go:108-125` |
| 10 | Event start: `attended := false // AU2/G14: authored HERE`, no `AllowOutward`; goes through `startLoopWith(ctx, p, startExtras{StartedBy:"event",…})` | `daemon/event_fire.go:72-100` |
| 11 | `startLoopWith` is "the one start door"; `startExtras` (StartedBy, TriggerID, Depth, FirstMessage) is internal, not on the wire | `daemon/start_loop.go:30-48,109-110` |
| 12 | The narrow trigger **overwrites** `attended` with `false`; `allowOutward` passes through "mechanically"; no key-level grant may exist (reflect + grep guard) | `narrow/trigger.go:21-46`; `narrow/trigger_test.go:205-243` |
| 13 | S40 AU2: *"No code path in S40 sets `attended` or `allow_outward` to anything else … The author's stronger reading is a one-line change at that call site plus a human-written column … recorded, not built"* | `S40-automation/design.md` AU2 |
| 14 | S40 AU1's three locks (no agent tool; socket refuses agent actors; store `CHECK (created_by_kind = 'user')`) are the authority-record precedent | `S40-automation/design.md` AU1 |
| 15 | S10's `EffectClass` policy type exists as a **stub**: `EffectClassSpec{Effect, State}` "a tool governed by the KIND of effect it has (reading, writing, spending)", evaluator answers could-not-determine | `policy/spec_stubs.go:75-82,244`; `policy/types.go:88-90` |
| 16 | `forge.merge` refuses unless `pr.BaseRef == feature/<loop>` (phantom-merge guard) and a review gate passes; checks/threads are recorded `not-configured` on GitHub today | `action/forge_actions.go:293,383,417,438` |
| 17 | Proposal kinds are a closed CHECK; widening is a table rebuild (0042, 0045 precedent) | `store/schema.go:122,125,2420-2452` |
| 18 | The kernel peer actor is `table.ActorFor`, never params; the CLI's literal is `{user, cli}` | `daemon/daemon.go:495`; S45 design fact 6 |
| 19 | `notifyHintFor` toasts exactly three loop kinds; `loop.blocked` is one of them | `daemon/lifecycle.go:187-197` |
| 20 | `loop.blocked` carries the reason kind and sentence (`action-denied`, `action-failed`, …) | `loop/execute.go:89-93`; `loop/advance.go` blocked kinds |

## AL1 — Grants are their own authority table, not a policy type

**Decision.** `autonomy_grant` is a new table (AL10 for its history). It is **not** an S10/S11 policy type and does not use the S45 `internal/desk`
policy write path.

**Why.** (1) The policy engine answers *per request* — agent, tool, command, route — against targets `all-agents|agent|loop|lane-type|mcp`; a grant is keyed to a
**root component** and is consulted at the **code-action** site, which the policy engine deliberately does not guard (class.go header: a code action is
the path that bypasses `bash` policy). (2) The existing `effect_class` type means *tools by kind of effect* (fact 15) and is an unevaluated stub; making it
mean *actions by registry class* would redefine S16's N7 decision. (3) A grant must be **pinned** per loop and checked **live** (AL6); the policy engine has
no pin. (4) S40's precedent for standing authority is a dedicated table with three locks (fact 14). What is reused: the S45 desk for proposal validate/apply,
the generic `proposal.commit`, the human-door guard pattern, deny-wins as a rule.

## AL2 — The subject vocabulary: derived from the registry, merge only by name

A grant's subject is one of:

- `{kind: "action", name: <registry action>}` — any registry name whose class is **not** `local` (a local action needs no grant; a grant on one is refused: "X is local — it runs without a grant").
- `{kind: "class", name: "outward"}` — shorthand for **every current outward action** at the time of each check (a new outward action added later is covered by the same grant; a person who wants
  otherwise grants by action name).
- **There is no `irreversible` class subject.** An irreversible action is granted **only by its own action name**. `forge.merge` is the one today.

The vocabulary is **read from the daemon** (`autonomy.vocabulary`, AL15), built from `action.Names()` and `action.Classes()` — adding a registry action
changes the vocabulary with no edit here. A test asserts the vocabulary equals the registry.

## AL3 — States, deny wins, where each may sit

- `state` ∈ `allow | deny`. A **deny** is a *veto*: it overrides any allow covering the same action, on the root or on any of its children, regardless of order or age.
- **Allow rows exist only on a root component** (`parent_id IS NULL`). A child inherits its root's allows (one level, as S44-extra CT6). Enforced by the store (sentence) and by a trigger (backstop).
- **Deny rows may sit on a root or on a child**, so a person can say "never on this repository" without touching the project.
- A veto covers the action at **action time for every unattended loop** on that component — including a loop whose flag was typed by a person (AL7). It does **not** change an **attended** loop's ask (open decision OD2).

## AL4 — Conditions: a closed vocabulary, evaluated where each is knowable

| Condition | Value | Evaluated | Source |
| --- | --- | --- | --- |
| `requireGates` | bool | **action time**, every time | S57 `MergeRule` seam. **Forced `true` on `forge.merge`** (a merge grant without it is unrepresentable) |
| `requireBudget` | bool, default `true` on every allow | **action time** | S55 `Budget` seam: the loop's governing budgets are not exhausted |
| `labels` | string list (≤ 8, each ≤ 64) | **pin time** | the start's item label set (S27 supplies; schedule/event/webhook supply none) — empty list = any |
| `maxPerDay` | int ≥ 1 | **action time** | count of `begun` rows in `autonomy_use` for this grant in the last 24 h (AL10) |

**Cannot say ⇒ deny.** A condition whose seam is unwired, errors or answers *cannot-say* makes the verdict **deny**, with a sentence naming the condition and
what is missing ("`forge.merge` denied — grant ag-… requires the merge rule (S57) and it could not be asked: the verification service is not wired").
This is "unknown stays unknown" applied to authority: absence of an answer is never an allow. Consequence: the slice is buildable and testable **before** S55/S57,
and a merge grant is inert until S57 exists.

Conditions are a typed struct with `additionalProperties: false`; an unknown key is refused at write. No free text.

## AL5 — Expiry

`expires_at` (RFC3339, UTC) is optional for outward grants and **mandatory for `forge.merge`**, at most **`MaxMergeGrantDays = 90`** from now (a named constant, a boundary test, a
value in `autonomy.vocabulary`). Expiry is honoured **live** (AL6): an expired grant stops loops pinned under it at their next action. Expiry is never written as a row — time passing
writes nothing; the check compares `expires_at` with the store clock. A grant is **never deleted**; `revoke` sets `revoked_*` and writes a change row.

## AL6 — Revocation semantics: pinned ceiling, live floor

**Decision.** A policy-pinned loop (AL8) may take an unattended outward/irreversible action only if **both** hold at the moment of the action:

1. **Ceiling:** the action is covered by an allow in the loop's *pinned* snapshot (what the root granted when the loop started).
2. **Floor:** a **live read** shows that the same grant row is not revoked, not expired, no deny row (root or the loop's component) covers the action, and every action-time condition (AL4) is met.

So: **widening never reaches a running loop** (a new allow is not in the snapshot); **narrowing always does** (revoke, veto, expiry or a failing condition is seen at the very next action).
This matches how blueprints are pinned (the loop's frame never moves) while keeping the vision's "revocation that revokes". A revoked/vetoed/expired action blocks the loop with
reason kind `action-denied` and a sentence naming the grant id, what changed, who and when. Revocation does **not** stop a loop that is between actions: `autonomy.revoke`'s result lists
`affectedLoops` (ids of running loops pinned under that grant) so the person can stop them with S55's `loop.stop` (cross-slice request R3); AL does not stop loops itself.

**Why not live-only or pin-only?** Live-only would let a later grant change what a running loop may do (a stolen-socket attacker widening mid-run). Pin-only would make revocation a
lie until the loop ended — a loop can wait days on a question. Ceiling + floor is the only combination that is safe in both directions.

## AL7 — Which starts consult the policy (and which never do)

| Start | Consults the policy at pin time? | Why |
| --- | --- | --- |
| Schedule / webhook fire (`StartFromTrigger`) | **yes** — origin `schedule` / `webhook` | unattended by construction; nobody to declare |
| Event fire (`StartFromEvent`) | **yes** — origin `event` | AU2's "stronger reading" |
| S27 scheduler start | **yes** — origin `scheduler`, with the item's labels | the reason this slice exists |
| Person `loop.start` (socket/CLI/app) | **no** — the caller's own `allowOutward` stays | the person decides for this run (S19 D10/O2) |
| Narrow-surface trigger | **no** — `attended` forced false, caller's `allowOutward` unchanged | S19: no key-level grant may exist (`TestNoKeyLevelGrantExists`) |
| Queue advance / roadmap materialize | **no** — they carry the declared `Attended` unchanged (`daemon/queue_reactor.go:147`, `roadmap_dispatch.go:143`) | out of scope; S27 owns a scheduler-originated item start |

**Vetoes apply to every unattended loop** whatever its pin source (AL3). Grants (allows) reach **only** the policy-resolved origins above. A person can still type `allowOutward` for their own unattended
run; they cannot type a merge: **no start parameter can carry merge authority**, ever (a guard asserts `LoopStartParams` has no such field and `startLoopWith` takes merge only from `startExtras`).

The resolution lives in **one function**, `Daemon.resolveAutonomy(origin, componentRoot, labels)` (a single file), whose result enters `startLoopWith` **only** through `startExtras` — never through
`protocol.LoopStartParams`. The existing literal assignments `attended := false` (`trigger_fire.go:108`, `event_fire.go:72`) and the absence of `AllowOutward` in those files are **untouched**; the
S40 AU2 sentence "no code path … sets `allow_outward` to anything else" stays literally true because **policy-pinned loops keep `loop.allow_outward = 0`** (AL8).

## AL8 — The pin shape: one immutable row, no new `loop` column

`loop_autonomy_pin(loop_id TEXT PRIMARY KEY, root_id TEXT NOT NULL, rev INTEGER NOT NULL, origin TEXT NOT NULL CHECK (origin IN ('schedule','webhook','event','scheduler')), labels TEXT NOT NULL DEFAULT '[]',
allowed TEXT NOT NULL, pinned_at TEXT NOT NULL)` — written in the **same transaction** as the loop INSERT (`CreatePinnedLoop`, fact 7), never updated or deleted (triggers refuse both), **no FK** to `loop` (fact 8: archive moves loop rows; the pin outlives them like history does).

- `allowed` is canonical JSON: `[{"grant":"ag-…","subject":"forge.openPR","conditions":{…}}]` — the grants that applied (labels already matched), expanded from the class shorthand **by action name at pin time**.
- `rev` = `MAX(autonomy_change.id)` over the root at pin time (0 if none) — a monotonic revision with no extra table.
- A loop **has a pin row iff it was policy-resolved and at least one grant applied.** No row ⇒ today's behaviour exactly.
- **`loop.allow_outward` stays 0 for policy-pinned loops.** Setting it to 1 would make the legacy `Resolve` run **every** outward action, wider than the grants. The new path (AL9) adds permission per action from the snapshot; the legacy flag keeps meaning "the person typed it".
- Pre-S56 loops have no pin row ⇒ unchanged. Migration backfills nothing.

## AL9 — The decision site: a pure wrapper, `Resolve` untouched

New `internal/action/grant.go` (no change to `class.go`; must not contain any `TestClassesAreNotConfigurable` banned substring):

```go
type GrantContext struct {            // all fields filled by the loop package from rows, never by a caller
    Pinned    bool                    // a pin row exists
    InPin     bool                    // the action is covered by the pinned snapshot
    Live      LiveState               // revoked | expired | vetoed | conditions: met / unmet / cannot-say (each with its sentence)
}
func ResolveWithGrant(name string, attended, allowOutward bool, g GrantContext) (CheckResult, error)
```

Rules, in order — compute `base := Resolve(name, attended, allowOutward)` first (an unknown name returns its error unchanged):

1. **Local or attended** ⇒ return `base` (attended asks and local runs are not this slice's business; OD2).
2. **Unattended, non-local, vetoed** (`g.Live.Vetoed`) ⇒ `Deny`, sentence names the veto's grant id. Applies **with or without a pin** and even when the person typed `allowOutward` (AL3/AL7).
3. **No pin, or the action is not in the pinned snapshot** ⇒ return `base` (today's behaviour exactly).
4. **Pinned and covered, but a live check fails** (revoked, expired, an action-time condition unmet or *cannot-say*) ⇒ `Deny`, sentence names the grant id, the specific failure, and the revision the loop was pinned at.
5. **Pinned, covered, every live check passes** ⇒ `Run` (this is the only way an unattended irreversible action can run), sentence names the grant id and revision.

A `GrantContext{}` (zero) returns **exactly** `Resolve`'s result for every action × attended × allowOutward (an exhaustive equivalence test). The sentences added are new constants; every existing sentence is untouched.

`internal/loop/execute.go` changes at one place: after reading the pinned row (lines 68-81) it builds the `GrantContext` through a store read (AL10's `AutonomyLive`) and calls `ResolveWithGrant` instead of `Resolve`. Everything after (`switch verdict.Verdict`, `blockTo`, `askPermission`) is unchanged. A read error on the autonomy path **returns the error** (never runs) — the same fail-toward-deny rule as `LoopAllowOutward`.

## AL10 — Tables and audit (P3)

| Table | Purpose | Shape |
| --- | --- | --- |
| `autonomy_grant` | the records | `id 'ag-'+newID`, `node_id REFERENCES component(id)` (a root for allows; a root or child for denies), `subject_kind`, `subject`, `state`, `conditions` (canonical JSON), `note`, `expires_at`, `created_by_kind` — user for allow rows, user or cli for deny rows (veto is the restrictive class of revoke, AL13; amended 2026-11-01 at T9's pre-flight, before merge), `created_by_id`, `proposed_by` (agent slug or ''), `created_at`, `revoked_at/by_kind/by_id/reason` (the only columns ever updated, once); `CHECK` ties `revoked_*` together and `state='deny'` ⇒ no expiry requirement; trigger: allow rows only on roots; trigger: refuse `parent_id` change on a component with an active allow grant ("revoke X's autonomy grants before attaching it under Y") |
| `autonomy_change` | append-only audit, **one row per changing write** (grant, deny, revoke) in the same transaction | `id AUTOINCREMENT`, `at`, `root_id`, `grant_id`, `change_kind CHECK IN ('grant','deny','revoke')`, `actor_kind CHECK IN ('user','cli')`, `actor_id`, `proposed_by`, `summary` (fixed template from ids/enums — **never user text**, SEC-033); `BEFORE UPDATE/DELETE` triggers abort |
| `autonomy_use` | append-only use log | `id AUTOINCREMENT`, `at`, `loop_id`, `grant_id`, `grant_rev`, `action`, `phase CHECK IN ('begun','completed','failed')`, `begun_id` (NULL on `begun`), `evidence_ref` (the loop event id/step key; ids only); `BEFORE UPDATE/DELETE` triggers abort |

`begun` is written **before** `action.Dispatch`, `completed`/`failed` after, so a crash leaves a `begun` with no outcome — visible as "effect may have happened", never silently absent (the receipt-first shape unsafe tools already use). Reads, refusals, dry-runs and no-op revokes write nothing. Revision numbers never reset; history outlives the record (no FK from `autonomy_change`/`autonomy_use` to `autonomy_grant`).

## AL11 — The human door and three locks (P4)

Socket methods (all `RequiresInitialize`, none `Debug`; registered only when the service is wired — V13 absent-not-refusing):

| Method | Kind | Result |
| --- | --- | --- |
| `autonomy.list {rootId?, state?, includeInactive?}` | read | `{grants: GrantView[]}` |
| `autonomy.show {id}` | read | grant + conditions + last change + `usedCount` + `pinnedLoops` |
| `autonomy.vocabulary {}` | read | subjects, classes, conditions, limits (AL15) |
| `autonomy.effective {rootId, origin?, labels?}` | read | per registry action: verdict an unattended loop started **now** would get, why, grant id, each condition met/unmet/cannot-say |
| `autonomy.pinned {loopId}` | read | the loop's pin snapshot + each grant's live status and the verdict per action |
| `autonomy.changes {rootId?, grantId?, limit?}` | read | audit rows with the one-sentence description |
| `autonomy.uses {rootId?, grantId?, loopId?, limit?}` | read | use rows (begun/completed/failed) |
| `autonomy.grant {rootId, subject, conditions?, expiresAt?, note?, dryRun?}` | **write** | `{grant}` — creates an `allow` |
| `autonomy.deny {nodeId, subject, note?, dryRun?}` | **write** | `{grant}` — creates a `deny` (veto) on a root or child |
| `autonomy.revoke {id, reason, dryRun?}` | **write** | `{grant, affectedLoops[]}` |

Actor is stamped from the kernel peer (`table.ActorFor`), **never** from params; a peer that cannot be resolved **refuses**. Three locks, each a test (the AU1 pattern): (1) **no agent tool** twinned to `autonomy.grant/deny/revoke`; (2) the socket refuses an actor whose kind is not `user`; (3) the store refuses before any SQL (`created_by_kind CHECK = 'user'` and the Go function takes a `store.Actor`). The five write-side absences (narrow surface, agent services, tool registry, records catalog except the two propose twins, any non-test file under `internal/narrow|agentsvc|tools|records`) get sibling guards with canaries (T7); existing guard bodies are never edited.

## AL12 — Agents: read and restrict, never grant

Catalog specs (family `autonomy`, `internal/records/catalog.go`, data only):

| Tool | Twin | Mutates | |
| --- | --- | --- | --- |
| `autonomy_list` | `autonomy.list` | no | grants with details |
| `autonomy_show` | `autonomy.show` | no | |
| `autonomy_effective` | `autonomy.effective` | no | what an unattended loop on a root could do |
| `autonomy_propose_revoke` | `autonomy.proposeRevoke` | **yes** | inert proposal to revoke grant `id` with a required `reason` |
| `autonomy_propose_deny` | `autonomy.proposeDeny` | **yes** | inert proposal to add a veto |

**There is no `autonomy_propose_grant`.** Both propose twins create an inert `proposal` of a **new closed kind** (`autonomy_revoke`, `autonomy_deny`) through the S45 `proposal.create`/`desk.Validate` path (`proposal.kind` CHECK table rebuild — read the **current** DDL), committed by a person through S45's generic `proposal.commit` with the **committer** as actor and the agent recorded as `proposed_by`. Schemas are `additionalProperties:false` with no identity/status/kind fields; per-agent open-proposal limit is S45's constant. Registered, **not granted** (`DefaultGrant()` unchanged); the `TestRegistryExactNames` bump is counted and dated (+5, from T0's real count).

## AL13 — The CLI: a client; offline asymmetry

`aeon autonomy list|show|vocabulary|effective|pinned|changes|uses|grant|deny|revoke`, each a client of the socket methods (parity both ways, P5); `--json` prints the wire result verbatim; `--yes` is a seatbelt required on `grant` and `revoke` (not a security control). **Deviation from P5's "offline fallback for every verb", recorded:** reads fall back to a read-only direct store door; **`deny` and `revoke` (restrictive) fall back to the direct door** so a person can pull authority when the daemon is broken (actor `{user, cli}`, fixed stderr line); **`grant` never falls back** — widening authority requires the peer-attested actor ("daemon not running — grants are written through the daemon so the audit carries who asked; start the daemon, or use `aeon autonomy deny`/`revoke` to restrict"). A dial failure that is not "nobody home" refuses, as in S45 PM4.

## AL14 — One resolver, two readers

`autonomy.Plan(rootNode, origin, labels, grants, now, conds)` is a **pure** function returning, per registry action, `{verdict, class, because, grantId?, conditions[]}`. It is called by (a) `resolveAutonomy` at start to build the pin, (b) `autonomy.effective`, (c) `autonomy.pinned` (with the snapshot as input). The action-time check (AL9) uses the same condition evaluator and sentences. A test asserts `effective` for a root equals what a loop actually pinned (same inputs ⇒ same snapshot), so the legibility view cannot drift from enforcement.

## AL15 — Descriptors and the UI contract (the panel is built later)

`autonomy.vocabulary` returns everything a form needs so a client never types a closed set: `subjects[{kind,name,class,requiresExpiry,forcedConditions[]}]`, `conditions[{name,type,description,evaluated:"pin"|"action",forced?}]`, `limits{maxMergeGrantDays, maxLabels}`, `origins[]`, `states[]`. T10 specifies the Settings panel in prose (P7: lists with details, daemon-side filters, the daemon's own confirmation statement on every authority-granting action, refusals verbatim, no optimistic rows, offline ⇒ read-only) and adds the daemon-side tests the panel's guards will need; **no `ui/` code is written in this slice**.

## AL16 — What does not change; no seeds

No existing sentence in `class.go`, no existing guard body, no `LoopStartParams` field, no narrow-surface entry, no S40 trigger lock, no attended-loop ask path. **No seed set** — authority records are never seeded (S49 AN2): a fresh install, `aeon init` and the seed engine create **zero** grants. `AUTONOMY-GAPS` **AG1** is answered in the brief (§2.2).

## AL17 — Security notes

No secret is involved. Audit summaries are fixed templates from ids and enums. A grant's `note`/`reason` are user text stored on the grant row only. `autonomy.*` methods are absent from `narrow.Actions()`/`BoundaryStatement()`. A denied action never leaks the other grants' contents in its sentence (it names the one grant id that mattered). The slice never lowers a guard; where it adds permission (AL9 b) the permission is per action, per loop, per revision, and revocable.

## Open decisions (recorded defaults — change a default only with a reason in the task's Notes)

| # | Question | Default | Why |
| --- | --- | --- | --- |
| OD1 | Grants as an S10 policy type via the desk, or a separate table? | Separate table (AL1) | engine answers per request; pin + live check; stub type means something else |
| OD2 | Does a veto also change an **attended** loop's ask? | No | the person is present and decides; a veto narrows unattended autonomy |
| OD3 | Must a merge grant carry an expiry? | Yes, ≤ 90 days | irreversible + standing authority; cheap to renew |
| OD4 | May allows sit on a child? | No (roots only); denies anywhere | one level of inheritance; vetoes are restrictive |
| OD5 | May `grant` run offline from the CLI? | No; `deny`/`revoke` may | widening needs the peer-attested actor; restricting must always be possible |
| OD6 | Do S10 `effect_class` deny policies veto code actions? | No | the autonomy table has its own deny; avoids two authorities for one thing |
| OD7 | Is a per-day rate condition included? | Yes, optional (`maxPerDay`) | cheap with the use log; common ask ("no more than 5 PRs a day") |
| OD8 | Does revoke stop running loops? | No — block at next action; list `affectedLoops` for S55's `loop.stop` | stopping is S55's authority; AL only denies actions |
| OD9 | Do narrow-surface loops get policy allows? | No | S19 O2: no key-level grant |
| OD10 | Do queue/roadmap-materialized loops get policy allows? | No (S27's scheduler origin does) | they carry a declared `Attended`; widening them is S27's call |
| OD11 | Unwired/unknown condition | Deny, naming it | unknown stays unknown |
| OD12 | What does a merge grant cover? | Exactly the registry's `forge.merge` (task PR into the loop's feature branch) | landing on the default branch is not an action today (S60) |
| OD13 | Default grants for a new root or at `aeon init`? | None, ever | authority is never seeded |

## Cross-slice requests

| # | To | Request |
| --- | --- | --- |
| R1 | **S55 LC** | A seam `budget.Exhausted(ctx, loopID) (state: ok\|exhausted\|cannot-say, sentence)` for the `requireBudget` condition; the cap-hit block kind `cap-hit` must not be an AL concern |
| R2 | **S57 VG** | A seam `verify.MergeRule(ctx, loopID, prNumber) (verdict: pass\|fail\|cannot-say, sentence, evidence)` for `requireGates`; its sentences are quoted verbatim in AL's deny sentence |
| R3 | **S55 LC** | `loop.stop` (human door) so `autonomy.revoke`'s `affectedLoops` can be stopped |
| R4 | **S27 CN** | Start through the daemon start door with `origin="scheduler"` and `labels`; never pass allow/merge fields; the item's label set is the `labels` input |
| R5 | **S58 SV** | Events `autonomy.revoked` / `autonomy.used` and notify hints (the closed set in `notifyHintFor` is S58's to widen; AL publishes events only) |
| R6 | **S60 MQ** | Any new default-branch landing action must be registered as `irreversible` so it appears in this vocabulary and needs its own grant |
| R7 | **S61 GI** | Intake may *propose* a charter's autonomy intent as a note; it never writes a grant (human door) |
| R8 | **S44-extra** | `RootOf(componentID)` and `parent_id`; the attach trigger in AL10 depends on its schema |
