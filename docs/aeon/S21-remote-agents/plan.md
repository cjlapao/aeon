# Plan — S21 Remote agents

> **Source material:** [`design.md`](design.md) · [`../S14-agent-services/design.md`](../S14-agent-services/design.md)
> for everything above the transport · S19's action table · S18's `RouteAccess`.

**Implements:** [`design.md`](design.md) · **Demo:** [`DEMO.md`](DEMO.md)
**Stage:** 4 · Space shuttle, fourth of six. Depends on **S14**, **S15**, **S19**, **S10**.

---

## What is settled before planning

**This is a transport, not a redesign.** S14 adopted A2A rather than inventing a shape, so the card, the
skill, the Task and the chain are **reused unchanged**. If a task here edits one of S14's files, something
has been duplicated.

**The policies exist.** `RouteAccess` (S18) for egress, `SkillAccess` (S10) for who may invoke what. **No
new policy type.**

**The surface exists.** The inbound callback is an enumerated projection on S19's table. **No listener
task.**

**Suspension exists.** An async remote Task is S6's suspension (AS4), which is why RA7 forbids a held
connection.

**So the plan is: identity, transport, egress, the inbound direction, and cost honesty.**

## How we will do it

**The peer and the trust store first** (T1, T2), because every later task dials through them and a dial
built first will acquire a "skip verification for now" path that never leaves.

**Then the transport** (T3) — TLS only, deadlines, degradation.

**Then egress** (T4), and it is placed deliberately: **before the request is built.** T4 comes after the
transport exists so the test can assert the *order* with a spy on the assembler, which is the only way to
catch a check that happens at the socket.

**Then the untrusted rule** (T5), **the inbound callback** (T6), **the chain** (T7), **cost** (T8), the
walk (T9).

**The ordering that matters:** **T2 before T3.** A trust store retro-fitted onto a working dial is a trust
store with a bypass — because the dial worked before it existed, and the path that made it work is still
there.

## Open questions this plan answers

| # | Question | How |
| --- | --- | --- |
| Q1 | Certificate, public key, or SPKI pin? | T2, **with the rotation story written down** |
| Q2 | A CA in the trust store, or leaf pins only? | T2 |
| Q3 | The deadline: per-call or per-Task? | T3 |
| Q4 | **Does a peer's internal fan-out count toward AS7's depth?** | T7 |

---

## Task sequence

| # | Task | Gate |
| --- | --- | --- |
| **T1** | `peer.go` — a configured peer, off by default | Nothing is discovered; a fresh install reaches nothing |
| **T2** | `trust.go` — pinning, and the rotation story | An unpinned valid certificate is refused |
| **T3** | `dial.go` — TLS only, deadlines, degradation | No plaintext mode; a hung peer degrades the step |
| **T4** | `egress.go` — `RouteAccess` before the request is built | Asserted on the order, with a spy |
| **T5** | Remote results are untrusted, always | No first-party path exists |
| **T6** | `callback.go` — verified **and bound** | An unsolicited completion is refused |
| **T7** | The chain through a remote peer | Depth counts our hops; **Q4 stated** |
| **T8** | `cost.go` — unknown stays unknown | Never zero; the parent becomes less certain |
| **T9** | Walk the demo, write the QA suite | `DEMO.md` runs as written |

### T1 — `peer.go`: a configured peer, off by default
- engineer: golang-engineer-loop
- tech: go
- depends_on: []
- est_changed_lines: ~240
- detail: tasks/T1.md
- success_criteria:
  - a peer carries an address, a **pinned identity** and a key reference
  - **unit test `nothing is discovered` passes**, by grep — no well-known crawl, no mDNS, no registry
  - unit test `a fresh install reaches nothing` passes
  - unit test `a peer without a pin is refused at configuration time` passes
  - the key is read from the environment or a path, never argv (C17)
- files_expected: `aeon/internal/remote/peer.go`, `aeon/internal/remote/peer_test.go`

### T2 — `trust.go`: pinning, and the rotation story
- engineer: golang-engineer-loop
- tech: go
- depends_on: [T1]
- est_changed_lines: ~280
- detail: tasks/T2.md
- success_criteria:
  - **unit test `an unpinned certificate is refused` passes** — a valid cert from a real CA for the right host
  - **unit test `a rotated key is refused, saying the pin no longer matches` passes**
  - unit test `the system CA set alone is not sufficient` passes
  - **Q1 and Q2 answered, with the rotation story written down**
- files_expected: `aeon/internal/remote/trust.go`, `aeon/internal/remote/trust_test.go`, `aeon/internal/remote/testdata/certs/**`

### T3 — `dial.go`: TLS only, deadlines, degradation
- engineer: golang-engineer-loop
- tech: go
- depends_on: [T2]
- est_changed_lines: ~280
- detail: tasks/T3.md
- success_criteria:
  - **unit test `no plaintext mode exists` passes**, by grep — no flag, no scheme, no localhost exception
  - **unit test `a hung peer degrades the step` passes**
  - **unit test `a deadline is distinguishable from an empty result` passes**
  - unit test `a failed call is not retried` passes, with a call count
  - Q3 answered: the deadline, per-call and per-Task
- files_expected: `aeon/internal/remote/dial.go`, `aeon/internal/remote/dial_test.go`

### T4 — `egress.go`: `RouteAccess` before the request is built
- engineer: golang-engineer-loop
- tech: go
- depends_on: [T3]
- est_changed_lines: ~240
- detail: tasks/T4.md
- success_criteria:
  - **unit test `RouteAccess is evaluated before the request is assembled` passes**, with a spy on the assembler
  - unit test `a denied egress reads no files` passes
  - unit test `the refusal carries S10's sentence` passes
  - **S18's `RouteAccess` gains an agent-egress subject**, with no new policy type
- files_expected: `aeon/internal/remote/egress.go`, `aeon/internal/remote/egress_test.go`, `aeon/internal/policy/spec_routeaccess.go`

### T5 — Remote results are untrusted, always
- engineer: golang-engineer-loop
- tech: go
- depends_on: [T3]
- est_changed_lines: ~180
- detail: tasks/T5.md
- success_criteria:
  - **unit test `a remote result is labelled untrusted` passes**, through S14's mechanism
  - **unit test `no first-party path exists for a remote peer` passes**, by grep
  - unit test `a peer claiming first-party is ignored` passes
- files_expected: `aeon/internal/remote/dial.go`, `aeon/internal/remote/untrusted_test.go`

### T6 — `callback.go`: verified and bound
- engineer: golang-engineer-loop
- tech: go
- depends_on: [T3]
- est_changed_lines: ~280
- detail: tasks/T6.md
- success_criteria:
  - **unit test `an unsolicited completion is refused` passes** — for a Task we never created
  - **unit test `a completion carrying another Task's id is refused` passes** — bound, not merely authenticated
  - unit test `a replayed completion is refused` passes
  - **unit test `no second listener is opened` passes** — S19's mounting
  - unit test `an async remote Task suspends with no held connection` passes
- files_expected: `aeon/internal/remote/callback.go`, `aeon/internal/remote/callback_test.go`, `aeon/internal/narrow/actions.go`

### T7 — The chain through a remote peer
- engineer: golang-engineer-loop
- tech: go
- depends_on: [T6]
- est_changed_lines: ~200
- detail: tasks/T7.md
- success_criteria:
  - a remote invocation records a chain edge, as a local one does
  - unit test `AS7's depth counts our hops` passes
  - unit test `a cycle through a remote peer is refused if observable` passes
  - **Q4 answered and stated**: the peer's internal fan-out is not our graph
- files_expected: `aeon/internal/remote/dial.go`, `aeon/internal/remote/chain_test.go`

### T8 — `cost.go`: unknown stays unknown
- engineer: golang-engineer-loop
- tech: go
- depends_on: [T7]
- est_changed_lines: ~200
- detail: tasks/T8.md
- success_criteria:
  - **unit test `a remote cost with no report is unknown, not zero` passes**
  - **unit test `an unknown remote child makes the parent not known` passes** (P32)
  - unit test `a reported remote cost is recorded with its provenance` passes
  - unit test `unknown never renders as a number` passes
- files_expected: `aeon/internal/remote/cost.go`, `aeon/internal/remote/cost_test.go`

### T9 — Walk the demo, write the QA suite
- engineer: technical-writer-loop
- tech: docs
- depends_on: [T8]
- est_changed_lines: ~220
- detail: tasks/T9.md
- success_criteria:
  - `DEMO.md` runs end to end against a fake peer; anything undocumented is added
  - **the pinning and callback-binding steps are confirmed to exercise their property**
  - `docs/test_suites/aeon-remote.md` exists, status `draft`, ids `QARA-01` upward
  - the suite covers pinning, rotation, egress ordering, callback binding and cost honesty
- files_expected: `docs/aeon/S21-remote-agents/DEMO.md`, `docs/test_suites/aeon-remote.md`

---

## Files

| File | Task | Notes |
| --- | --- | --- |
| `internal/remote/peer.go` | T1 | — |
| `internal/remote/trust.go` | T2 | — |
| `internal/remote/dial.go` | T3, T5, T7 | **three tasks; T3 first** |
| `internal/remote/egress.go` | T4 | — |
| `internal/remote/callback.go` | T6 | — |
| `internal/remote/cost.go` | T8 | — |
| `internal/policy/spec_routeaccess.go` | T4 | the agent-egress subject |
| `internal/narrow/actions.go` | T6 | the mounting, **not a new surface** |

**No file in `internal/agentsvc/` appears here.** That is the slice's own check on itself: everything above
the transport is S14's, and D14 asserts it by grep.

## Ordering rationale

**T2 before T3**, and this is the one to get right. **A trust store retro-fitted onto a working dial is a
trust store with a bypass** — the dial worked before verification existed, and the code path that made it
work does not disappear on its own.

**T4 after T3, and it tests an order rather than a behaviour.** The check belongs **before the request is
assembled**, not before it is sent: a request built and discarded has already read the files it was going
to carry, and in a long-running daemon that assembly may be logged. **A spy on the assembler is the only
way to catch the difference**, because both placements produce the same refusal.

**T6 is the slice's sharpest security task.** Verification feels sufficient and is not: a callback that is
authenticated but **unbound** lets anybody who can reach the endpoint inject a skill result into a
transcript. That is a fabricated answer, which is what S19's N6 refuses for questions.

**T8 last of the code tasks** because it needs a chain with a remote edge to be worth testing — and because
`unknown` reappearing as `$0.00` is the failure the whole cost design exists to prevent, so it should be
asserted against the finished thing.

## What this slice does not do

No service mesh. **No inbound skill serving** — Aeon *being* a remote agent is publishing a card over HTTP,
which is S19's surface and a different threat model. No federation. No CA. **No retry** — a retry doubles
the wait and the model cannot tell one outage from two.
