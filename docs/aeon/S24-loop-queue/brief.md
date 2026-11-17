# Brief — S24 Loop Queue

**Implements:** [`design.md`](design.md) — written 2026-10-26, against the built S11/S12/S13 (the brief's
own §7 questions needed code that did not exist when this brief was written). **Demo:** [`DEMO.md`](DEMO.md)
— predictive, not yet walked. **Plan:** [`plan.md`](plan.md) — nine tasks, `tasks/T1.md`–`T9.md`. This brief
is still the contract; `design.md` resolves every §7 question below and names two more the brief could not
have anticipated (loop status has no CHECK and `blocked` is overloaded; a gate question needs an existing
loop row, which a cold-start queue does not have).
**Architecture:** [`../ARCHITECTURE.md`](../ARCHITECTURE.md) — P29 and P31/P32 are binding. S11's **G14**
(attendedness declared and pinned, never inferred) is binding and is the one rule this slice has to extend
carefully rather than quietly relax — see §5's Q-decision.
**Stage:** 5 · Fleet, first of six — and the one exception: buildable now, over systems that already exist,
so it gets a brief instead of a sketch. **Depends on:** S1–S10 (daemon, store, policy and the attendedness
pin), S8 (blueprints), S9 (app — for a test-bench surface). **Does not depend on S16** (git-forge — now
built, 2026-10-26 — but this slice is designed not to need it regardless, a deliberate decoupling, not an
availability gap; see R1). **Feeds:** S26 Roadmap (renders this slice's ordering and sizing) and S27 Continuous (the
fleet-wide generalization of what this slice does for one project).
**Repo:** Aeon lives at `aeon/` in this repository (P24) · Go 1.26 · `cgo` enabled (B3)
**Checked 2026-10-26 against P34** (`ARCHITECTURE.md`) — already compliant: CG9 (§5) and T7 give every
queue operation (`append`, `list`, `reorder`, `remove`, `setSize`, `settings`) a `queue.*` socket twin of
the identical `internal/queue` call the CLI verb makes; no CLI-only gap found.

---

## 1. What this is

**A project stops being one loop at a time, typed in by hand.** Today, finishing a loop and starting the next
is a person: merge, then remember to start the next one. This slice gives a project an ordered, open-ended
backlog of loop specs — append to it at any time — that the daemon advances through on its own, one loop at a
time, unless the project asked to be asked first.

**It is deliberately smaller than S26/S27.** No cross-loop projection (S25 History), no forward Gantt (S26
Roadmap), no fleet-wide scheduler (S27 Continuous). One project, one ordered list, advancing on a signal that
already exists — the loop's own terminal status.

## 2. Why it is cheaper than it looks

**Nothing it needs is unbuilt except the queue itself.** A project already groups loops (S4's schema). A loop
is already started the same way whether a person or this slice does it — `loop.start` with a goal, a root, a
blueprint. Attendedness is already a pinned, declared-not-inferred concept (G14, S11). The "ask before
proceeding" shape already exists as a pending question (S6). This slice adds one new table, two project-level
settings, one native tool, and the orchestration that watches a loop finish and decides what happens next —
not a second execution path.

## 3. Definition of Done

| | |
| --- | --- |
| **D1** | **A project holds an ordered, open-ended queue of items.** Appending while the queue is running never disturbs the order of items already materialized or started |
| **D2** | **At most one queue item runs per project at a time.** The next item is not materialized or started until the current one reaches a terminal status |
| **D3** | **Advancing is gated by `project.queueAdvance` (`auto` \| `ask`).** `auto` starts the next item immediately on a terminal status; `ask` surfaces a pending question and advances only once answered |
| **D4** | **The queue-advance gate is independent of the loop's own completion/merge approval.** Changing one never implicitly changes the other — a blueprint can allow an agent-approved merge while the project still asks before advancing, or the reverse |
| **D5** | **A queue item's attendedness is declared when the item is added** (explicitly, or the project's declared default) and carried unchanged onto the loop it materializes — never inferred from the daemon starting that loop with nobody watching (G14, extended to this new caller) |
| **D6** | **The default attendedness for a new item is unattended.** `attended` is an explicit opt-in, per item or as a project default a person turned on, never a silent consequence of `queueAdvance: auto` |
| **D7** | **Sizing mode is a project setting (`manual` \| `skill`).** In `skill` mode, creating an item dispatches a sizing run before the item is `ready`; in `manual` mode nothing runs automatically |
| **D8** | **A size is a range with its basis, never a bare number** (P31/P32's shape), whether entered by a person or returned by the sizing skill |
| **D9** | **The sizing skill reports its result through one native tool**, called once, with a known shape — never by the daemon parsing free text out of a chat reply |
| **D10** | **A queue item has its own status**, distinct from the loop status it may eventually point at — before a loop exists there is no loop status to read, and the two must not be collapsed into one field |
| **D11** | **The queue is never the source of truth for what a loop did.** Once materialized, the loop's own rows are authoritative; the queue item only points at it (S26's own rule, honoured here first) |
| **D12** | **A pending item can be reordered or removed.** A running or done item's position is history and is fixed |

**D5 and D6 are the two that matter.** D5 because it is the whole reason a self-sustaining queue does not
quietly widen what an unattended loop is allowed to do. D6 because the safe default has to be the one nobody
has to think about, and the risky one has to be the one somebody deliberately turned on.

## 4. The seams

**Downward — the existing `loop`, `project` and `blueprint` rows.** This slice reads them and writes loops the
same way a person does: through the existing start path, with the same inputs (goal, root, blueprint) a human
would type in. No second loop-creation path.

**Downward — the attendedness pin (G14, S11).** Unchanged. This slice only supplies the *value* at the moment
it starts a loop; the pinning mechanism, and everything G14 already governs about what an unattended loop may
not do, stays exactly as S11 built it.

**Sideways — the existing pending-question shape (S6).** `queueAdvance: ask` reuses it rather than inventing a
second kind of "waiting for a person," the same way a suspended step already does.

**Upward — S26 Roadmap and S27 Continuous.** Both consume this slice's ordering and sizing rather than
building their own: S26 renders a queue's items as part of its forward Gantt where one exists; S27 is this
slice's advance-on-terminal-status idea, generalized from one project's list to whatever is ready anywhere.

**New — one native tool, sizing-scoped.** The sizing skill's run gets exactly one extra tool (`report_size` or
similar), the same mechanism S11's nine tools already use, not a new kind of tool-calling.

## 5. Locked decisions

- **A project is a repo.** The queue is per-project, never cross-project — that is S27's job, not this
  slice's.
- **Two gates, kept apart.** The blueprint decides when a loop is *done* (including whether its merge needs a
  human or an agent's approval — existing policy/effect-class machinery, unchanged). The project decides
  whether *advancing the queue* needs a human, independently of how the loop finished (D3/D4).
- **Q-decision — attendedness is declared at add time, not at the moment the daemon actually starts the
  loop.** G14 requires attendedness to be *declared, never inferred*. A queue that runs unattended by design
  has nobody present at the actual start instant, so without this decision every queue-started loop would be
  unattended by the same reasoning S27's sketch already used for a scheduler's auto-start. This slice instead
  lets the declaration happen *earlier* — when a person added the item, or set the project's default — which
  still satisfies "declared, never inferred"; it only moves *when* the declaration is made. **Nothing about
  G14 itself changes.** This is the one piece of this brief most worth a second pair of eyes before building.
- **Sizing is native, not MCP.** The sizing skill reports through an Aeon tool call, the same mechanism S11's
  nine tools use. No dependency on S12 (MCP client, not built).
- **A sizing run is dispatched like any other step — to an agent, not a bespoke execution path.** `sizing:
  skill` names a project *mode*, nothing else. It is not S14's A2A `Skill` (published via an Agent Card) and
  not the opencode skill-loading mechanism S13 retired (`dev-loop-protocol`, A5) — this project already has
  two other meanings for "skill," and the sizing run is deliberately a third, unrelated one. Concretely: the
  daemon starts a loop/step and assigns it to an agent (one of S13's handler bodies, or one authored for this
  purpose) the same way any blueprint step picks an agent, with `report_size` added to that step's toolset.
  R3 is the consequence that matters: because it is an ordinary step, it carries the project's normal policy,
  attendedness and cost-cap rules with it — there is no separate "sizing lane" to forget to govern.
- **No new "done" signal.** Advancing reacts to whatever terminal status the loop/runtime already reports.
  "Merged" is one blueprint's particular shape of done (via S16, once it exists); a blueprint that never
  touches git-forge still has *a* done, and the queue advances on that just the same.

## 6. Out of scope

- **N1 — No cross-project queue or scheduler.** S27 Continuous.
- **N2 — No forward Gantt or roadmap rendering.** S26 Roadmap. This slice produces the data; it does not wire
  the UI kit's Gantt component.
- **N3 — No merge mechanism.** S16 Git/forge. This slice only reacts to a loop reaching a terminal status —
  it does not open, approve or merge anything itself.
- **N4 — No external MCP sizing.** A real MCP tool server is S12's dependency; this slice's sizing is a
  native tool only.
- **N5 — No relaxation of G14's default.** Unattended stays the default for everything this slice starts;
  `attended` is opt-in (D6).
- **N6 — No change to the policy/effect-class machinery.** This slice is a caller of the existing attendedness
  pin and the existing approval mechanics, not a second implementation of either.

## 7. Questions to answer before building

| # | Question | Where |
| --- | --- | --- |
| Q1 | The queue item's own status vocabulary and transition table — `pending` / `sizing` / `ready` / `running` / `done` / `skipped` / `cancelled`, or a different closed set? | design.md |
| Q2 | What, precisely, counts as a loop's "terminal status" for advance purposes? `loop.status` has no `CHECK` constraint today (free `TEXT`, validated in Go) — the real enumeration needs reading from the runtime code, not guessed here | design.md |
| Q3 | Where does a queue item's goal/brief/files/context live — the same shape S8's blueprint inputs and S9's `contextManifest` already use, or a new free-form field? | design.md |
| Q4 | What happens when a loop's terminal status is a *failure*, not a success? The happy path (done → start next) is this brief's whole example; a failed item stopping the queue is the safer default, but it is a decision, not an oversight to discover later | design.md |
| Q5 | Can a sizing run itself be sized, or is the sizing run exempt from `sizing: skill` recursing into itself? (It should be exempt — a sizing run is not a queue item — but say so explicitly) | design.md |

## 8. Risks

- **R1 — Coupling "advance" to "merged" specifically.** The natural shortcut is to wait for a git-forge merge
  event. **The guard is D-shape, not D-numbered: advance on the loop's own terminal status**, with "merged"
  as one blueprint's way of reaching it — so this slice works today, without S16, and does not silently break
  the day a non-git blueprint uses it.
- **R2 — Attendedness inherited silently.** The queue could accidentally hand a loop outward/irreversible
  capability nobody actually attended to. **D5/D6 are the guard**: declared explicitly, defaulting to the
  safe answer.
- **R3 — The sizing run escapes governance.** It is still a loop/step with a tool call, and it would be easy
  to special-case it out of the normal cost/policy/attendedness path because "it's just sizing." **It must
  go through exactly the same path any other step does.**
- **R4 — `queueAdvance: auto` becomes the default.** It should not be. **The safe default is `ask`**, matching
  every other default-deny bias in this project; a project turns `auto` on deliberately.
