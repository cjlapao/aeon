# Brief — S19 The narrow surface

> ## Q3 and Q5 are answered — by framework v0.10.1, and the scope shrinks
>
> **Q3: "Does the daemon already have an HTTP listener from the scaffolding, or is this the first?"**
> **It does, as of framework v0.10.1.** `framework/api` is a registerable HTTP server. Q3 already said
> *"if the scaffolding's is available, use it rather than a second listener"* — so that is the answer, and
> **this slice no longer builds a server.**
>
> At v0.10.0 the opposite was true and was verified: `framework/core/api` was response/error/code helpers
> only, and the real server was the scaffolding's `internal/api`, unreachable across the module boundary.
> The upgrade changed the answer, not the question.
>
> **Already done, outside the loop:** `api` is **registered** in `aeon/internal/bootstrap/register.go` and
> **pinned off** via `system.ForceOff("api")` in `bootstrap.Pins()`. The framework defaults it to
> `Enabled:true` on `:8080`, so registering it alone would open a port nobody asked for.
>
> **`ForceOff` is stronger than N15 asked for:** configuration cannot turn it on *at all*, and the
> orchestrator will not auto-enable it to satisfy another service's dependency. Two tests assert it, and
> `TestAPIIsPinnedOff` failing means the daemon is listening.
>
> **This slice now owns:** removing that pin, and the three things that make binding deliberate — an
> explicit bind address, a required key, and the enumerated action table. Not a listener.
>
> **Q5: "What does the framework provide for auth, and does it fit one key one user?"**
> **It provides nothing usable, and that is fine.** `api_keys`, `role` and `claim` remain
> scaffolding-internal and do not cross the module boundary. They are built for multi-tenant claims, which
> is not this. `api.New(svc).WithTokenValidator(...)` is the seam: **supply the one-key-one-user check
> here** rather than registering a service built for a different problem.
>
> **Still true, and unchanged:** the HTTP surface is an enumerated projection, never a mirror of the socket
> (P25) · `trigger` is one-way · no operational control on this surface (R6, N14) · nothing about policy
> authoring appears here.


**Implements:** [`design.md`](design.md) · **Demo:** [`DEMO.md`](DEMO.md)
**Inherits:** N1, N5–N8, N12 from [`docs/_archive/aeon-v1-horizontal/12-mcp-adapters`](../../_archive/aeon-v1-horizontal/12-mcp-adapters/design.md) and R6 from
`09-server`. New decisions begin at **N13**.
**Stage:** 4 · Space shuttle, second of four. Depends on S2, S8, S11, S16. **Independent of S18, S22, S23.**
**Checked 2026-10-26 against P34** (`ARCHITECTURE.md`) and found already compliant, not amended: this
slice derives its whole surface from S2's existing socket methods (N13, "derived, not parallel") and N8
*deliberately* forbids adding any new socket method — the opposite risk from S18/S22/S25/S30's gaps. Its one
CLI verb (`aeon narrow list`) shows a static, compiled-in action table, the same category as `aeon version`,
not stateful config the app would need to read or write.

> **Template deviations from `proposal-brief-protocol`, stated rather than silent:**
> **(a)** Implements a design, not a proposal — §7 records where this brief overrides it.
> **(b)** §14 is the demo contract, present in every slice of this plan.
> **(c)** This slice's decisions are mostly about **what it refuses to build**. §8's non-goals carry more weight than
> usual, and two of them are permanent rather than deferred.

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
| **D1** | **No operational control exists**, asserted as an **enumeration over the whole table** — nothing matching stop, kill, pause, resume, retry or answer. Not a check for specific names: the risk is an action added later that nobody thought to forbid (N5, R6). |
| **D2** | **The boundary is legible.** A client listing the surface finds the stated absence **and its reason** — not an unknown-tool error (N14). |
| **D3** | **Every action's `SocketMethod` exists in S2's method table**, asserted against the table rather than a hand-written list (N13). |
| **D4** | **stdio and HTTP expose the same set**, asserted by **set equality** (N18). |
| **D5** | **Every entry states what it cannot do**; an empty `Cannot` **fails** (N12). |
| **D6** | **Questions are readable and unanswerable**, asserted over the whole table (N16). And **no action creates one** (N6). |
| **D7** | **HTTP binds no port without an explicit address**, and an address **without a key refuses to start** (N15). |
| **D8** | **A keyed caller cannot answer a gate**, and the refusal says a gate needs a person (N16). |
| **D9** | **CI is read-mostly by construction.** A triggered loop is unattended, so `outward` and `irreversible` actions deny — asserted **end to end**, not by reading policy (S16's L22, G7). |
| **D10** | **`allowOutward` at start is the whole opt-in**, and it is per-loop rather than per-key or per-caller. Amended 2026-10-30 by S56: schedule, webhook, event and scheduler starts may be pinned from a person-authored per-root autonomy policy (`loop_autonomy_pin`); `loop.allow_outward` stays 0 for them; the narrow surface is unchanged. See S56-autonomy-policy/design.md AL7/AL8. |
| **D11** | **A slow or failing narrow caller does not delay a running loop** (N8). |
| **D12** | **The product works with neither mounting enabled** (N1). |
| **D13** | **No MCP client exists** (N17), and S13's stale pointers are corrected — that correction is already landed. |
| **D14** | **`make test` and `make lint` green**, `make fmt` leaves no diff. |

**D1, D2 and D9 are the three that matter.** D1 because the enumeration is what survives someone adding an action in a
year. D2 because absence without legibility sends people hunting for a version that does not exist. D9 because it proves
the property holds by composition rather than by a list somebody maintains.

**D1–D13 must each exist as named tests.**

D14 is a standing condition, never a task's sole acceptance criterion (R1).

---

## 3. The problem

P25 settled the shape three stages ago: the socket carries the full protocol, HTTP carries a **narrow enumerated
projection** shared with MCP-stdio. This slice builds the narrow half.

**R6 is why it is narrow**, and the wording has not needed changing since v1's `control.ts`: *"handing an agent the means
to start loops is handing it a way around every bound placed on one."*

**Three things make it more interesting than a projection.**

**Absence versus refusal.** A `kill` tool that refuses is discoverable, which is what R6 forbids. A `kill` tool that does
not exist produces *"unknown tool"*, which reads like a version mismatch. Neither alone is right, so the capability is
absent **and** the boundary is documented in the surface (N14).

**What a key is not.** The socket's access control is filesystem permissions (P26), which do not project onto a port. A key
authenticates a caller; it does not make the caller a human. Which settles whether the surface can answer a gate: **no**
(N16) — and S11 already took the consistent position by denying an unattended `ask` rather than auto-answering it.

**What CI actually gets.** An unattended loop, so S16's effect classes deny `outward` and `irreversible`. The narrow surface
is **read-mostly by construction**, which is stronger than a permission list because nothing has to be configured correctly.

## 4. Goal

One enumerated action table, mounted on stdio and HTTP, that lets Claude Code drive a loop and CI trigger one — and that
cannot start, stop, kill, or answer anything.

## 5. Shape and the seam

```
Claude Code ──stdio──┐
                     ├──▶ ONE action table ──▶ socket ──▶ daemon
CI ──HTTP + key──────┘         │
                          each entry names its SocketMethod (N13)
                          each entry states its Cannot (N12)
                          nothing matches stop|kill|pause|answer (D1)
```

### The seams

**One table, two mountings** (N18). An action on HTTP but not stdio is a third surface, and P25 says there are two.

**Derived, not parallel** (N13). Each action names the socket method it projects and a test asserts existence. Third time
this reasoning applies: S9's generated types, S18's single policy extractor, this.

**The key is at the mounting, not in the table.** Authentication is HTTP's problem; the action set is identical either way,
so a keyed caller and an editor get the same capabilities and neither gets more.

**Effect classes are not this slice's** (S16). The narrow surface does not decide what a loop may do outwardly — it
triggers a loop, and the loop's attendedness decides. That is why D9 holds by composition.

## 6. Locked decisions

**Inherited:** N1 (both mountings optional), N5 (read plus a narrow action set, never operational control), N6 (cannot
create a question), N7 (two transports, one adapter), N8 (degrade, never block), N12 (enumerated in one place, each entry
stating what it cannot do), R6 (nothing here becomes an MCP tool for control).

**New:** N13 (derived from the socket protocol), N14 (absent **and** legible), N15 (HTTP off by default; address plus key),
N16 (read questions, never answer), N17 (no MCP client in the alpha), N18 (no mounting-specific action).

## 7. Overrides to the design

| # | Design says | This brief requires |
| --- | --- | --- |
| O1 | An address without a key refuses to start | The key must be supplied **out of band** — a file path or an environment variable, **never a command-line argument**. A key on a command line is in the process list and in shell history, and the failure is silent and permanent. |
| O2 | `allowOutward` is the opt-in | It is **per loop, not per key**. A key that permanently grants outward actions is a key whose theft is unbounded; a per-loop flag makes each grant a decision someone made at a moment. Amended 2026-10-30 by S56: a policy-pinned loop gains its authority from the pin, never from the key — no standing grant enters the narrow surface (S56 T7 `TestNoStandingGrantInTheNarrowSurface`). See S56-autonomy-policy/design.md AL7/AL8. |
| O3 | The boundary is documented in the surface | The document must name **what to use instead** — the CLI command, or the app. A boundary that says only "no" leaves the caller stuck; one that says "no, use this" is a redirection. |
| O4 | Read actions are read-only | They must also be **scoped to this daemon's own loops**. There is one user (§8 N5), so this is not a tenancy check — it is a guard against a read action that takes an id and returns whatever it finds, which is the shape that becomes a tenancy bug the day multi-user arrives. |
| O5 | The design does not say what an HTTP error looks like | Errors are **discriminated the way S16's forge errors are**: transport, auth, not-found, refused. A caller polling a loop needs to tell "your key is wrong" from "that loop does not exist" from "the daemon is restarting". |
| O6 | The surface is request/response | A read action must be **cheap enough to poll**. CI will poll; a status read that costs a full transcript scan makes a two-second interval expensive. State the cost of each read action. |
| O7 | The table is enumerated in one place | Adding an entry must require **editing that one file and its test fixture**. If an action can be registered from elsewhere, N12's guarantee is decoration — the same reasoning S16's O2 applied to the closed action registry. |
| O8 | HTTP is off by default | When it **is** on, the daemon must say so **at startup and in `aeon doctor`** — the address and that a key is set (never the key). A listening port nobody remembers opening is the thing N15 exists to prevent, and defaults only help the first time. |

## 8. Non-goals

- **N1 — No MCP client**, in the alpha (N17). Aeon exposes itself; it does not consume third-party servers. S13's
  `DEFERRED` records name the demand.
- **N2 — No operational control, ever.** **Permanent, not deferred.** Not behind a flag, not with a stronger key, not for
  a trusted caller. R6 is a security position, not a roadmap item.
- **N3 — No answering or creating questions** (N16, N6). **Also permanent.**
- **N4 — No streaming over HTTP.** The socket carries streaming; a second path would need its own filtering, cursor and
  teardown (S3's M23).
- **N5 — No multi-tenant auth.** One key, one daemon, one user.
- **N6 — No remote daemon discovery.** Configured, never discovered (N11's rule, applied here).
- **N7 — No effect-class decisions.** S16 owns those; this slice triggers a loop and the loop's attendedness decides.
- **N8 — No new socket methods.** If a narrow action needs one that does not exist, **that is a finding** about S2, not
  something to add here.

### Standing non-goals

- **Never accept a key on a command line** (O1).
- **Never log a key**, in any error path. Redact to `<redacted>`.
- **Never write outside `aeon/`**, except `docs/` and `docs/test_suites/`.
- **Never run `git clean`.**

## 9. Repo invariants

**I1 — No action matches operational control**, asserted by enumeration over the table (D1).

**I2 — The table is the only registration point** (O7). Nothing registers an action from elsewhere.

**I3 — Every entry names an existing socket method** (N13, D3).

**I4 — stdio and HTTP sets are equal** (N18, D4).

**I5 — No action answers or creates a question** (N16, N6, D6).

**I6 — A key never reaches a command line, a log, or an error message** (O1).

**I7 — Read actions are scoped to this daemon's loops** (O4).

**I8 — No new socket method is added** (N8).

**I9 — Public API changes ship with their tests in the same task.**

---

## 10. Investigation brief

### 10.1 What to read

1. **`docs/_archive/aeon-v1-horizontal/12-mcp-adapters/design.md`** — N1–N12. **Note which are the client's** (N2, N3, N4, N9, N10, N11) and
   therefore out of scope here (N17).
2. **`docs/_archive/aeon-v1-horizontal/09-server/design.md`** — R6, and the `control.ts` reasoning it inherits.
3. **`../ARCHITECTURE.md` §4 and P25, P26** — the two-surface table, and why the socket's access control does not project.
4. **`internal/protocol/`** (S2) — the method table this projects from, and its conformance fixtures.
5. **`internal/loop/attended.go`** (S11's T6) — attendedness, which is what makes D9 hold.
6. **`internal/action/class.go`** (S16's T2) — effect classes, which this slice does not touch and depends on.
7. **The MCP specification's stdio transport** — enough to mount correctly; this is a server, not a client.

### 10.2 The patterns to copy

- **S16's closed registry with an asserted closedness** (its O2) — the same shape N12 needs.
- **S16's discriminated forge errors** as the model for O5.
- **S9's derived-from-the-contract approach** for N13.
- **S11's `AllowToken`** as the example of making a boundary structural rather than conventional.

### 10.3 Confirm each from the code

| # | Question | Where |
| --- | --- | --- |
| Q1 | Does S2's method table expose enough for every read action, or would one need a new method? | `internal/protocol/`. **If a new method is needed, that is a finding about S2** (N8) — record it, do not add it |
| Q2 | How does S2's method table represent itself — can a test enumerate it? | `internal/protocol/`. D3 asserts against it |
| Q3 | Does the daemon already have an HTTP listener from the scaffolding, or is this the first? | `internal/daemon/`, and S1's B8 (service removal). **If the scaffolding's is available, use it rather than a second one** |
| Q4 | How does `loop.start` currently take its attendedness, and can a caller set it? | `internal/loop/attended.go`, `cmd/aeon/loop.go`. D9 and O2 depend on it |
| Q5 | What does the scaffolding provide for auth, and does it fit one key one user? | The framework's services. **If it provides more than needed, S1's removal discipline applies** |
| Q6 | How does `aeon doctor` report configuration today? | O8 needs a line there |
| Q7 | What is the cost of each candidate read action — does any require a full transcript scan? | `internal/store/`. O6 requires stating it |

### 10.4 Project-specific questions

| # | Question |
| --- | --- |
| Q8 | What is the full action table — name, socket method, kind, and `Cannot`? Produce it before planning. |
| Q9 | Where does the key come from (O1), and what happens when the file or variable is missing? |
| Q10 | What exactly does the boundary document say, and what does it point at instead (O3)? |
| Q11 | How is a read action scoped to this daemon's loops (O4)? |

---

## 11. Behavioural contract

- One table; stdio and HTTP sets equal; no mounting-specific action (D4, N18).
- Every entry names an existing socket method (D3) and states what it cannot do (D5).
- Nothing matches operational control, asserted by enumeration (D1).
- The boundary is legible and names what to use instead (D2, O3).
- Questions readable, never answerable, never creatable (D6).
- HTTP binds only with an explicit address **and** a key supplied out of band (D7, O1).
- A keyed caller cannot answer a gate (D8).
- A triggered loop is unattended; outward actions deny (D9).
- `allowOutward` is per loop (D10, O2).
- Read actions are scoped to this daemon's loops (O4) and cheap enough to poll (O6).
- Errors are discriminated: transport, auth, not-found, refused (O5).
- A slow caller never delays a loop (D11).
- Both mountings optional (D12).
- When HTTP is on, startup and `doctor` say so (O8).
- No new socket method; a gap is a finding (N8).

---

## 12. How to decompose

- **R1** — every task is gated by at least one **named test**.
- **R2** — a task that adds behaviour adds its test in the same task.
- **R3** — a file in more than one task's `files_expected` needs a stated reason.
- **R4** — **the table comes first**, with its enumeration tests. Both mountings project it, and building a mounting first
  means the table is shaped by whichever came first.
- **R5** — **the boundary document is part of the table's task**, not a later addition. N14 is absence *plus* legibility,
  and a legibility bolted on afterwards is one nobody wrote a test for.
- **R6** — **the two mountings are separate tasks**, and the set-equality test lives with the second. It cannot be written
  until both exist.
- **R7** — **auth is its own task**, because O1's out-of-band requirement and I6's never-logged requirement are both easy
  to get wrong and neither is about the action set.
- **R8** — no task over roughly 300 changed lines or 12 files without justification.
- **R9** — task files contain no deliberation: no "hmm", "let me", "on reflection", "I think", "not sure",
  "FIXME". `TODO(engineer):` in a code skeleton is the one permitted form.
- **R10** — a task carries no open question.
- **R11** — the final task walks `DEMO.md` and amends the QA suites.
- **R12** — **no task adds a socket method** (N8) or an MCP client (N17).

## 13. Pre-dispatch self-check

### A. Mechanical

| # | Run | Pass condition |
| --- | --- | --- |
| A1 | `grep -rniE "\bhmm\b\|\blet me\b\|\bon reflection\b\|\bFIXME\b\|\bI think\b\|\bnot sure\b" tasks/` then `grep -rn "TODO" tasks/ \| grep -v "TODO(engineer)"` | Both empty (R9, R10) |
| A2 | List every task with `est_changed_lines` and file count | Nothing over ~300 lines without justification (R8) |
| A3 | List every task with the **test name** that gates it | Every task has one (R1) |
| A4 | List every file in more than one task's `files_expected` | Each justified (R3) |
| A5 | Paste Q8's action table in full | **A missing table blocks the plan.** D1, D3 and D5 all assert against it |
| A6 | Paste Q1's answer | **If any read action needs a socket method that does not exist, record the finding — do not add one** (N8) |
| A7 | Paste Q9's answer | Where the key comes from, and the missing-key behaviour |
| A8 | Paste Q7's answer | Each read action's cost. **A full transcript scan for a status poll is a finding** (O6) |
| A9 | Enumerate every action and grep the list for stop/kill/pause/resume/retry/answer | Empty (D1, I1) |
| A10 | `grep -rn "key\|token\|secret"` over the planned CLI flags | **No key on a command line** (O1, I6) |
| A11 | `make fmt` then `git status --porcelain` | No output |

### B. Scope

| # | Check | Ref |
| --- | --- | --- |
| B1 | Confirm no task builds an MCP client | N1, N17 |
| B2 | **Confirm no task adds operational control, in any form** | N2, R6 |
| B3 | Confirm no task answers or creates a question | N3 |
| B4 | Confirm no task adds streaming over HTTP | N4 |
| B5 | Confirm no task adds multi-tenant auth | N5 |
| B6 | Confirm no task discovers a daemon | N6 |
| B7 | Confirm no task decides an effect class | N7 |
| B8 | Confirm no task adds a socket method | N8, R12 |
| B9 | Quote the `out_of_scope` lines covering N1–N8 | §8 |

### C. Acceptance

| # | Check | Ref |
| --- | --- | --- |
| C1 | Map each of D1–D13 to a task and a named test | §2 |
| C2 | For D1, confirm the test is an **enumeration over the table**, not a check for named actions | D1, I1 |
| C3 | For D2, confirm the assertion is that a client **receives the reason**, not that a doc exists | D2 |
| C4 | For D3, confirm the assertion reads **S2's table**, not a copied list | D3, I3 |
| C5 | For D4, confirm **set equality**, both directions | D4 |
| C6 | For D6, confirm the assertion is over the **whole table**, not one action | D6, I5 |
| C7 | For D7, confirm an address without a key **refuses to start** | D7 |
| C8 | For D9, confirm the assertion is **end to end**, not a policy read | D9 |
| C9 | Confirm no test writes a key into a log, a fixture, or an error | I6 |

### D. Ordering

| # | Check | Ref |
| --- | --- | --- |
| D1 | Confirm the table precedes both mountings | R4 |
| D2 | Confirm the boundary document is in the table's task | R5 |
| D3 | Confirm the mountings are separate, with set-equality on the second | R6 |
| D4 | Confirm auth is its own task | R7 |

### E. Structure

| # | Check | Ref |
| --- | --- | --- |
| E1 | Confirm the table is the only registration point | I2, O7 |
| E2 | Confirm every entry names an existing socket method | I3 |
| E3 | Confirm no action answers or creates a question | I5 |
| E4 | Confirm read actions are scoped to this daemon's loops | I7, O4 |
| E5 | Confirm no new socket method | I8 |

### F. Project-specific

| # | Check |
| --- | --- |
| F1 | Paste the action table (Q8), including every `Cannot`. |
| F2 | State the key's source and the missing-key behaviour (Q9, O1). |
| F3 | Quote the boundary document, including what it points at instead (Q10, O3). |
| F4 | State how a read action is scoped (Q11, O4). |
| F5 | State whether the scaffolding's HTTP listener and auth are reused (Q3, Q5). |
| F6 | State each read action's cost (Q7, O6). |

## 14. The demo contract

> **Present in every slice of this plan.** A slice is not done when its tests pass. It is done when someone who
> did not build it can follow `DEMO.md` and watch the thing work.

[`DEMO.md`](DEMO.md) is a deliverable, and the final task (R11) walks it and fixes whatever does not match.

**§1 starts by proving a negative** — no port, connection refused — because a default that opens something is the failure
N15 exists to prevent, and the only way to show a default is to look before changing anything.

**§2's third question is the slice.** Ask Claude Code to kill a loop, and it must discover the capability is **absent** and
**find out why, plus what to use instead**. A refusing tool would be discoverable, which is what R6 forbids; a bare
*"unknown tool"* reads as a version mismatch and sends someone hunting. Both halves, or the demo has not shown it.

**§4 shows that CI's limits hold by composition.** A CI-triggered loop is unattended, so an outward action denies without
this slice deciding anything. If that has to be configured, the property is weaker than claimed.
