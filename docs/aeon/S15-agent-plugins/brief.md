# Brief — S15 Agent plugins

**Implements:** [`design.md`](design.md) · **Demo:** [`DEMO.md`](DEMO.md) · **Plan:** [`plan.md`](plan.md)
**Architecture:** [`../ARCHITECTURE.md`](../ARCHITECTURE.md) — **P26 is binding.**
**Stage:** 3 · Car, sixth of eight. Depends on **S10**, **S12**, **S14**. **S21 extends it over the wire.**
**Repo:** Aeon lives at `aeon/` in this repository (P24) · Go 1.26 · `cgo` enabled (B3)
**Checked 2026-10-26 against P34** (`ARCHITECTURE.md`) — **a real gap found, confirmed against the actual
code, and **now closed by its own slice (built)**: [`S37-agent-admission`](../S37-agent-admission/brief.md)** (S15 itself
is already built and stays unedited — S37 adds a new `proposal` kind, it does not retrofit this package).
N5 below says plainly: *"No consent UI. An S9 amendment; the CLI demonstrates it."* **Confirmed in
`internal/protocol/agent.go`'s own `agent.plugins` entry** (the one real socket method this slice has —
read-only, lists what is imported): its `Cannot` field states outright, *"it cannot import, enable, disable
or revoke — the four state verbs live ONLY in the CLI."* Verified against `internal/protocol/method.go`: no
`agent.import`/`agent.enable`/`agent.disable`/`agent.revoke` is registered. **S35-trust-desk is the planned
fix for three of the four** (`agent.enable/disable/revoke` as plain socket writes, §5 there — not yet
built), **and explicitly does not cover import**: S35's own §5 treats import as already finished before its
scope starts (*"the imported agent already passed blocklist + consent at import (S15)"*). **Import itself
is the one widening action in this slice** — a stranger's agent gaining capability for the first time, past
D2/D4/AP6's blocklist-and-`Allow`-refusal gate — so S37 puts it behind S35's exact staged-proposal mechanism
(`proposal.create`/`list`/`discard`, human-only `aeon proposal commit`), adding one `kind` to S35's registry
rather than inventing a second one.

---

## 1. What this is

**Where agents we did not write arrive.**

By the end of this slice a stranger's prose, a stranger's tool declarations and a stranger's policy are all
inside the system. The slice's whole job is to be safe about that.

## 2. Why it is short

**It invents no trust model.** Three things were built earlier so this one could be small:

| | |
| --- | --- |
| **PL1** | allow is operator-authored, deny may be authored by anyone → **an imported agent's policy can only narrow it** |
| **AS2** | cards are signed → *"is this really from who it claims"* has an answer that is not trust |
| **MC2** | private tools are invisible → an imported agent cannot see what others hold |

**That was designed before there was anything to import.** This slice spends its decisions on the
*process* — import, check, consent, enable, revoke — not on re-litigating trust.

## 3. The two things most likely to be got wrong

**Consent reads as capabilities.** An imported agent **declares** what it wants; under MC4 a grant is an
operator-authored policy, so **a declaration is a request.** A consent screen that renders as a list of
things the agent *has* teaches the operator that declarations are grants — and that lesson, once learned,
is applied to every future install.

**Revocation revokes "eventually".** Refusing new invocations is the easy half. A Task already in flight
will complete and its result will reach a prompt unless something stops it — and **a revoked agent's
answer landing in a model's context is exactly what revocation was supposed to prevent.**

## 4. Definition of Done

| | |
| --- | --- |
| **D1** | **Import leaves the agent `disabled`.** Asserted immediately after import, before anything else |
| **D2** | **An agent requesting a blocklisted tool is refused at import**, naming the policy |
| **D3** | **The import check calls S10's evaluator** — no tool list in this package, asserted by grep |
| **D4** | **An `Allow` policy in an imported package is refused**, with the source named (PL1, AP6) |
| **D5** | A `Deny` policy in an imported package is accepted |
| **D6** | **An unverifiable card is refused, not warned about**, and **no skills are registered from it** |
| **D7** | **Consent renders *requests*, not capabilities** — and a test asserts requested ≠ granted |
| **D8** | **Revoke refuses an in-flight Task's result on arrival, and records it** (AP7) |
| **D9** | Revoke stops new invocations immediately |
| **D10** | Disable is reversible; **revoke is terminal** |
| **D11** | **`revoked` ≠ `not-installed`** — the record survives, and a re-import is a new record |
| **D12** | **A package claiming first-party is ignored**, and its results are still labelled (AS12, AP9) |
| **D13** | **The suite passes with no imported agents** (AP11) |
| **D14** | **Nothing fetches** — no HTTP client, no registry URL, no auto-update, asserted by grep (AP10) |

**D7 and D8 are the two that matter**, per §3. **D12 is the one with the longest reach**: it answers S14's
Q3, and if a package could claim first-party it would skip AS12's labelling entirely.

## 5. The seams

**Downward — S10.** The blocklist *is* policy, and the import check is a policy evaluation (AP3). **This
makes the import gate and the use gate the same gate**, so they cannot disagree.

**Downward — S14.** Cards, skills and Tasks unchanged. An imported agent is the same object (AP1).

**Downward — S12.** A requested tool is granted the same way any tool is: an operator-authored
`ToolAccess` policy.

**Upward — S21.** Same import model, over a network, with TLS and a trust store.

## 6. Locked decisions

- **AP2** — **three steps, never two.** Nothing arrives enabled.
- **AP3** — **the blocklist is a policy**, not a list in this package.
- **AP5** — **consent shows requests.**
- **AP6** — **an imported `Allow` is refused.**
- **AP7** — **revoke refuses in-flight results.**
- **AP9** — **first-party is set by the registry**, never read from the package.
- **AP10** — **nothing fetches.**

## 7. Out of scope

- **N1 — No marketplace, registry service or auto-update** (AP10).
- **N2 — No remote agents.** S21.
- **N3 — No sandboxing**, and this needs saying precisely: an imported agent is **a prompt, a card and
  declarations.** It runs no code of its own. **Its tools are the sandbox**, granted by policy. **If a
  future format ships executable code, that is a new slice with a new threat model** — not an extension of
  this one.
- **N4 — No dependency resolution.** An agent that needs another names it; it does not pull it.
- **N5 — No consent UI.** An S9 amendment; the CLI demonstrates it.

## 8. Questions to answer before building

| # | Question | Where |
| --- | --- | --- |
| Q1 | What is a package — a directory, a tarball, a markdown file with front matter? | T1 |
| Q2 | Does revoking an agent revoke the policies that granted it? | T5 |
| Q3 | **Can a disabled agent's in-flight Task complete?** | T5 |
| Q4 | Where does an imported body sit relative to the 25? | T1 |

**Q3 is the one that keeps AP12 meaningful.** Disable means *"not now"*; revoke means *"never again"*. If
both refuse in-flight work they read identically, and AP12's cheap-reversible distinction is gone. **The
design's answer is that disable lets the Task finish and revoke does not** — state it either way.

**Q2's answer is "no", and it has a consequence.** The grants are operator-authored policies with their own
lifecycle, and silently deleting an operator's policy is a surprise. **But then a re-import inherits those
grants** — so the consent screen must show that it is consenting to something already granted, which is a
different sentence from a first install.

## 9. Risks

- **R1 — Consent becomes a capability list.** The natural rendering. **D7 is the guard**, and the test that
  requested ≠ granted is what makes it concrete.
- **R2 — In-flight results slip through.** The refusal path is on *arrival*, which is a code path that only
  runs in a race. **D8 must be tested with a real revoke-mid-Task**, not a unit stub.
- **R3 — A separate import-time blocklist appears.** It is easier to write a list here than to call the
  evaluator. Then the two disagree, and the import gate becomes advisory. **D3's grep is the guard.**
- **R4 — A package claims first-party.** It is one field and it would be read without thinking. **D12 is
  the guard**, and the consequence is skipping AS12 entirely.
