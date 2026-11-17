# Design — S13 Agents (Stage 3 · Car)

**Date:** 2026-09-06
**Status:** approved (design); not yet implemented
**Stage:** 3 · Car, second of four
**Depends on:** S8 (the handler registry), S11 (policy, and the corpus extractor). **Blocks S16 and S17.**
**Inherits:** A1–A10 from [`docs/_archive/aeon-v1-horizontal/06-agents`](../../_archive/aeon-v1-horizontal/06-agents/design.md). New decisions start at **A11**.

---

## Context

S8 built the handler registry with **two** entries so a blueprint could demonstrate routing. S11 attached policy to
handlers and left resolver names `unverifiable` until this slice. Nothing has yet written real software.

The superseded design's central claim is the one that shapes everything here, and it is stronger than it reads.

### A verbatim port is not wasteful. It is broken — and measurably

Every earlier document assumed the 26 `*-loop` bodies could be carried across. They cannot, and the reason is now a
measurement rather than an argument:

| Measured 2026-09-06, across all 26 `*-loop` bodies | Count |
| --- | --- |
| Reference `loop_get` | **26 of 26** |
| Reference `memory_append` | **26 of 26** |
| Reference `event_append` | **26 of 26** |
| Reference `loop_readTask` | 20 |
| Reference `loop_readFile` | 23 |
| Reference `loop_writeFile` | 19 |
| Open by loading the `dev-loop-protocol` skill (793 lines) | **26 of 26** |
| Carry a `FINAL REPORT` ritual line | **26 of 26** |

**Not most. All of them.** Every single body instructs the model to call at least three tools that do not exist in
Aeon's nine-tool set, and every single one begins by loading the skill whose 793 lines are the context injection this
project exists to remove.

So a port is not a quality decision with a cheap alternative. **The cheap alternative does not run** — the model would
try `loop_get`, fail, and the step would burn its iterations discovering that.

That also makes the failure mode **mechanically detectable**, which is the one piece of leverage available on 2,902
lines of prose rewriting: a handler body may not name a tool outside the nine (A11), and a test can say so.

### The front matter does **not** port mechanically

The superseded design says *"the frontmatter ports mechanically. The bodies do not."* **That is half right**, and the
half that is wrong would have been discovered mid-rewrite:

| Declared in `tools:` across the 26 `*-loop` originals | Count | Fate |
| --- | --- | --- |
| `task` | **26 of 26** | **Dropped.** A1: nothing spawns. Not renamed — removed |
| `todowrite` | **26 of 26** | **Renamed** to `todo` |
| `codesearch` | 25 | Retired (W1) |
| `lsp` | 24 | Retired (W1) |
| `patch` | 13 | Retired (W1) |

`name`, `description`, `mode` and `temperature` do port mechanically. **`tools` needs a translation table** (A19), and
`task` is the one that must be *dropped* rather than mapped — a rename would reintroduce delegation through the back
door of a config file.

And the `description` is **model-facing prose**, so it is subject to the same checks as the body (A20):

| Ritual phrase in `description:` | Count |
| --- | --- |
| "it declines" — the caller guard | **25 of 26** |
| `FINAL REPORT` | 22 |
| "NEVER runs git" | 17 |
| "one task per turn" | 14 |
| `memory_append` | 13 |

A rewrite that carefully strips the body and ports the description verbatim ships a handler that tells the model to
decline work from anyone but `dev-loops` — an orchestrator that no longer exists.

### Content that needs something Aeon does not have

**24 of the 26 bodies reference MCP**, mostly for browser automation: qa-engineer's *"UI cases — Playwright MCP first,
HTTP fallback second"* is the clearest case.

> ⚠️ **This section predates N17's reversal.** The MCP client now lands at **S12**, which **precedes this slice** — so
> MCP content is no longer deferred-because-absent; it is a **granted tool**. What `DEFERRED` should hold here is an
> open amendment ([`ROADMAP.md`](../ROADMAP.md) §7).

That content is not ritual, not became-mechanism, and not keepable domain — the tool does not exist. So it is a fourth
disposition: **deferred, recorded, naming the capability** (A18). Dropping it silently means that when an MCP client
lands, nobody remembers which handlers wanted one or what for.

### The split is three-way, not two

A4 frames the rewrite as ritual stripped, domain kept, and gives the example that makes the distinction real:
*"re-read prior failures" is ritual, "exit 137 is an OOM kill, not a lint finding" is domain.*

**But that second line is no longer domain.** S5's W5 makes `oomKilled` a **fact the tool reports**, derived from exit
137 and `SIGKILL`. The model does not have to infer it, and prose telling it to infer it is now stale documentation
that will eventually contradict the tool.

So there are three categories, and the middle one is the trap (A12):

| Category | Disposition | Example |
| --- | --- | --- |
| **Ritual** | Delete — the engine performs it | `loop_get` + `loop_readFile` to assemble context; `event_append` checkpoints; the `FINAL REPORT` line; the caller guard |
| **Domain that became mechanism** | **Delete** — the tool now guarantees it, and repeating it in prose creates a second source that drifts | "never run git" (the allow-list enforces it); "one task per turn" (the blueprint controls it); "check the branch head before committing" (S16's check-then-act) |
| **Domain** | Keep | Language idiom, review criteria, what makes a good plan, when a migration is unsafe |

### A single line can split across categories

The superseded design's own example is the sharpest case, and it does **not** simply delete. *"exit 137 is an OOM kill,
not a lint finding"* contains two claims:

| Claim | Category | Because |
| --- | --- | --- |
| exit 137 / `SIGKILL` **means** an OOM kill | **became mechanism** | W5 reports `oomKilled` as a fact. The model no longer decodes it |
| an OOM-killed linter's **silence is not a pass**, and the fix is not in your code | **domain — kept** | The tool reports *what happened*. Nothing reports *what it means for the work* |

`aeon-agents.md`'s QAC-05 already required the second half be present, and named its absence the worst of five possible
losses: *"a model treating an OOM kill as a lint finding will edit correct code to satisfy a linter that never ran."*
**That case is right, and it is what forced this refinement** — an earlier draft of A12 would have deleted the whole
line and broken it.

So the rule is: **delete the decoding, keep the consequence.** Splitting one line across two categories is precisely the
judgement A4 said cannot be automated, and it is why the drop record's line references have to be granular enough to
show half a line moving.

### What survives, and what is not this slice's to retire

A2 said the `mode: primary` agents stop being agents and become blueprints. Re-measured, there are **16**, not 15 —
and the sixteenth is `dev-loops-aeon`, which this project created to build Aeon.

**It is not retired here** (A15). A10 already settled the principle: `opencode/agents/` and `opencode/skills/` are
untouched because *they are driving the loop that builds their replacement*. `dev-loops-aeon` is the most literal case
of that there is.

The counts moved in exactly one way and it is worth noting why: **75 files rather than 74, 51 `variant` rather than
50, 16 primary rather than 15 — all because this project added one agent to build itself.** The frontmatter is
otherwise unchanged and still uniform: all 75 declare `name`, `description`, `mode`, `temperature` and `tools`; 38
declare `permission`, which matches S11's measurement exactly.

## Goal

Twenty-five handlers whose bodies carry only what the model still needs to know, a resolver that picks between them by
tech tag, and a structured handoff — such that a blueprint step dispatches to `golang-engineer` and it writes working
Go.

## Decisions

A1–A10 are inherited. New:

| # | Decision | Why |
| --- | --- | --- |
| A11 | **No handler body may name a tool outside the nine**, asserted mechanically against the tool registry | It is the only mechanical guard available on 2,902 lines of prose rewriting, and it catches the exact failure a lazy port produces. All 26 originals fail it today, which is what makes it worth automating |
| A12 | **Three categories, not two**: ritual, **domain that became mechanism**, and domain — and **a single line may split across them.** The middle is deleted; the domain half of a split line is kept | Prose duplicating a mechanism is a second source of truth that drifts, and the drift favours the prose because that is what the model reads. But *"exit 137 is an OOM kill, **not a lint finding**"* is half mechanism and half consequence, and deleting the whole line loses the part nothing else reports |
| A13 | **An unresolved tech tag fails with a structured reason. There is no generic fallback** | A generic handler writing Go badly is worse than a clear refusal, because the badness is discovered later and attributed to the model rather than to a missing handler |
| A14 | **Each rewrite ships a committed drop record**, categorising what was removed | A4 requires a human diff, and a review nobody can reproduce is a review that happened once. The record is what lets the next person tell a deliberate omission from a mistake |
| A15 | **`dev-loops-aeon` is not retired by A2** | It is the loop building Aeon. A10's reasoning applies to it more directly than to anything else in the directory |
| A16 | **The registry consumes S11's corpus extractor output** rather than re-parsing agent files | Two parsers over the same front matter is how the policy the guard enforces and the policy the registry reports come to disagree |
| A18 | **Content needing a capability Aeon lacks is recorded as deferred**, naming the capability — not silently dropped | 24 of 26 bodies reference MCP. Dropped silently, that intent is lost; when a client lands nobody knows which handlers wanted one |
| A19 | **`tools` does not port mechanically.** A translation table, and **`task` is dropped rather than renamed** | All 26 declare `task` and `todowrite`; 25 declare `codesearch`. Renaming `task` would reintroduce delegation through a config file, which is precisely what A1 removed |
| A20 | **`description` is model-facing prose and subject to the body's checks** | 25 of 26 describe a caller guard that declines work from anyone but `dev-loops`. A carefully rewritten body with a verbatim description ships that instruction anyway |
| A17 | **A rewrite may not add domain content the original did not have** | This is a port, not an authoring exercise. If a handler seems to need instruction nobody wrote, that is a finding to raise — the original agent worked, and inventing guidance during a mechanical pass is how a body acquires advice nobody validated |

## Architecture

```
aeon/internal/handler/
  registry.go     name → Handler (S8's, now with 25 entries)
  load.go         front matter + body → Handler
  resolve.go      by-tech-tag, and the resolver registry (A13)
  handoff.go      the structured handoff schema
  bodies/         25 markdown files — the rewritten handlers
    golang-engineer.md  python-engineer.md  …
  drops/          25 drop records (A14)
  testdata/handlers.json    the committed list S8 established
```

### A handler

```go
type Handler struct {
    Name         string
    Description  string
    Temperature  float64
    Tools        []string        // allow-list, ⊆ the nine (A6, G10)
    Policy       policy.Policy   // from S11's extractor (A16)
    SystemPrompt string          // the rewritten body
    Handoff      HandoffSchema
}
```

`Tools` is an **allow-list, not a map** (A6). v1's 34 `filesystem_*: false` entries are dead config — a `false` entry
denies something never granted — and S11's T12 already made accepting one a configuration error.

### The resolver (A13)

```go
// Resolve maps a tech tag to a handler name. A closed registry, like predicates.
func Resolve(tag string, st loop.LoopState) (string, error)
```

`by-tech-tag` is a **named resolver**, not an expression — the same closed-registry reasoning as S8's conditions (L5).
Adding a resolver is a code change, deliberately.

A tag with no handler is a **structured refusal** carrying the tag and the handlers that do exist, surfaced as S8's
`BlockedReason`. Not a fallback: a generic handler producing plausible-looking Go is a failure discovered three steps
later and blamed on the model.

### The handoff

A5 observed that the skills' real content is mostly **file schemas** — `prompt.md`'s fixed section order,
`discovery.md`'s sections, `plan.md`'s shape — and that each belongs either to the one handler that writes that file
or becomes a `produces` declaration plus a handoff schema.

S8's `produces:` already enforces that the artifact exists. The handoff carries what the *next* step needs:

```go
type Handoff struct {
    Outcome  runtime.Outcome   // ok | reject | blocked | failed (L4)
    Reason   string
    Fields   map[string]string // declared per handler
}
```

`Fields` is declared by the handler and validated against its schema, so a handler that promises a summary and does
not produce one is a `reject` rather than a silent gap. S16 adds `pr_summary` to that vocabulary (L3); the mechanism is
here.

## The demo

See [`DEMO.md`](DEMO.md): a blueprint step dispatches to `golang-engineer` and it writes working Go against a task
spec; `by-tech-tag` picks a different handler for a Python task and **fails honestly** on a tag nobody handles; a
handler body is shown to be a fraction of its original and its **drop record** explains what went; and the mechanical
guard is demonstrated by pointing it at an original body and watching it fail.

## Testing

| What | How |
| --- | --- |
| **No forbidden tool** | Every one of the 25 bodies, against the tool registry. **And the same check pointed at an original `*-loop` body, which must fail** — a guard that passes everything proves nothing (A11) |
| No skill loading | No body references `dev-loop-protocol` or any skill |
| The description too | The same checks run on `description`, not only the body (A20). **25 of 26 originals fail on the caller guard alone** |
| `tools` translated | `task` **absent**, `todowrite` → `todo`, `codesearch`/`lsp`/`patch` gone (A19) |
| Deferred content recorded | Every body that referenced MCP has a `DEFERRED` entry naming the capability (A18) |
| No ritual residue | No body contains `FINAL REPORT`, a caller guard, or an instruction to append to memory or events |
| Front matter | All 25 parse; `tools` ⊆ the nine; `temperature` present |
| Policy comes from the extractor | The registry's policy for an agent is **byte-identical** to S11's corpus entry for it, asserted per agent (A16) |
| Drop records | One per handler, categorising every removal as ritual / became-mechanism / domain-kept, and **accounting for the line-count difference** (A14) |
| No invented content | Each drop record's "kept" set is traceable to lines in the original (A17) |
| The 25 are the right 25 | Every `*-loop` original has a handler **except `git-loop`** (A3), asserted by enumeration |
| Resolution | Known tags resolve; an unknown tag is a **structured refusal** naming the tag and the available handlers (A13) |
| No fallback | A test asserting no code path substitutes a default handler |
| Handoff | A handler declaring a field and not producing it is a `reject`; the four outcomes round-trip |
| Blueprint validation | S11's resolver reserve list flips from `unverifiable` to a **real check**: `by-tech-tag` resolves, and an unknown resolver is now `invalid` |
| `opencode/` untouched | A filesystem spy over the whole slice's test run (A10) |
| `dev-loops-aeon` survives | It is not in the retirement set (A15) |

The first row is the one that earns its keep, and only because of its second half. **A mechanical check that no
original body could pass is a check that is actually measuring something** — every one of the 26 references
`loop_get`, so pointing the guard at an original is a live negative control.

## What Stage 5's accumulated learnings must not do

Stage 5 gives agents a database entry — icons, presentation, and **learnings recorded as the agent works.** Two constraints
belong here rather than there, because they are properties of what this slice built.

**The markdown body stays the reviewable artefact.** Twenty-five bodies were hand-rewritten and diffed against their
originals, each with a drop record, precisely so an agent's content is **reviewable in a pull request**. A database entry is
a projection and a learnings store; **an agent that exists only in a database is an agent nobody can review in a diff.**

**And learnings are retrieved, never concatenated into the body.** This is the constraint that matters most. If accumulated
learnings are appended to a system prompt, then after six months an agent's prompt is half unreviewed generated text — which
is **exactly the context injection this project exists to remove**, arriving by a route nobody was watching.

So a learning is retrieved for relevance at step time (S22's machinery), **bounded**, and attributed in the transcript. The
body stays the size it was, and the agent still improves.

**One thing Stage 5 must not do**: choose an agent by embedding similarity. A13 made `by-tech-tag` a closed registry with
**no fallback** because *"a generic handler writing Elixir produces something plausible, the loop reports `ok`, and the
badness surfaces three steps later attributed to the model."* Similarity-based dispatch reintroduces that exactly.
Embeddings for **search and discovery in the UI** are free; for **dispatch** they undo A13.

## Out of scope

- **No git or forge actions.** **S16.** `git-loop` becomes no handler at all (A3), because its work becomes code
  actions.
- **No blueprints for the 16 primaries.** A2 says they become blueprints; **S17** authors the ones the ladder needs.
  This slice retires none of them, and `dev-loops-aeon` is retired at cut-over, not here (A15, A10).
- **No skill retirement.** A5 distributes their content into the bodies rewritten here; nothing in `opencode/` is
  deleted (A10).
- **No `search`.** S22.
- **No new tools.** The nine are complete after S11.
- **No spawning, no delegation, no `task` tool.** A1: nothing nests. Blueprints replace delegation, which is why this
  is a registry rather than a lifecycle manager — no spawn, no abort, no per-subtree cost rollup, because there is no
  tree.
  - **Amendment (S14 T6, landed 2026-10-25).** The no-spawn half stands — nothing spawns; `agentsvc` has no
    `exec` and its leak test reads `/proc` to prove it. The **no-rollup half is withdrawn**: an agent asking
    an agent produces a graph, and the edges exist whether or not anybody records them. Refusing to record
    them did not make the cost unattributable — it made it *unattributed*, which is worse, because
    unattributed reads as zero. `aeon/internal/agentsvc/cost.go` now rolls cost up along the recorded chain
    (`agent_chain` rows), propagating P32's three states: a parent is never more certain than its least
    certain child, and a failed child's measured floor still counts. The original sentence above is kept
    verbatim because the reasoning trail is why these documents are trusted.
- **No agent authoring UI.** Never promised for the alpha.

## Amendments to earlier slices

| Where | Change | Status |
| --- | --- | --- |
| `docs/_archive/aeon-v1-horizontal/06-agents/design.md` | *"The frontmatter ports mechanically"* is **half wrong**: `tools` names five tools that do not exist (A19), and `description` carries ritual in 25 of 26 (A20). Corrected | **landed with this design** |
| `docs/_archive/aeon-v1-horizontal/06-agents/design.md` | Its counts — 74 files, 50 `variant`, 15 primary — are **stale by exactly one each**, and the cause is this project adding `dev-loops-aeon` to build Aeon. Corrected, with A15 recording that it is not retired | **landed with this design** |
| S8 | Its handler registry gains 23 more entries; `testdata/handlers.json` grows from 2 to 25 | T10 |
| S11 | Its resolver reserve list stops being `unverifiable` — `by-tech-tag` is now a real check | T10 |
| This document, A1's non-goal (the bullet above) | **The no-rollup conclusion is withdrawn, the no-spawn conclusion is not** — the graph of asks exists whether or not its edges are recorded, and unrecorded cost reads as zero. `internal/agentsvc/cost.go` (S14 T6) rolls up along `agent_chain` rows with P32's state propagation | **landed with S14 T6** |
| `docs/test_suites/aeon-agents.md` | **QAC-05 corrected A12**: it required the OOM line's *consequence* be kept, which an earlier draft of A12 would have deleted. Its step 1 now checks the consequence rather than the exit code. **QAC-09 is superseded** — it asserts `ask` resolves to deny, which S11's G5 reversed. Its "48 non-loop agents" is now 49, and its TypeScript framing is corrected | **landed with this design** |

## Open questions

None. Which handler a given tech tag maps to is authorship, recorded in the resolver's table and reviewable there,
not an open design question.
