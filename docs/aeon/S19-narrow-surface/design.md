# Design — S19 The narrow surface (Stage 4 · Space shuttle)

**Date:** 2026-09-06
**Status:** approved (design); not yet implemented
**Stage:** 4 · Space shuttle, second of four
**Depends on:** S2 (the socket protocol), S8 (loops), S11 (attendedness), S16 (effect classes).
**Independent of S18, S22, S23.**
**Inherits:** N1, N5–N8, N12 from [`docs/_archive/aeon-v1-horizontal/12-mcp-adapters`](../../_archive/aeon-v1-horizontal/12-mcp-adapters/design.md) and R6 from
`09-server`. New decisions start at **N13**.

---

## Context

P25 settled the shape three stages ago: **two transports, two surfaces.** The socket carries the full protocol; HTTP
carries a **narrow enumerated projection**, shared with MCP-stdio. Access control on the socket is filesystem permissions
(P26).

This slice builds the narrow half, and the reason it is narrow is R6's, unchanged since v1's `control.ts`:

> **Nothing this slice adds becomes an MCP tool.** *Start and stop are human authority; handing an agent the means to
> start loops is handing it a way around every bound placed on one.*

### One surface, two mountings — not two surfaces

N7 gives the shape: stdio for editors, HTTP for a remote caller, **same adapter**. And N12 requires the action set be
enumerated in one place, *"because an action list that grows by accident is how N5 erodes."*

The failure mode worth naming: **an action available over HTTP but not stdio is a third surface** (N18), and P25 was
explicit that Aeon has two. So there are no mounting-specific actions, and the enumeration is one table.

**And the projection must be derived, not parallel** (N13). Each narrow action names the socket method it projects, and a
test asserts that method exists. Two hand-maintained surfaces drift — which is the same reasoning S9 used for generated
types and S18 used for policy coming from one extractor, arriving here for the third time.

### Absence versus refusal

The demo says: *"Attempt `kill` from either and watch it refused."* That phrasing hides a real choice.

| Approach | Problem |
| --- | --- |
| `kill` exists as a tool and refuses | It is **discoverable**, and R6's point is not handing an agent the means. A refusing tool still tells an agent the capability is nearby |
| `kill` simply does not exist | A client gets *"unknown tool"*, which reads like a **version mismatch** — so someone goes looking for the newer version that has it |

Neither alone is right. So: **the capability is absent, and the boundary is documented in the surface itself** (N14). A
client listing what exists finds a stated note — *no operational control, by design* — rather than either a refusing tool
or a silence that looks like a gap.

Absence plus legibility. An agent cannot call what is not there; a human debugging finds out why in the place they are
already looking.

### What an API key is not

The socket's access control is filesystem permissions on a `0600` path (P26). **That does not project onto a port.** So
HTTP needs a key — and the honest framing of what a key buys matters, because it is easy to over-read.

A key authenticates *a caller*. It does not make the caller a human. Which settles the sharpest question in the slice:

**Can the narrow surface answer a question?**

**No** (N16). A gate exists to reach a human, and an API key is not a human. If CI can answer a gate, the gate is
decoration — and S11 already established the consistent position: an **unattended** loop *denies* an `ask` rather than
having it auto-answered (G7, G14). Letting an API key answer would be the same mistake with a credential attached.

So the narrow surface **reads** questions and never answers them. N6 already forbade *creating* one, for the adjacent
reason: *"a fabricated prompt, once answered, feeds a value into a transcript as though the model had asked for it."*

### And HTTP is off by default

A daemon that listens on a port by default is a daemon somebody exposed by accident (N15). Binding requires an explicit
address **and** a key; neither has a default that opens anything.

### What CI actually gets

Worth stating, because the answer is smaller than people expect and that is the design working.

CI drives an **unattended** loop. So S16's effect classes apply: `outward` and `irreversible` actions resolve to **deny**
(L22 with G7). Which means **CI cannot open or merge a pull request unless someone opted in at loop start.**

Combined with N16, the narrow surface is **read-mostly by construction** rather than by policy. That is a stronger
property than a permission list, because nothing has to be configured correctly for it to hold.

### The MCP client's status

**N17 is reversed.** The MCP client lands **day one as S12**, with three tool tiers and registry-held grants — see
[`ROADMAP.md`](../ROADMAP.md) §5. This slice no longer speaks to the client's status; S12 does.

**The MCP client is out of the alpha.** Aeon exposes itself over MCP; it does not consume third-party MCP servers.

The reasoning is v1's and it still holds: `project-initializer` was the only agent naming real MCP tools and it became a
blueprint (A2), so nothing in the ladder depends on a client. And a client is a *consumer* of third-party tools, which
brings its own security surface — N2's allow-list question, N3's untrusted descriptions, N4's capability escalation — none
of which is small.

**S13's `DEFERRED` records name what wants one**: 24 of 26 handler bodies referenced MCP, mostly for browser automation.
That is the demand signal, recorded where someone will find it.

## Goal

One enumerated action table, mounted on stdio and HTTP, that lets Claude Code drive a loop and CI trigger one — and that
cannot start, stop, kill, or answer anything.

## Decisions

N1 (both adapters optional), N5 (read plus a narrow action set, never operational control), N6 (cannot create a
question), N7 (two transports, one adapter), N8 (degrade, never block), N12 (enumerated in one place) and R6 are
inherited. New:

| # | Decision | Why |
| --- | --- | --- |
| N13 | **The narrow surface is derived from the socket protocol**: each action names the method it projects, asserted by test | Two hand-maintained surfaces drift. Third time this reasoning has applied — S9's generated types, S18's single policy extractor, and now this |
| N14 | **Operational control is absent, and the boundary is documented in the surface** | A refusing tool is discoverable and R6's point is not handing an agent the means; a silent absence reads as a version mismatch. Absence plus legibility is the only combination that is neither |
| N15 | **HTTP is off by default**, and binding requires an explicit address **and** a key | A daemon listening on a port by default is a daemon somebody exposed by accident. P26's filesystem permissions do not project onto a port |
| N16 | **The narrow surface reads questions and never answers them** | A gate exists to reach a human and an API key is not a human. S11 already denies an unattended `ask` rather than auto-answering; a credential does not change that |
| N17 | ~~**The MCP client is out of the alpha.**~~ **REVERSED 2026-09-11 — the client lands day one as S12** ([`ROADMAP.md`](../ROADMAP.md) §6) | The original reasoning — *nothing in the ladder depends on a client* — was **true of the ladder and false of the product**. The security half stands and is why S12 is its own slice: a client's surface (N2, N3, N4) is not small, which is an argument for **private per-agent namespaces**, not for deferral |
| N18 | **No mounting-specific action.** One table, two mountings | An action on HTTP but not stdio is a third surface, and P25 says there are two |

> *Annotation, 2026-11-03, S58 — annotate, do not rewrite (AGENTS.md: a built slice's contract updates only when scope changes; it did not):* **S58 adds an inbox and outbound channels on the human door only; the narrow surface is unchanged (9 actions) and still cannot answer.** N16's sentence above is the permanence statement this names; the proof is `TestSupervisionWritesAreAHumanDoor` and the six subtests of `TestSupervisionWritesNeverReachOtherDoors` (cmd/aeon), the nine itself pinned byte-identically by `TestOriginalNarrowRecordsGuardsAreByteIdentical` beside S45's pin. (The T9 spec said "2026-10-30" — the slice's drafting day; the build day is this one, and the record says the truth.)

## Architecture

```
aeon/internal/narrow/
  actions.go      THE table — one place (N12), each entry naming its socket method (N13)
  boundary.go     what is absent and why, discoverable (N14)
  stdio.go        the MCP mounting
  http.go         the HTTP mounting — off by default (N15)
  auth.go         the key, and what it does not mean

aeon/docs/NARROW.md   the surface, its limits, and why each limit exists
```

### The table

```go
type Action struct {
    Name        string
    SocketMethod string        // N13 — the method this projects. Asserted to exist
    Kind        Kind           // read | trigger
    Cannot      []string       // N12 — what this entry cannot do, stated
}
```

`Cannot` is not documentation decoration. N12 requires *"every entry states what it cannot do"*, and a field makes that
enforceable: an entry with an empty `Cannot` fails, because every narrow action is narrower than its socket method and the
difference is the point.

### The two kinds, and nothing else

| Kind | Examples | Not |
| --- | --- | --- |
| `read` | loop status, step transcript, cost, questions, blueprint validation | — |
| `trigger` | start a loop | stop, kill, pause, answer, retry |

**`trigger` is one-way.** Starting is a decision a caller is entitled to make; everything after it is either the loop's or
a human's.

## The demo

See [`DEMO.md`](DEMO.md): a loop driven from **Claude Code over MCP-stdio**; one triggered from **CI over HTTP with a
key**; `kill` **absent from both, with the boundary legible** rather than an unknown-tool error; a gate **read but not
answerable** from either; and HTTP proven **closed** until explicitly bound.

## Testing

| What | How |
| --- | --- |
| Derived, not parallel | Every action's `SocketMethod` **exists in S2's method table**, asserted against the table rather than a list (N13) |
| One table, two mountings | The stdio and HTTP surfaces expose **the same set**, asserted by set equality (N18) |
| Every entry states what it cannot do | An empty `Cannot` **fails** (N12) |
| No operational control | An enumeration over the table: nothing matching stop, kill, pause, resume, retry or answer (N5, R6) |
| The boundary is legible | A client listing the surface finds the stated absence and its reason — **not** an unknown-tool error (N14) |
| Questions read, never answered | The transcript's questions are readable; no action answers one, asserted over the whole table (N16) |
| A question cannot be created | Same, over the whole table (N6) |
| HTTP closed by default | The daemon binds no port without an explicit address; and an address without a key **refuses to start** (N15) |
| The key is not a human | A keyed caller attempting to answer a gate is refused, with a reason saying a gate needs a person (N16) |
| CI is read-mostly by construction | A triggered loop is **unattended**, so `outward` and `irreversible` actions deny (S16's L22, G7) — asserted end to end rather than by reading policy |
| Degrades, never blocks | A slow or failing narrow caller does not delay a running loop (N8) |
| Optional | The product works with **neither** mounting enabled (N1) |

Two rows carry the slice. **"No operational control"** asserted as an *enumeration over the table* rather than a check for
specific names, because the risk is an action added later that nobody thought to forbid. And **"CI is read-mostly by
construction"**, because it proves the property holds through composition rather than through a permission list somebody
has to maintain.

## Out of scope

- **No MCP client** (N17). Aeon exposes itself; it does not consume third-party servers. S13's `DEFERRED` records the
  demand.
- **No operational control, ever** (N5, R6). Not behind a flag, not with a stronger key, not for a trusted caller.
- **No answering questions** (N16).
- **No websocket or SSE on HTTP.** The socket carries streaming; the narrow surface is request/response, and a second
  streaming path would need its own filtering, cursor and teardown — S3's M23 already made that argument.
- **No multi-tenant auth.** One key, one daemon, one user. Anything else is a product decision nobody has made.
- **No remote daemon discovery.** N11's *configured, never discovered* applies here too.

## A webhook is a `trigger`, not a third surface

Stage 5 reacts to forge events — an issue opened becomes a roadmap item, a bug report becomes a triage candidate. That is a
**`trigger`**, which this table already has, plus **signature verification and payload mapping.** It is not a new surface,
and P25 says there are two.

Two things follow, and both are this slice's rules rather than Stage 5's:

**It inverts the trust direction.** Everything else reaches *out* to configured hosts (C19). A webhook means something
reaches **in**, which is a listening port — so N15 applies unchanged: **off by default, explicit address, key required.** A
webhook receiver is the HTTP mounting with a verified payload, not an exception to it.

**And a third party must not be able to trigger outward work.** *"Pick up a bug and start fixing it"* is a stranger causing
Aeon to open a pull request. S11's amendment covers it: an auto-started loop is **unattended by construction**, so outward
actions deny and the opt-in lives on the webhook's own configuration — a deliberate per-source grant, never a property of
having a valid signature.

A valid signature proves **who** sent it, not **what they may cause.**

## Amendments to earlier slices

| Where | Change | Status |
| --- | --- | --- |
| S13 | **N17's reversal changes S13's `DEFERRED` treatment of MCP content.** 24 of 26 bodies reference MCP, and S12 precedes S13 — so that content is no longer deferred, it is a granted tool | **open — see `ROADMAP.md` §7** |
| `docs/test_suites/aeon-agents.md` | Same correction in QAC-17 | **landed with this design** |

## Open questions

None. Which read actions the table contains is authorship, reviewable in one file, and N12's `Cannot` field is what keeps
it honest.
