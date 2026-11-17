# Aeon — architecture

This is the authoritative document for **Aeon**. It supersedes the framing in earlier
`docs/aeon/` documents, which were written for a *port* of the dev loop rather than a *product*.
Where a numbered slice folder still holds a valid contract, this document says so; where a decision in
one of them is now wrong, [§9](#9-what-is-superseded) lists it explicitly so nothing downstream reads a
stale contract as live.

## 1. What we are building

**Aeon** — a coding platform that is opinionated about method, shaped as **a daemon and its clients**.

- **A Go daemon**, built on `go-backend-scaffolding`, that owns loop execution and state.
- **A protocol** over a unix socket / named pipe, which is the product boundary. Any surface can be a
  client: the Tauri app, a VS Code extension, the CLI, CI.
- **Model connectors** to OpenAI, Anthropic, and anything OpenAI-compatible.
- **Chat** as the way a human participates in a running loop.
- **Agents** — specialists that read and write code.
- **The loop engine** — the part nobody else ships: the process itself as user-editable data.

**The daemon shape is the product thesis, not an implementation detail.** Every competitor keeps session
state in a process: close the terminal and the work is gone; use a different editor and the work is
invisible. Aeon's state lives in a store the daemon serves, so a loop can be started from the CLI,
watched in an editor, and answered in the app — and it survives a reboot. That is the one thing a
chat-in-a-process cannot retrofit without being rebuilt.

Free-style tools optimise the first ten minutes. This optimises the change landing correctly. Every
unit of work has a phase, a gate and a definition of done; the process is a blueprint in git, so it is
reviewable, diffable and forkable; handoffs are structured rather than hoped for; cost and provenance
are recorded per step, including when they are unknown.

It is **v2**. Everything under `packages/loop-state-*` and `opencode/` keeps running untouched, on the
registry-pinned version it already uses.

## 1a. The daemon, and what this repository specifies

**The transport is not built here.** The unix socket / named pipe listener, its codec, the HTTP
transport, config layering, logging, the plugin registry, health and the API-key machinery are
`go-backend-scaffolding`'s, and are built there. That project already has what makes this cheap: its
service packages carry **zero** `net/http` or `gorilla/mux` imports, so a socket dispatcher calls the
same services an HTTP handler does — no duplicated handlers, no service refactor.

What this repository specifies is **Aeon**: the protocol's method table, the domain services that
register as plugins, loop supervision and recovery, and the clients.

| Concern | Owned by |
| --- | --- |
| Socket / named-pipe transport, codec, dispatcher | scaffolding |
| HTTP transport, API keys, roles, rate limiting | scaffolding |
| Config layering, logging, plugin registry, health | scaffolding |
| **The protocol's method table and wire shapes** | **Aeon** |
| **Model layer, store, runtime, permissions, tools, agents, loop engine** | **Aeon** |
| **Loop supervision and crash recovery** | **Aeon** |
| **Clients: Tauri app, CLI, VS Code** | **Aeon** |

### One daemon per machine, multiplexing projects

One daemon per machine — and per server, where Aeon runs on one. It multiplexes projects rather than
running one instance per repository, and the store already supports that: it is one file per user with
`project_root` on every scoped table (S3, S4). Those decisions were made for other reasons and turn out
to be exactly what a multiplexing daemon needs.

The concurrency guard stays **per project root**: one running loop per repository, many repositories at
once. A git worktree is a distinct project root, so worktrees keep working for free.

**Inside that one loop, steps may run concurrently — up to a per-project cap (P33, 2026-10-24).** The
project's `maxParallelAgents` (default **1**, which is exactly the serial behaviour every slice through S30
was written against) bounds how many of the loop's steps the daemon runs at once. The daemon is still the
only thing that starts work: agents never start agents (A1 holds), an async Task is still a suspension
(AS4 holds), and tools inside one step still run serially (K9 holds). What changes is the loop engine's
position — a set of in-flight steps rather than one. **S32** builds it; until then the cap is effectively 1.

On a shared server the socket is per-user — filesystem permissions are the access control (P26), and one
socket shared between users would defeat that.

### The daemon owns loop execution

Loops run **inside the daemon**, not as a detached child per loop. That is a change from an earlier
design, and it buys a simpler recovery story at a real cost, both named here so neither is discovered
later.

**What it buys:** one process to supervise, one place where budgets and concurrency are enforced, and no
orphan reconciliation — there is no child to lose track of.

**What it costs:** a daemon crash stops **every** loop on the machine, where a per-loop process lost one.
And the daemon becomes long-lived, stateful and multiplexing, which is precisely the shape that grows
without bound if anything accumulates. Two consequences follow, and neither is optional:

- **Store-as-truth stops being hygiene and becomes load-bearing.** K6 (the runtime persists nothing) and
  K3 (declared context, no accumulation across steps) are what keep a multiplexing daemon flat.
- **The daemon enforces a per-loop memory budget and a global cap** (P28). A measured 4.7 GB in 35
  minutes is what an unbounded harness looks like; the language raises the floor and the discipline sets
  the ceiling.

## 2. Chat is the interaction channel, not a driver

The loop is always the driver. Chat is how a human participates in it.

This collapses five things that look like separate features into one runtime capability:

| Surface | What it actually is |
| --- | --- |
| Talking through a goal before work starts | a clarify step that suspends, asks, refines, repeats |
| An agent hitting a question mid-build | a step suspends |
| Brainstorming a brief | a step looping suspend → answer → refine |
| A gate | a step suspending with a yes/no question |
| A tool permission prompt | a step suspending with a permission question |

One primitive: **a step can suspend, ask, and resume when answered.** Chat is its UI.

v1 has the signal but not the substrate — `awaiting_user` is a column on the `loop` table holding a
flag and a reason string, and there is no question, answer or message table among the twelve. It can
say *that* it is blocked and has nowhere to hold the conversation that unblocks it.

## 3. The spine: suspend, exit, resume cold

**A step that asks a question exits its process. Resume reconstructs the step from persisted state
alone.**

The alternative — hold the process open — is simpler to build, but "I asked a question and went to
bed" is the normal case in this workflow, not the edge case. Holding pins a process per waiting loop
and loses the step on any crash or reboot.

### The mechanism

No new concept is needed at the model layer. A question is a tool call that takes a long time to
return:

```
step runs
  → agent calls question({ text, options?, kind })
  → runtime persists the canonical transcript + the pending tool_use id, then exits
  → hours pass; the machine may reboot
  → a human answers in chat
  → runtime reloads the transcript, appends tool_result(answer), and continues the step
```

From the model's side this is `tool_use → tool_result`, which all three providers understand natively.
No "the human said…" preamble, no prompt gymnastics.

### What must be durable

- The **canonical transcript** as sent, including the pending `tool_use` block.
- The pending question's **tool_use id**.
- The step's identity: loop, task, phase, agent, blueprint node.
- The **frozen declared-context manifest** (see below).
- Accumulated **cost and `cost_state`**.
- The step's **todo list** (amends W8).

### Five consequences worth naming

**Transcripts become a schema, not a runtime detail.** A persisted transcript outlives a release, so
the canonical message format carries a version and resume rejects or migrates an older shape loudly —
the same discipline the blueprint schema gets.

**Persist canonical, never wire.** A transcript stored in a provider's wire shape cannot be resumed on
a different provider, and cannot survive a change to adapter code while a step is in flight.

**K5 and K6 survive intact.** `runStep` stays stateless and still persists nothing itself — the
*caller* owns the transcript. Only the signature changes: `runStep` must be able to return "suspended,
here is the pending call" and to accept "resume from this transcript plus this answer."

**Declared context is frozen at first assembly** and reused verbatim on resume, so the transcript stays
internally consistent. But files may have changed while the step slept, so the manifest records a
content hash per declared file and resume appends a note listing any that changed. Acting on stale
content is a real failure; silently re-resolving the context is a worse one.

**Tools already run are never re-run.** Their results are in the transcript, which *is* the record of
what happened. Idempotency on resume falls out for free.

### What it buys

A reboot costs nothing. No process is pinned per waiting loop. And the runner supervisor gets
*simpler*, not harder — a suspended loop has no live process, so there is nothing to reconcile.

## 3a. Crash recovery is the spine with a different trigger

A suspension and a crash are the same problem. Both leave a step whose process is gone and whose only
record is what the store holds; both are resumed by reconstructing the step from persisted state alone.
P2 already requires that. So **there is no separate recovery subsystem** — there is `resumeStep`, reached
by a second door.

What differs is **where** the interruption lands. A suspension happens at a clean point: a `question`
call, the transcript consistent, everything flushed. A crash lands anywhere.

| Case | Where it landed | Record shows | Recovery |
| --- | --- | --- | --- |
| **A** | between turns | the last entry is a complete `tool_result` set | `resumeStep`. Identical to a suspension |
| **B** | mid-model-call | the assistant turn was never committed; nothing executed | re-issue the call. Costs tokens twice; safe |
| **C** | mid-tool-execution | `tool_use` committed, `tool_result` absent | **unknown** — see below |

### Case C, which decides whether this is sound

The record cannot say whether a tool ran, ran partially, or never started. So **every tool declares a
re-run class** (P27), and recovery acts on it rather than guessing:

| Class | Tools | On recovery |
| --- | --- | --- |
| `safe` | `read`, `glob`, `grep`, `search`, `webfetch` (GET-only, W9) | re-run |
| `detectable` | `write` (same content converges), `edit` (exact-match, so a re-run **fails loudly** rather than corrupting), `todo` | re-run; a failure is information, not damage |
| `unsafe` | **`bash`**, alone | cannot re-run — see the receipt rule |

**The receipt rule.** Before executing an `unsafe` tool the runtime writes a receipt; after it completes,
a completion record carrying the captured result. **The receipt therefore has three states, not two**
(S7, refining P27):

| Receipt state | Means | On recovery |
| --- | --- | --- |
| **absent** | the tool never started | re-run — nothing happened |
| **present, with a completion** | it ran and its result was captured; only the transcript write was lost | **append the captured result and continue** |
| **present, no completion** | it started and the outcome is unknown | **fail the step**, naming the tool |

The middle state is why receipts are worth writing at all. Without it, a crash in the window between a
command finishing and its result reaching the transcript is indistinguishable from a crash mid-command —
and a `go test ./...` that took four minutes and passed would be discarded for nothing.

The last state is where failing beats guessing: `git commit` run twice is not recoverable by inspection,
and a recovery that silently re-runs it produces a repository nobody can reason about. The step is marked
`failed` with a reason naming the tool, and the blueprint's redo edge decides what happens next.

A receipt is **its own table**, not a column on `step_message`: it must exist before the tool runs, and the
message row holding that tool's result does not exist until after it finishes.

This is the receipt-first pattern the L-series already specifies for code actions, extended to the one
tool that needs it. Which is the point worth noticing: **the idempotency work was already done**; it only
had to reach one layer further down.

### What recovery must never do

- **Never re-resolve declared context.** The manifest is frozen (K13); a recovered step reads the same
  hashes and reports what changed (K14), exactly as a resumed one does.
- **Never replay a committed `tool_result`.** The transcript is the record of what happened (K15).
- **Never reset a bound.** `iterationsUsed` and the absolute `deadlineAt` live on the manifest so a step
  cannot escape its budget by crashing repeatedly.

## 4. The substrate

```
  Tauri app        VS Code ext        CLI              CI / webhooks
      │                 │              │                     │
      └──── unix socket / named pipe ──┘              HTTP (off by default,
                    │  full protocol                  api-key, narrow surface)
                    ▼                                        │
        ┌───────────────────────────────────────────────────────────┐
        │  the daemon  (go-backend-scaffolding + Aeon plugins)      │
        │                                                           │
        │   supervision ──▶ loop driver ──▶ step runtime ──▶ model  │
        │        │               │               │            layer │
        │        │               │               ├──▶ tools         │
        │        │               │               ├──▶ agents        │
        │        │               │               └──▶ permissions   │
        │        └───────────────┴──▶ store  (one file, N projects) │
        └───────────────────────────────────────────────────────────┘
```

Everything above the model layer is provider-agnostic. Everything below supervision must be
reconstructible from the store — that is what makes both suspension and crash recovery work.

**Two transports, two surfaces** (P25). They are deliberately not the same surface twice:

| | Socket / named pipe | HTTP |
| --- | --- | --- |
| Access control | filesystem permissions, plus a peer-credential check | API key, role, rate limit |
| Default | on | **off** |
| Clients | Tauri, VS Code, CLI — first-party | CI, webhooks, dashboards |
| Surface | **the full protocol** | **a narrow projection** |
| Read | everything | loops, phases, tasks, steps, cost, thread |
| Write | everything | create a loop · answer a pending question · write a document |
| Refuses | — | start / stop / kill · phase transition · task status · **creating a question** |

The reason is v1's, and it applies with more force to a network endpoint than it did to an MCP tool:
*"exposing them as MCP tools would hand an agent the means to bypass the very guards written to constrain
it."* An API key that can start, stop and kill loops drives the machine.

**The narrow surface is one enumerated constant**, mounted on both HTTP and MCP-stdio (P25). That is a
consolidation: three transports, two surfaces, one action table, one test that enumerates the forbidden
actions and proves each unreachable — rather than three surfaces each reasoning separately about what it
refuses.

**Answering is exposed; creating is not.** A fabricated question, once answered, feeds a value into a
step's transcript as though the model had asked for it.

## 5. The canonical message format

``00-core-v1`/tasks/T6.md` passes `req.messages` to the wire untouched. That is a
one-provider-family decision and it has to go.

Anthropic hoists the system prompt to a top-level `system` parameter, uses typed content blocks, pairs
`tool_use`/`tool_result` by id, and streams `content_block_delta`. OpenAI keeps `role: "system"` in the
array, accumulates `tool_calls` deltas by index, and returns tool output as a `tool`-role message.
These are not variations on one shape.

So: **one canonical internal format, adapters translating both directions, provider shapes never
reaching the engine.**

```
Message = { role: 'system' | 'user' | 'assistant' | 'tool', content: Block[] }
Block   = Text | Thinking | ToolUse | ToolResult
```

- `toWire(messages, route)` builds a provider body; `fromWireStream(events)` yields canonical deltas.
- **Usage is normalised** into one shape — input, output, cacheRead, cacheWrite, reasoning — where any
  field may be absent. Absence is what feeds the three-state cost model rather than being coerced to
  zero.
- Unknown provider fields are **dropped, not forwarded**. The one escape hatch stays the variant's
  explicit `extraBody`.

## 6. How it gets built — five stages, seventeen slices

The decomposition is **vertical**: every slice ends with something you can run, watch and judge. An earlier
version of this plan was horizontal — model layer, then store, then runtime, then tools — which meant
nothing worked until slice nine and the UI arrived last. Horizontal plans hide their risk at the end.

**Skateboard → bicycle → car → space shuttle.** Each stage is a complete, usable thing rather than a
fraction of the next one. [`README.md`](README.md) carries the full table with each slice's demo; the
summary:

| Stage | Slices | When it lands you can |
| --- | --- | --- |
| **0 · Garage** | S1 bootstrap | `make dev` and it starts; `make build` gives six binaries |
| **1 · Skateboard** | S2 protocol · S3 model layer · S4 store · S5 one step | run one real step against a repo, stream it, and replay it from the store |
| **2 · Bicycle** | S6 suspend · S7 crash recovery · S8 blueprint · S9 the app | run a scripted loop, answer its questions from an app, and lose nothing to `kill -9` |
| **3 · Car** | S11 permissions+tools · S13 agents · S16 git · S17 the ladder | have a real feature built, reviewed and merged |
| **4 · Shuttle** | S18 providers · S19 narrow surface · S22 retrieval · S23 first run | run it against any provider, drive it from any editor, and hand it to a stranger |

Three properties of this ordering are deliberate and cut against instinct:

- **The UI lands at S9, in the middle.** "Watch a loop and answer its question" is a bicycle-shaped
  capability, not a finishing touch.
- **A slice may ship a worse version of something it will redo.** The skateboard has two tools; the car has
  nine. That is how you learn whether the tool interface is right while changing it costs an afternoon.
- **Two adapters land in S3, not one.** The canonical format exists because pass-through breaks at the
  *second* provider (P3); validating it against one adapter is the mistake the v1 plan made.

**The socket transport is not among these slices** — it is built in `go-backend-scaffolding` (§1a).

## 7. Distribution

**Go, and one binary per platform.** The daemon and the CLI are the same binary; `aeon serve` is the
daemon, `aeon run` is a protocol client. No Node, no Python, no Docker, no external service.

That is the deployment story the daemon shape earns, and it is what makes the editor-extension path real
rather than aspirational: `gopls`, `rust-analyzer` and `biome` are all single binaries, and that is
precisely why they are easy to embed. A Node daemon inside a VS Code extension means bundling a runtime or
demanding one.

| Surface | Ships as |
| --- | --- |
| daemon + CLI | one Go binary. **In the alpha: a binary and a `PATH`** (S23's N4, N5). *Afterwards*: `curl \| sh`, brew, scoop, and an npm wrapper that downloads it (the `esbuild` / `biome` pattern) so `npm i -g aeon` works without a Node dependency — all of which is distribution work that belongs **after the first five minutes are known to work** |
| Tauri app | signed per-platform installers; spawns or attaches to the daemon |
| VS Code extension | bundles or downloads the daemon binary (the `rust-analyzer` pattern) |
| CI | the same binary, `aeon run` |

**`sqlite-vec` needs nothing on the user's machine.** `sqlite-vec-go-bindings/cgo` vendors
`sqlite-vec.c` and registers it through `sqlite3_auto_extension()`, so the extension is compiled into the
binary. The cost is a C toolchain per build target, which is CI's problem rather than a user's. The
pure-Go WASM route (`ncruces`) would remove even that, but at the versions checked it does not compile —
`sqlite3.Binary` was removed from `ncruces/go-sqlite3` at v0.35 and the bindings still reference it — so it
needs version pinning and is not the default.

### Built on `go-backend-scaffolding`, with most of it switched off

The scaffolding is a multi-tenant SaaS backend. Three of its properties make it the right base anyway, all
verified rather than assumed:

- **Its service packages carry zero `net/http` or `gorilla/mux` imports.** Transport and service are
  already separated, so a socket dispatcher calls the same services an HTTP handler does.
- **`core.GlobalTenantID` exists** and `privilege_guard.go` branches on it, so single-user degenerates
  cleanly instead of being fought.
- **`bootstrap.Register(svc)` is a plugin registry with per-service config gates**, so what Aeon does not
  need is configuration and a shorter registration list rather than surgery.

Switched off or unused: `oauth`, `webauthn`, `otp`, `session`, `sms`, `email`, `notification`,
`object_storage`, `media`, `redis`, `jobs`, `flipt`, `honeytrap`, `ipban`, `bruteforce`, `tracing`,
`profiling`, `metrics`. Most of those defend or instrument a public multi-tenant endpoint, and there is not
one.

Kept and used: `config`, `logging`, `database`, `encryption`, `validation`, `osutils`, `idempotency`,
`events`, `health`, `update`, and — **only for the HTTP surface** — `api_keys`, `role`, `claim` and
`ratelimit`. Those four were surplus while the only transport was a loopback port with no auth; with HTTP
as a deliberate integration surface they are the right answer.

### Layering, enforced by the compiler where possible

```
aeon/
  cmd/aeon/            the one binary: `serve` is the daemon, everything else a client
  internal/
    protocol/          the method table and wire types — slice 1
    model/             canonical format + three adapters
    store/             schema, migrations, transcripts, questions
    runtime/           runStep, suspend, resume, the crash cases
    permissions/       pure evaluator
    tools/             the nine, with re-run classes
    agents/            handler registry
    loop/              blueprints, decide, execute
    supervise/         loop control, budgets, recovery on start
    surface/           the narrow action table for HTTP and MCP-stdio
    retrieval/         embeddings, hybrid search
  ui/                  the web UI (TypeScript, its own toolchain) — Tauri's frontend
  blueprints/standard/ shipped defaults, project-overridable
```

| Package | May import |
| --- | --- |
| `protocol` | nothing internal — it is the contract |
| `model`, `store`, `permissions` | nothing internal |
| `runtime` | `model`, `store` |
| `tools` | `runtime`, `permissions` |
| `agents` | `runtime`, `tools` |
| `loop` | everything above |
| `supervise`, `surface`, `cmd` | everything |

`internal/` means nothing outside the module can import any of it — a boundary the **compiler** enforces,
which is stronger than the lint the TypeScript design relied on. A `depguard` rule covers the table above,
where Go itself has nothing to say (P17).

### There is still one repository, and Aeon is still extracted later

Aeon is built at `aeon/` inside this repository during construction, for the reason it always was: every
slice reads reference material from `opencode/` or `packages/loop-state-*` that a separate checkout cannot
reach. Extraction stays a `git subtree split --prefix=aeon`, and stays mechanical because nothing under
`aeon/` imports from `packages/` (P24).

What changes is that `aeon/` is now a **Go module** rather than an npm package, and the client that is not
Go — the Tauri app's web UI — lives at `aeon/ui/` with its own toolchain, as it always did.

## 8. Decisions

`P` numbers are new. Amendments to the existing `K`/`L`/`A`/`W`/`R`/`D` series are in §9.

| # | Decision | Why |
| --- | --- | --- |
| P1 | Chat is the interaction channel; the loop is always the driver | Five features collapse into one primitive |
| P2 | A suspended step exits its process and resumes cold | Overnight questions are the normal case |
| P3 | Canonical message format; adapters at the boundary | Pass-through is a one-provider decision |
| P4 | Persisted transcripts are canonical and versioned | They outlive a release and a provider choice |
| P5 | Declared context is frozen at assembly; changed files are reported on resume | Consistency first, staleness disclosed |
| P6 | `question` is one tool with a `kind`, covering gates, clarify and permissions | One mechanism, five uses |
| P7 | Compaction is runtime-triggered with a model-authored summary | The runtime owns the budget; the model knows what is safe to drop |
| P8 | Retrieval returns `file:line` ranges, never chunk text | Locate, do not quote |
| P9 | Retrieval is hybrid (vector + FTS5) and complements `grep`, never replaces it | Code questions are usually structural |
| P10 | Storage stays embedded — SQLite plus `sqlite-vec` | "Install Postgres first" breaks self-contained |
| P11 | **One Go module**, layered by package under `aeon/`; the web UI is the one separate toolchain | Nothing is versioned or released independently. Go packages give the layering for free, and `internal/` makes a boundary the compiler enforces |
| P12 | v1 is untouched; Aeon is built at `aeon/` in this repository and extracted at the alpha | Every slice reads reference material from `opencode/` or `packages/`, which a separate repo cannot reach; a copy would go stale |
| P13 | Three providers in v1: OpenAI, Anthropic, OpenAI-compatible | The third covers vLLM, LiteLLM, Ollama, OpenRouter |
| P14 | Unattended runs need a per-kind answer policy | Otherwise the first overnight run hangs on question one |
| P15 | Blueprints carry a `schemaVersion` | A hand-written blueprint must survive v2.3 |
| P16 | One chat transcript per loop; creating the loop opens the conversation | Goal-shaping *is* the clarify phase, so nothing precedes the loop |
| P17 | Layering enforced by Go's `internal/` plus a `depguard` rule, not by convention | `internal/` is checked by the compiler, which is stronger than any lint; `depguard` covers the rest of the layer table |
| P18 | The 25-handler starter pack ships publicly, as data under `aeon/blueprints/standard/` | An empty library is a poor first impression; data is forkable, code is inherited |
| P19 | Embeddings default to `text-embedding-3-small` @ 1536; model and dimension stored in the index | A dimension mismatch must force reindex, not return plausible wrong results |
| P20 | `maxOutputTokens` is required on every request | Anthropic rejects a call without `max_tokens`; OpenAI treats it as optional |
| P21 | `thinking` blocks round-trip verbatim, signature included; compaction never touches the last assistant turn's | Anthropic rejects a tool-use turn whose prior thinking was altered or dropped |
| P22 | The wire-level request is `ModelRequest`; `StepRequest` is the step-level one | `01-core` and `01-model-layer` both named a type `StepRequest`, and they are different types in one package |
| P23 | K3's rule holds across steps; a **within-step** compaction path is still required | A single step's tool loop can accumulate thirty bash results, and suspension now lets one step span a human round-trip. "No context grows large enough to compact" was reasoned about steps, not about the loop inside one |
| P24 | Nothing under `aeon/` may import from `packages/` | It is what keeps `git subtree split` a mechanical extraction rather than a refactor |
| P25 | Two transports, **two surfaces**: the socket carries the full protocol, HTTP a narrow enumerated projection shared with MCP-stdio | An API key that can start, stop and kill loops drives the machine. One action table, one policy, one test |
| P26 | Access control on the socket is **filesystem permissions**, plus a peer-credential check | It is the reason the daemon needs no auth for first-party clients, and it replaces "bind loopback and hope" |
| P27 | Every tool declares a **re-run class** — `safe`, `detectable`, `unsafe` — and `bash` alone is unsafe, guarded by a receipt written before execution | A crash lands mid-tool-execution where a suspension never does, and the record cannot say whether the tool ran |
| P28 | The daemon enforces a **per-loop memory budget and a global cap** | A long-lived multiplexing process is the shape that grows without bound; the language raises the floor, the cap sets the ceiling |
| P29 | The daemon owns loop execution; there is **no detached process per loop** | Simpler supervision and no orphan reconciliation, at the cost that a daemon crash stops every loop — recovered automatically on restart (§3a) |
| P30 | **Go**, one binary for daemon and CLI, built on `go-backend-scaffolding` | The single binary is what makes the editor-extension path real; the scaffolding's services are already transport-agnostic |
| P31 | **Wherever verification can be unavailable, the answer has three states, not two** — checked-and-good, checked-and-bad, and *could not check*, the last naming what is missing and when it arrives. Aeon never reports "fine" for "I could not tell" | **Eight** independent places have converged on this shape (ROADMAP.md §6): cost (`known`/`partial`/`unknown`), the `bash` receipt (§3a), blueprint validation (S8, where a registry arrives slices later), connection liveness (S9), **verification itself** (S17 — an OOM-killed linter found nothing because it did not run, which the protocol already distinguished as `unverified`), **provider capabilities** (S18 — `verified` / `declared` / `contradicted`), **policy acknowledgement** (S10 — enforced-and-acknowledged / enforced-but-unacknowledged / could-not-determine), and **bug validation** (S29's sketch — reproduced / not-reproduced / could-not-tell). At eight the shape is load-bearing rather than observed: assume any new verification will need it. A two-state answer forces every one of them to lie, and the lie is always the reassuring direction |
| P33 | **The orchestrator may run several steps of one loop concurrently, bounded per project by `maxParallelAgents` (default 1).** Only the daemon dispatches; agents never start agents (A1), async Tasks stay suspensions (AS4), tools within a step stay serial (K9), and one running loop per project root is unchanged | Added 2026-10-24 at the user's direction. Independent lanes (a survey and a lint, two reviewers) waiting on each other is wall-clock nobody wants to pay, and the chat-first loop workspace (S31) has to show what several agents are doing at once. **A cap, not a pool**: default 1 means a project that never touches the setting behaves exactly as before, and a higher value is a deliberate choice a person made. Spawning stays forbidden because the cost of A1's reversal was a process tree and its abort semantics — none of which a daemon-scheduled step needs. S32 owns the engine, workspace-conflict, recovery and budget questions this raises |
| P32 | **An aggregate over uncertain values is itself uncertain.** A total, an average or a chart over values that include an `unknown` or a `partial` is reported as a **floor or a range**, never as a figure | S9 already floors a per-loop total when one step is `partial`. Over a project's whole history the same rule is far more tempting to break, because **a dashboard wants a number** — and a "total spend" over a year containing forty unknown costs is *at least* $X. P31's pressure comes from not knowing; P32's comes from wanting to present |
| P34 | **The daemon surface is the primary interface — the CLI is secondary.** Anything built from today is designed **API-first**: the primary door is a method on the daemon's own surfaces (the socket protocol for first-party clients; the narrow HTTP / MCP-stdio projection for key-holders — P25). The CLI is one client of that surface and never the sole door. **A capability that is not reachable through the daemon surface is not finished and is not accepted as finished** | Added 2026-10-26 at the user's direction. The app is what people use; a capability visible only to someone typing `aeon …` is invisible to the app, invisible to Aeon itself (its tools are socket clients too), and decays into a CLI-shaped wiki. The CLI keeps full standing — the terminal is sometimes the fastest door and sometimes the only provably-human one — but never the **only** door. This does **not** soften **PL15**: committing live policy stays a human-only door (the CLI today; the staged-authoring pattern — inert proposals on the socket, commit through a human-only door — arrives with the S35 design), because authoring the guards that constrain the daemon cannot itself be a socket write. The exception is carved narrowly so the rule keeps its teeth everywhere else |

### The four tools discussed

**`question`** — structured (`options`) for gates and permissions, free-form for clarify and
brainstorming. Subsumes the permission prompt as `kind: 'permission'`.

**`todo`** — stays within-step, but the *storage* assumption changes: within-step now spans a human
round-trip of arbitrary length, so todos persist with the step rather than living in process memory.
Still not the task ladder; the ladder is the loop's durable structure, todos are working memory inside
one step.

**Compaction** — mostly not a tool. The runtime detects pressure because it owns the token budget; the
model authors the summary because it knows what is semantically safe to drop. Declared context (K3)
makes this far less critical than it is in OpenCode: context does not accumulate across steps, so
compaction is needed only for long single steps and the chat transcript. One hard constraint —
**compaction must never drop a `tool_use` without its `tool_result`, or a pending question.** Anthropic
rejects the request outright; OpenAI degrades silently, which is worse. That is the bug class, and it
gets a test.

**Retrieval** — two corpora, and the second is the more valuable: the **code**, chunked by symbol, and
the **loop history** — past briefs, decisions, handoffs, `memory.md`. *"Have we decided this before?"*
is a question nobody's tool answers, and the corpus already accumulates. Staleness is handled by a
content hash per chunk with incremental reindex triggered by the loop after it writes files — not a
filesystem watcher. With no embedding provider configured, search degrades to keyword-only **and says
so**, the same honesty the cost model uses. Embedding a repo costs money and reports through the same
path.

## 9. What is superseded

Read these as *dead*, not as alternatives:

| Where | Decision | Status |
| --- | --- | --- |
| ``00-core-v1`` | provider seam is one OpenAI-compatible endpoint | **dead** — S1 replaces it |
| ``00-core-v1`/tasks/T6.md` | `req.messages` passed to the wire untouched | **dead** — P3 |
| ``00-core-v1`` K5, K6 | stateless `runStep`, persists nothing | **hold**, but the signature changes (§3) |
| ``00-core-v1`` K3 | "no compaction subsystem exists" | **amended** — the rule holds, the conclusion does not (P23) |
| ``00-core-v1`` | `StepResult.costUsd` + `costPartial` (two fields) | **dead** — slice 1's three-state `Cost` union replaces both |
| `06-agents` | `ask` resolves to deny | **dead** — S5, P6 |
| `05-tools` W8 | `todo` lives within the step | **holds in scope, dead in storage** — §8 |
| `09-server` R1 | a detached child process per loop | **amended** — a suspended loop has no process |
| `09-server` R6 | no new MCP tool for operational control | **holds**, and generalises |
| `07-sqlite-driver` (deleted) D1–D5 | migrate to `node:sqlite` | **moot** — the store is Go now |
| every slice | TypeScript, `zod`, `vitest`, `node:sqlite`, npm packaging | **dead** — Go (P30). The *logic* stands; the language and its idioms do not |
| `09-server` | HTTP + SSE on loopback, serving the UI from one origin, "no authentication anywhere" | **dead** — P25/P26. A socket with filesystem permissions replaces the caveat |
| `09-server` R1 | a detached child process per loop, `(pid, host)` identity, orphan reconciliation | **dead** — P29. The daemon runs loops; recovery replaces reconciliation |
| `10-ui-chat` U1 | the UI is served by the server from the same origin | **dead** — a browser cannot speak a unix socket. The Tauri shell stops being optional packaging and becomes the UI |
| `12-mcp-adapters` | a standalone MCP slice with its own action reasoning | **absorbed** — its `ACTIONS` constant becomes the narrow surface (P25), mounted on HTTP and stdio |
| `13-cli` C8 | `aeon run` is the runner the server spawns | **revised** — `aeon run` is a protocol client; the daemon runs the loop |
| `13-cli` C1 | one published npm package | **revised** — one Go binary, with an npm wrapper that downloads it |
| `README.md` (old) | chat driver 6c, interactive workflows 6e deferred | **dead** — they are the product |
| `SEEDING.md` | seeds against this repository | **stale** — retarget to the v2 repo |

## 10. Resolved questions

All three are now decided; nothing is open.

**The starter handler pack ships publicly** (P18) — as data under `aeon/blueprints/` and `agents/`, not as
code. An engine with an empty library is a poor first impression, the 25 handlers *are* the
demonstration of the method, and shipping them as forkable data means a user edits an opinion rather
than inheriting one. They live under `aeon/blueprints/standard/`, explicitly marked as one opinion and
overridable, so "we ship a standard one but we do not impose" holds literally.

**Embeddings default to `text-embedding-3-small` at 1536 dimensions** (P19). Cheapest of the credible
options, and its `dimensions` parameter allows Matryoshka truncation to 512 for speed without
re-embedding. A local model over the OAI-compatible path is a config change. **The model id and
dimension are stored in the index**, and a mismatch on open forces a reindex rather than silently
comparing vectors from two different spaces — the failure mode that produces plausible, wrong results.

**Chat is per loop** (P16) — see §8.

## 11. Decision prefix registry

Each slice owns a prefix so ids never collide across documents. **Derived from the slices themselves, not asserted.**

| Prefix | Owner |
| --- | --- |
| `P` | this document — product and cross-cutting |
| `B` | S1 bootstrap |
| `T` | S2 protocol (transport-independent method table) |
| `M` | S3 model layer, continued by S18 providers |
| **`DB`** | **S4 store** — renamed from `S` on 2026-09-16 |
| `K` | S5 one step, S6 suspend, S7 recovery (inherited from `01-core`; K1–K10 keep their numbers) |
| `W` | tools — S5, S6, S7 and S11 (inherited from `05-tools`) |
| `L` | S8 blueprints, S16 git/forge, S17 full ladder (inherited from the loop slices) |
| `U` | S9 app (inherited from `10-ui-chat`) |
| `G` | S11 permissions (guards) |
| `A` | S13 agents |
| `N` | S19 narrow surface (inherited from `12-mcp-adapters`) |
| `V` | S22 retrieval (vectors) |
| `C` | S23 first run |

**Reserved for the slices not yet written** ([`ROADMAP.md`](ROADMAP.md) §2):

| Prefix | Owner |
| --- | --- |
| `PL` | S10 policy |
| `MC` | S12 MCP client |
| `AS` | S14 agent services |
| `AP` | S15 agent plugins |
| `TR` | S20 triggers |
| `RA` | S21 remote agents |
| `LW` | S31 loop workspace |
| `PA` | S32 parallel agents |
| `OL` | S38 organization layers |
| `CR` | S39 chat runs |
| `AU` | S40 automation |
| `EV` | S59 execution environment (EV1–EV22) |

### `S` is not a decision prefix

**`S<n>` means slice, everywhere.** S4 used `S1`–`S20` for its own decisions until 2026-09-16, which collided with
slice references *inside the same sentence* — `design.md:62`'s `S16` meant S4's decision while `:72`'s meant the
retrieval slice. They are now `DB1`–`DB20`. **Do not reintroduce a single-letter prefix that shadows `S`, or any
existing row above.**

> ⚠️ **One ambiguity remains, and it is pre-existing:** `T` is both S2's decision prefix **and** the task-file prefix
> (`tasks/T1.md`), so `T4` in a plan usually means the task. It has caused no documented error, but it is the same
> shape as the `S` collision — worth watching, and worth not making worse.

`D` is retired with `07-sqlite-driver`. `R` is retired with `09-server`; its decisions live in S2 and S19.
