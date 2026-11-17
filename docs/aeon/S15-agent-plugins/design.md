# Design — S15 Agent plugins (Stage 3 · Car)

**Date:** 2026-10-14
**Status:** approved (design); not yet implemented
**Stage:** 3 · Car, sixth of eight
**Depends on:** S10 (policy, the authorship rule), S12 (tools), S14 (cards, skills, Tasks). **Blocks nothing in this stage; S21 extends it over the wire.**

> Decision ids are prefixed **AP**. See the
> [prefix registry](../ARCHITECTURE.md#11-decision-prefix-registry).

---

## Context

S14 gave agents cards and skills. Every one of them we wrote.

**This slice is where agents we did not write arrive** — and it is the slice whose whole job is to be safe
about that, because by the end of it a stranger's prose, a stranger's tool declarations and a stranger's
policy are all inside the system.

### The safety argument already exists

**Nothing here invents a trust model.** Three things were put in place earlier precisely so this slice
would be short:

| | |
| --- | --- |
| **PL1** | *Allow is operator-authored. Deny may be authored by anyone. Deny wins.* So **an imported agent's policy can only narrow it** |
| **AS2** | Cards are **signed**, so *"is this really from who it claims"* has an answer that is not trust |
| **MC2** | Private tools are **invisible**, so an imported agent cannot see what other agents hold |

**That is the whole security model, and it was designed before there was anything to import.** This slice
spends its decisions on the *process* — import, check, consent, enable, revoke — rather than on
re-litigating trust.

### Import is three steps, and never two

> **import → security check → enable.**

**There is no import-and-run.** An agent that arrives enabled is an agent somebody will paste a URL for
and discover afterwards. The gap between arriving and running is where the check and the consent live, and
collapsing it is how every plugin ecosystem learns this lesson.

### Requests are not grants, and the consent screen must say so

An imported agent **declares** what it wants: tools, skills it will call, routes. **Declaring is not
having** — under MC4 a grant is an operator-authored policy, so the agent's declaration is a *request*.

**The consent screen shows the request and what granting it would mean.** It does not show a list of
things the agent already has, because at that moment it has nothing. **This is the single easiest thing to
get wrong in the UI**, and getting it wrong teaches the operator that declarations are grants.

### "Revocation that revokes" is a claim with a sharp edge

Revoking an agent must stop it acting. The easy half is refusing new invocations. The hard half:

> **What happens to a Task that is already in flight?**

A Task whose agent is revoked mid-flight will still complete and its result will still reach a prompt —
unless something stops it. **A revoked agent's answer landing in a model's context is exactly what
revocation was supposed to prevent**, so the result is refused on arrival and the refusal is recorded
(AP7).

That is a real cost: work already paid for is discarded. It is the right trade, because the alternative is
revocation that revokes *eventually*.

---

## Goal

**Agents we did not write can be imported, checked, consented to, enabled, disabled and revoked — and
revocation actually stops them.**

Demonstrable: import an agent, watch the blocklist refuse one that asks for a forbidden tool, consent to
one that does not, run it, then revoke it mid-Task and watch the result refused.

---

## Decisions

| # | Decision | Why |
| --- | --- | --- |
| AP1 | **An imported agent is the same object as a first-party one, from a different source** | Two kinds of agent means two code paths, and the second one is always the less tested. S14's card, skills and Task model apply unchanged |
| AP2 | **Import, check, enable — three steps, never two.** Nothing arrives enabled | An agent that runs on arrival is one somebody pasted a URL for and discovered afterwards. The gap is where the check and the consent live |
| AP3 | **The system blocklist is a policy** — `ToolAccess` deny for `all-agents` — **checked at import, and deny wins** | No new mechanism. S10 collapsed grant and blocklist into one object; the import check is a policy evaluation, so the thing that refuses at import is the thing that would refuse at use |
| AP4 | **Four states, not two: not-installed · installed-disabled · installed-enabled · revoked.** `revoked` is not `not-installed` | A revoked agent's record must survive, or *"what did we once run"* has no answer. And disabled-vs-revoked is the difference between *"not now"* and *"never again"* |
| AP5 | **Consent shows the agent's *requests* and what granting them would mean.** Declaring is not having | Under MC4 a declaration is a request. A consent screen that reads as a list of capabilities teaches the operator that declarations are grants — **the easiest and worst mistake available here** |
| AP6 | **An imported agent may carry `Deny` policy and may never carry `Allow`** (PL1), **enforced at import** | The dangerous direction is closed at the type level. An agent narrowing itself is always fine; an agent granting itself is refused, with the source named |
| AP7 | **Revocation refuses an in-flight Task's result on arrival**, and records the refusal | Otherwise a revoked agent's answer still reaches a prompt, and revocation revokes *eventually*. The cost is discarding work already paid for; the alternative is a guarantee that is not one |
| AP8 | **An unverifiable card is refused at import** (AS2), not imported-with-a-warning | A warning scrolls past. And a card that fails verification is not partially usable — the skills in it are the thing being attested |
| AP9 | **An imported agent cannot claim first-party.** The flag is set by the registry at import, never read from the package | This answers S14's Q3. If a package could declare itself first-party it would skip AS12's untrusted labelling, which is the one thing standing between a stranger's prose and an unlabelled prompt |
| AP10 | **Import is never automatic.** No marketplace fetch, no auto-update, no dependency resolution | Each of those is a path by which code arrives without anybody deciding. MC3 said the same thing about MCP servers, for the same reason |
| AP11 | **The suite passes with no imported agents** | AS11 again, third time. The 25 bodies are first-party and must not acquire a dependency on this machinery |
| AP12 | **Enable and disable are reversible and cheap; revoke is deliberate and recorded** | A disabled agent is a decision somebody may undo today. A revoked one is a decision with a reason worth keeping |

---

## Architecture

```
aeon/internal/plugin/
  import.go     the three-step path; nothing arrives enabled (AP2)
  check.go      the security check: blocklist, signature, authorship (AP3, AP6, AP8)
  state.go      the four states and their transitions (AP4, AP12)
  consent.go    what is requested, and what granting it means (AP5)
  revoke.go     revocation, including in-flight results (AP7)
  provenance.go where the agent came from; first-party is ours to set (AP9)
```

### The state machine

```
                   ┌──────────────┐
   import ───────▶ │  DISABLED    │ ◀────── disable ──┐
                   └──────┬───────┘                   │
                          │ enable                    │
                          ▼                           │
                   ┌──────────────┐                   │
                   │   ENABLED    │ ──────────────────┘
                   └──────┬───────┘
                          │ revoke
                          ▼
                   ┌──────────────┐
                   │   REVOKED    │   terminal, and recorded
                   └──────────────┘
```

**`revoked` is terminal and is not `not-installed`.** Re-importing the same agent after a revocation is a
new import with a new record — so *"we revoked this once and somebody put it back"* is visible rather than
inferred.

### The check, and why it is a policy evaluation

The blocklist is **not a list in this package.** It is `ToolAccess` deny policies targeting `all-agents`,
and the import check **evaluates them** (AP3).

That matters because it makes the import gate and the use gate **the same gate**. A tool the blocklist
refuses at import is refused at use for the same reason by the same code — so there is no way for the two
to disagree, which is the failure a separate import-time list would eventually produce.

### Provenance, and the one flag an agent cannot set

```go
type Provenance struct {
    Source     string    // where it came from
    ImportedAt time.Time
    FirstParty bool      // SET BY THE REGISTRY. Never read from the package.
}
```

**AP9 answers S14's Q3.** A package that could declare itself first-party would skip AS12's untrusted
labelling — which is the single thing standing between a stranger's prose and an unlabelled prompt.

---

## The demo

Import, refuse, consent, run, revoke mid-flight. In full: [`DEMO.md`](DEMO.md).

---

## Testing

| Property | How |
| --- | --- |
| **Nothing arrives enabled** | Import, then assert the state is `disabled` before anything else |
| **The blocklist refuses at import** | An agent requesting a blocklisted tool is refused, naming the policy |
| **The import gate and the use gate are the same** | Grep: no tool list in this package; the check calls S10's evaluator |
| **An `Allow` in an imported package is refused** | With the source named (PL1, AP6) |
| A `Deny` in an imported package is accepted | An agent narrowing itself is fine |
| **An unverifiable card is refused** | Not warned about, and no skills are registered from it |
| **Consent shows requests, not capabilities** | The rendered text says *requests*, and a granted-vs-requested test asserts they differ |
| **Revoke refuses an in-flight result** | Create a Task, revoke, complete the Task → **the result is refused and recorded** |
| Revoke stops new invocations | Immediately |
| Disable is reversible; revoke is terminal | Asserted on the transitions |
| `revoked` ≠ `not-installed` | The record survives; a re-import is a new record |
| **An imported agent cannot set first-party** | A package claiming it is ignored, and the result is still labelled (AS12) |
| **The layer is deletable** | The suite passes with no imported agents (AP11) |
| Nothing fetches | Grep: no HTTP client, no registry URL, no auto-update (AP10) |

---

## Out of scope

- **No marketplace, no registry service, no auto-update** (AP10). Each is a path by which code arrives
  without a decision.
- **No remote agents.** S21 — importing an agent and reaching one over a network are different problems.
- **No sandboxing.** An imported agent is a prompt, a card and declarations; it runs no code of its own.
  **Its tools are the sandbox**, and they are granted by policy. **If a future plugin format ships
  executable code, that is a new slice with a new threat model** — not an extension of this one.
- **No dependency resolution.** An agent that needs another agent names it; it does not pull it.
- **No consent UI.** The app showing the consent screen is an S9 amendment; the CLI demonstrates it.

---

## Amendments to earlier slices

| Slice | Change | State |
| --- | --- | --- |
| **S14** | **Q3 answered: first-party is a registry field set at import, and a package cannot claim it** (AP9) | **this slice's T6** |
| **S13** | The registry holds imported agents beside the 25. **The 25 are unchanged** | **this slice's T1** |
| **S9** | The consent screen **with an approve-flow**, the agent list with its four states, and the enable/disable control | open — S9 amendment |
| **S15 (self, 2026-10-25)** | **The app runs the demo**: Test bench → Demos gains the "Agents · plugins (S15)" panel — live reads (`agent.plugins`), refusals as data, labeled replay; **no management verb, no grant control** (R6, MC4). Made as S14's amendment was; the walk task's §11 records what was verified live versus at test level | **this slice's T10** |
| **S23** | `doctor` reports imported agents, their states, and anything the blocklist is suppressing | open — S23's T8 |

---

## Open questions

| # | Question | Where the answer comes from |
| --- | --- | --- |
| Q1 | What does an imported agent package actually look like — a directory, a tarball, a single markdown file with front matter? | T1. **The constraint is AP10 and §Out of scope: it carries no executable code.** Mirror how a skill is added today |
| Q2 | Does revoking an agent revoke the policies that granted it? | T5. **The design's answer is no** — the grants are operator-authored policies with their own lifecycle, and silently deleting an operator's policy is a surprise. But then a re-import inherits them, which must be **shown at consent** |
| Q3 | Can a disabled agent's in-flight Task complete? | T5. **Probably yes** — disable means *"not now"*, not *"never"*, and AP12 makes it cheap and reversible. **Revoke is the one that refuses** (AP7). State it, because the two reading the same way would make AP12 meaningless |
| Q4 | Where does an imported agent's prompt body sit relative to the 25? | T1. Same registry (AP1), and **its untrusted labelling is what distinguishes it** — not its location |
