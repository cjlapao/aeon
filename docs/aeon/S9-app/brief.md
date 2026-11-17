# Brief — S9 The app

> ## Amended 2026-10-15 — a test bench already exists, and S9's scope is unchanged
>
> **Superseded 2026-10-19, at the user's direction (S10):** the per-slice bench tab is no longer the
> standing obligation — the cumulative `docs/aeon/DEMO.md` walkthrough is (`aeon/AGENTS.md` §4b);
> existing bench tabs remain as history.
>
> What that amendment put in place, and what this slice inherited as fact: from S3 onward each slice
> **shipped the UI surface that exercised it** — a tab per slice, labelled as a bench in the UI,
> driving the daemon through one generic `rpc(method, params)` bridge.
>
> **That is not an early draft of this slice.** S9 builds the real app — loops, the ladder, the thread,
> chat, questions answered inline, a designed experience. The bench is for seeing the truth; this is for
> living in. In the user's words: *"we will still need a ui final one where we put everything together,
> create a nice ui and the chat components."*
>
> **What this slice inherits, and should not rebuild:**
>
> | | |
> | --- | --- |
> | **U11's generic bridge, already built** | `rpc(method, params)` plus `onEvent`. The method table lives in `internal/protocol` and nowhere else — no Tauri command per method |
> | A persistent session with **correlation** | Request/response by id, notifications demultiplexed by their absence, a lost connection emitted as `aeon://closed` so the UI goes honest |
> | The **three-state** renderers | `costLine` mirrors the CLI's deliberately, so the app and `aeon ask` cannot disagree about what a cost means. Three connection states. Errors in three parts |
> | The **credential rule** | A key goes in and never comes out, enforced by two types. Nothing in browser storage. `AGENTS.md` §4c |
> | A **kit finding** | `Alert` discards its children — it renders `title` and `description` only. Five alerts were silently losing their explanatory sentence before this was found. See `ui/README.md` |
>
> **What this slice decides:** whether the bench's tabs survive as a developer surface, move behind a
> developer-mode flag, or are replaced outright. The honest default is to keep them somewhere reachable:
> *"is the daemon there, at what path, speaking what version"* is a question the real app will also need to
> answer.

> ## The toolkit is `@cjlapao/ui-kit@0.2.3` (updated 2026-10-18)
>
> **Not a new decision** — v1's `packages/loop-state-ui` already depends on it, so the port brings
> components built on it. The app shell, the thread (`TimelinePanel`), the blueprint shape (`TreeView`,
> `ConnectionFlow`) and the stat tiles come for free. `react-router-dom` is a peer dependency;
> `@cjlapao/ui-kit/styles.css` is imported once at the entry (prebuilt Tailwind v4.3.3).
>
> **Two gaps, and they are this slice's thesis.** `StepStatus` lacks `not-needed` and `skipped`, which D13
> requires rendered *distinctly*; and there is no socket-liveness three-state. **T4 settles both before the
> renderers are written** — by extending the kit or composing locally, never by reusing the nearest type.
> See [`PLAN.md`](../PLAN.md) §Phase A′.


**Implements:** [`design.md`](design.md) · **Demo:** [`DEMO.md`](DEMO.md)
**Inherits:** U2–U9 from [`docs/_archive/aeon-v1-horizontal/10-ui-chat`](../../_archive/aeon-v1-horizontal/10-ui-chat/design.md). U1 is superseded by
S1; U10 is **corrected** (§9 I2). New decisions begin at **U11**.
**Stage:** 2 · Bicycle, fourth of four — **the halfway point of the plan**. Depends on S1, S5, S6, S7, S8.

> **Template deviations from `proposal-brief-protocol`, stated rather than silent:**
> **(a)** Implements a design, not a proposal — §7 records where this brief overrides it.
> **(b)** §14 is the demo contract, present in every slice of this plan.
> **(c)** This is the first slice whose work is mostly **TypeScript and Rust**, not Go. The rules are the same; the
> toolchain is not.

---

## 1. How to use this brief

> **Constant. Copy verbatim.**

This is a **brief, not a plan.** It states what must be true when the work is done, what must not change along
the way, and where to look to find out the rest. It contains no task breakdown and no implementation code.

1. **Read** the design and the demo, then §7 for where this brief overrides the design.
2. **Investigate** — §10 names what to read. Do not plan before you have those answers.
3. **Plan and decompose** — §12 gives the rules your task sequence must obey.
4. **Self-check** — §13, against your finished plan, before dispatching anything.

Where this brief states a decision, it is settled. Where it poses a question, answer it from the code.

**On the self-check:** every answer needs an artifact — a command's output, an enumeration, a quoted line.

---

## 2. Definition of Done

| # | Check |
| --- | --- |
| **D1** | **An unknown is never rendered as a zero, a dash, or an absence** (U17). Asserted for **every** three-state value: cost, blueprint validation, recovery outcome, liveness. A snapshot test proves the string `$0.00` never appears for an `unknown` cost. |
| **D2** | **`partial` cost reads as a floor** — "at least $X" — and a **total over a partial part is itself a floor**. A confident total over an uncertain component is the same error one level up (U6). |
| **D3** | **The bridge is generic** (U11). `src-tauri/` contains **no protocol method names** — asserted by grepping the Rust source against S2's method table. One name appearing means U11 is broken and a second method table has been born. |
| **D4** | **Types are generated from S2's contract** (U12), checked in, and **CI fails if regenerating produces a diff**. |
| **D5** | **Staleness is rendered with an age and the socket path** (U13). A dropped socket shows stale; the age counts up. |
| **D6** | **Silence with a live socket stays `current`** — the wolf-crying case. A loop parked at a gate for five minutes shows no warning. Asserted as its own test, because getting this backwards makes D5's warning worthless. |
| **D7** | **Reconnect re-reads.** After a drop and restore the app arrives at true current state, including what happened while disconnected, and does **not** resume from its last `seq` alone. |
| **D8** | **A step question and a gate render identically** (U14), differing only in text and options. Asserted by comparing rendered output. |
| **D9** | **No optimistic rendering** (U9, U19). A refused `answer` leaves the thread unchanged and the options live. Asserted with a bridge that rejects. |
| **D10** | **The app opens no file under the state directory and no database** (U16). Asserted in both the web layer and the Rust shell. |
| **D11** | **The webview loads only bundled assets**, with a locked CSP and no remote origin loadable (U15). |
| **D12** | **The ladder updates live**, from subscriptions only. No polling anywhere — asserted by there being no interval timer driving a data read (U3). |
| **D13** | **`not-needed`, `skipped` and `pending` render distinctly**, and a bypassed lane shows **its condition** (S8's O8). |
| **D14** | **A recovery-failed step names the tool and the receipt state** in the app (S7's K33). *"It started and we cannot say what happened"* is a different fact from *"it failed"*. |
| **D15** | **`unverifiable` validation renders distinctly from both `valid` and `invalid`**, naming the slice that brings the missing registry. |
| **D16** | **A step's transcript is complete on expand.** Truncation in the summary view is fine; silent shortening of the underlying data is not. |
| **D17** | **A compacted step says so**, and the summary call's cost is in the total. |
| **D18** | **The declared context manifest is shown**, with a file that changed while suspended marked as changed (S6's K14). |
| **D19** | **Every screen has a real empty state** — no loops, no steps yet, no blueprints — and a real error state. A blank panel is indistinguishable from a broken one, which is the failure mode U12's generated types exist to prevent. |
| **D20** | **The v1 port is accounted for and guarded.** Pure helper tests pass; S8-specific compositions are tested against generated types. The manifest accounts for every v1 module and new component. The v1 platform and phase/task tests are excluded where their filesystem permissions or old schema conflict with U16/D10 and S8. |
| **D21** | **Nothing in the demo requires a terminal.** Every step of `DEMO.md` is done from the app, except the deliberate `pkill` and `make dev`. |
| **D22** | **`make test`, `make lint` green across all three toolchains**; `make fmt` leaves no diff. |

**D1, D3 and D6 are the three that matter.** D1 is the slice's thesis. D3 is what stops a second protocol table
existing. D6 is the one whose absence makes D5 useless.

**D1–D21 must each exist as named tests.**

D22 is a standing condition, never a task's sole acceptance criterion (R1).

---

## 3. The problem

S1 built the window sixteen slices early so its integration would never be a surprise. This is where it becomes
the product — and the risk is not that it fails to work. It is that it works while quietly discarding the most
expensive property the system has.

**Four independent designs in Aeon refuse to collapse an uncertain answer into a certain one:** cost
(`known`/`partial`/`unknown`), the `bash` receipt (ran / did not / **unknown**), blueprint validation (valid /
invalid / **could not check**), and now liveness (current / **stale** / disconnected). That is P31.

A UI is exactly where that gets thrown away, and it happens by accident: `unknown` renders as `—`, `—` becomes
`$0.00`, and nobody notices because nothing errors. Every one of those four slices paid a real cost to keep
uncertainty visible. **This slice either delivers that or wastes it.**

The second problem is structural. The web layer cannot speak a unix socket, so the Rust shell owns the connection.
The naive shape — one Tauri command per protocol method — produces a second copy of S2's closed method table,
written in Rust, that drifts. P25 said Aeon has *two* surfaces. It must not grow a third.

## 4. Goal

Everything in S6–S8, from the app: start a loop and watch it run, answer its question inline without knowing which
mechanism asked, read a step's transcript, see cost in three honest states, and be told when what you are looking
at is stale.

## 5. Shape and the seam

```
webview (React) ──▶ rpc(method, params) ──▶ Rust shell ──▶ socket ──▶ daemon
       ▲                                        │
       └──── aeon://event (one channel, carrying seq) ◀──┘
```

### The seams

**One bridge command** (U11, D3). The Rust side does not know the method list, so it cannot disagree with it.

**One event channel**, carrying the daemon's `seq`. The web layer filters and tracks the last `seq` it processed.
`seq` is the `event` table's rowid (S4), so a **filtered subscriber legitimately sees gaps** — which is why
reconnect **re-reads** (D7) rather than trusting continuity.

**Generated types** (U12, D4) are the seam between the protocol and the app. Hand-maintained types drift, and in a
UI drift shows as a blank panel rather than an error.

**The three-state renderers** (`lib/uncertain.tsx`) are one shared place, so a fifth uncertain value cannot be
flattened by someone who never read P31.

**No filesystem seam at all** (U16, D10). The app knows only what the bridge told it.

## 6. Locked decisions

**Inherited:** U2 (chat primary, board secondary), U3 (live from subscriptions only, no polling), U4 (questions
inline, never modal), U5 (approval only through the question primitive — a rule awaiting S16), U6 (three cost
renderings), U7 (capability check with browser fallback), U8 (never write to the store), U9 (no optimistic
answers).

**New:** U11 (one generic bridge), U12 (generated types), U13 (liveness rendered), U14 (step question and gate
indistinguishable), U15 (bundled assets only), U16 (no store access at all), U17 (an unknown is never a zero,
dash or absence), U18 (the thread is per loop), U19 (no optimistic rendering of anything).

**U1 is superseded** by S1: the Rust shell is the UI, not packaging around a served page.

## 7. Overrides to the design

| # | Design says | This brief requires |
| --- | --- | --- |
| O1 | Staleness shows how long since last confirmed | It must also **name what it is waiting on** — reconnecting, or given up — and show the socket path. "Stale" without a cause sends people to the wrong problem, and the path is the first thing anyone debugging this needs (S1's T5 established that for the disconnected state). |
| O2 | The bridge is one command | Its errors must be **discriminated**: a transport failure, a JSON-RPC error from the daemon, and a timeout are three different things and the UI must render them differently. A single "request failed" makes a daemon-side domain refusal look like a broken socket. |
| O3 | Types are generated | The generator is **checked in and runnable by hand**, not a hidden CI step. A contributor who cannot regenerate types locally will hand-edit the generated file, and the CI diff check will then read as an obstacle rather than a guard. |
| O4 | The thread holds messages, questions and step summaries | It must be **ordered by the daemon's sequence**, not by client-side timestamp. Two events in the same millisecond, or a clock skew between reads, reorders a thread — and a reordered thread makes a question look like it was answered before it was asked. |
| O5 | The design does not say what the app does with an event type it does not know | It **ignores it**, per S2's additive rule, and does not error. A client that breaks on a new event type makes every future slice a breaking change. Asserted with a synthetic unknown event. |
| O6 | The design does not say what happens when the daemon's protocol version is newer than the app's | The app **says so plainly and keeps working read-only** where it can, rather than refusing to start. A version mismatch after a partial upgrade is an ordinary state, and an app that will not open is a worse answer than one that says "some things may be missing". |
| O7 | Questions render inline with their options | The option set must come **from the question**, never from a hardcoded list. A gate's `answers` are declared in the blueprint (S8), and a hardcoded `[approve, reject]` would silently drop `revise`. |
| O8 | The demo is done from the app | Add a check that **no screen requires a horizontal scroll at 1024px wide**. This is the first slice with a real layout, and a ladder that overflows is one nobody reads. |

## 8. Non-goals

- **N1 — No diffs, no approval UI.** **S16.** U5 is a rule awaiting something to diff.
- **N2 — No permission prompts.** **S11.**
- **N3 — No agent picker, no tech-tag UI.** **S13.**
- **N4 — No blueprint builder.** Never promised for the alpha.
- **N5 — No HTTP client.** The app is a socket client; S19's narrow surface is for CI and MCP.
- **N6 — No retrieval panel.** S22.
- **N7 — No code signing or notarisation.** S23.
- **N8 — No remote or multi-machine daemon.** One per machine (P29).
- **N9 — No changes to the daemon's protocol** beyond what S2's additive rule already allows. If the app needs a
  method that does not exist, **that is a finding to raise**, not a reason to add one here.
- **N10 — No edits to `packages/loop-state-ui`.** It is read and ported from. The app lives at `aeon/ui/` (S1's B5).

### Standing non-goals

- **Never write outside `aeon/`**, except `docs/` and `docs/test_suites/`.
- **Never run `git clean`.**
- **Never point a test at the real store or the real socket.** A fake bridge, always.

## 9. Repo invariants

**I1 — One protocol method table, in Go.** The Rust shell and the web layer never enumerate methods (U11, D3).

**I2 — U10 is corrected, not followed.** Reuse pure helpers and component ideas only where S8's contract supports
them. The v1 `lifecycle.ts` and phase/task widgets describe a different schema, and its `platform/tauri.ts` grants
direct filesystem reads forbidden by U16/D10; these do not port as-is. `client.ts`, `connection.tsx`, and
`useLabels.tsx` fetch from the v1 HTTP API and do not port. The S9 product compositions use generated S8 types and
the generic bridge. `ui/PORT.md` records every decision and `test/port.test.ts` guards the boundary.

**I3 — The app has no filesystem access to the store** (U16, D10).

**I4 — No polling where a subscription exists** (U3, D12).

**I5 — No optimistic rendering** (U19, D9).

**I6 — Three-state values render through the shared renderers** (U17), so a fifth cannot be flattened by someone
who never read P31.

**I7 — Unknown event types are ignored, not errors** (O5).

**I8 — Public API changes ship with their tests in the same task.**

---

## 10. Investigation brief

### 10.1 What to read

1. **`docs/_archive/aeon-v1-horizontal/10-ui-chat/design.md`** — U1–U10 and the screens. Note U10's correction (I2).
2. **`packages/loop-state-ui/src/`** — what exists. **Read `api/client.ts` and `api/connection.tsx` to confirm
   they cannot port**, and the components to confirm they can. Do not edit this package (N10).
3. **`aeon/ui/`** (S1's T5) — the window, the three connection states, the one existing Tauri command, and how it
   resolves the socket path.
4. **`internal/protocol/`** (S2, S6, S7, S8) — the method table, the event types, and the **conformance fixtures**
   the generator should read.
5. **`../ARCHITECTURE.md` §8 P25, P31** — two surfaces, and the three-state rule.
6. **S8's `loop show`** (T11) — the app's ladder must render what that prints, including conditions on bypassed
   lanes.

### 10.2 The patterns to copy

- **S2's conformance fixtures** as the generator's input, so the app and daemon read one contract.
- **v1's `platform/*`** — inspect its filesystem permissions before reuse; S9's browser fallback is the injectable bridge.
- **S7's structured `RecoveryFailure`** and S8's `BlockedReason` — both are structured so the app can render kinds
  distinctly. Use the structure; do not parse the prose.
- **S1's three connection states**, extended rather than replaced.

### 10.3 Confirm each from the code

| # | Question | Where |
| --- | --- | --- |
| Q1 | Does S2's conformance fixture set describe **every** method and event, or only some? | `internal/protocol/testdata/conformance/`. **If it is partial, the generator's coverage is partial and that must be stated**, not silently gapped |
| Q2 | How does S1's existing Tauri command resolve the socket path, and does it handle Windows named pipes? | `aeon/ui/src-tauri/` |
| Q3 | Does the daemon emit a `seq` on every event, and is it monotonic across **filtered** subscriptions? | `internal/protocol/` and S4's `seq` decision. D7 depends on the answer |
| Q4 | Does S2 expose a protocol version the app can compare against (O6)? | `internal/protocol/`. **If not, that is a finding** — do not invent a handshake |
| Q5 | What exactly does `useLabels` fetch, and is it needed at all, or was it a v1 API concern? | `packages/loop-state-ui/src/api/useLabels.tsx`. If the labels are static UI strings, the re-point is a deletion |
| Q6 | Which of v1's component tests are transport-free and will pass unchanged? | `packages/loop-state-ui/src/components/*.test.tsx`. Enumerate them; that list is D20 |
| Q7 | Does `make dev` already start the app such that a Vite HMR change is picked up, or is a rebuild needed? | `Makefile` (S1's T6). Seventeen slices of contributors will feel the answer |

### 10.4 Project-specific questions

| # | Question |
| --- | --- |
| Q8 | What is the staleness threshold, and how is it distinguished from silence (D5, D6)? State the mechanism, not the number — a heartbeat, a request round trip, or a socket-level signal. |
| Q9 | What does the generator produce — types only, or typed method signatures? State it, and how a method removed from the daemon fails the app's build rather than at runtime. |
| Q10 | How is the thread ordered (O4), and what happens to two events with the same `seq`? |
| Q11 | Which of the six screens can be built independently, and which share state? Name the shared state and where it lives. |

---

## 11. Behavioural contract

- One bridge command; no method names in Rust or the web layer (D3, I1).
- One event channel carrying `seq`; unknown event types ignored (O5, I7).
- Types generated from S2's contract, checked in, CI-diff-verified, hand-runnable (D4, O3).
- Live from subscriptions only; no polling (D12, I4).
- Liveness: stale shows an age, a cause and the socket path; **silence with a live socket stays current**; reconnect
  **re-reads** (D5, D6, D7, O1).
- Cost, validation, recovery and liveness each render three states through the shared renderers; an unknown never
  renders as zero, dash or absence (D1, D2, D14, D15, I6).
- A step question and a gate render identically; options come from the question (D8, O7).
- No optimistic rendering; a refused answer changes nothing (D9, I5).
- The thread is per loop, ordered by the daemon's sequence (U18, O4).
- No filesystem access to the store; bundled assets only; locked CSP (D10, D11, I3).
- Bridge errors are discriminated: transport, domain, timeout (O2).
- A newer daemon protocol version is stated plainly and the app keeps working where it can (O6).
- Every screen has a real empty state and a real error state (D19).
- No screen scrolls horizontally at 1024px (O8).

---

## 12. How to decompose

- **R1** — every task is gated by at least one **named test**.
- **R2** — a task that adds behaviour adds its test in the same task.
- **R3** — a file in more than one task's `files_expected` needs a stated reason.
- **R4** — **the bridge, the generator, and the liveness model are three separate tasks**, and all three precede
  any screen. A screen built before them is a screen built against a mock that will not match.
- **R5** — **the three-state renderers are their own task**, and they come before the screens that use them. They
  are the slice's thesis and every screen consumes them.
- **R6** — **the port is its own task** (I2), and it enumerates what came over and what was left behind. A port
  spread across screen tasks is a port nobody can review.
- **R7** — one task per screen, at most. Six screens is not six tasks if two are thin — **`Blueprints` and
  `System` may share one**.
- **R8** — no task over roughly 300 changed lines or 12 files without justification. **A screen task may exceed the
  file count** if it is one screen's components; say so.
- **R9** — task files contain no deliberation: no "hmm", "let me", "on reflection", "I think", "not sure",
  "FIXME". `TODO(engineer):` in a code skeleton is the one permitted form.
- **R10** — a task carries no open question.
- **R11** — the final task walks `DEMO.md` and amends the QA suites.
- **R12** — **no task edits `packages/loop-state-ui`** (N10) or `docs/_archive/aeon-v1-horizontal/10-ui-chat` (I2, already landed).

## 13. Pre-dispatch self-check

### A. Mechanical

| # | Run | Pass condition |
| --- | --- | --- |
| A1 | `grep -rniE "\bhmm\b\|\blet me\b\|\bon reflection\b\|\bFIXME\b\|\bI think\b\|\bnot sure\b" tasks/` then `grep -rn "TODO" tasks/ \| grep -v "TODO(engineer)"` | Both empty (R9, R10) |
| A2 | List every task with `est_changed_lines` and file count | Nothing over ~300 lines without justification (R8) |
| A3 | List every task with the **test name** that gates it | Every task has one (R1) |
| A4 | List every file in more than one task's `files_expected` | Each justified (R3) |
| A5 | Paste Q1's answer | **If S2's conformance set is partial, state which methods and events the generator cannot cover** — a silent gap here is a blank panel later |
| A6 | Paste Q3's and Q8's answers | `seq` monotonicity under filtering, and the staleness **mechanism**. D6 depends on both |
| A7 | Paste Q6's enumeration of v1's transport-free component tests | That list **is** D20 |
| A8 | Paste Q4's answer | **If S2 exposes no protocol version, say so and raise it** — do not invent a handshake (N9) |
| A9 | Enumerate the four three-state values and name the renderer each uses | All four through the shared renderers (I6) |
| A10 | `make fmt` then `git status --porcelain` | No output |

### B. Scope

| # | Check | Ref |
| --- | --- | --- |
| B1 | Confirm no task builds a diff view or an approval flow | N1 |
| B2 | Confirm no task builds a permission prompt | N2 |
| B3 | Confirm no task builds an agent picker | N3 |
| B4 | Confirm no task builds a blueprint editor or builder | N4 |
| B5 | Confirm no task adds an HTTP client | N5 |
| B6 | Confirm no task adds a protocol method | N9 |
| B7 | Confirm no task edits `packages/loop-state-ui` or `docs/_archive/aeon-v1-horizontal/` | N10, R12 |
| B8 | Confirm no task touches signing or notarisation | N7 |
| B9 | Quote the `out_of_scope` lines covering N1–N10 | §8 |

### C. Acceptance

| # | Check | Ref |
| --- | --- | --- |
| C1 | Map each of D1–D21 to a task and a named test | §2 |
| C2 | For D1, confirm the assertion is on **rendered output** and includes the literal `$0.00` check | D1 |
| C3 | For D3, name the grep and its source of truth — S2's method table, not a hand-written list | D3 |
| C4 | For D6, confirm it is a **separate test** from D5, not an assertion inside it | D5, D6 |
| C5 | For D7, confirm the test asserts a **re-read**, not merely that the UI recovered | D7 |
| C6 | For D8, confirm the assertion compares **rendered output**, not props | D8 |
| C7 | For D9, confirm the bridge fake **rejects**, and the thread is asserted unchanged | D9 |
| C8 | For D10, confirm the assertion covers **both** the web layer and the Rust shell | D10 |
| C9 | For D20, confirm the ported tests run in CI, not just locally | D20 |

### D. Ordering

| # | Check | Ref |
| --- | --- | --- |
| D1 | Confirm the bridge, generator and liveness tasks all precede every screen | R4 |
| D2 | Confirm the three-state renderers precede the screens | R5 |
| D3 | Confirm the port is one task and enumerates what was left behind | R6 |
| D4 | Confirm no screen task depends on another screen task | R7 |

### E. Structure

| # | Check | Ref |
| --- | --- | --- |
| E1 | Confirm no protocol method name appears in Rust or in hand-written TypeScript | I1, D3 |
| E2 | Confirm the generated directory is marked generated and excluded from hand editing | U12, O3 |
| E3 | Confirm no interval timer drives a data read | I4, D12 |
| E4 | Confirm every three-state value goes through the shared renderers | I6 |
| E5 | Confirm unknown event types are ignored | I7, O5 |

### F. Project-specific

| # | Check |
| --- | --- |
| F1 | State the staleness mechanism and its threshold, and how silence is distinguished from it (Q8, D5, D6). |
| F2 | State what the generator emits and how a removed daemon method fails the **build** rather than at runtime (Q9). |
| F3 | State the thread's ordering rule and the same-`seq` tie-break (Q10, O4). |
| F4 | State which screens share state and where it lives (Q11). |
| F5 | State whether `useLabels` survives the port or is deleted (Q5). |
| F6 | State whether `make dev` picks up a web change without a rebuild (Q7). |

## 14. The demo contract

> **Present in every slice of this plan.** A slice is not done when its tests pass. It is done when someone who
> did not build it can follow `DEMO.md` and watch the thing work.

[`DEMO.md`](DEMO.md) is a deliverable, and the final task (R11) walks it and fixes whatever does not match.

**Nothing in it may require a terminal** (D21), except the deliberate `pkill` and `make dev`. This is the slice
where the CLI stops being the only way in, and a demo step that reaches for a shell is the finding.

**§4 is the section that matters most, and it has three parts for a reason.** Part one kills the daemon and the UI
must go honest. Part two lets a loop sit at a gate for five minutes and the UI must stay **current** — because
silence is not staleness, and a UI that cries wolf makes part one's warning worthless. Part three reconnects and
must **re-read** rather than trusting a `seq` gap it cannot bound.

**§5 is the thesis.** If an unknown cost renders as `$0.00`, four slices of design work were wasted at the last
mile, and the demo is the only place that gets caught.

**§8 checks the app never reaches around the daemon** — no store file open, ever. It looks like paranoia and it is
the invariant that keeps the store from becoming an accidental public contract.
