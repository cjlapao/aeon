# Brief — S27 Continuous Work

**Implements:** `design.md` — not written yet, deliberately. **Promoted from [`SKETCH.md`](SKETCH.md)**
2026-10-26, **to brief depth only**, by explicit choice. At the time this brief was written, it sat on three
layers of unbuilt work (S16, S24, S26); **S16 is now built (confirmed 2026-10-26)**, leaving two: S24 and
S26 (S26 itself blocked on S24/S25). The sketch's own self-assessment — *"the largest change to the
daemon's model in the whole plan"* — still holds regardless of which dependencies are built; it was never
really about S16 specifically. What changed since 2026-09-07: S24/S25/S26 now have real designs to ground
against, not just sketches, and §3's collision argument is now checked against S16's actual code, not just
its brief. What a full `design.md` would still need is named in §7.
**Architecture:** [`../ARCHITECTURE.md`](../ARCHITECTURE.md) — P28 (memory budget + global cap), P29 (daemon
owns every loop, no detached process) are both directly load-bearing here, more than for any slice so far.
**Stage:** 5 · Fleet. **Depends on:** S8 (blueprints, built), S11 (G14/attendedness, built), **S16** (git-forge
— **built**, see §3), **S24** (queue, specced to tasks, not built), **S26** (roadmap, specced to tasks, not
built, and itself blocked on S24/S25). **Repo:** Aeon lives at `aeon/` in this repository
(P24) · Go 1.26 · `cgo` enabled (B3)
**Checked 2026-10-26 against P34** (`ARCHITECTURE.md`): no code exists yet to audit, but see the new §7 Q6
— whoever writes `design.md` must give the scheduler's own controls (enable/disable, concurrency,
Q1/Q3/Q4's answers once settled) a socket surface from the first task, not a CLI flag discovered later,
since a scheduler the app cannot see or stop is a background process the primary interface has no door
onto.

---

## 1. What this is

**Agents stop being one-shot.** A worker picks up a `ready` roadmap item, runs it, and when it finishes
checks whether there is another — anywhere, not just in the project it just worked on. It is S24's
advance-on-done idea, widened from "the next item in one project's queue" to "whatever item anywhere is
ready," picked up by a scheduler instead of materialized by a person.

**This turns Aeon from a thing you start into a thing that is running**, and that sentence is the whole
reason this brief stops at brief depth: it is one thing to spec a scheduler's shape, another to have watched
S24's queue and S26's roadmap actually run first and learned what their real failure modes are before
building the thing that calls them unattended, at scale, across every project at once.

## 2. Why it is the largest change

Everything built so far assumes a loop is **started deliberately** — a person at a CLI, the app, or S19's
`trigger` (itself still a configured, named, auditable thing someone set up). A scheduler is a genuinely new
kind of caller: one that starts work when nobody asked *at that moment*.

**Additive in mechanism**: a scheduler is another caller of `loop.start` (S8's runner already starts a
goroutine per running loop — no new execution model). **Not additive in consequence**: it changes who is
watching when something goes wrong, system-wide, continuously, rather than for one person's one loop.

## 3. The two collisions the sketch already named, resolved against real mechanisms

**Collision 1 — G14 (attendedness declared at start, pinned, never inferred). Who declares it for a loop
nobody started?**

**Resolved, and now genuinely verified against S16's real, built code (2026-10-26), not just its brief**:
`internal/action/class.go`'s own comment, quoted exactly — *"G14: unattended resolves outward and
irreversible to deny."* (`S16-git-forge/brief.md` D8 said the same thing first, when S16 was still only a
brief — the reasoning held, and checking it against the shipped code rather than the plan is what "verified"
should have meant all along.) An auto-started loop is unattended by construction (S24's D6 precedent, the
same safe default); S11's own rule — an `ask` on an unattended loop denies **at once, with no question row**
(quoted in `aeon/AGENTS.md` §4) — means a scheduled loop structurally cannot open or merge a pull request,
not by a new check this slice adds, but by mechanisms S10/S11/S16 already enforce for any unattended loop,
confirmed live in the class each now ships. **The opt-in moves to the roadmap item**, the moment someone was
actually thinking about what this work may do — see §6's amendment.

**Collision 2 — P28 (memory budget, global cap) meets a scheduler that starts work whenever something is
ready.**

**Not resolved here, named as the real open question it is** (§7 Q1): queue, refuse, or preempt when the
cap is hit. The sketch's instinct — queuing is almost certainly right, and visible in the roadmap rather than
silent — is kept as a direction, not locked as a decision, because nothing in S24/S25/S26's actual builds
has yet tested what "visible" should look like under real load.

## 4. What "ready" means — not redefined here

**S26's definition, used verbatim**, per `S26-roadmap/brief.md`'s own D4: *"brief agreed, dependencies
complete, priority not `unknown`."* S26 owns this; this slice is its first and, so far, only real consumer.
A scheduler that treated `unknown` priority as low would silently never run those items — the automated
version of the exact failure S29 Triage exists to prevent (`S29-triage/brief.md` D2, already cited by S26).

## 5. What has to be true before this can run, named plainly

- **A real `aeon loop stop`/loop-level cancel does not exist yet.** Checked: `internal/protocol/method.go`
  has `step.stop` (pauses one step, built, S9) and no loop-level equivalent. The sketch's Q4 — *"`aeon loop
  stop` is human authority (R6)"* — assumed a mechanism that is not yet real. Whoever builds this slice
  needs it first, or needs to confirm it landed elsewhere in the meantime.
- ~~S16's effect classes govern the merge-blocking guarantee this brief leans on (§3), and S16 is not
  built.~~ **Resolved 2026-10-26 — S16 is built, and §3 now cites its real code
  (`internal/action/class.go`) directly, not just its brief.** No longer a risk.
- **S24's queue and S26's roadmap need to exist as running code**, not just designs, before this slice's
  "pick up whatever is ready, anywhere" can be measured against anything real.

## 6. Amendment this slice will need from S26 — not made yet, on purpose

**`S26-roadmap`'s `roadmap_item` table has no `attended` field.** It doesn't need one today: every
materialization path in S26's own design (`S26-roadmap/design.md` R4) is a live, explicit call — a person
calling `aeon roadmap` or the socket method *right then*, the same way a direct `loop.start` call carries
its `attended` argument live. **A scheduler breaks that assumption**: it materializes items with nobody
present at that moment, so the attendedness declaration has to have been made earlier and stored, the exact
shape S24's `queue_item.attended` already has (D5/D6 there). **This brief does not add that column to S26's
schema now** — S26 is complete and correct without it, and adding a field only a not-yet-committed slice
would use is exactly the premature-engineering this project's house style refuses. It is named here so
whoever eventually builds S27 for real knows the first thing their own T1 does is a small, additive migration
on `roadmap_item` — not a redesign of it.

## 7. Questions a real `design.md` still has to answer

| # | Question | Notes |
| --- | --- | --- |
| Q1 | P28's collision (§3): queue, refuse, or preempt when the global cap is hit? | The sketch's instinct (queue) is plausible, not verified |
| Q2 | When a scheduled, unattended loop reaches `blocked`, what does the roadmap item show? | S24's answer ("any `blocked` halts, raises a question") assumes a human can answer the question soon. Nobody is attending a scheduler-started loop by construction — the question sits until *someone* looks, which may be a materially different UX from S24's, not a copy of it |
| Q3 | How many loops run at once — a setting, or derived from P28's budget? | Loops vary in memory use, so a budget does not translate to a count deterministically; almost certainly a setting, informed by budget, not derived from it alone |
| Q4 | Does the scheduler respect a working-hours window? | Leaning toward: unrestricted by default, because the real safety control against runaway cost is S10's `CostCap` policy type, not a clock — a time window is a preference, and treating it as the safety mechanism would be the mistake the sketch's own "surprise bill" line warns about |
| Q5 | Where does the loop-level stop/cancel mechanism actually live (§5), and does it predate this slice or does this slice build it? | Blocking: nothing in this brief can finish without an answer. *Annotated 2026-11-02 (S55 T12): A loop-level stop/cancel and budgets landed in S55.* |
| Q6 | The scheduler's own controls — on/off, concurrency setting, Q1's queue/refuse/preempt choice once made — are these socket methods from the first task, or a CLI command discovered after the fact? (P34, added 2026-10-26) | Must be socket-first: the daemon already runs the scheduler with nobody watching a terminal, so the app is the only place a person would ever see or touch it. Mirror S24's `queue.*`/CLI pairing (CG9), not S27's own older habit of designing the CLI verb first |

## 8. What it must not become (unchanged from the sketch, restated as locked)

- **Not autonomous outward action.** The unattended default (§3) is the guard; it must never acquire an
  override that lives anywhere but a roadmap item a person wrote.
- **Not invisible.** A scheduler that starts work is exactly as legible as a person starting it — same
  events, same app, same S25 history entry, because it *is* identical apart from who asked.
- **Not a way around S17's caps.** The loop ceiling (`LoopCeiling`, found and quoted while building S24)
  exists because a loop that cannot converge should stop. A scheduler that restarts a blocked item on a
  timer has removed that guard without touching it — explicitly refused here, before anyone is tempted to
  build the convenient version.

---

## 9. Superseded questions — answered 2026-10-30 by [`design.md`](design.md)

*Appended, not rewritten: §§1–8 above are the brief as it stood on 2026-10-26. The slice now has `design.md` (CN1–CN26), `plan.md` and tasks T0–T12.*

| Q | Status | Answer, in one line | Rule |
| --- | --- | --- | --- |
| Q1 — queue, refuse or preempt at the cap | **Answered: queue, visibly.** | Deferral is a recorded decision naming slots used, the cap and the weight; never preempt; a unit heavier than the whole cap is refused once. The cap counts agent slots — P28's memory budget still has no code. | CN9, CN15 |
| Q2 — what the item shows when an unattended loop blocks | **Answered.** | The repository is held (a blocked loop occupies it), the item/loop keep S24/S26's states, and a deduplicated `needs-you` decision carries the verbatim blocked kind; `scheduler.attention` lists it for S58's inbox. | CN8, CN13 |
| Q3 — loops at once | **Answered: a setting.** | `scheduler.maxAgentSlots` (default 2), weighted by each repository's `max_parallel_agents`; one scheduler loop per repository. | CN8, CN9, CN21 |
| Q4 — working-hours window | **Answered: optional, default unrestricted.** | Per repository, the S20 window code reused; it gates every pick including finish-driven ones; budgets (S55), not a clock, are the safety control. | CN10 |
| Q5 — where loop stop/cancel lives | **Answered: S55 LC, a hard dependency.** | `scheduler.stopAll` calls it and is not registered without it. S20's `trigger.pause` does not stop the scheduler (separate switches). | CN18, CN24 |
| Q6 — socket-first controls | **Answered.** | `scheduler.*` human-door methods first, the CLI a client, agents read-only, a descriptor-driven Settings panel. | CN18, CN20, CN22 |

**§5's first bullet** (no loop-level stop) is now S55's deliverable. **§6's amendment** (a stored attendedness declaration on `roadmap_item`) is **inverted by S56**: the scheduler authors
`attended=false` itself, outward/merge permission comes from per-root grants, and the item gets only an `autonomy_cap` column that can **narrow** (CN3). **§4** ("ready" verbatim) holds, with one
addition the brief could not see: S26's "dependencies complete" has no dependency graph behind it (`roadmap/transitions.go:98-122`), so T3 adds one (CN7).

## 7. Status

**Built 2026-11-05 (13/13, T0–T12).** The continuous scheduler starts S26 `ready` items — roadmap and queue — as **unattended** loops (`attended=0`, provenance `scheduler`, the one start door), under S55 budgets, S56 grants narrowed by the item's `autonomy_cap`, and S57 gates; every act is an append-only, deduplicated, templated decision row audited to the person who enabled it (CN2). The controls are twelve `scheduler.*` human-door socket methods — absent from the narrow surface, agent services, the tool registry and the records catalog — default-off, never seeded, no inheritance down the tree; the CLI is a pure socket client (the slice's one P5 deviation: no offline arm); the Settings panel is descriptor-driven. Windows, rate backstop and aging reuse the S20 code across DST; claim-before-start makes a killed-mid-pick start exactly once (walked ×20 under `-race`); a blocked item holds its repository forever — witnessed live, one claim and one loop ever; preview equals the tick. Close-out evidence — AC1–AC12 table, migration drills (74/74 `-race`), the walked `aeon/scripts/qa-s27-continuous.sh` (rc=0, 2026-11-05) with its quoted lanes, and the suite-vs-T0 adjudication (battery B's 11 reds: every one reproduced at the main tip or green solo) — is in [`tasks/T12.md`](tasks/T12.md) and [SLICES](../SLICES.md); the walk is recorded at [DEMO §S27](../DEMO.md). Slot: **S27**.
