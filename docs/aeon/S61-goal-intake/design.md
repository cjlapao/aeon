# Design — S61 Goal intake

Rules are **GI1–GI20**, cited from `plan.md` and the task files. Shapes cite
[`MANAGEMENT-PATTERNS.md`](../MANAGEMENT-PATTERNS.md) (P1–P11). No code exists yet; every "Verified fact" was read in
the tree on 2026-10-30 and T0 re-walks each one.

## Verified facts (2026-10-30)

| # | Fact | Where |
| --- | --- | --- |
| V1 | CLI verb switch has no `init`; `presets.Init(InitArgs, existing, reachable) (Result, bool, error)` has no caller outside tests; `InitArgs.KeyFile` is a **path** ("never a value", D5/C17) | `cmd/aeon/main.go:66-200`; `internal/presets/presets.go:60-88,200-232` |
| V2 | Route methods on the socket: `routes.list/set/delete/probe` | `protocol/method.go:74-83` |
| V3 | `component.create` daemon path: slug regex, cap, icon, `filepath.Clean`, then `validateLoopWorkspace` (absolute, `EvalSymlinks`, `Stat`, `IsDir`, `ReadDir`) — so a **non-existent path is refused before the store**; it does not check "is a git repository" | `daemon/components.go:65-100`; `daemon/blueprint.go:149-173` |
| V4 | `store.CreateComponent` checks only root-not-registered | `store/components.go:67-112` |
| V5 | `forge.Forge` = 10 methods (issues, PRs, reviews); optional-interface pattern already used (`StatusLister`, `ThreadLister`, type-asserted at the merge gate); `GitHub` reads its token **per request** from env (`AEON_FORGE_TOKEN`, `GITHUB_TOKEN`, `GH_TOKEN`) | `forge/forge.go:132-138,215-227`; `forge/github.go:80-91` |
| V6 | Action registry: 9 names, package-private, `Dispatch` is the only door; `Entry{Name, Args, Evidence}`; classes package-private; `Resolve(name, attended, allowOutward)` | `action/class.go:52-62,157`; `action/registry.go:48,152` |
| V7 | The blueprint validator accepts `code-action` names from `action.Names()` (`unknown-action` otherwise) | `blueprint/validate.go:418-436` |
| V8 | `git.Client`: `BranchExistsLocal/Remote, CurrentBranch, LocalHEAD, StageEverything, StatusPorcelain, AssertClean, CommitEverything, Push, Run, RemoteRefSHA, IsAncestor`; `Push` always `push -u origin <branch>` then verifies by `ls-remote`; no force-push anywhere | `git/plumbing.go:84-289` |
| V9 | `proposal` table: kind CHECK widened by a 0016-shaped table rebuild with a copy guard; commit/reject were CLI-only until S45 (PM1) | `store/schema.go:125,2420-2452` |
| V10 | `brainstorm.yaml`: `after: converse`, `converse: brainstormer`, one lane `capture` (model-step, `produces: [proposal.md, brief.md]`, `emits: goal`); `roadmapIdeationBlueprint = "brainstorm"` | `blueprints/brainstorm.yaml`; `daemon/roadmap_dispatch.go:63-68` |
| V11 | `brainstormer` tools `[read, write, glob, grep, question, todo, goal_create, goal_update, goal_submit_review, attach, run_start]`; `goal_create` files `in_review` and stamps project/source/author | `handler/bodies/brainstormer.md`; `records/catalog.go:65-66` |
| V12 | `Goal` has no acceptance; `goal_component` rows are copied to the new version on `UpdateGoal`; `GoalComponent` carries `EstimateUSD/Confidence` as P31 pointers | `store/goals.go:20-60,241-306` |
| V13 | `Brief{Acceptance, Constraints []string}` JSON columns, versioned per loop | `store/briefs.go:21-38` |
| V14 | `roadmap.add` params carry `Provenance` (a closed vocabulary — T0 lists it); items default status `idea`, priority `unknown` is a first-class state (S29 D2) | `protocol/roadmap.go:75-85`; `roadmap/types.go:142-157` |
| V15 | `loop.start` requires a visible workspace directory; loop tables key `component_root` as TEXT; telemetry resolution **tolerates an unregistered root** (`TestRecordXWithAnUnregisteredRootKeepsTheDetail`) — T0 re-proves a loop can run in a directory that is no component's root | `daemon/start_loop.go:46,109`; `store/telemetry.go:255`; `store/telemetry_test.go:234` |
| V16 | bash inherits the daemon's whole environment (`cmd.Env = nil`), no sandbox | `tools/spawn.go:141-159` |
| V17 | Every audit/revision row uses fixed templates from ids and enums (SEC-033); the records catalog is stdlib-only | MANAGEMENT-PATTERNS P3; `records/catalog.go` header |

**Finding F-1 (contradicts the parent assumption, not AUTONOMY-GAPS):** the **store** does no filesystem check, but the
**daemon's** `component.create` already refuses a missing directory (V3). The intake therefore must **scaffold before it
registers** (it cannot register a path that does not exist), and no "add an fs check" request to S44-extra is needed.
Only the *git-ness* of the root is unchecked; the intake verifies that itself (GI11).

## GI1 — The charter is a row-history, current derived

`charter_revision(id PK, root_id TEXT NOT NULL REFERENCES component(id), revision INTEGER NOT NULL, body TEXT NOT NULL
(canonical JSON), summary TEXT NOT NULL (fixed template), actor_kind, actor_id, source TEXT NOT NULL CHECK IN
('person','proposal','intake'), source_ref TEXT, created_at, UNIQUE(root_id, revision))`. Append-only **by trigger**
(P3). The **current** charter of a root is `max(revision)`; there is no mutable charter row, so there is no second
truth. Revision numbers never reset; history outlives edits. No FK from history to anything deletable (nothing is
deleted anyway).

## GI2 — The charter body is a closed schema and it grants nothing

Canonical JSON object, `additionalProperties:false`, byte caps (whole body ≤ 64 KiB):

| Field | Type | Notes |
| --- | --- | --- |
| `goal` | string ≤ 4096 | the one-paragraph statement |
| `constraints[]`, `nonGoals[]` | strings ≤ 512, ≤ 50 each | |
| `acceptance[]` | `{id, text ≤ 512, check?: "manual"\|"command"\|"test", ref? ≤ 512}`, ≤ 100 | stable `id` (`ac-<n>`), the seed of goal criteria (GI5) |
| `stack[]` | tags ≤ 64 each, ≤ 30 | free tags; feed template choice, nothing enforced |
| `proposed` | `{budgetUsd?, autonomy?, environmentRef?, verify?[]}` | **proposals only**: `budgetUsd` ≥ 0 (nil = unknown, P31); `autonomy` is an enum **read from S56's descriptor** (`charter.vocabulary`, never typed here); `environmentRef` names an S59 record; `verify[]` are command strings **never executed by this slice** |

A charter has **no authority**: nothing in `internal/policy`, `internal/loop`, `internal/action` reads it. A test greps
that no non-charter package imports `internal/charter` except the daemon seams listed in T7 (the "grants nothing"
guard, with a canary). `proposed.*` is rendered as "not applied" with a pointer to the owning slice's human door.

## GI3 — A charter belongs to a root; children inherit

A standalone component is a root (S44-extra CT1), so it has its own charter. In a tree, **only the root** has a charter;
a child shows its root's. `charter.save` on a child is refused with "X is a component of project Y — the charter lives
on the project". Promote/attach keep existing charters (attach: the child's charter stays as history, no longer
current — the view says so).

## GI4 — Who writes: a person directly, an agent only by proposal

`charter.save {rootId, body, note?}` is a **human-door** socket method (the 0600 + peer-credential surface; actor from
`table.ActorFor`, never params). An agent's `charter_propose` creates an **inert `charter_save` proposal**
(`desk.Validate` at create), committed by a person through S45's generic `proposal.commit` with the **committer** as
actor. P4 guard amendment with sibling tests and canaries (T7): the write methods are absent from the narrow surface,
agent services, the tool registry and the records catalog (except the named propose twins).

## GI5 — Acceptance criteria become goal data: `goal_criterion` (decided)

Options considered: (a) a JSON column on `goal` — touches `goal`'s DDL, which **S44-extra T3 rebuilds**; (b) reuse
`brief.acceptance` — a brief exists per loop only after approval, so it cannot be the pre-work definition of done;
(c) a separate versioned table. **Decision: (c).**
`goal_criterion(goal_id REFERENCES goal(id) ON DELETE CASCADE, goal_version, ordinal, criterion_id TEXT, text, check_kind
CHECK IN ('manual','command','test') NULL, check_ref TEXT NULL, PRIMARY KEY(goal_id, goal_version, ordinal))`.
`UpdateGoal` copies the rows to the new version exactly as it copies `goal_component` (`goals.go:304-306`) — a new test
proves it. `goal.show` returns `criteria[]`. S57 reads `criteria[]` as the definition of done; `brief.acceptance` stays
the brief author's refinement and is **advisory** to S57 (cross-slice request R1). The table is created **after**
S44-extra T3 has landed (T0 verifies), so it is in the post-rebuild `goal` FK list from birth.

## GI6 — The intake is a person-driven session with a derived state

`intake(id PK 'int-…', name, target_path TEXT NULL, scratch_path, loop_slug TEXT NULL, root_id TEXT NULL, created_by_kind
CHECK 'user', created_by_id, created_at)`; `root_id` may be set **once** (trigger refuses any other update). There is no
`state` column: **state is derived** — `interviewing` (loop live, no plan proposal) → `planned` (an open `intake_plan`
proposal exists) → `committing` (steps ledger partial) → `committed` (ledger complete) / `abandoned` (proposal
rejected and loop closed). `intake_step(intake_id, ordinal, step, outcome CHECK IN ('done','skipped','failed'), detail
(fixed template), at)` is append-only; it is the resume ledger (GI7).

The interview runs in a **scratch workspace**: a directory the daemon creates under its state dir
(`<state>/intake/<id>/`, 0700) and passes as the loop's `workspaceRoot`, because `loop.start` requires an existing
directory (V15) and the project's directory does not exist yet. The scratch root is **not** a component root; T0
proves a loop runs there and records what telemetry does with it (GI20).

## GI7 — The plan is a proposal; one commit door

Kinds added to the closed `proposal.kind` CHECK by **one** table rebuild: `charter_save`, `intake_plan` (read the
**current** DDL first, change only the CHECK, update the Go mirror and the literal counts — P6).

`intake_plan` payload (closed schema, ≤ 128 KiB): `{intakeId, charter (GI2 body), components: [{name, slug, template,
role: "root"\|"child"\|"standalone", description?, defaultBranch?, tags[]}] (≤ 8), projectName? (required when ≥ 2
components), epics[{key, title, objective}], goals[{epicKey?, title, type, bodyNote, componentSlugs[], criteria[{text,
check?, ref?}]}], items[{componentSlug, epicKey?, title, priority? ("unknown"\|label), estimate?}], reason}`.
**No path, no secret, no identity** appears in the payload — the person chooses the target path at commit time.

**`intake.commit {intakeId, proposalId, digest, targetPath, options}`** is the one commit door (a dedicated human-door
method, not the generic `proposal.commit`; the generic door refuses an `intake_plan` with "this proposal needs a target
path and a publish choice — use `intake.commit`"). `digest` binds to what the person saw in `intake.plan`'s preview
(S45 PM7). `options = {publish: "none"\|"private"\|"public", owner?, repoNameSuffix?}` — default `none`.

## GI8 — Bootstrap actions are in the closed registry and person-door only

`repo.scaffold` and `forge.createRepo` are added to `action/registry.go` + `class.go` (one definition, one class table;
`TestEveryRegisteredActionHasAClass` extends). A new `Entry.Door` value `person` marks them. Rules:

- `Names()` (the list the **blueprint validator** receives) **excludes** person-door actions, so a blueprint naming one
  is `unknown-action` (V7); a separate `PersonNames()` lists them for the daemon.
- `Dispatch` of a person-door action requires a `Caller{Kind:"person", ID}` built only by the daemon's `intake.commit`
  from the peer-stamped actor; any other caller is refused with a fixed sentence (test with a canary caller).
- `repo.scaffold` is class **local**; `forge.createRepo` is **outward**. Unattended resolution already denies outward
  (V6) and `allowOutward` cannot reach it (it is not loop-invocable at all) — the guard is defence in depth, tested.
- Both **preview first**: `intake.plan`/`intake.preview` return the exact file list (path, mode, byte size, SHA-256),
  the git commands that will run, and the remote request (owner, name, visibility) — no side effect.

## GI9 — `repo.scaffold`: a new path, a closed template language, no surprises

Inputs: `targetDir` (absolute), `template` (slug@version), `vars` (closed set: `name, slug, description, stack[],
defaultBranch`). Steps (each a test): refuse non-absolute or containing `..` after `Clean`; `EvalSymlinks` the **parent**;
the parent must exist and be a directory; the target must **not exist, or be an empty directory**; refuse a target that
is, contains or lies inside any registered component root, `$HOME` itself, or the state dir; create with 0755 (dirs)
and template modes {0644, 0755}; write each file with `O_EXCL`; re-`Lstat` every write path to refuse symlinks;
`git init -b <defaultBranch>`; `git add -A`; commit "Initial commit (Aeon intake <id>)" using the repo's **existing**
git identity — if `user.name`/`user.email` resolve to nothing, **refuse with a sentence** naming how to set them
(never invent an identity, GI12). On any failure after creation the daemon removes **only what this run created**
(tracked in the ledger), never a pre-existing directory.
Templates render with Go `text/template`, `missingkey=error`, variables above only, no functions beyond `lower/upper/
join`, output capped (512 KiB total, 200 files). A template file's relative path is validated (no absolute, no `..`,
no `.git`, no NUL, ≤ 255 bytes).

## GI10 — `forge.createRepo`: implemented, staged off, three keys (decided)

Added through an **optional** interface `forge.RepoCreator{CreateRepo(ctx, RepoRequest) (*Repo, error)}` implemented by
`GitHub` (POST `/user/repos` or `/orgs/{org}/repos`) and `Fake` — the shared `Forge` interface is **not** widened
(V5 pattern). `RepoRequest{Owner, Name, Private bool, Description}`; `Private` defaults **true**; names validated by
a closed regex; the description is the component's short description only — **never charter text**. The action then
`git remote add origin <url>` and `Push` the default branch (existing `Client.Push`, which verifies by `ls-remote`).
It runs only when **all three** hold:
1. the setting `intake.allowRemoteCreate` (S44 config store, **default false**, human-only door) is true;
2. S56 AL's workspace-scope grant for effect `forge.createRepo` exists (cross-slice request R3 — until S56 ships, the
   action refuses with "remote repository creation needs an autonomy grant (S56)");
3. the person's `intake.commit` carries `publish ∈ {private, public}` and `public` additionally requires
   `confirmPublic: true` (a repository is outward publication of its content; org rule: code and roadmaps are
   confidential — default private).
Never unattended. No token is read, logged or stored by this slice (the forge client's per-request env read is
unchanged); the error taxonomy (`auth`, `refused`, `conflict` — name taken) surfaces its three-part sentence verbatim.

## GI11 — Registration is tree-aware and happens after the path exists

Order inside `intake.commit` (GI13 ledger): for each component in plan order — scaffold → **verify git** (`git rev-parse
--is-inside-work-tree` and a non-empty `HEAD`) → register via the **same store/daemon create function** `component.create`
uses (V3 validation included; S44-extra's `parentId`). Tree shapes: `standalone` ⇒ create a root; `root`+`child` ⇒ create
the first child standalone, then `component.promote` it into a repo-less root named `projectName`, then create the
remaining children with `parentId`. A failure after partial registration leaves the earlier components registered
(they are real, scaffolded repos) and the ledger says exactly where it stopped. **No change to `component.create`'s
acceptance is requested** (F-1).

## GI12 — Git identity and branch names

The initial commit uses the repository's resolved git identity; unresolved ⇒ refusal sentence (no default). Initial
branch = the plan's `defaultBranch` or `main`; `component.default_branch`/`branch_prefix` are set from the plan
(they exist: `store/components.go`). Aeon adds no remote credentials and no hooks.

## GI13 — What `intake.commit` creates, in one resumable run

Steps (ordinal order, each recorded in `intake_step`; a re-run **skips** a `done` step after re-verifying its
evidence, so crash-resume never duplicates): `verify-digest` → per component (`scaffold`, `verify-git`, `publish`?
(only when `publish≠none`), `register`) → `promote/attach` (tree) → `charter` (revision, source `intake`) → `records`
(one DB transaction: epics, goals + criteria, roadmap items) → `link` (set `intake.root_id`). Records land as:
epics via `CreateEpic`; goals via `CreateGoal` with `SourceKind:"intake"`, `Submit:true` ⇒ **`in_review`** (a person
still approves each, goal.decide stays human); roadmap items via the roadmap store with status `idea`, priority
`unknown` unless the plan names a label, provenance `intake` (T0: the roadmap `Provenance` vocabulary — if it is a CHECK,
adding `intake` is a one-word additive widening in this slice's migration; if Go-only, a Go constant). The DB transaction
is atomic; filesystem steps are idempotent by evidence (a directory with the template's file hashes and a matching
initial commit is "already done"; a mismatch is a refusal, never an overwrite).

## GI14 — Templates are seeded records

Table `scaffold_template(id PK, slug UNIQUE, version INTEGER, origin CHECK IN ('system','user'), title, description,
stack_tags JSON, files JSON, created_at, updated_at)` + append-only `scaffold_template_revision` (BP7 shape). Files
JSON: `[{path, mode, content}]`. Seed set **`templates.scaffold`** (P8, class **inert**) ships three starter templates:
`minimal` (README, AGENTS.md, .gitignore), `go-service`, `node-ts` (each: README, AGENTS.md skeleton, .gitignore, a
generic CI workflow file). Golden-guarded: per-record `Version` ("content changed but version did not — bump it"),
and a **content scan test**: no host path (`/Users/`, `/home/`, drive letters), no token-shaped string, no `secrets.`
reference, no URL except the documented placeholder set. An operator may edit/add templates (human door
`template.save`, a revision per save; a shipped template then reads `diverged`, never overwritten by re-seed, explicit
`restore` — SD6/BP6 shape). No delete.

## GI15 — Seed sets declared by this slice

Three small sets, each against the S43-extra engine and each area's existing applier (cross-slice request R4: "an
applier may serve a second set"): `templates.scaffold` (inert, GI14), `agents.intake` (inert: the `intake-architect`
body + registry entry — adopting slice work after S43-extra merged), `blueprints.intake` (inert: `goal-intake`, written
through S48's blueprint write path). No guard-class set; startup never applies anything that widens a guard.

## GI16 — `aeon init` is not this slice (finding, decided)

`aeon init` is documented but unreachable (V1). It is provider onboarding (S23's first-run; `internal/presets`), writes
routes — a different domain with its own design questions (config write path, "existing config" detection, the `presets.list`
surface). **Decision: not wired here.** The wizard's step 0 asks `routes.list`; with no route it refuses with a sentence
pointing at `aeon routes set`. The finding is recorded in T0's Notes and in the close-out for S23's owner. Default
override: if the operator wants it in this slice, T10 gains a verb — the standing rule stays: a key is a **file path or
environment**, never a flag value.

## GI17 — Agent tools (rule 2; P6)

Catalog specs (family `charter`, `intake`, `template`; each with a socket twin; **registered, not granted**):

| Tool | Twin | Mutates | |
| --- | --- | --- | --- |
| `charter_list`, `charter_show` | `charter.list/get` | no | list with details / one charter + revisions (own-view not needed: charters are project data) |
| `template_list`, `template_show` | `template.list/get` | no | |
| `intake_status` | `intake.status` | no | derived state + ledger |
| `charter_propose` | `charter.propose` | **yes** | inert `charter_save` proposal |
| `intake_propose` | `intake.propose` | **yes** | inert `intake_plan` proposal; the daemon stamps `intakeId` from the loop's identity |

Schemas `additionalProperties:false`; no identity/status/path/option fields; per-agent open-proposal limit (S45
constant); a payload ≤ 128 KiB. No agent tool commits, scaffolds, publishes, saves a template or touches a goal's
criteria directly. `TestRegistryExactNames` bumped **counted and dated** from T0's real count (+7).

## GI18 — Security and confidentiality

No secret value in params, payloads, templates, ledger or audit; audit summaries are fixed templates (SEC-033). The
charter and plan are **data**: text in them is never treated as instruction by the daemon; the intake agent is told so.
Remote repo creation: private by default, public needs an explicit flag. The scratch workspace is 0700. A prompt-injection
note: interview content may contain instructions; the only writes are proposals a person reads before committing, and
the commit applies a **closed-schema** payload (no free-form command string is ever executed: `proposed.verify[]` is
stored, not run).

## GI19 — What does not move

`component.create/promote/attach` semantics (S44-extra), `goal.decide` (human), roadmap/queue/triage semantics, the
`brainstorm` blueprint and `roadmap.ideate`, `forge.*` existing actions and the merge gate, the S19 narrow surface,
`allowOutward`, G14, the policy corpus, S43/S44.

## GI20 — Cost attribution of the scratch loop (recorded, not solved here)

The intake interview costs money but runs in a root no component owns, so no component rollup carries it (V15). T0
records what the workspace tier does. Default: `loop.show` still shows the loop's cost (per-loop is stored per step);
after `intake.commit` the intake row links `root_id`; S55 LC is asked (R2) to treat intake loops as chargeable to the
root once linked. No telemetry code changes here.

## Open decisions (each has a recommended default; none blocks the plan)

| # | Decision | Default | Why |
| --- | --- | --- | --- |
| OD1 | Wire `aeon init` here? | **No** — finding for S23 (GI16) | different domain; presets/config design |
| OD2 | `forge.createRepo` in scope? | **Implemented, off** (GI10) | irreversible-adjacent; three keys |
| OD3 | Acceptance storage | **`goal_criterion` table** (GI5) | avoids the `goal` rebuild (S44-extra T3) |
| OD4 | Commit door | **dedicated `intake.commit`** (GI7) | needs target path + publish choice; generic commit refuses |
| OD5 | Intake agent | **new `intake-architect`**, not `brainstormer` | different tools and contract |
| OD6 | Setting `intake.workspaceRoot` (confine new projects under a base dir) | **unset ⇒ no confinement beyond GI9's rules**; if set, targets must lie under it | avoid inventing a policy; allow an operator to tighten |
| OD7 | Max components per intake | **8** | bounded commit, bounded preview |
| OD8 | Charter on a child | **refused — root only** (GI3) | one truth |
| OD9 | Roadmap provenance `intake` | additive value (GI13) | traceability |
| OD10 | Template language | **Go `text/template`, closed variables** (GI9) | no code execution, no new dependency |
| OD11 | Wizard UI | **spec only** (`ui-spec.md`) | operator: UI after the daemon pieces |
| OD12 | Charter `proposed.autonomy` vocabulary | **read from S56 descriptor**; until S56 ships the field is absent from the schema | never type a vocabulary twice |

## Cross-slice requests (this slice edits none of them)

| # | To | Request |
| --- | --- | --- |
| R1 | S57 VG | Read `goal.show` `criteria[]` as the definition of done; `brief.acceptance` advisory; accept `check` kinds `manual/command/test` |
| R2 | S55 LC | Charge an intake loop's cost to the root once `intake.root_id` is set; accept a `proposed.budgetUsd` only through S55's own human door |
| R3 | S56 AL | A **workspace-scope** grant for effect `forge.createRepo` (default none; human door); an autonomy descriptor `charter.vocabulary` reads |
| R4 | S43-extra / S48 | An area applier may serve a second seed set (GI15) |
| R5 | S59 EV | A readable record id space for `environmentRef` validation |
| R6 | S27 CN | The first roadmap arrives as `idea`/`unknown` items; S27 must not schedule `unknown` priority (its own D4 already says so) |
| R7 | S44-extra | None. (F-1: `component.create` already validates the directory.) |
