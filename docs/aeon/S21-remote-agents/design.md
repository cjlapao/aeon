# Design — S21 Remote agents (Stage 4 · Space shuttle)

**Date:** 2026-10-14
**Status:** approved (design); not yet implemented
**Stage:** 4 · Space shuttle, fourth of six
**Depends on:** S14 (cards, skills, Tasks), S15 (import, consent, revoke), S19 (the narrow surface), S10 (`SkillAccess`).
**Inherits:** AS1–AS13 from [`../S14-agent-services/design.md`](../S14-agent-services/design.md) and AP1–AP12 from [`../S15-agent-plugins/design.md`](../S15-agent-plugins/design.md). New decisions start at **RA1**.

> Decision ids are prefixed **RA**. See the
> [prefix registry](../ARCHITECTURE.md#11-decision-prefix-registry).

---

## Context

S14 made agents askable. S15 made them importable. **Every one of them runs inside this daemon.**

This slice is the first time Aeon talks to something it does not own, on a machine it does not control.

### The model is identical. The trust story is not

**Nothing about the card, the skill or the Task changes.** A2A is the same protocol over the wire as over a
socket — that is what A2A is for, and S14 adopted it rather than inventing a shape precisely so this slice
would be a transport rather than a redesign.

**What changes is every assumption the local case got for free:**

| Locally | Remotely |
| --- | --- |
| P26: filesystem permissions and peer credentials | **nothing. The kernel is not the channel** |
| The peer is a process we started | **the peer is a claim** |
| A hung call wastes a goroutine | **a hung call is somebody else's outage** |
| Data stays on this machine | **a skill request is an egress** |

**So the slice is nine tasks about those four rows**, and none about cards or skills.

### The thing that is genuinely new

**A skill invocation becomes data leaving the machine.**

Locally, passing a file path to an agent is bookkeeping. Remotely, it is **sending that path — and whatever
the skill needs to read — to somebody else.** The payload of a skill request is the product's own source
code, its transcripts, its plans.

**That is why egress is a policy and not a setting** (RA4). *"Never send code to a non-local route"* was
already written as `RouteAccess` for models (S18); the same sentence has to be sayable about agents, and
it has to be sayable **before** the call, not audited after.

### What this slice refuses to become

**Not a service mesh.** No discovery, no health-based routing, no retries with backoff across a fleet.
A remote agent is **a configured endpoint with a pinned identity** — the same discipline MC3 applied to
MCP servers and AP10 to plugins, for the same reason: **a peer that arrives without a config edit is a
peer nobody decided on.**

---

## Goal

**An agent on another machine can be asked for a skill — over TLS, against a pinned identity, with the
request authorized as an egress and the answer treated as a stranger's.**

Demonstrable: configure a remote peer, invoke its skill, watch the trust store refuse an unpinned
certificate, watch `RouteAccess` refuse an egress, and watch a remote outage degrade a step rather than
hang it.

---

## Decisions

| # | Decision | Why |
| --- | --- | --- |
| RA1 | **TLS is required, and there is no plaintext mode** — not for localhost, not for a flag, not for development | A plaintext option is the one somebody uses in production. Locally the socket already exists and is better (P26); if a peer is remote, it is remote |
| RA2 | **A peer is pinned**: its certificate or its public key, in a trust store Aeon owns. **The system CA set is not sufficient on its own** | A valid certificate for *some* host proves TLS, not identity. Pinning is what makes *"this is the peer we consented to"* true, and S15's signed cards are the same argument one layer up |
| RA3 | **A remote peer is configured, never discovered** (MC3, AP10 again) | No `/.well-known` crawling, no registry, no mDNS. A peer that arrives without a config edit is a peer nobody decided on — and under RA4 it is also an egress nobody authorized |
| RA4 | **A remote skill invocation is an egress, authorized by policy before the call** | **The genuinely new risk.** A skill request carries this product's code, transcripts and plans to another machine. `RouteAccess` already says *"never send code to a non-local route"* for models; it must be sayable about agents, **and evaluated before the request is built** |
| RA5 | **A remote agent's results are untrusted, always.** There is no first-party remote | AS12 and AP9 taken to their conclusion. First-party means *reviewed in our pull request*; nothing on another machine qualifies, whatever it claims |
| RA6 | **A remote call has a deadline and degrades** — it never blocks a step (MC9's discipline) | A third-party server hanging must not hang a loop. Remotely the failure is somebody else's outage, which means it will happen on a schedule nobody controls |
| RA7 | **An async remote Task is still a suspension** (AS4), and the completion arrives as an inbound call to our own narrow surface — not a held connection | A held connection cannot survive a daemon restart, which is the one thing P2 exists to guarantee. The callback is another **enumerated projection** (P25), signature-verified like a webhook (TR9) |
| RA8 | **The inbound callback is verified and bound to a Task we created.** An unsolicited completion is refused | Otherwise anybody who can reach the endpoint can inject a skill result into a transcript — which is a fabricated answer, the failure S19's N6 refuses for questions |
| RA9 | **Remote peers are off by default**, and enabling one requires an explicit address, the pinned identity and a key (N15's shape) | A daemon that reaches out by default is a daemon somebody exposed by accident, in the outbound direction |
| RA10 | **The cost of a remote Task is `unknown` unless the peer reports it**, and `unknown` stays `unknown` | P31. A remote agent's spend is not observable here, and inventing a number — or treating absence as zero — is the precise failure the whole cost design exists to prevent |

---

## Architecture

```
aeon/internal/remote/
  peer.go        a configured peer: address, pinned identity, key (RA2, RA3, RA9)
  trust.go       the trust store: pinning, verification, refusal (RA2)
  dial.go        TLS only; deadlines; degradation (RA1, RA6)
  egress.go      RouteAccess before the request is built (RA4)
  callback.go    the inbound completion, verified and bound (RA7, RA8)
  cost.go        unknown stays unknown (RA10)
```

**Everything above the transport is S14's, unchanged.** `card.go`, `skill.go`, `task.go`, `chain.go` and
`invoke.go` are reused — if this slice finds itself editing them, something has been duplicated rather
than transported.

### The two directions, and why the inbound one exists

```
OUTBOUND   aeon ──TLS, pinned──▶ remote peer        a skill request
INBOUND    aeon ◀──narrow surface── remote peer     the completion of an async Task
```

**The inbound direction is not a convenience.** An async Task cannot be a held connection (RA7): a held
connection dies with the daemon, and **P2 exists so that a suspended step survives exactly that.** So the
completion comes back as a call to our own surface, verified and bound to a Task we created.

**Which makes RA8 load-bearing.** Without the binding, the callback endpoint is a way to inject a skill
result into a transcript — a fabricated answer, which is what S19's N6 refuses for questions and the same
reasoning applies.

### Egress, before the request exists

```
invoke → RouteAccess evaluated ─── denied ──▶ refused, nothing sent
                                └─ allowed ─▶ build request → dial → …
```

**Before the request is built, not before it is sent.** A request assembled and then discarded has already
read the files it was going to carry, and in a long-running daemon that assembly may be logged. **The check
belongs where the decision is, not where the socket is.**

---

## The demo

Configure a peer, invoke, refuse an unpinned certificate, refuse an egress, survive an outage. In full:
[`DEMO.md`](DEMO.md).

---

## Testing

| Property | How |
| --- | --- |
| **No plaintext mode exists** | Grep: no flag, no scheme, no localhost exception |
| **An unpinned certificate is refused** | A valid cert from a real CA for the right host, not pinned → **refused** |
| **A pinned peer whose key rotates is refused** | And the refusal says the pin no longer matches |
| **Nothing is discovered** | Grep: no `/.well-known` crawl, no mDNS, no registry |
| **`RouteAccess` is evaluated before the request is built** | Asserted on the order, with a spy on the assembler |
| **A remote result is untrusted** | Always, with no first-party path — asserted by grep |
| **A hung peer degrades the step** | The step continues; the deadline is a reported fact |
| **A deadline is not an empty result** | Distinct, as MC9 required locally |
| **An async remote Task suspends** | No held connection, no goroutine — asserted |
| **An unsolicited callback is refused** | A completion for a Task we never created |
| **A callback for another Task's id is refused** | Bound, not merely authenticated |
| **A remote cost with no report is `unknown`** | Not zero, not omitted — and the parent becomes `partial` or worse (P32) |
| Peers are off by default | A fresh install reaches nothing |
| **S14 is unedited** | Grep: no changes to `card.go`, `skill.go`, `task.go`, `invoke.go` |

---

## Out of scope

- **No service mesh.** No discovery, no health-based routing, no fleet retries. A peer is a configured
  endpoint (RA3).
- **No inbound skill serving.** Aeon asking a remote agent is this slice. **Aeon *being* a remote agent for
  somebody else is not** — that is publishing a card over HTTP, which is S19's surface and a different
  threat model.
- **No federation or peer-of-peer.** A chain through a remote agent is still bounded by AS7's depth and
  cycle rules, and **what the peer does internally is not our graph.**
- **No mutual-TLS client certificate issuance.** If a peer requires one, it is configured like any other
  credential — from the environment or a path (C17). We do not become a CA.
- **No retry.** A failed remote call is a reported fact (RA6). A retry doubles the wait and the model
  cannot tell one outage from two — MC9's reasoning, unchanged.

---

## Amendments to earlier slices

| Slice | Change | State |
| --- | --- | --- |
| **S19** | The callback endpoint is added to the existing action table as an enumerated projection. **No new listener** | **this slice's T6** |
| **S18** | `RouteAccess` gains an agent-egress subject alongside model routes | **this slice's T4** |
| **S14** | AS12's *"not first-party"* is absolute for remote peers (RA5). **No change to the mechanism** | satisfied by T5 |
| **S23** | `doctor` reports configured peers, their pin status, and anything whose pin no longer verifies | open — S23's T8 |

---

## Open questions

| # | Question | Where the answer comes from |
| --- | --- | --- |
| Q1 | Pin the certificate, the public key, or an SPKI hash? | T2. **Key or SPKI survives certificate renewal; a certificate pin does not** — which matters because a peer renewing a cert should not read as a compromise. Decide and state the rotation story |
| Q2 | Does the trust store hold a CA we accept, or only leaf pins? | T2. Leaf pins are stricter and do not scale; a private CA scales and widens. **For the stated use — a handful of known peers — leaf pins are probably right** |
| Q3 | What is the deadline, and is it per-call or per-Task? | T3. **Per-call for sync, and a Task-level expiry for async** — because an async Task's deadline cannot be a connection timeout once RA7 removes the connection |
| Q4 | Can a remote peer appear in the delegation chain, and does its internal fan-out count toward AS7's depth? | T7. **The chain records what we invoked**; what the peer does internally is not observable and not ours. **So depth counts our hops** — state it, because the alternative is a bound that cannot be enforced |
