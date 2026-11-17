# Brief — S60 Integration: a merge queue, catch-up and conflict handling

Written 2026-10-30. Prefix **MQ**. Covers [`AUTONOMY-GAPS.md`](../AUTONOMY-GAPS.md) **G4** ("nothing serialises the landing of parallel work"). Shapes from
[`MANAGEMENT-PATTERNS.md`](../MANAGEMENT-PATTERNS.md) (**P1–P6, P9–P11**; P7 only for a bench-level view, P8 does not apply — no seed set of its own).
**Depends on:** S16 (forge and git actions, built), S32 (worktree ledger — **closed first**, see §5), S44-extra (the component tree and
`EffectiveComponent`), S48 (blueprints as rows — two seeded blueprints), S56 (the `merge` grant), S57 (verification gates and the merge rule),
S45 (`internal/desk`, generic `proposal.commit`), S47 (surface classification). Cross-slice contracts are in `design.md` §Cross-slice requests.

## 1. Why

The gap document said S32 "fans out branches; nothing serialises their landing". Reading the code shows the gap is **larger**: today **nothing
lands a feature branch on the repository's default branch at all.** `dev-loop` creates `feature/<loop>`, merges every task PR **into the feature
branch** with `forge.merge`, and stops (the old finish lane was removed — `dev-loop.yaml:168-185`: "leave-open is what the loop already built").
`development-map-pipeline` ends with a `merge-feature` step that calls the same `forge.merge`, whose guard **refuses any PR whose base is not the
feature branch** (`forge_actions.go:318-326`), and the `which-base` gate that asks "main or unstable?" has no Go consumer. So an autonomous
project that finishes work has a feature branch full of merged tasks and a person who must open and merge the last PR by hand. And when two loops
on one repository do reach the end, nothing orders them, catches either up with a moved base, or knows what to do with a conflict.

## 2. What this slice delivers

1. **A per-repository merge queue**: rows ordered by position, **one landing at a time per base branch**, enforced by a partial unique index, with
   an append-only transition history (who moved what, why — fixed templates, never user text).
2. **Work done by loops, not by a new process.** Each stage of a landing is a **loop** on a seeded blueprint (`land-feature`, `resolve-conflict`)
   so cost, suspend/resume, crash recovery, attendedness (G14), effect classes, S55 stop and S53 history all apply unchanged. The queue owns
   *order and exclusivity*; loops own *work*.
3. **Catch-up without rewriting history.** There is no rebase and no force-push (`TestNoForcePushAnywhere` stays green, unchanged). A branch that
   is behind is brought current by **merge-forward onto a fresh integration ref** (`integrate/<…>`) built in a **dedicated worktree**; the
   person's checkout is never touched.
4. **Six new closed-registry actions** with effect classes: `git.integrate` (local), `git.concludeIntegration` (local),
   `git.publishIntegration` (outward), `forge.openLandingPR` (outward), `forge.land` (**irreversible**, its own phantom-merge guard generalised to
   the queue's base), `queue.enqueue` (local).
5. **Conflicts become a state with a way out**: attended ⇒ a question; unattended ⇒ a resolution loop on `resolve-conflict` (if the repository's
   conflict policy says so) or a blocked item surfaced to S58; **never a side chosen without a verification pass**; conflicting **paths** are
   recorded, **hunks never are**.
6. **Stale detection** as an honest read (`behind N`, `unavailable` — never a confident zero) and **re-verification after every integration**: a
   green gate is bound to the head SHA it verified, and landing refuses if the head moved.
7. **Branch settings finally used**: `default_branch` and `branch_prefix` (stored since S38, read by nothing) resolve through S44-extra's
   `EffectiveComponent`; names are byte-identical while settings are empty (characterized first), and are **pinned per loop** so a later edit
   cannot orphan a running loop's branches.
8. **Daemon first, CLI second, tools third**: `mergequeue.list|show|enqueue|dequeue|reorder|retry|status|settings|vocabulary` socket methods;
   `aeon mergequeue …` as their client; agent read tools, an own-item `dequeue`, and a **propose-only** enqueue.

## 3. What is true today (measured 2026-10-30; T0 re-walks every line)

| Fact | Where |
| --- | --- |
| Nine registry actions, closed and pinned by tests; classes are code | `internal/action/class.go:52-70`; `registry_test.go:23,70` |
| Branch names hardcoded `task/<loop>/<task>`, `feature/<loop>` | `action/git_actions.go:62-65` |
| Feature branch is created with `git branch <name>` — from whatever HEAD the person's checkout is on | `git_actions.go:~120` (`CreateBranch`) |
| Every action runs in `Dir = st.Root`, base always derived as `feature/<loop>` | `loop/execute.go:226-262` |
| `forge.openPR` head is always the task branch; `forge.merge` refuses base ≠ feature, then post-checks `IsAncestor` | `forge_actions.go:145-191, 297-361` |
| `mergeGate` refuses with no review or a non-approve latest review; `checks`/`threads` are "not reported" on GitHub today | `forge_actions.go:383+`; S57 owns `ListStatuses` |
| `git.mergeBranch` does `checkout <task>` **in the person's tree** and merges feature into it; it is the only local conflict detection; embedded in **no** blueprint | `git_actions.go:197-224` |
| No rebase, no force-push, no branch or tag deletion exist anywhere in `internal/git`, and a test greps for them | `git/worktree_test.go:108-140` |
| `component.default_branch` / `branch_prefix` stored and returned, read by no consumer; `loop.default_branch/base_branch/branch_mode/feature_branch` columns exist with no non-test writer | `daemon/components.go:50,95,128`; `store/loops.go:32-35,248-261` |
| S32 ledger `loop_worktree`, `owner_kind IN ('lane','task')`, path `<componentRoot>/.worktrees/<kind>-<id>` | `migrations/20261030071034_s32_parallel_agents.sql`; `loop/advance_parallel.go:353` |
| **The unit's worktree is not the unit's workspace**: `runOneUnit` creates it, then calls `dep.Model(ctx, unitSt, …)` with the loop's own state; the daemon builds `WorkspaceRoot: st.Root` | `advance_parallel.go:209-221`; `daemon/blueprint.go:645` |
| `AddWorktree`'s "branch exists ⇒ already made" shortcut returns without creating the directory | `git_actions.go:240-250` |
| `loop.finished`/`loop.blocked` reactors exist for queue, roadmap, triage — the model for a merge-queue reactor | `daemon/queue_reactor.go`, `roadmap_reactor.go`, `daemon/blueprint.go:567-612` |
| Loop archive **moves** a loop out of the live tables | `store/archive.go` — so a queue row must not FK to `loop` |

## 4. Out of scope

- A forge-native merge queue (GitHub's); the PR-per-landing model is what this slice builds (open decision OD1).
- Rebasing or force-pushing anything, ever; deleting branches or tags (stale `integrate/*` refs are listed, never deleted — N9 stands).
- Releases, tags, deploys (G11).
- More than one forge (G12) and cross-repository ordering.
- The tree UI. A bench-level view only (T11); the product panel follows the app work.
- Choosing the **merge rule** (reviews, checks): S57. This slice *consumes* it before `forge.land`.

## 5. Dependency on S32 (stated, not hidden)

S32 is **Partial** (7/9). Two of its open points block this slice's adoption task (T5): (a) parallel units do not run inside their worktrees
(table above); (b) `AddWorktree`'s reuse shortcut. T0 re-verifies both. **Default:** this slice builds the one missing seam additively —
`RuntimeState.WorkDir`, an optional per-step workspace override that the daemon's step builder honours — and S32's own close-out adopts it as a
free closure of its gap. S32's documents are not edited from here. If S32 closes the gap first, T5 reuses its seam instead.

## 6. Acceptance

1. Two loops finishing on one repository are landed **one at a time, in position order**; the schema refuses a second active slot (proved by raw SQL).
2. A feature branch whose base moved is caught up by merge-forward onto a new `integrate/…` ref in its own worktree; the person's checkout, branch
   and working tree are byte-identical before and after (a fixture proves it).
3. A real two-branch **conflict** fixture ends `conflicted` with the conflicting **paths** recorded and **no hunk text anywhere in the database or events**.
4. With mode `off` (the default) nothing is enqueued automatically and nothing lands; with `auto` and no S56 merge grant, an unattended landing loop is
   **denied at `forge.land`** with the existing G14 sentence and the item ends `failed(reason=denied)`.
5. A gate green for head SHA A does not permit landing head SHA B (`forge.land` refuses a mismatched `verified_sha`).
6. Branch names with empty settings are byte-identical to today's goldens; with `branchPrefix = "aeon/"` they carry it; a running loop's names do not change
   when the setting does.
7. `TestNoForcePushAnywhere`, `TestRegistryIsClosedAsserted`, every G14 test and the S19 narrow-surface guards pass **unedited**; the registry count bump is deliberate and dated.
8. CLI ⇄ socket parity holds in both directions; the human-door guard (P4) has teeth; `make protogen && make check-protogen && make guard` green.

## 7. Status

**Built 2026-11-04 (13/13, T0–T12).** A per-repository merge queue lands work **merge-forward**: rows ordered per base branch, **one landing slot** held by CAS, catch-up cuts `integrate/mq-<item>` refs and merges — never a rebase, never a force-push, refs kept after landing (MQ6); conflicts are **paths and counts only** (SEC-033) on the `ask/resolve/abort` policy via the resolution loop; verification is **bound to the integration sha** and fails closed — no gate evidence, no green. Queue authority is a **human door** (MQ13: ten `mergequeue.*` socket methods, absent from every non-human surface; `mergequeue.proposeEnqueue` is the agent's only filing door and the `mergequeue_enqueue` kind is inert until a person commits); the ten CLI verbs are socket clients with no offline arm (the slice's one P5 deviation). The G14 floor is untouched and re-pinned: an unattended land **denies** unless S56's separate merge grant exists, and `--allow-outward` never grants land. T0's merge states held: S44-extra/S56/S57 merged ⇒ their seams are live; S48/S32 not merged ⇒ the landing loop rides shipped blueprints and T5 shipped the `integration` worktree-ledger owner as its named default. Close-out evidence — acceptance table, migration drills, the walked `aeon/scripts/qa-s60-mergequeue.sh` (rc=0, 2026-11-04) with its quoted lanes, and the suite-vs-T0 comparison — is in [`tasks/T12.md`](tasks/T12.md) and [SLICES](../SLICES.md); the walk is recorded at [DEMO §S60](../DEMO.md). Slot: **S60**.
