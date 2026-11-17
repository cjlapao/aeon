# Design — S60 Integration: merge queue, catch-up, conflicts

Rules **MQ1–MQ18**, cited from `plan.md` and the task files. Task shapes follow [`MANAGEMENT-PATTERNS.md`](../MANAGEMENT-PATTERNS.md) (P1–P11);
a deviation is recorded here with its reason. Facts in §Verified facts are re-walked by T0.

## Verified facts (read 2026-10-30)

| # | Fact | Where |
| --- | --- | --- |
| F1 | `classes` is a package-private map of **nine** names; `Resolve(name, attended, allowOutward)`: unattended outward runs only with `allowOutward`; unattended irreversible **always** denies, sentence `… (loop is unattended; allowOutward does not grant it)` | `action/class.go:52-70,157-180` |
| F2 | The registry is closed and asserted; counts pinned in tests | `action/registry.go` header; `registry_test.go:23,70,253,289`; `review_test.go:215` ("one receipt-first and one natural of the nine") |
| F3 | `BranchNames{Loop,Task}`: `TaskBranch()="task/"+loop+"/"+task`, `FeatureBranch()="feature/"+loop`; identities validated by `^[a-z0-9][a-z0-9._-]*$` | `git_actions.go:40-65` |
| F4 | `CreateBranch`: local-exists ⇒ reuse; remote-exists ⇒ reuse; else `git branch <name>` **from current HEAD**; a task branch is then checked out **in `dir`** | `git_actions.go:94-130` |
| F5 | `actionArgs`: `Dir = st.Root`; `Base = names.FeatureBranch()` whenever a task is known; `pr_number` from any evidence row | `loop/execute.go:226-262` |
| F6 | `OpenPR` head is always `names.TaskBranch()`; base required; remote head must exist | `forge_actions.go:145-191` |
| F7 | `MergePR`: PR base must equal the feature branch (else `KindRefused`), then `mergeGate`, then forge merge (default **squash**), then `IsAncestor(mergeSHA, feature)` else `PhantomMergeError` | `forge_actions.go:297-361` |
| F8 | `mergeGate`: no reviews ⇒ refuse; latest review must be `approve`; an outstanding `request_changes` is cleared only by the same author's later approve; `ListStatuses`/`ListUnresolvedThreads` absent on `GitHub`/`Fake` ⇒ "not-configured" evidence | `forge_actions.go:383+`; `forge/forge.go:132-138` |
| F9 | `MergeBranch` checks out the **task** branch in `dir` and `git merge --no-edit feature`; `CONFLICT` in the output ⇒ `GitError{KindConflict}`; no blueprint embeds it | `git_actions.go:197-224` |
| F10 | `git` package surface: no rebase, no force-push, no branch/tag deletion; `TestNoForcePushAnywhere` bans `--force`, `force-with-lease`, `+refs/`, `branch -D/-d`, `push -f`, `update-ref -d`, `tag -d` over every `.go` line | `git/plumbing.go` (methods listed at 72-289); `git/worktree_test.go:108-140` |
| F11 | `Push` is `push -u origin <branch>` then re-reads `ls-remote` (`push-unverified` if different) | `git/plumbing.go:220` |
| F12 | `dev-loop`: `git-feature-start` (`git.createBranch which:feature`) → per task `…pr → review → merge (forge.merge) → close-issue`; terminal feature-gate lane **removed**; no step lands the feature on the default branch | `blueprints/dev-loop.yaml:51-59,131-139,168-185` |
| F13 | `development-map-pipeline`: `merge-feature` = `forge.merge` (guard F7 requires base = feature); gate `which-base` answers `main`, `unstable` or `abort` has **no Go consumer** (only the YAML names it) | `blueprints/development-map-pipeline.yaml:95-113` |
| F14 | `component.default_branch`/`branch_prefix` flow only through `component.create/update` and are returned; `loop.default_branch/base_branch/branch_mode/feature_branch` are scanned in `store/loops.go:248-261` and written by nobody | `daemon/components.go:50,95,128`; `store/loops.go:32-35` |
| F15 | S32: `loop_worktree(id, component_root, loop_slug, owner_kind CHECK IN ('lane','task'), owner_id, path, branch, base_sha, created_at, removed_at, UNIQUE(component_root, loop_slug, owner_kind, owner_id, removed_at), FK→loop ON DELETE CASCADE)` | `migrations/20261030071034_s32_parallel_agents.sql` |
| F16 | `runOneUnit` → `ensureWorktree` (ledger row first, then `git worktree add`) → `dep.Model(ctx, unitSt, …)`; the daemon's step builder sets `WorkspaceRoot: st.Root`; `wt.Path` is used only for removal | `loop/advance_parallel.go:209-221,231-268,280`; `daemon/blueprint.go:645` |
| F17 | `AddWorktree` returns `Reused` when the branch exists, without creating the directory; base = feature tip else HEAD | `git_actions.go:240-270` |
| F18 | `ReconcileWorktrees` three cases (reattach, rebuild, cleanup); `terminalOwner` knows `task` and `lane` only | `loop/reconcile_worktree.go:46-170` |
| F19 | Reactors on `loop.finished`/`loop.blocked`: `onQueueTerminal`, `onRoadmapTerminal`, `onTriageTerminal`; started from `publishLoop` in goroutines | `daemon/blueprint.go:567-612`; `daemon/queue_reactor.go` |
| F20 | `loop.archive` moves a loop out of the live tables in one transaction ⇒ a durable record that must outlive the loop cannot FK to `loop` | `store/archive.go:15-40` |
| F21 | Blocked-reason kinds already include `action-denied`, `action-failed`, `action-denied-by-person`; blocked loops are retryable only for missing handlers today | `loop/execute.go:41,89-93`; `loop/retry.go` |
| F22 | `notifyHintFor` is a closed set of three kinds (`loop.finished`, `loop.blocked`, `loop.waiting`) | `daemon/lifecycle.go:187-197` |
| F23 | `workspace` package reads git state read-only with a scrubbed env, three states `ok`, `not-a-repo` or `unavailable`; ahead/behind are pointers | `workspace/git.go` header and `gitEnv` (559-573) |
| F24 | `queue_item` (S24) is keyed `component_root NOT NULL REFERENCES component_root(component_root)` with `position`/`attended`; the model for this slice's table shapes | `store/schema.go:2225`; `store/queue.go` |

## MQ1 — Scope and vocabulary

The queue lands **one feature branch onto one base branch of one repository**. A **repository** is a component with a primary root (S44-extra CT3):
a standalone root or a child; a pure project root has none and no queue. Everything is keyed by `component_root` (the path, as `queue_item`,
`roadmap_item`, `bug` do — F24); the component id is derived with `ComponentForRoot`. Words used everywhere: **item** (a queue row), **branch**
(the feature branch being landed, `feature/<loop>` or any named branch a person enqueues), **base** (the branch it lands on), **integration ref**
(`integrate/<…>`, a new branch built by catch-up), **slot** (the one active landing per `(component_root, base_branch)`), **landing loop** and
**resolution loop** (loops on seeded blueprints).

## MQ2 — The queue is order and exclusivity; loops do the work

The queue never runs git or the forge itself. A daemon **driver** (`internal/mergequeue` pure core + `daemon/mergequeue_reactor.go`, the F19 shape)
decides *which item is next* and *which stage it is in*, and **starts loops** for the work: a **landing loop** on blueprint `land-feature` and, on
conflict, a **resolution loop** on `resolve-conflict`. Consequences, each a test: cost, suspend/resume, crash recovery, history and S55 stop come
free; attendedness is the loop's own pinned declaration (G14); every outward/irreversible step goes through `action.Resolve` unchanged. The driver
is an ordinary caller of `loop.start` (the S27 argument), never an agent.

## MQ3 — One landing at a time, physically

States (closed, a `CHECK`): `queued`, `catching_up`, `verifying`, `ready`, `merging`, `merged`, `conflicted`, `failed`, `removed`
(the operator's list with `rebasing` named `catching_up`, since nothing is rebased — MQ6).

- **Slot** = status ∈ {`catching_up`, `verifying`, `ready`, `merging`}. A partial unique index
  `merge_queue_one_slot ON merge_queue_item (component_root, base_branch) WHERE status IN (…slot…)` makes a second active landing a database error.
- **Open** = slot states ∪ {`queued`, `conflicted`}. A second open item for the same branch is refused by
  `merge_queue_one_open ON (component_root, branch) WHERE status IN (…open…)`.
- `conflicted` **releases the slot** (a conflict must not stall every item behind it — OD2); when resolved the item returns to `queued` at its
  **original position** (positions are a stable integer with gaps; reorder rewrites in one transaction).
- Terminal: `merged`, `failed`, `removed`. `failed` carries a closed `reason_kind` (§MQ4); `retry` creates **no** new row — it moves `failed → queued`
  (history keeps both transitions).

## MQ4 — Rows and the append-only history (P3)

Migration **A** (`<ts>_merge_queue.sql`):

```sql
CREATE TABLE merge_queue_item (
  id                  TEXT PRIMARY KEY,                      -- 'mq-' || newID()
  component_root      TEXT NOT NULL REFERENCES component_root (component_root),
  base_branch         TEXT NOT NULL,
  branch              TEXT NOT NULL,
  loop_slug           TEXT,                                  -- the building loop; plain TEXT, NO FK (F20)
  head_sha            TEXT NOT NULL,                         -- branch tip at enqueue / last catch-up
  base_sha            TEXT NOT NULL,                         -- base tip last integrated
  integration_branch  TEXT,
  integration_sha     TEXT,
  pr_number           INTEGER,
  verified_sha        TEXT,                                  -- the head the last green gate covered (MQ11)
  landed_sha          TEXT,
  landing_loop_slug   TEXT,
  resolve_loop_slug   TEXT,
  status              TEXT NOT NULL CHECK (status IN ('queued','catching_up','verifying','ready','merging','merged','conflicted','failed','removed')),
  reason_kind         TEXT CHECK (reason_kind IS NULL OR reason_kind IN
                        ('denied','conflict','gate-failed','head-moved','protected','forge','git','stale-exhausted','removed-by-person','loop-stopped','unknown')),
  position            INTEGER NOT NULL,
  attended            INTEGER NOT NULL DEFAULT 0 CHECK (attended IN (0,1)),   -- declared at enqueue, never inferred (S24 D5/D6 shape)
  catch_ups           INTEGER NOT NULL DEFAULT 0,
  enqueued_by_kind    TEXT NOT NULL CHECK (enqueued_by_kind IN ('user','cli','blueprint','daemon','proposal')),
  enqueued_by_id      TEXT NOT NULL,
  created_at          TEXT NOT NULL,
  updated_at          TEXT NOT NULL
);
CREATE UNIQUE INDEX merge_queue_one_slot ON merge_queue_item (component_root, base_branch)
  WHERE status IN ('catching_up','verifying','ready','merging');
CREATE UNIQUE INDEX merge_queue_one_open ON merge_queue_item (component_root, branch)
  WHERE status IN ('queued','catching_up','verifying','ready','merging','conflicted');
CREATE INDEX merge_queue_order ON merge_queue_item (component_root, base_branch, position);

CREATE TABLE merge_queue_event (                           -- append-only by trigger (P3)
  id              INTEGER PRIMARY KEY AUTOINCREMENT,
  component_root  TEXT NOT NULL,
  item_id         TEXT,                                    -- NULL for a config change; NO FK (history outlives the record)
  kind            TEXT NOT NULL CHECK (kind IN ('transition','reorder','config')),
  from_status     TEXT, to_status TEXT,
  reason_kind     TEXT,
  actor_kind      TEXT NOT NULL CHECK (actor_kind IN ('user','cli','blueprint','daemon','agent')),
  actor_id        TEXT NOT NULL,
  detail          TEXT NOT NULL DEFAULT '{}',              -- ids, counts, enums, SHAs, PATHS — never hunk text, never user prose
  at              TEXT NOT NULL
);
CREATE TRIGGER merge_queue_event_no_update BEFORE UPDATE ON merge_queue_event BEGIN SELECT RAISE(ABORT,'merge_queue_event is append-only'); END;
CREATE TRIGGER merge_queue_event_no_delete BEFORE DELETE ON merge_queue_event BEGIN SELECT RAISE(ABORT,'merge_queue_event is append-only'); END;

CREATE TABLE merge_queue_config (                          -- one row per repository; absent row = mode 'off'
  component_root  TEXT PRIMARY KEY REFERENCES component_root (component_root),
  mode            TEXT NOT NULL DEFAULT 'off'      CHECK (mode IN ('off','manual','auto')),
  conflict_policy TEXT NOT NULL DEFAULT 'ask'      CHECK (conflict_policy IN ('ask','resolve','block')),
  land_method     TEXT NOT NULL DEFAULT 'squash'   CHECK (land_method IN ('squash','merge')),
  max_catch_ups   INTEGER NOT NULL DEFAULT 3       CHECK (max_catch_ups BETWEEN 1 AND 10),
  updated_at      TEXT NOT NULL,
  updated_by_kind TEXT NOT NULL, updated_by_id TEXT NOT NULL
);
```

`merge_queue_item` rows are never deleted. Every **changing** write appends one event row **in the same transaction** (none for reads, refusals,
dry-runs or no-ops). The actor is the kernel peer via `table.ActorFor` for socket writes, `{blueprint, <loop slug>}` for `queue.enqueue`, and
`{daemon, mergequeue}` for the driver — never params. `detail` is built by one function from ids, counts and enums; a test greps that no free-text
column can reach it (SEC-033).

## MQ5 — The state machine (pure core, `internal/mergequeue`)

`Next(item, facts) Decision` is a pure function; facts are values the reactor gathers (loop statuses, branch/base SHAs, `behind`, config, S56
grant, S57 gate evidence). Transitions (anything else is refused with a sentence naming the legal moves):

| From | Event | To | Work started |
| --- | --- | --- | --- |
| — | enqueue | `queued` | — |
| `queued` | head of its base, slot free, config `mode ≠ off` | `catching_up` | landing loop (blueprint `land-feature`) started |
| `catching_up` | integration built, no conflict | `verifying` | (same loop continues) |
| `catching_up` | conflict | `conflicted` | per `conflict_policy` (MQ9) |
| `verifying` | gates green for `integration_sha` | `ready` | — |
| `verifying` | gates red | `failed(gate-failed)` | — |
| `ready` | mode `auto` ∧ S56 merge grant ∧ S57 merge rule satisfied, **or** a person lands | `merging` | `forge.land` step runs |
| `merging` | landed, `IsAncestor` proved | `merged` | — |
| `merging` | refused (protection, head moved, denied) | `failed(<reason>)` or back to `queued` when the base moved (bounded by `max_catch_ups`) | — |
| open | person dequeues | `removed(removed-by-person)` | landing loop stopped via S55 `loop.stop` |
| `failed` | `retry` | `queued` | — |
| `conflicted` | resolution loop finished `done` | `queued` (original position) | — |

`mode`: `off` (default; nothing starts, **nothing is enqueued automatically**; a person's `enqueue` is accepted and sits `queued`), `manual` (items advance
to `ready` and wait for a person's `land`), `auto` (advances through `merging` when every grant and rule allows). The reactor runs on
`loop.finished`/`loop.blocked` of landing/resolution loops, on `enqueue`/`dequeue`/`reorder`/`settings`, and on a periodic tick that also **reconciles
every open item against its loop and git** at startup (idempotent: it re-derives, never replays).

## MQ6 — No history rewrite: catch-up is merge-forward onto a new ref (decision recorded)

`git.rebase` is **not built** and `TestNoForcePushAnywhere` is not touched. A rebased branch that was already pushed can only be published by a
force-push, which the project removed as a capability (N8/N9, F10); a rebase of an unpublished branch buys nothing the queue needs. Instead:

1. `git.integrate` (local) creates, **in a dedicated integration worktree** (MQ7), a **new** branch `<prefix>integrate/<item-id>` at the **current base
   tip** and merges the feature branch into it (`git merge --no-edit`, the F9 mechanism). Result: a branch whose first parent is the base tip — it
   fast-forwards onto the base whenever the base has not moved again.
2. Conflict ⇒ the merge is **left in progress** in the worktree (index with unmerged paths) and the action returns a typed `ConflictResult`
   (paths only). Nothing is committed, nothing pushed.
3. `git.publishIntegration` (outward) pushes `integrate/<item-id>` with the existing plain `Push` (F11) — a **new ref**, so a plain push always
   succeeds as a fast-forward of nothing.
4. If the base moves again before landing, the next catch-up builds a **new** `integrate/<item-id>-<n>` ref (`n = catch_ups`), bounded by
   `max_catch_ups`. Old refs are left (listed by `mergequeue.show`, never deleted — N9; forge "delete head branch on merge" settings or a person
   clean them).

The banned-token list is unchanged and still passes; a new sibling test asserts the new code adds **no** banned token and no `rebase`.

## MQ7 — Never touch the person's checkout; integration worktrees ride the S32 ledger

F4/F9 show today's actions check branches out **in `dir`** (the person's tree). The queue never does: every integration happens in
`<componentRoot>/.worktrees/integrate-<item-id>` created with `git worktree add -b <integration branch> <path> <base tip>`. The worktree is recorded
in `loop_worktree` with a **widened** `owner_kind CHECK IN ('lane','task','integration')` (table rebuild, T2) and `owner_id = <item id>`, so S32's
write-ahead order (row first, then `git worktree add`), settle-once removal and `ReconcileWorktrees` (extended with a `terminalOwner` case for
`integration`: terminal when the item is `merged|failed|removed`) apply unchanged. `AddWorktree`'s F17 shortcut is **not** reused: a new
`AddIntegrationWorktree` verifies the directory exists when the branch does, and rebuilds it if not.
Steps of the landing loop must run **inside** the worktree: T5 adds the optional `RuntimeState.WorkDir` seam (§Brief 5) that the daemon's step
builder (`blueprint.go:645`) and `actionArgs` (`execute.go:226`) honour — `WorkDir` empty ⇒ today's behaviour byte-identically.

## MQ8 — New closed-registry actions (effect classes are code)

| Action | Class | Does | Strategy | Evidence |
| --- | --- | --- | --- | --- |
| `queue.enqueue` | local | writes/reuses the queue row for **this loop's** feature branch, declares `attended` from the loop pin; refuses if the repo has no known base (MQ15) | natural-idempotent (one open row per branch) | `queue_item_id` |
| `git.integrate` | local | MQ6 steps 1–2 in the integration worktree | check-then-act (branch exists ⇒ reuse/rebuild) | `integration_branch`, `integration_sha`, `integration_dir`, or `conflict_paths` |
| `git.concludeIntegration` | local | after a resolution: refuses if any unmerged path or conflict marker remains (`<<<<<<<`/`>>>>>>>` scan of tracked files), stages, commits the merge, asserts a clean tree | check-then-act | `integration_sha` |
| `git.publishIntegration` | **outward** | plain `Push` of an `integrate/` ref only (a regex refuses any other name); verified by `ls-remote` | check-then-act | `pushed_sha` |
| `forge.openLandingPR` | **outward** | opens (or reuses, keyed on head ref) the PR `head = integration branch`, `base = item base`; body from a fixed template + the loop's own summary prose | check-then-act | `pr_number` |
| `forge.land` | **irreversible** | MQ12 | check-then-act | `merge_sha`, `review_*`, `checks`, `threads`, `verified_sha` |

Registry count **9 → 15** is a **deliberate, dated, counted** bump of `TestEightActionsNoRender`'s sibling data and the "of the nine" assertion
(`review_test.go:215`) — logic untouched, data only, comment line "DELIBERATE (S60 MQ8, 2026-10-30)". `Dispatch` remains the only execution door;
the closed-registry test and the "no blueprint can introduce an action" test pass unedited. The six names are **not** added to any narrow
surface or agent tool (P4 guard, MQ13).

## MQ9 — Conflicts: a state with three exits, never a silent side

On `git.integrate`'s conflict the action fails with a typed `ConflictError{Paths}`; `executeCodeAction` maps it to a **new blocked-reason kind
`action-conflict`** (F21) carrying the **paths** (bounded count) — the loop is `blocked`, as every other action failure is. The driver (reacting to
`loop.blocked`) reads that kind and moves the item `catching_up → conflicted`, recording `detail={"paths":[…],"count":N}` — **paths only; the hunks
exist only in the worktree** (they can contain anything; the database and event log never see them).

`conflict_policy` then chooses:

- **`ask`** (default): attended item ⇒ the landing loop's gate question (`resolve | abort`, a closed set, answered by a person) — `resolve` starts a
  resolution loop; `abort` ⇒ `failed(conflict)`. **Unattended** item ⇒ `ask` collapses to **blocked-visible**: the item stays `conflicted`, the S58
  inbox shows it (event kind `mergequeue.conflicted` — cross-slice request), nothing else happens. (The G14 rule: an unattended `ask` denies at once.)
- **`resolve`**: the driver starts a **resolution loop** on `resolve-conflict` (lanes: `resolve` model-step with the engineer selected `by-tech-tag`
  **inside the integration worktree**; `conclude` code-action `git.concludeIntegration`; `verify` gate/verifier step; `after: done`). The loop **cannot
  land anything**: its blueprint contains no outward or irreversible action (a test enumerates its steps).
- **`block`**: stays `conflicted` for a person.

**No auto-resolution without verification** is structural: a resolved item re-enters `queued` and goes through the *whole* landing pipeline again,
including `verifying` against the **new** `integration_sha` (MQ11) — the resolver's own claim of success is never the evidence.

## MQ10 — Stale detection (a read, three states)

`mergequeue.status` reports, per open item, `behind` = commits on `origin/<base>` not in the branch and `ahead`, from `internal/workspace`'s
read-only, scrubbed-env git (F23). **Three states, never two** (P31): `ok{behind,ahead}`, `unavailable{reason}` (no remote ref, fetch not recent, git
error), `not-a-repo`. `unavailable` never renders as `0`. The driver treats `behind > 0` at `ready` as "base moved" (MQ5). Fetching is a daemon-owned
`git fetch origin <base>` (local effect, no ref created) performed by the driver before it decides; a failed fetch is `unavailable`, not an error.

## MQ11 — Re-verification is bound to the head it verified

`verifying` runs the gates **inside the integration worktree** (S57's gate step; until S57 lands, `land-feature` uses the existing `verifier-engineer`
model step and T0 records that interim). The pass records `verified_sha = integration_sha`. `forge.land` receives `verified_sha` as evidence and
**refuses** if the PR's current head SHA (read from the forge) ≠ `verified_sha`, or if `integration_sha ≠ verified_sha` (`head-moved`). So a green
result for A can never land B, and a push to the integration ref after verification invalidates it. The S57 merge rule (reviews/checks) is evaluated
**before** `forge.land` is reachable (MQ12).

## MQ12 — Landing: a direct PR merge, one guard family generalised

`forge.land` is a **new** action, not an edit of `forge.merge` (whose guard and tests stay exactly as they are):

1. PR base must equal the **item's base branch** (not the feature branch) — the same phantom-merge reasoning as F7 with the right base; head must
   equal the item's integration branch.
2. The **existing `mergeGate`** is reused verbatim (F8): an approving latest review, no outstanding request-changes; S57 supplies checks via
   `ListStatuses` and its merge rule through a seam `action.MergeRule` (cross-slice request) consulted after `mergeGate` and before the forge call.
   Today that means **a review is required to land** (F8); who may give it is S57/AG4's decision, not this slice's.
3. Method: the repository's `land_method` (default **squash**; OD4). The forge call is the existing `Forge.MergePR`.
4. Post-guard: `IsAncestor(mergeSHA, origin/<base>)` after a `git fetch`; failure ⇒ `PhantomMergeError` (F7), item `failed(forge)`.
5. A branch-protection refusal (`KindRefused`) is **honest failure** `failed(protected)` — no workaround, no second method (N4 stands; **no forge-native
   merge queue** in v1 — OD1).
6. Class `irreversible`: unattended it denies **unless S56 grants `merge`** for this repository (MQ13); attended it asks the person with an ask text
   built by `action.BuildAsk` (extended with a `forge.land` case: repository, PR number, base, queue position, verified SHA, behind/ahead, gate summary).

## MQ13 — Authority: grants, the human door, and mode `off`

- `merge_queue_config.mode` is **authority-adjacent**: `auto` is only effective if S56's per-repository `merge` grant exists; with `auto` and no grant the
  landing loop's `forge.land` step denies at once (G14 sentence verbatim) and the item ends `failed(denied)`. S56 supplies `Resolve`'s extra input
  (cross-slice request); **`allowOutward` never widens `irreversible`** (F1) and this slice does not change that.
- Writes of queue authority are **socket methods on the person's 0600 + peer-credential surface** and **absent** from the narrow surface, agent
  services, the tool registry and the records catalog, except named propose/own-item twins — the **P4 shape**:
  human-door set `{mergequeue.settings, mergequeue.reorder, mergequeue.enqueue, mergequeue.dequeue, mergequeue.land, mergequeue.retry}`
  (`dequeue` has a separate own-item agent twin, so its guard entry is the socket method only);
  sibling guards in `cmd/aeon` (positive half, grep over `internal/narrow|agentsvc|tools|records`, canary `…GuardsHaveTeeth`), pre-amendment logic pinned by hash.
- `queue.enqueue` (the code action) is authored by the **pinned blueprint**: only a blueprint step can call it, and a blueprint is human-authored or
  seeded. With `mode = off` the action **still enqueues** (the row sits `queued`; `off` only stops the driver), so turning the mode on later lands
  what already finished (OD11). It stamps `attended` from the loop pin.

## MQ14 — Enqueue paths (three, one validator)

1. **Blueprint**: `queue.enqueue` (MQ8) in a lane of a seeded blueprint (`land-feature`'s own caller is S27/dev-loop revision; the `dev-loop` revision is an S48
   request, not edited here).
2. **Person**: `mergequeue.enqueue {componentRoot, branch, baseBranch?, attended?}` (human door); `attended` defaults to **true** (a person is present),
   recorded; refuses a branch that does not exist locally or on the remote and a base that is unknown (MQ15).
3. **Agent**: `mergequeue_propose_enqueue` ⇒ an inert proposal of closed kind `mergequeue_enqueue` through S45's desk (`desk.Validate` at create);
   a person commits it with `proposal.commit`; the committed enqueue is `attended=false`, actor `{proposal, id}`. Kind added by a `proposal.kind`
   table rebuild (P6).
One function `ValidateEnqueue` serves all three (create/commit parity test).

## MQ15 — Branch naming and base: settings finally used, pinned per loop (P2)

Today: names are hardcoded (F3), the base is "whatever HEAD is" (F4), `default_branch`/`branch_prefix` are inert (F14), and the loop's
`feature_branch/base_branch/default_branch/branch_mode` columns have no writer. This slice:

- `BranchNames` gains `Prefix` (validated `^([a-z0-9][a-z0-9._-]*/)?$`); `TaskBranch()` = `prefix+"task/"+loop+"/"+task`, `FeatureBranch()` =
  `prefix+"feature/"+loop`, `IntegrationBranch(item)` = `prefix+"integrate/"+item`. `NewBranchNames(loop, task)` keeps its signature (prefix empty) and
  `NewBranchNamesWith(prefix, loop, task)` is added — no caller breaks.
- **Resolution order** for the base: the loop's pinned `base_branch` → the repository's `default_branch` (S44-extra `EffectiveComponent`) →
  `refs/remotes/origin/HEAD` read from git → **none** (the queue then refuses to enqueue with the sentence "no base branch is known for this repository — set
  its default branch (`aeon component update <id> --default-branch main`)"). Feature-branch **creation keeps today's `git branch <name>` from HEAD when no base
  is known** (byte-identical), and uses `git branch <name> <base-ref>` when one is.
- **Pinned**: at `loop.start` the daemon writes `feature_branch`, `base_branch`, `default_branch` (and `branch_mode='standard'`) onto the loop row —
  the first writer those columns have; actions read the pinned `feature_branch` when non-empty, else derive as today (in-flight loops keep working).
  Changing `branchPrefix` later affects **new** loops only.
- Characterization (T1) records today's names and base behaviour; with empty settings every golden is byte-identical.

## MQ16 — S32 interplay (what this slice relies on and changes)

- Relies on the ledger and reconcile (MQ7). **Changes** (adoption task T5, after S32's close-out per Brief §5): widens the ledger CHECK, adds the
  `integration` owner to `terminalOwner`/`ReconcileWorktrees`, adds `WorkDir`. None of S32's tests is edited; S32's `owner_kind` literal assertions
  gain the new value in **sibling** tests listed in T5 Notes.
- Parallel units' merges (task PR → feature, at the forge) are **unchanged**: a task-level conflict at the forge still surfaces as `KindConflict`
  ⇒ blocked `action-conflict`; the same `action-conflict` reactor can later serve it (follow-up, not here; OD12).

## MQ17 — Surfaces (P34: daemon first)

| Method | Params | Result | Door |
| --- | --- | --- | --- |
| `mergequeue.list` | `{componentRoot?, status?, includeTerminal?, limit?}` | `{items: ItemView[]}` (position, status, reason, branch, loop, pr, behind/ahead, attended, links) | read |
| `mergequeue.show` | `{id}` | item + transitions (`merge_queue_event`) + integration refs + worktree + the **next-step sentence** | read |
| `mergequeue.status` | `{componentRoot}` | config, slot holder, queue length, base tip/`origin` state, three-state staleness per item | read |
| `mergequeue.vocabulary` | `{}` | states, reasons, modes, policies, methods, legal transitions (descriptor — no closed set typed in a client) | read |
| `mergequeue.enqueue` | `{componentRoot, branch, baseBranch?, attended?}` | `ItemView` | **human** |
| `mergequeue.dequeue` | `{id}` | `ItemView` | human; **own-item** twin for agents (queued/conflicted only, own loop's branch) |
| `mergequeue.reorder` | `{id, beforeId?}` (position among open `queued` items) | `{items}` | **human** |
| `mergequeue.retry` | `{id}` (`failed → queued`) | `ItemView` | **human** |
| `mergequeue.land` | `{id, digest?}` (`ready → merging` now, mode `manual`) | `ItemView` | **human** (attended: the `forge.land` ask still applies) |
| `mergequeue.settings` | `{componentRoot, mode?, conflictPolicy?, landMethod?, maxCatchUps?}` | `ConfigView` + `statement` for an authority-widening change (`auto`) | **human** |

Error codes: one block of ≥ 4 contiguous codes from T0 (`ErrMergeQueueRefused` with the reason in `what`, three-part data; `ErrMergeQueueNotFound`;
`ErrMergeQueueConflictState`; `ErrMergeQueueBaseUnknown`), a conformance fixture per code (D9), handshake want-list and `make protogen`. V13:
absent (not refusing) when the service is nil. S47's surface table classifies each method.

Agent tools (records catalog, family `mergequeue`, registered-not-granted, counted registry bump): `mergequeue_list`, `mergequeue_show`,
`mergequeue_status` (reads; own-view where S52-style privacy applies — an agent sees items of repositories it has run in), `mergequeue_dequeue`
(own item, restrictive), `mergequeue_propose_enqueue` (inert proposal). **No** tool is twinned to enqueue-direct, reorder, retry, land, settings or any
of the six actions.

CLI: `aeon mergequeue list|show|status|enqueue|dequeue|reorder|retry|land|settings|vocabulary` — a socket client (P5); no offline door (these verbs
have no meaning without the daemon's loops — recorded deviation); `--yes` seatbelt on `settings --mode auto` and `land`.

## MQ18 — Security notes

- **No hunk, no file content, no diff** is stored in any row or event; conflicting **paths** are (bounded to 100, each ≤ 512 bytes, control characters refused).
- Integration branch names are validated before any git call (`integrate/<item-id>[-n]`, item ids are generated, never user text).
- `git.publishIntegration` pushes **only** `integrate/` refs, plain, verified (F11); no `--force`, no deletion, no rebase.
- PR bodies: fixed template with ids and the loop's own summary prose (**prose by declaration**, marked as such, never a source of identifiers — L3).
- No token or credential touches params, events or audit; the forge token source is unchanged (env, per request).
- The driver never starts a landing for a repository with `mode = off`; it never widens anything — it only advances items a person or a pinned blueprint
  enqueued, through actions the effect-class gate already governs.
- Cost: landing and resolution loops carry S55 caps like any loop; a landing loop's cost is attributed to the item (`loop_slug` join) in `mergequeue.show`.

## Cross-slice requests (contracts I rely on; the owners must meet them)

| To | Request |
| --- | --- |
| **S44-extra** | `EffectiveComponent` returns `defaultBranch` and `branchPrefix` with sources (CT6 lists them as inheritable); `RootOf`; `ComponentForRoot` for repositories. No change to its design needed. |
| **S56 (AL)** | `action.Resolve` takes a per-repository, human-authored `merge` grant: `forge.land` may run unattended **only** with it; the grant record is addressable by `component_root`; the policy revision that allowed a landing is returned so MQ4 transitions can name it. `allowOutward` keeps not granting `irreversible`. |
| **S57 (VG)** | (a) a gate step kind usable in `land-feature`/`resolve-conflict` that runs in `WorkDir` and records the verified SHA; (b) `GitHub.ListStatuses` (so `mergeGate`'s `checks` is real); (c) the `action.MergeRule` seam evaluated before `forge.land`; (d) the answer to AG4 (does an Aeon review count). Until (a) lands, MQ uses `verifier-engineer`. |
| **S48 (BP)** | Seed set `blueprints.landing` (inert): `land-feature`, `resolve-conflict`; optional `dev-loop` revision adding a final `queue.enqueue` lane. |
| **S58 (SV)** | Event kinds `mergequeue.ready`, `mergequeue.conflicted`, `mergequeue.failed`, `mergequeue.merged` and their `notifyHintFor` entries (F22 is a closed set). |
| **S55 (LC)** | `loop.stop` on a landing/resolution loop ⇒ the driver moves the item to `removed(loop-stopped)`/`queued` per T7's table; caps apply to landing loops. |
| **S45** | Generic `proposal.commit` and `desk.Validate` for the new `mergequeue_enqueue` kind. |
| **S47** | Surface classification of the ten `mergequeue.*` methods. |
| **S27 (CN)** | May call `queue.enqueue` when it finishes an item; otherwise unaffected. |
| **S59 (EV)** | Gates in the integration worktree run in the project's environment record when one exists. |

## Open decisions (recommended default — the builder records the final)

| # | Decision | Default |
| --- | --- | --- |
| OD1 | Land by direct PR merge, or through GitHub's merge queue | **Direct PR merge** (N4 stands; native queue = follow-up) |
| OD2 | Does `conflicted` hold the slot | **No — releases it**; returns to its original position |
| OD3 | Catch-up strategy | **Merge-forward onto a new integration ref** (rebase rejected: needs force) |
| OD4 | Landing method | **squash** (per-repository setting, `merge` allowed) |
| OD5 | Per-project default settings (tree inheritance) | **None in v1**: per-repository rows only |
| OD6 | Stale `integrate/*` refs | **Listed, never deleted** by Aeon |
| OD7 | Who may reorder / settings / land | **Human door only** |
| OD8 | Catch-up retry bound | **3** (`max_catch_ups`, 1–10) |
| OD9 | `attended` for a person's enqueue | **true**, declared and recorded |
| OD10 | Cross-fork landings | **Out of scope** (same-repository branches only) |
| OD11 | Does `queue.enqueue` honour `mode = off` | **No** — it enqueues; `off` only stops the driver |
| OD12 | Reuse `action-conflict` for task-PR conflicts at the forge | **Later** (follow-up slice) |
| OD13 | Merge commit vs. fast-forward of the integration ref onto base | **PR merge at the forge** decides (squash default) |
