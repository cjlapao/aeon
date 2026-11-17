# Seeding a slice into the loop

Each **`S<n>-` folder** in this directory is **one loop** — seventeen of them, described in
[`README.md`](README.md). This is the sequence that turns a folder into a running loop, and the reasoning behind the
parts that are not obvious.

`docs/_archive/aeon-v1-horizontal/` is reference material, not loops. Stage 5 slices are [sketches](ROADMAP.md), not seedable.

**[`TASKS.md`](TASKS.md) lists every task in execution order**, and names the two things to settle before
seeding: 111 tasks lack the `engineer`/`tech` fields `tasks_derive` requires, and six slices have no task
files yet.

> [!IMPORTANT]
> **Run these in THIS repository.** Aeon is built at `aeon/` during construction and extracted to its own repo at
> the alpha ([`ARCHITECTURE.md` §7](ARCHITECTURE.md#7-distribution)). Every slice reads reference material from
> `opencode/` or `packages/loop-state-*`, which an engineer in a separate repository cannot reach.
>
> So the loop's project root is this repo's root, and every path a task writes begins `aeon/`. Nothing under
> `aeon/` may import from `packages/` (P24) — that invariant is what keeps the later extraction a
> `git subtree split` rather than a refactor.
>
> **One consequence:** the concurrency guard is per project root, so an Aeon loop and a v1 loop cannot run
> concurrently in this checkout. They share a working tree and a branch, so that is correct rather than a
> limitation — use a worktree if you need both.

## Why seeding is not a file copy

Loop documents live in the **store**, not on disk. `loop_writeFile` writes a document into
`loop_document` and mirrors it to `{projectRoot}/.projects/loops/{slug}/` as a convenience; the mirror
is disposable and `loop_materialize` rebuilds it. Engineers read documents through
`loop_readFile(slug, "plan")` and `loop_readTask(slug, taskid)` — never by opening a path.

So copying `plan.md` into a loop directory does **not** seed a loop. The document has to be written
through the MCP, or the store does not know it exists.

Two facts that make the sequence below work, both confirmed in
`packages/loop-state-mcp/src/loop-files.ts`:

- `loop_writeFile` accepts a friendly name (`plan`, from `KNOWN_FILES`) **or** a path under an allowed
  directory — `ALLOWED_DIRS = ['tasks', 'scratch']`, extensions `.md`, `.jsonl`, `.txt`. So
  `tasks/T1.md` is a legal name.
- `tasks_derive` reads `plan.md` and creates the task rows. Writing the plan is not enough; the ladder
  comes from deriving it.

## The sequence, per slice

Run these against **this repository** — Aeon is built at `aeon/` inside it.

```
 1.  loop_init(loopSlug, goal, title?, mode?)
 2.  loop_writeFile(loopSlug, "prompt",    <the slice's brief §3 + §4, or a pointer to brief.md>)
 3.  loop_writeFile(loopSlug, "discovery", <a pointer to design.md, plus its Decisions table>)
 4.  loop_writeFile(loopSlug, "plan",      <the folder's plan.md, verbatim>)
 5.  loop_writeFile(loopSlug, "tasks/T1.md", <tasks/T1.md, verbatim>)     ← once per task
 6.  tasks_derive(loopSlug)
 7.  loop_setPhaseStatus(loopSlug, "clarify",   "done")
     loop_setPhaseStatus(loopSlug, "discovery", "done")
     loop_setPhaseStatus(loopSlug, "plan",      "done")
 8.  loop_setPhase(loopSlug, "gate")
 9.  loop_setAwaitingUser(loopSlug, "approve the pre-written plan")
10.  → answer the gate, then run the loop
```

`loop_init` detects the branches itself. **Do not pass `featureBranch`** — reading the checked-out
branch and passing it is the recorded bug where a loop adopted a previous loop's branch *and its open
PR*.

### Why steps 2 and 3 exist at all

The loop's own agents read `prompt.md` and `discovery.md`. An engineer whose `tasks/T3.md` says "read
the design" needs the design reachable through the tools it has, and a handler cannot open
`docs/aeon/S3-model-layer/design.md` unless something points it there. So `discovery` carries a pointer and
the design's Decisions table — enough to act on, with the full document one `read` away.

Keep them short. The task specs are the contract; these two are orientation.

### Why steps 7 to 9 exist

**`clarify`, `discovery` and `plan` are already done.** The design is the discovery, the brief is the
contract, and `plan.md` + `tasks/` is the plan. Marking them `done` is what stops
`project-planner-loop` re-planning work that has been reviewed — and it turns its phase into a
verification pass, which is what you asked for when you chose to plan everything up front.

Step 9 is deliberate rather than ceremonial: **a pre-written plan should still be approved before it
builds.** The gate is where a human confirms the plan matches what they want, which is the one check
the up-front planning removed from the loop.

## Order

[`README.md`](README.md)'s dependency graph is authoritative:

```
S1 ─▶ S2 ─▶ S3 ─▶ S4 ─▶ S5                            skateboard
                          │
                          ├─▶ S6 ─▶ S7
                          └─▶ S8 ────┴─▶ S9              bicycle
                                            │
      ┌─────────────────────────────────────┘
      └▶ S10*─▶ S11 ─▶ S12*─▶ S13 ─▶ S14*─▶ S15*         car
                                       └──▶ S16 ─▶ S17
                                                   │
      ┌────────────────────────────────────────────┘
      └▶ S18  S19 ─▶ S20* ─▶ S21*   S22  S23            shuttle

* not yet specced — see ROADMAP.md §2
```

**S1 blocks everything**, and it is the smallest slice at 7 tasks. That is deliberate: `dev-loops-aeon` has never been
run, so **S1 is where you want to find out whether the agent works** — not S11 at 13 tasks.

**S2 → S3 → S4 → S5 is sequential**, and two of those orderings are load-bearing rather than conventional:
**S4 precedes S5** because a step persists its transcript and there is nothing to build a runtime against until the
tables exist; and **S3 precedes S4** because the store persists canonical messages, so their shape has to be settled
first.

**S6/S7 and S8 are independent** — suspension and blueprints touch different code — so they can run **concurrently in
separate worktrees**. S9 needs both.

**The car stage is a chain, and the order is the security model**: **S10 policy** before **S11 permissions** (the
corpus becomes `CommandAccess`), before **S12 tools** (grants are policies), before **S13 agents**, before **S14
services**, before **S15 plugins**. **Each layer can only narrow what the one above it granted** — build one out of
order and it has nothing to narrow.

**S11 precedes S13**, because a handler's `bash` calls the policy evaluator. That one was never obvious from the names.

**S18, S22 and S23 are independent of each other.** Order them by what you need first. **S20 triggers needs S19**,
because a webhook endpoint is an enumerated projection on the narrow surface, not a second listener; and **S21 remote
agents needs S19 and S14**. **S22 is the only optional slice** — the
product must work fully without it, and its own demo starts by removing it.

**One loop per project root.** Two concurrent loops in this checkout would share a working tree and a branch, so run
concurrent slices in **separate worktrees** — which is also why the `existing-worktree` branch mode exists (S16's D6).

## Loop slugs and goals

One loop per slice. **The slug is stable** — it appears in the loop's own records, so keeping it means a loop is
findable six months later.

| Slice | `loopSlug` | `goal` | Amends QA suite |
| --- | --- | --- | --- |
| `S1-bootstrap` | `aeon-bootstrap` | Verify the hand-made `aeon/`, pin the framework, register an allow-list, and make `make dev` start a daemon and open a window | `aeon-bootstrap` |
| `S2-protocol` | `aeon-protocol` | Build JSON-RPC over the socket, with subscriptions and daemon lifecycle events | `aeon-protocol` |
| `S3-model-layer` | `aeon-model` | Build the canonical message format, two adapters, and three-state cost | `aeon-model` |
| `S4-store` | `aeon-store` | Build the schema, migrations, transcripts, questions and two pools | `aeon-store` |
| `S5-one-step` | `aeon-one-step` | Run one bounded model step against a repository with `read` and `bash` | `aeon-runtime` + `aeon-tools` |
| `S6-suspend` | `aeon-suspend` | Add the `question` tool, suspend by exiting, and resume cold | `aeon-runtime` + `aeon-tools` |
| `S7-recovery` | `aeon-recovery` | Recover a crashed step: three cases, re-run classes, the `bash` receipt | `aeon-runtime` + `aeon-tools` + `aeon-store` |
| `S8-blueprints` | `aeon-blueprints` | Build the blueprint format, its validator, `decide`, and a two-step loop | `aeon-loop` |
| `S9-app` | `aeon-app` | Build the app: loops, the ladder, the thread, questions inline, honest cost | `aeon-ui` |
| `S10-policy` | `aeon-policy` | Build the governance interface: the closed type set, the pure deny-wins evaluator, the authorship rule at the write boundary, and the record | `aeon-policy` |
| `S11-permissions` | `aeon-permissions` | Build the guard from the measured corpus, and the remaining six tools | `aeon-permissions` + `aeon-tools` + `aeon-ui` |
| `S12-mcp-client` | `aeon-mcp-client` | Build the three tool tiers, private namespaces that peers cannot see, and grants held as policy | `aeon-mcp` |
| `S13-agents` | `aeon-agents` | Build the handler registry and rewrite the 25 bodies with drop records | `aeon-agents` |
| `S14-agent-services` | `aeon-agent-services` | Build A2A cards, typed skills and Tasks; agent-to-agent with the chain recorded and cost rolled up | `aeon-agents` |
| `S15-agent-plugins` | `aeon-agent-plugins` | Import agents we did not write: the three-step path, the blocklist check, consent as requests, and revocation that revokes | `aeon-agents` |
| `S16-git-forge` | `aeon-git-forge` | Build eight idempotent code actions, governed by effect class | `aeon-loop` |
| `S17-full-ladder` | `aeon-ladder` | Run the full ladder: phases, two-level caps, redo edges, three verify outcomes | `aeon-loop` |
| `S18-providers` | `aeon-providers` | Add the OpenAI-compatible path, capability probes, and cross-provider resume | `aeon-model` |
| `S19-narrow-surface` | `aeon-narrow` | Build one action table on stdio and HTTP, with no operational control | `aeon-mcp` |
| `S20-triggers` | `aeon-triggers` | Build the clock and webhooks: one catch-up, DST, HMAC, and the two off switches | `aeon-triggers` |
| `S21-remote-agents` | `aeon-remote` | Build A2A over the wire: TLS, pinned identity, egress as policy, and a bound callback | `aeon-remote` |
| `S22-retrieval` | `aeon-retrieval` | Build hybrid retrieval over code and loop history — **optional** | `aeon-retrieval` |
| `S23-first-run` | `aeon-first-run` | Build `init`, presets, `doctor`, the error shape and the README | `aeon-cli` + `aeon-runcontrol` |

**Which primary runs them:** [`opencode/agents/dev-loops-aeon.md`](../../opencode/agents/dev-loops-aeon.md). It is a
sibling of `dev-loops-brief` whose PHASE 2 is a **plan audit** rather than planning — it flattens the dependency graph
into a linear chain, renumbers to `T1..Tn` with a mapping table, splits anything oversized, verifies DoD coverage, and
runs the brief's §13 self-check. **It has never been run**, which is the other reason to start with S1.

### QA suites are shared, and that is deliberate

**Fourteen suites for seventeen slices.** Three slices share `aeon-runtime` (S5, S6, S7) and three share `aeon-loop`
(S8, S16, S17), because they are stages of one capability and *a second suite for the same capability is the
duplication the convention exists to prevent.*

So a slice's qa phase **amends** its suite — appending cases, never renumbering — and marks anything not yet applicable
**with the slice that brings it**, rather than deleting it. Every slice from S3 onward has done this, and the amendment
notes at the top of each suite are where the corrections live.

## When a loop finishes a task, the record is still updated by hand

**A loop does not update [`STATUS.md`](STATUS.md).** Its agents write their task's Notes if the task file
tells them to, and nothing else — so after a loop lands a task, whoever is watching it does the rest:
[`aeon/AGENTS.md` §4a](../../AGENTS.md) lists the four things.

This is the most likely place for the record to drift, precisely because a loop running unattended feels
like it is keeping track of itself. It is keeping track of *its own* phases and tasks, in its own store —
which is invisible to the next person opening this directory.

**Until Aeon runs Aeon, the index is a human artefact.** Treat a finished loop the way you would treat a
finished hand-written task.

## What to check after seeding, before running

1. `loop_get(loopSlug)` shows the three phases `done` and the phase pointer at `gate`.
2. `loop_readFile(loopSlug, "plan")` returns the plan verbatim.
3. `loop_readTask(loopSlug, "T1")` returns T1's spec.
4. The task count from `tasks_derive` equals the plan's `task_count`. **A mismatch means the plan's
   task list and its `task_count` disagree**, which is worth fixing before a loop runs on it rather
   than discovering at task N.
5. `loop_audit(loopSlug)` reports no findings.

## A caution about the QA suites

The suites in [`../test_suites/`](../test_suites/) are **written before implementation** and marked
`draft`. `qa-engineer-loop` curates that folder on a PASS run, and its schema rule is that TC/QAC case
IDs are stable — append, never renumber. So when the qa phase runs, it should **amend** the existing
suite for that capability rather than write a new one, and the drafts are there to be corrected by
what actually happens rather than treated as fixed.
