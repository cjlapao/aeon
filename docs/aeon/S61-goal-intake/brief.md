# Brief — S61 Goal intake: charter, project bootstrap and first roadmap

Written 2026-10-30. Prefix **GI**. Closes AUTONOMY-GAPS **G6** ("from *build app X* to a project"). Task shapes follow
[`MANAGEMENT-PATTERNS.md`](../MANAGEMENT-PATTERNS.md); a deviation is recorded in `design.md`.
**Depends on:** S44-extra (the component tree — a project is a *root component*), S45 (`internal/desk`, the generic
`proposal.commit`, the human-door guard pattern), S43-extra (the seed engine), S48 (blueprint rows — cross-slice
request), S26 (roadmap). **Cross-slice requests** (this slice builds nothing in them): S55 LC (budgets), S56 AL
(autonomy grants), S57 VG (verify commands, definition of done), S59 EV (environment reference), S27 CN (consumes the
first roadmap). T0 re-checks what has merged.

## 1. Why

Today Aeon starts after a project exists. A person who says *"build me an app that does X"* gets nothing that turns the
sentence into a project:

- there is no **charter** — no place where the goal, constraints, non-goals, acceptance criteria and stack are written
  down once, as data a later slice can read;
- there is no **bootstrap** — no code anywhere runs `git init`, writes a first `AGENTS.md`/`README`/CI skeleton, or
  creates a remote repository (grep of `internal/` and `cmd/`: no `git init`, no clone, no `CreateRepo`;
  `forge.Forge` has issue/PR/review methods only, `internal/forge/forge.go:215-227`; `internal/git/plumbing.go` has no
  init);
- there is no **first roadmap** — the ideation chat (`roadmap.ideate` → blueprint `brainstorm`) captures one goal, not
  a project's first epics, goals and items;
- **acceptance criteria are not data on a goal** — `store.Goal` has no such field (`store/goals.go:20-45`); only a
  per-loop `Brief` carries `Acceptance []string` (`store/briefs.go:21-38`), and it exists only after a goal is approved
  and a brief authored. S57 (verification gates) needs a definition of done that exists **before** work starts.

S27 (continuous work) can only run a project that exists, has a roadmap, and has checkable "done". This slice makes
that start possible — with a person in the loop at every commit.

## 2. What exists today (measured 2026-10-30; T0 re-walks every line)

| Fact | Where |
| --- | --- |
| `aeon init` is documented (README.md:9, docs/QUICKSTART.md:8-17), `internal/presets.Init` implements it (`presets.go:200`), but the CLI verb switch has **no `init` case** and nothing outside `internal/presets` calls it | `cmd/aeon/main.go:66-200`; grep `presets\.` |
| A component is created by `component.create` (name, slug, root); the **daemon** refuses a root that is not an absolute, visible, listable directory (`validateLoopWorkspace`), the store only checks the root is unregistered | `internal/daemon/components.go:65-100`; `internal/daemon/blueprint.go:149-173`; `store/components.go:67-112` |
| The ideation chat: `roadmap.ideate` starts loop `ideate-item-<id>` on blueprint `brainstorm` (`after: converse`, agent `brainstormer`, lane `capture` producing `proposal.md` + `brief.md`, emitting a `goal`); planning uses `project-planner` with an inline one-lane blueprint | `daemon/roadmap_dispatch.go:63-70,129`; `blueprints/brainstorm.yaml` |
| An agent's `goal_create` files the goal **`in_review`** — a person approves it | `records/catalog.go:65-66` |
| Goals have components-per-version with copy-forward on update; no criteria | `store/goals.go:20-45,241-306` |
| `Brief{Acceptance, Constraints []string}` per loop, versioned | `store/briefs.go:21-38` |
| The closed action registry (9 names, three effect classes, unattended outward/irreversible denied, `forge.merge` irreversible) | `action/class.go:52-62,157-180`; `action/registry.go:48` |
| `git.Client` has branch/commit/push helpers, **no init, no remote add** | `git/plumbing.go:84-289` |
| `proposal.kind` is a closed CHECK widened by table rebuild (last shipped widening: `agent_import`) | `store/schema.go:125,2420-2452`; `protocol/proposals.go` header |
| Agent bodies are embedded files + a registry list (`brainstormer`, …) | `internal/handler/bodies/`, `handler/registry.go:70` |

## 3. What this slice delivers

1. **The charter** — one append-only, versioned record per **root component**: goal statement, constraints,
   non-goals, structured acceptance criteria, stack tags, and *proposals* for budget, autonomy level, environment and
   verify commands. A charter **grants nothing**; it is advice a person applies in the owning slice's own door.
   Daemon-first methods, a list with details, agent read tools, an agent *propose-only* write path, a CLI client.
2. **Acceptance criteria as goal data** — a `goal_criterion` table (versioned with the goal, copied forward like
   `goal_component`), the contract S57 reads as definition of done.
3. **The intake** — a person-driven session: a seeded blueprint `goal-intake` and agent `intake-architect` interview
   the person in a scratch workspace and produce one inert **`intake_plan` proposal** (charter + component layout +
   template choices + first epics, goals and roadmap items). Nothing is created until a person commits it.
4. **Bootstrap actions** in the closed action registry, **person-door only** (no blueprint may name them):
   `repo.scaffold` (local: a *new* directory, `git init`, files from a template, an initial commit) and
   `forge.createRepo` (outward; implemented but **staged off** by default). Each previews before it acts.
5. **Scaffold templates** — seeded records (`templates.scaffold`, inert), editable, revisioned, free of secrets and
   host paths by a golden-guarded test.
6. **One commit door**, `intake.commit` — digest-bound, resumable, idempotent, ledgered: scaffold → git → (optional)
   publish → register components (tree-aware) → charter revision → epics/goals/items, with every goal landing
   **`in_review`** and every roadmap item **`idea` / priority `unknown`**.
7. A daemon descriptor and a spec for the app's **new-project wizard** (no UI code here; the app work builds on it).

## 4. Out of scope

- The wizard's React code (spec only — `ui-spec.md`); `aeon init` (a finding handed to S23's follow-up, GI16).
- Applying a charter's proposed budget / autonomy / verify commands — those are S55/S56/S57 human doors.
- Releases, deploy pipelines, remote CI provisioning, branch-protection setup (G11).
- Importing an *existing* repository as a project (that is `component.create`/`promote` as today).
- Any unattended path: nothing in this slice runs without a person's `intake.commit`.

## 5. Acceptance

1. A person can go from a one-sentence goal to: a charter revision, N registered components in the right tree shape,
   N new git repositories each with an initial commit, and a first set of epics/goals/roadmap items — through
   `intake.commit` only, with every created goal `in_review` and every item `idea`.
2. `repo.scaffold` never writes into an existing non-empty path, never follows a symlink out of the target, and is
   absent from `action.Names()` as seen by the blueprint validator; `forge.createRepo` is off until three independent
   conditions hold and never runs unattended.
3. The charter, intake, template and criteria writes are human-door socket methods (absent from narrow, agent
   services, the tool registry, the records catalog except the named propose twins); guard tests with canaries.
4. Agents can read charters/templates/intake status and *propose* a charter or an intake plan; they cannot commit,
   scaffold, publish or save directly.
5. `intake.commit` is resumable: killing the daemon between any two steps and re-running completes without duplicate
   directories, commits, components, goals or items.
6. Every created record carries provenance (`intake`, intake id, committer); every audit summary is a fixed template
   from ids and enums.
7. `make protogen && make check-protogen && make guard` green; migration drills pass.

## 6. Status

Built 2026-11-05 — 13/13 tasks, merged and pushed as the branch `s61-goal-intake` reached main.
The plane exists end to end: an interview's settled plan lands as directories, repositories, a
component tree, a charter revision and records through **one digest-bound, resumable door a person
commits** (`intake.commit`), with every created goal `in_review` and every roadmap item `idea` —
approval is always a person. The charter/template/intake writes are fifteen human-door socket
methods, absent from every agent surface except the two inert propose twins (GI3/GI4); `repo.scaffold`
and `forge.createRepo` are person-door actions invisible to blueprints (GI12), remote creation is off
until three independent keys hold (GI10/GI11), and the ledger `intake_step` carries a run across a
crash — proved by the nineteen-kill-point matrix AND by real `kill -9`s against a live daemon
(DEMO §S61, rc=0, 52 assertions, 2026-11-05). Acceptance per §5 is walked item-by-item in T12's
Notes; the slice's own walk found and fixed two real resume gaps (own-half take-back after a
mid-scaffold death; adoption of a landed-but-unrecorded create), the roadmap read plane's refusal
of the fourth stored provenance word, plus the preview's missing git commands, a bare-`aeon` panic
and a middle-of-path symlink write-through — each pinned by a named test. Two settings keys, both default-off; no blueprint or UI surface added (the wizard descriptor
is daemon-side; the screen is a later task).
