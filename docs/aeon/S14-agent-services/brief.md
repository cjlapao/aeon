# Brief — S14 Agent services

**Implements:** [`design.md`](design.md) · **Demo:** [`DEMO.md`](DEMO.md) · **Plan:** [`plan.md`](plan.md)
**Architecture:** [`../ARCHITECTURE.md`](../ARCHITECTURE.md) — **P2, P31, P32 and A13 are binding.**
**Stage:** 3 · Car, fifth of eight. Depends on **S10**, **S12**, **S13**. **Blocks S15, S21.**
**Repo:** Aeon lives at `aeon/` in this repository (P24) · Go 1.26 · `cgo` enabled (B3)
**Checked 2026-10-26 against P34** (`ARCHITECTURE.md`) — **already compliant, verified against the real
code, not just this brief.** T1's own Q2 answer reads "a card is a file, for now" (written before T10), but
**T10 (built) added `agent.card` as a real, registered, read-only socket method** (`internal/protocol/agent.go`,
`internal/protocol/method.go:535`) — confirmed by reading the code, not the brief alone. No gap: the app can
already read an agent's signed card the same way the CLI does. (The same file's `agent.plugins` entry notes
that import/enable/disable/revoke are CLI-only today — that is **S15's** gap, recorded there, not this
slice's.)

---

## 1. What this is

**Agents publish what they can do, and can ask each other to do it.**

S13 gave Aeon 25 agents that cannot talk to each other. This slice lets them — without spawning anything,
without a concurrency model, and with the cost attributed along a recorded chain.

## 2. The amendment this slice carries

**A1 said:** *"no spawn, no abort, no per-subtree cost rollup, because there is no tree."*

**Right about process. Wrong about accounting.**

Nothing spawns, here or ever — a delegated unit of work is a **row**, exactly as S6 made suspension a row.
But an agent asking another agent produces a **graph**, and a graph has edges whether or not anybody
records them. **Refusing to record them does not make the cost unattributable; it makes it unattributed** —
which is worse, because unattributed reads as zero.

So: **still nothing spawns, and the chain is recorded.**

## 3. Why it is cheaper than it sounds

**An async Task is a suspension, not a concurrency model** (AS4). The step fires it and **exits** — S6's
machinery exactly (P2) — and resumes when the Task completes.

So *"always-on background agents"* costs **no worker pool, no queue, and no change to S5's serial
execution.** The hard part was solved two stages earlier for other reasons.

## 4. Definition of Done

| | |
| --- | --- |
| **D1** | **One of S13's 25 bodies, unchanged, still dispatches** — no card, no skills, no tools |
| **D2** | **The whole suite passes with agent services switched off** (AS11) |
| **D3** | A valid card verifies; **a tampered card is refused, not warned about** |
| **D4** | A skill request missing a required field is refused **before any model call** |
| **D5** | **An async Task suspends the step** — asserted on the step row being `suspended`, with **no goroutine and no process** |
| **D6** | **A completion resumes cold** — kill the daemon between creation and completion, restart, the loop resumes |
| **D7** | **Every chain edge is a row**, and the chain is reconstructable after a restart |
| **D8** | **A cycle is refused with the cycle named** — not as a cost-cap failure |
| **D9** | A chain one deeper than the bound is refused **at the boundary** |
| **D10** | **A parent's cost includes its children's** |
| **D11** | **A `partial` child makes the parent `partial`** (P32) |
| **D12** | Denying `SkillAccess` makes the skill uninvokable, **and there is no second mechanism** |
| **D13** | A non-first-party skill result is labelled untrusted in the assembled prompt (AS12) |
| **D14** | **No path exists from a similarity score to an invocation** — asserted by grep (AS9) |
| **D15** | A Task resumed after a policy change evaluates against **the set it was created under** (AS13) |

**D5 and D8 are the two that matter.** D5 because it is the whole reason this slice is cheap, and because
"suspends" is easy to implement as "blocks" and pass a casual test. D8 because a cycle diagnosed as a cost
overrun is the wrong answer to a structural bug — and it is what will happen if nobody checks.

**D11 is the one most likely to be skipped.** A rollup that sums numbers is easy; a rollup that propagates
*uncertainty* is the point.

## 5. The seams

**Downward — S6's suspension.** An async Task uses it; it does not reimplement it. If this slice grows a
goroutine, AS4 is being violated.

**Downward — S10's `SkillAccess`.** Authorization is a policy, not a second ACL. A2A's `securitySchemes`
is *expressed through* it (AS8).

**Downward — S13's `Handler`.** It gains an **optional** card and skills. **The 25 bodies do not change.**

**Sideways — S8.** A lane must be able to target a skill, which means a step carries `skill:` and
`params:`. **That is a prerequisite for T4, not a consequence** — see §8.

**Upward — S15 and S21.** S15 imports agents (which is why cards are signed now); S21 takes the same card,
skill and Task model over the wire.

## 6. Locked decisions

- **AS4** — **async is a suspension.** No goroutine, no worker pool, no queue, no timeout-into-background
  promotion.
- **AS5** — **nothing spawns.** A Task is a row.
- **AS7** — **a cycle is refused, naming the cycle.**
- **AS8** — **one authorization mechanism**: `SkillAccess`.
- **AS9** — **dispatch stays closed.** Similarity picks what to read about, never what to call.
- **AS1 / AS11** — **everything is optional, and that is a test.**

## 7. Out of scope

- **N1 — No remote agents.** S21. Same card, skills and Task model; TLS, a trust store and egress policy
  are what it adds.
- **N2 — No agent import.** S15 — and AS2's signatures exist *for* it.
- **N3 — No authoring UI.** An S9 amendment.
- **N4 — No concurrency.** AS4 means there is none to add.
- **N5 — No scheduling.** An always-askable agent is not a timer. S20.
- **N6 — No similarity dispatch.** Ever (AS9, A13).
- **N7 — No new transport.** JSON-RPC 2.0 over the socket Aeon already has (P25).

## 8. Questions to answer before building

| # | Question | Where |
| --- | --- | --- |
| Q1 | The depth bound, as a number | T5 |
| Q2 | Is the card a file, a socket method, or both? | T1 |
| Q3 | Does a first-party result need the AS12 label? | T8 |
| Q4 | **Can a skill invocation suspend for a human question?** | T3 |

**Q4 is the interesting one.** A skill whose agent needs an `ask` is a Task that suspends — which
**composes**, because suspension is already a row. The real question is whether the *caller's* step must
also suspend. **Probably yes**, and whichever way it goes it must be stated, because the alternative is a
caller blocked on a human it cannot see.

**And one prerequisite rather than a question:** **S8's step must carry `skill:` and `params:` before T4
can target a skill from a lane.** If that amendment has not landed, T4 is blocked — say so rather than
working around it with a free-text convention.

## 9. Risks

- **R1 — "Suspends" becomes "blocks".** A goroutine that waits passes a naive test and destroys P2. **D5
  asserts on the step row and the absence of a goroutine.**
- **R2 — The cycle check is a depth check.** They are different: depth catches runaway, a cycle catches a
  structural mistake, and **conflating them diagnoses the wrong thing.** D8 and D9 are separate.
- **R3 — The rollup sums and forgets uncertainty.** **D11** is the guard, and P32 is the reasoning.
- **R4 — Similarity reaches dispatch.** S22 will index cards, and *"find an agent that can do X"* is the
  obvious next step. **D14's grep is the guard**, and A13 is the argument.
