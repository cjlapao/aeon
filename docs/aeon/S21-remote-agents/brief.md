# Brief — S21 Remote agents

**Implements:** [`design.md`](design.md) · **Demo:** [`DEMO.md`](DEMO.md) · **Plan:** [`plan.md`](plan.md)
**Architecture:** [`../ARCHITECTURE.md`](../ARCHITECTURE.md) — **P2, P25, P26, P31 and P32 are binding.**
**Stage:** 4 · Space shuttle, fourth of six. Depends on **S14**, **S15**, **S19**, **S10**.
**Checked 2026-10-26 against P34** (`ARCHITECTURE.md`) — no CLI-only gap found.
**Repo:** Aeon lives at `aeon/` in this repository (P24) · Go 1.26 · `cgo` enabled (B3)

---

## 1. What this is

**The first time Aeon talks to something it does not own, on a machine it does not control.**

## 2. The model is identical; the trust story is not

**Nothing about the card, the skill or the Task changes.** A2A is the same protocol over the wire as over a
socket — S14 adopted the standard rather than inventing a shape precisely so this slice would be a
transport.

**What changes is everything the local case got for free:**

| Locally | Remotely |
| --- | --- |
| P26 — filesystem permissions, peer credentials | **nothing. The kernel is not the channel** |
| The peer is a process we started | **the peer is a claim** |
| A hung call wastes a goroutine | **a hung call is somebody else's outage** |
| Data stays on this machine | **a skill request is an egress** |

**This slice is nine tasks about those four rows, and none about cards or skills.**

## 3. The genuinely new risk

**A skill invocation becomes data leaving the machine.**

Locally, handing a file path to an agent is bookkeeping. Remotely it is **sending that path — and whatever
the skill reads — to somebody else.** The payload of a skill request is this product's own source, its
transcripts, its plans.

**So egress is a policy, not a setting** (RA4) — and it is evaluated **before the request is built**, not
before it is sent. A request assembled and discarded has already read the files it was going to carry.

## 4. Definition of Done

| | |
| --- | --- |
| **D1** | **No plaintext mode exists** — no flag, no scheme, no localhost exception, asserted by grep |
| **D2** | **An unpinned certificate is refused**, even a valid one from a real CA for the right host |
| **D3** | **A rotated key is refused**, and the refusal says the pin no longer matches |
| **D4** | **Nothing is discovered** — no `/.well-known` crawl, no mDNS, no registry, by grep |
| **D5** | **`RouteAccess` is evaluated before the request is built**, asserted on the order with a spy on the assembler |
| **D6** | **A remote result is always untrusted** — no first-party path exists, by grep (RA5) |
| **D7** | **A hung peer degrades the step**; the deadline is a reported fact |
| **D8** | **A deadline is distinguishable from an empty result** |
| **D9** | **An async remote Task suspends** — no held connection, no goroutine |
| **D10** | **An unsolicited callback is refused** — a completion for a Task we never created |
| **D11** | **A callback carrying another Task's id is refused** — bound, not merely authenticated |
| **D12** | **A remote cost with no report is `unknown`**, never zero, and the parent becomes less certain (P32) |
| **D13** | Peers are off by default; a fresh install reaches nothing |
| **D14** | **S14's files are unedited** — `card.go`, `skill.go`, `task.go`, `invoke.go`, by grep |

**D5 and D11 are the two that matter.**

D5 because it is the only genuinely new risk in the slice, and the natural implementation checks at the
socket — by which time the files are read.

D11 because **a callback that is authenticated but not bound is a way to inject a skill result into a
transcript.** That is a fabricated answer, which is exactly what S19's N6 refuses for questions.

**D14 is how you know the slice stayed a transport.**

## 5. The seams

**Downward — S14, unchanged.** Cards, skills, Tasks, the chain. **If this slice edits them, something has
been duplicated rather than transported.**

**Downward — S10.** `RouteAccess` for egress; `SkillAccess` still governs who may invoke what. **Two
policies, both existing.**

**Downward — S19.** The callback is an enumerated projection on the existing surface. **No listener.**

**Downward — S15.** A remote peer is consented to the way an imported agent is. **Pinning is S15's
signature argument one layer up.**

## 6. Locked decisions

- **RA1** — **TLS only.** No plaintext, for any reason, including development.
- **RA2** — **pinned identity.** The system CA set alone is not sufficient.
- **RA3** — **configured, never discovered.**
- **RA4** — **egress is a policy, evaluated before the request is built.**
- **RA5** — **no first-party remote.** Ever.
- **RA7** — **no held connection.** An async Task suspends; the completion comes inbound.
- **RA10** — **unknown cost stays unknown.**

## 7. Out of scope

- **N1 — No service mesh.** No discovery, health routing or fleet retries.
- **N2 — No inbound skill serving.** Aeon *asking* a remote agent is this slice; **Aeon *being* one** is
  publishing a card over HTTP — S19's surface, a different threat model.
- **N3 — No federation.** A chain through a remote peer is bounded by AS7, and **what the peer does
  internally is not our graph.**
- **N4 — No CA.** If a peer wants a client certificate, it is configured like any credential — environment
  or path, never argv (C17).
- **N5 — No retry.** A failed call is a reported fact. **A retry doubles the wait and the model cannot tell
  one outage from two.**

## 8. Questions to answer before building

| # | Question | Where |
| --- | --- | --- |
| Q1 | Pin the certificate, the public key, or an SPKI hash? | T2 |
| Q2 | Does the trust store hold a CA, or only leaf pins? | T2 |
| Q3 | What is the deadline, and is it per-call or per-Task? | T3 |
| Q4 | **Does a remote peer's internal fan-out count toward AS7's depth?** | T7 |

**Q1 has a consequence worth stating.** A **certificate** pin breaks on renewal; a **key or SPKI** pin
survives it. That matters because **a peer renewing a certificate should not read as a compromise** — and
if it does, the first renewal teaches somebody to disable pinning.

**Q4's answer is forced by observability.** The chain records what *we* invoked. What the peer does
internally is not observable and not ours — **so depth counts our hops.** State it, because the
alternative is a bound that cannot be enforced and therefore is not one.

## 9. Risks

- **R1 — A plaintext escape hatch.** Somebody will want one for a local test, and the local case already
  has a socket. **D1 is the guard.**
- **R2 — Pinning defeated by renewal.** See Q1. If a routine renewal reads as an attack, pinning gets
  turned off. **D3 must pass *and* the rotation story must be written down.**
- **R3 — Egress checked at the socket.** The natural place, and too late. **D5's spy on the assembler is
  the guard.**
- **R4 — The callback is authenticated but unbound.** Verification feels sufficient. **D11 is the guard**,
  and the failure is an injected transcript entry.
- **R5 — Remote cost becomes zero.** Absent data rendered as `$0.00` is the exact failure the cost design
  exists to prevent, and a remote peer is the easiest place for it to reappear. **D12 is the guard.**
