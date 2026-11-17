# Design — S14 Agent services (Stage 3 · Car)

**Date:** 2026-10-14
**Status:** approved (design); not yet implemented
**Stage:** 3 · Car, fifth of eight
**Depends on:** S10 (`SkillAccess`), S12 (tools), S13 (the 25 bodies). **Blocks S15, S21.**
**Amends:** **A1** — *"nothing spawns; there is no tree"*. Still nothing spawns; delegation is a **graph**.

> Decision ids are prefixed **AS**. See the
> [prefix registry](../ARCHITECTURE.md#11-decision-prefix-registry).

---

## Context

S13 gave Aeon 25 agents. Each is a handler the loop dispatches to, and **they cannot talk to each other.**

That was deliberate and it was right: A1 refused delegation because the thing it refused was *spawning* —
a process tree with orphans, aborts and a cost rollup nobody could compute. **Blueprints replaced
delegation, and the loop organises.**

This slice does not undo that. It says what A1 got wrong about the *other* thing.

### What A1 got right, and what it conflated

> *"No spawn, no abort, no per-subtree cost rollup, because there is no tree."*

**Right about process.** Nothing spawns, here or ever. A delegated unit of work is a **row**, not a child —
the same choice S6 made for suspension, for the same reason.

**Wrong about accounting.** *"There is no tree"* was used to conclude there is nothing to roll up. But an
agent asking another agent to do something produces a **graph**, and a graph has edges whether or not
anybody records them. Refusing to record them does not make the cost unattributable — **it makes it
unattributed.**

So A1 is amended: **still nothing spawns, and the chain is recorded.** Cost rolls up along the edges.

### Why an agent needs to ask another agent at all

The loop organises, and that remains true. But **an agent solving a problem may need help that is not a
tool.**

> *"Otherwise that agent would need to invent tools."*

A planner wanting a report from Reddit does not want `reddit.search` — it wants *"find what people are
saying about X and summarise it."* That is not a tool call; it is a request for **judgement about how to
use tools.**

**A tool is a capability. An agent is a capability with judgement about how to use it.** That distinction
is the whole reason both tiers exist (AS10), and it is why context7 can reasonably be *both* a public MCP
tool and an agent that wraps it.

### Async is a suspension, not a concurrency model

**This is what makes the slice cheap.** An A2A Task that will take minutes is not a goroutine, a worker
pool or a queue. **The step fires it and exits** — S6's machinery exactly (P2) — and resumes when the Task
completes.

So "always-on background agents" costs **no new concurrency**, and S5's serial execution is untouched. The
hard part was solved two stages earlier, for other reasons.

### The standard, and what we take from it

**A2A** (Linux Foundation): Agent Cards at `/.well-known/agent.json`, signed; Skills with
`{id, name, description, tags, examples, inputModes, outputModes}`; Tasks as the unit of work; JSON-RPC 2.0
transport; `securitySchemes` for per-skill authorization.

**We adopt the vocabulary and express the authorization as policy.** `securitySchemes` becomes
`SkillAccess` (S10), because a second authorization mechanism beside policy is a second place for the
authorship rule to drift — and MC4 already refused that for tools.

**MCP vertical, A2A horizontal, policy orthogonal to both.**

---

## Goal

**Agents publish what they can do, and can ask each other to do it — with the chain recorded, the cost
attributed along it, and no process spawned.**

Demonstrable: an agent publishes a card, another discovers and invokes its skill, the work suspends and
resumes, and the cost appears attributed to both the caller and the callee.

---

## Decisions

| # | Decision | Why |
| --- | --- | --- |
| AS1 | **A card, skills and tool ownership are all optional.** An agent with none is markdown and a prompt, and the ladder runs on it unchanged | The adoption story, and a tested invariant (AS11). The 25 bodies S13 wrote need **no change** — richer agents are opt-in, per agent |
| AS2 | **The card is A2A's**, at `/.well-known/agent.json`, **signed** | Not inventing a format. Signed because S15 imports third-party agents, and *"is this really from who it claims"* needs an answer that is not trust |
| AS3 | **A skill is a typed request**, with A2A's field set. A skill is **not** a tool and not a prompt | A typed request is reviewable and a free-text ask is not. `inputModes`/`outputModes` are what let a caller know a skill can accept a file before sending one |
| AS4 | **A Task is a row. An async Task is a suspension (P2), not a concurrency model** | The step exits and resumes when the Task completes — S6's machinery exactly. **This is why the slice is cheap**: no worker pool, no queue, no change to S5's serial execution |
| AS5 | **Still nothing spawns.** A delegated unit of work is a row, never a child process | A1's correct half, kept. No orphans, no abort semantics, no process tree to reason about |
| AS6 | **The delegation chain is recorded, and cost rolls up along it** (amending A1) | *"There is no tree"* was right about process and wrong about accounting. Refusing to record the edges does not make cost unattributable — it makes it **unattributed**, which is worse because it looks like zero |
| AS7 | **A chain has a depth bound, and a cycle is refused with the cycle named** | A graph admits cycles. A asks B asks A is a loop that spends money until a cap fires, and the cap would report *"cost exceeded"* rather than *"you have a cycle"* — the wrong diagnosis for a structural bug |
| AS8 | **`SkillAccess` policy governs who may invoke whose skills.** A2A's `securitySchemes` is expressed through it, not beside it | One authorization mechanism. A second would be a second place for S10's authorship rule to drift, and MC4 already refused exactly this for tools |
| AS9 | **Discovery covers cards, skills and public tools. Dispatch stays closed** (A13) | Similarity picks what to *read about*, never what to *call*. A13 refused a generic fallback because *"a generic handler writing Elixir produces something plausible, the loop reports `ok`, and the badness surfaces three steps later attributed to the model"* — similarity-based dispatch is that failure with a better excuse |
| AS10 | **A capability may be a tool, an agent, or both** | A tool is a capability; an agent is a capability **with judgement about how to use it**. context7 as a tool is a raw call; wrapped by an agent it publishes *"find the relevant docs and summarise"* |
| AS11 | **The suite passes with the agent-services layer switched off** | AS1 as a test rather than a promise. S22 earned this discipline for retrieval; it is the same one |
| AS12 | **A skill's result is untrusted when the agent is not first-party**, labelled where it reaches a model | W23 and MC7 arriving a third time. A skill result is prose from another agent, and S15 makes that agent importable from anywhere |
| AS13 | **A Task carries the policy set it was created under**, and a resumed Task evaluates against that set | S7's replay discipline applied to delegation. Otherwise a Task created under one policy completes under another, and the record cannot say which governed it |

---

## Architecture

```
aeon/internal/agentsvc/
  card.go        the A2A card: shape, signing, verification (AS2)
  skill.go       typed requests; input/output modes (AS3)
  task.go        a Task is a row; sync and suspending paths (AS4)
  chain.go       the recorded graph; depth bound; cycle refusal (AS6, AS7)
  cost.go        rollup along recorded edges (AS6)
  invoke.go      agent → agent, through SkillAccess (AS8)
  untrusted.go   labelling third-party skill results (AS12)
  transport.go   JSON-RPC 2.0 over the local socket; remote is S21
```

### The chain is the object

```
loop → lane → skill → TASK ─┬─ agent A
                            └─ agent A → skill → TASK → agent B
```

**Edges are rows, not pointers.** The chain survives a daemon restart because it is in the store — which is
what makes cost rollup possible across a crash, and what makes S7's recovery able to resume a Task it did
not create.

### Async, concretely

| | |
| --- | --- |
| **synchronous** | the step waits; the Task completes within the step |
| **asynchronous** | **the step exits** (S6). The Task is `running`; a completion resumes the loop cold |

**There is no third mode and no timeout-into-background promotion.** A caller chooses at invocation,
because a sync call that silently becomes async is a step whose transcript has a gap nothing explains.

### Cost rollup, and the honest part

A parent's cost is its own plus its children's. **And a child whose cost is `partial` makes the parent's
`partial`** — P32: an aggregate over uncertain values is itself uncertain.

**That is the whole reason this is worth recording.** A rollup that reports a confident total over an
uncertain child is the same error one level up, and it is the error the app exists to prevent (S9's D-items).

### Why dispatch stays closed

S22 will index cards and skills, and the temptation is obvious: *"find an agent that can do X."*

**No.** A13 closed the handler registry with no fallback, and the reasoning transfers exactly: a plausible
agent doing work badly, reported `ok`, surfacing three steps later as a model failure. **Similarity picks
what to read about; a name picks what to call.**

---

## The demo

A card, a discovered skill, a suspending Task, and cost attributed along the chain. In full:
[`DEMO.md`](DEMO.md).

---

## Testing

| Property | How |
| --- | --- |
| **An agent with nothing still runs** | One of S13's 25 bodies, unchanged, dispatched as before |
| **The layer is deletable** | The whole suite passes with agent services off (AS11) |
| A card verifies | A valid signature verifies; a tampered card is **refused**, not warned about |
| A skill is typed | A request missing a required field is refused before any model call |
| **An async Task suspends the step** | Asserted on the **step's absence**: no goroutine, no process — S6's suspension, with the step row `suspended` |
| A completion resumes cold | Kill the daemon between creation and completion; restart; the loop resumes |
| **The chain is recorded** | Every edge is a row; the chain is reconstructable after a restart |
| **A cycle is refused, naming the cycle** | A → B → A refused with the path, **not** a cost-cap failure |
| The depth bound holds | A chain one deeper than the bound is refused at the boundary |
| **Cost rolls up** | A parent's total includes its children's |
| **A partial child makes the parent partial** | P32, asserted — the rollup must not report a confident total over an uncertain part |
| `SkillAccess` governs | Denying the policy makes the skill uninvokable; **no second mechanism** |
| A third-party result is labelled | AS12, on the assembled prompt |
| Dispatch is closed | Grep: no path from a similarity score to an invocation (AS9) |
| A Task carries its policy set | A Task resumed after a policy change evaluates against the original set |

---

## Out of scope

- **No remote agents.** The card, the skills and the Task model are the same over the wire; a remote peer
  adds TLS, a trust store and egress policy. **S21.**
- **No agent import.** S15 — and its security check is why AS2 wants signatures now.
- **No agent authoring UI.** Showing the registry and a card is an S9 amendment.
- **No concurrency.** AS4 means there is none to add: a Task is a row and an async Task is a suspension.
- **No scheduling.** An always-askable agent is not a timer. **S20** brings the clock.
- **No similarity dispatch**, ever (AS9, A13).

---

## Amendments to earlier slices

| Slice | Change | State |
| --- | --- | --- |
| **A1** | *"Nothing spawns; there is no tree"* → **still nothing spawns, and the chain is recorded.** The no-rollup conclusion is withdrawn | **this slice, T6** |
| **S13** | `Handler` gains an optional card and skills. **The 25 bodies need no change** | **this slice's T1, T2** |
| **S8** | A lane may target an agent's skill, so a step gains `skill:` and `params:` | **open — S8 amendment, needed by T4** |
| **S6** | An in-flight Task is a suspension reason | **this slice's T3** |
| **S7** | Recovery resumes a Task it did not create, from the chain | **this slice's T3** |
| **S22** | Cards and skills indexed for **discovery**; dispatch stays closed | open — S22 |

**S8's amendment is a prerequisite for T4**, not a consequence. A lane cannot target a skill until a step
can carry one.

---

## Open questions

| # | Question | Where the answer comes from |
| --- | --- | --- |
| Q1 | What is the depth bound, as a number? | T5, with reasoning. **Small** — the shape being demonstrated is a planner asking a specialist, which is two |
| Q2 | Is the card served over the local socket, a file, or both? | T1. The A2A path is an HTTP convention and **S19 owns HTTP**; locally a file plus a socket method may be enough until S21 |
| Q3 | Does a first-party agent's result need the AS12 label? | T8. **The design says no** — first-party bodies are reviewed in a pull request. **But S15 makes "first-party" a property of the registry, not of the code**, so decide where that boundary is read. **ANSWERED at S15 T6** (answered here because the answer lives there): the boundary is read by the **registry only** — `FirstParty` is set by the compiled bodies (`handler.RegisteredBodies` via `plugin.Agents`) and the import path sets `false` unconditionally; it is never unmarshalled from package input (no struct tag in the plugin package may name it — grep-pinned), and a package's `first_party: true` claim is recorded, reported by `aeon agent list --provenance`, and honoured by nothing (AP9). Labelling stays the ONE `ModelFacingResult`: unlabelled for the compiled 25, S14's region for everyone else — including a claimant (grammar and both outcomes pinned by `agentsvc/provenance_label_test.go`) |
| Q4 | Can a skill invocation itself suspend for a human question? | T3. A skill that needs an `ask` is a Task whose agent suspends — which **composes**, and the question is whether the *caller's* step must also suspend. **Probably yes**, and it should be stated |
