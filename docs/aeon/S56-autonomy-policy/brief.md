# Brief — S56 Autonomy policy: what a loop nobody started may do outward

Written 2026-10-30. Prefix **AL**. Covers [`AUTONOMY-GAPS.md`](../AUTONOMY-GAPS.md) **G2**. Follows the standing rules in
[`CONFIGURATION-ORDER.md`](../CONFIGURATION-ORDER.md) and the task shapes in [`MANAGEMENT-PATTERNS.md`](../MANAGEMENT-PATTERNS.md).
Cross-slice contracts: **S55 LC** (loop stop/cancel, budgets), **S57 VG** (verification gates, merge rule), **S27 CN** (continuous
scheduler), **S58 SV** (supervision), **S60 MQ** (merge queue). The org model is the **S44-extra component tree** (`rootId` = a root
component).

## 1. Why

Today a loop can do something outward — push, open an issue or a PR, post a review — in exactly one unattended case: the person who started
it typed `allowOutward: true` **for that run** (S19 T6, D10/O2). Everything that starts a loop with nobody present is pinned
`attended = 0` and `allow_outward = 0`: a clock-fired loop (`daemon/trigger_fire.go`), an event-fired loop (`daemon/event_fire.go`), and the
narrow surface's trigger (forced `attended=false`, `internal/narrow/trigger.go:32-46`). `forge.merge` is **irreversible** and is
**never** allowed unattended, whatever the flag says (`internal/action/class.go:171-173`).

That was right while a person was always there at start. It stops being workable the moment a scheduler (S27) or a trigger starts work
continuously: there is nobody at start time to declare anything, so every such loop would build and commit locally and **never push, open a
PR or merge** — autonomy is structurally impossible, not merely off. S40's own design recorded the way out and refused to build it:
*"The author's stronger reading is a one-line change at that call site plus a human-written column … it is recorded, not built"*
(`S40-automation/design.md` AU2). **This slice builds that column.**

## 2. What this slice delivers

1. **An autonomy policy**: human-authored **grant** rows per **root component** — `allow` or `deny`, naming an action from the closed registry
   (or the single class shorthand `outward`), with closed **conditions**, an optional **expiry**, a note, and an append-only change history.
   Default for every root: **nothing granted** — today's behaviour, byte for byte.
2. **Merge is its own grant.** `forge.merge` (irreversible) can only be granted **by its own name**, never implied by `outward`, only with
   its merge-rule condition forced on (S57's rule must pass at the moment of the merge) and a mandatory expiry. This answers
   AUTONOMY-GAPS **AG1**: *supported, default off, separately granted, only when S57's merge rule passes.*
3. **Pinned ceiling, live floor.** A loop started with no person present is pinned, in the same transaction as its blueprint, with the
   grants that applied to its root at that moment. A later **widening** never reaches a running loop. A later **revocation, veto or expiry** is
   honoured **at the next outward action** — the vision's "revocation that revokes".
4. **Every effect taken under a grant is recorded** (begun / completed, with the grant and its revision) in an append-only use log.
5. **Doors:** socket methods on the person's 0600 + peer-credential surface (daemon first); agent tools that **read** and that may only
   **propose restrictions** (revoke, deny — never a grant); an `aeon autonomy` CLI client; and the descriptors a future Settings panel renders.
6. **Legibility:** `autonomy.effective` answers "what could an unattended loop started on this root do right now, and why not the rest";
   `autonomy.pinned` answers the same for one loop and says whether each pinned grant is still live.

## 3. What exists today (measured 2026-10-30; T0 re-walks every line)

| Fact | Where |
| --- | --- |
| Effect classes `local`/`outward`/`irreversible` are **code**, closed, no setter | `internal/action/class.go:25-47,70`; `TestClassesAreNotConfigurable` `class_test.go:195` |
| Nine registry names; `forge.merge` is the only `irreversible`; five are `outward` | `class.go:52-62` |
| `Resolve(name, attended, allowOutward)`: unattended outward runs only with `allowOutward`; unattended irreversible **always denies** | `class.go:157-180` |
| The decision site reads `attended` and `allow_outward` from the **pinned loop row**, errors fail toward deny | `internal/loop/execute.go:68-93` |
| `loop.allow_outward INTEGER NOT NULL DEFAULT 0`, written once at creation | migration 0031 `schema.go:1517`; `store/loops.go:183-187,356-368` |
| Schedule and event starts hard-code `attended := false` and pass no `AllowOutward` | `daemon/trigger_fire.go:108-115`; `daemon/event_fire.go:72-78` |
| Narrow trigger forces `attended=false`; `allowOutward` rides through as the caller's own per-loop field; no key-level grant may exist | `narrow/trigger.go:21-46`; `TestNoKeyLevelGrantExists` `narrow/trigger_test.go:205` |
| `LoopStartParams.Attended`/`AllowOutward` are the only start-time declarations | `protocol/blueprint_loop.go:29-45` |
| The one start door | `daemon/start_loop.go:42-125` (`startLoopWith`, `startExtras`) |
| S10 has an `effect_class` **policy type** that is a stub (`could-not-determine`) governing *tools by kind of effect* | `policy/spec_stubs.go:75-82,244`; `policy/types.go:88-90` |
| `forge.merge` already refuses unless the PR's base is the loop's feature branch (phantom-merge guard) and a review gate passes | `action/forge_actions.go:293,383` |
| No component-level or project-level autonomy concept exists | — |

## 4. Out of scope

- **Verifying** that a merge is safe — S57's merge rule. This slice only *requires* it as a condition and fails closed when it cannot be asked.
- **Budgets and stopping** — S55 (`budget`, `loop.stop`, `loop.raise`). This slice only requires "budget not exhausted" as a condition.
- **The scheduler** — S27 calls the resolution this slice defines; it does not live here.
- **Landing the feature branch on the default branch** — not a registry action today; S60 would add one and it would appear in this
  slice's vocabulary automatically (it derives from `action.Names()`).
- The Settings panel itself (descriptors and a UI specification are delivered; building the UI is after).
- Changing what a **person-started** or **attended** loop may do. Those paths are untouched.
- Seeding any grant. Authority records are **never seeded** (S49 AN2, S40 AU1 lock 3).

## 5. Acceptance

1. With **no grant rows**, every behaviour is byte-identical: the 9×2×2 `Resolve` matrix, every S19/S20/S40 pin test, every guard — proven by T1's
   goldens run unedited after the slice.
2. With an `allow forge.openPR` grant on a root, a schedule- or event-started loop on that root opens a PR (forge fake sees it); on another root it is
   denied with T2's sentence verbatim.
3. `forge.merge` runs unattended **only** with its own unexpired grant **and** the S57 merge-rule condition met at the moment of the call; `outward`
   never implies it; a missing/unwired condition denies with a sentence naming it.
4. A grant revoked, vetoed or expired **after** a loop was pinned stops that loop's **next** outward action; a grant added after the pin never widens it.
5. Authoring is human-door only: absent from the narrow surface, agent services, tool registry and records catalog (except the two named restrictive
   propose twins), each guard shown to fail by a canary; an agent can never create an `allow`.
6. Every change and every unattended effect taken under a grant is recorded append-only; summaries are fixed templates from ids and enums.
7. `make protogen && make check-protogen && make guard` green; migration drills (fresh, upgrade, idempotent re-open, append-only proofs) pass.

## 6. Status

**Built 2026-11-02** (12/12; close-out evidence in `tasks/T11.md`, walked record in `DEMO.md` §S56). **Depends:** S44-extra (root components)
and S45 (`internal/desk`, generic `proposal.commit`, guard pattern) — both were merged before T8 started, and the branch was rebased onto
S45-landed main (`169cf1ee`). **Conditions need:** S55 and S57 — **still nil in production at close-out**: S55 is store-layer only (4/13) and
S57 is not built, so `requireBudget` and `requireGates` answer cannot-say on every production start, and **a merge grant, though grantable,
pinnable and auditable, is inert until S57 wires its `verify.MergeRule` seam** (the walk proves the met path with a stubbed seam). **Feeds:** S27.

**Handoff to S27 (R4) — the exact call.** S27 starts its loops through the daemon's one start door, `startLoopWith`
(`daemon/start_loop.go`), carrying the pin from `resolveAutonomy(ctx, autonomy.OriginScheduler, componentRoot, labels)`:
`origin="scheduler"`, the item's label set as `labels` (the pin's condition matching reads them), and **never authority fields** — there is
no allow/merge field on the wire for S27 to pass; the pin is internal to the daemon, resolved once at pin time, and written in the same
transaction as the loop. `autonomy.OriginScheduler` is already reserved by this slice — referenced only by its pin test, and no S56 start
path calls it — so S27's job is exactly one: its own scheduler fire path that reaches `startLoopWith` with that origin and labels, plus the
`loop.stop` it takes from S55 to act on `autonomy.revoke`'s `affectedLoops` (R3).
