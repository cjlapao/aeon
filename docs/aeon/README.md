# Aeon

## Updating the Aeon documents

1. Read [SLICES.md](SLICES.md) before choosing work. It is the single maintained slice/task completion tracker.
2. When implementation changes, update the task’s Notes and its SLICES.md row in the same change. Record actual code paths, decisions, deviations, and dated validation commands/results.
3. Track build and validation separately. Mark built only when implementation exists; mark validation recorded only with linked results. Name failures, skipped checks, missing credentials, native UI gaps, and uncommitted work. A specification, unchecked demo script, or passing mock is not live acceptance.
4. At slice close-out, update the slice row and its remaining gaps. Record the walked demo and findings in the slice’s DEMO.md and extend [DEMO.md](DEMO.md) as required by [AGENTS.md §4b](../../AGENTS.md#4b-a-slice-extends-the-cumulative-demo).
5. Keep PLAN.md, ROADMAP.md, TASKS.md, and STATUS.md as historical planning/evidence references. Do not append completion narratives or mirror task checkboxes there. Change a planning contract when scope/dependencies change; link the decision from the task and tracker.
6. Add new slices/tasks to SLICES.md when their files exist. Resolve conflicting IDs before assigning them. Do not invent completion from a folder name or copy old totals.
7. **Starting a slice in its own worktree, in parallel with others?** A new migration and a new bench page each have a conflict-free way to land — never a new entry in `internal/store/schema.go`'s own literal, never a new array entry plus import plus switch branch in `ui/src/app/routes.ts`/`TestBench.tsx`. See [AGENTS.md §3](../../AGENTS.md) (migrations, amended 2026-10-30) and [AGENTS.md §5 "Adding a bench page"](../../AGENTS.md#5-conventions). A shared switch body two slices must both extend (e.g. `internal/action/git_actions.go`, `internal/loop/advance.go`) has no such mechanism yet — expect to resolve that merge by hand.
8. **No file over 1000 lines (SIZE1).** A change that would push a code file past 1000 lines splits it **in that change**; a file already over is broken when the next substantial change lands in it (or earlier, if cheap); a new file is designed under the line from the start, holding only the functions that make sense together, so a reader opens exactly the files the job needs and no more. Split by cohesion, not by size. The full rule and mechanics: [AGENTS.md §4e](../../AGENTS.md#4e-files-stay-under-1000-lines). **Deferred debt, measured 2026-11-02:** three tracked docs are already over the line — `DEMO.md` (3,369), `STATUS.md` (1,574), `SLICES.md` (1,219) — and their splits are deliberately deferred (operator ruling, 2026-11-02). Until they are split, new content goes to per-slice files wherever it can, not into those three.


> **Where are we?** [SLICES.md](SLICES.md) lists every slice, its location, task build state, and validation limits.
> The stage descriptions below explain the intended product; their older build annotations are historical.
>
> [VISION.md](VISION.md) describes the product. [ROADMAP.md](ROADMAP.md) preserves planning decisions.
> Building or restyling a management page in the app? [UI-DESIGN-LANGUAGE.md](UI-DESIGN-LANGUAGE.md) is the reference.

Named for the Greek *aeon* — an age, a span of time that contains its own order.

A coding platform that is opinionated about method, shaped as **a daemon and its clients**. Read
[`ARCHITECTURE.md`](ARCHITECTURE.md) for what it is and why; this file is **how it gets built**.

## The rule this roadmap follows

**Every slice ends with something you can run, watch and judge.** Not a layer that compiles — a thing that
does something.

That is a deliberate correction. An earlier version of this plan decomposed Aeon **horizontally** — model
layer, then store, then runtime, then tools — which meant nothing worked until slice nine and the UI arrived
last. Horizontal plans feel orderly and hide their risk at the end: you discover the design is wrong after
building all of it.

So the decomposition is **vertical**, and it grows the way a vehicle does:

> **Skateboard → bicycle → car → space shuttle.**
> Each stage is a *complete, usable thing*. A skateboard is not half a bicycle; it is a smaller way to get
> somewhere. The bicycle is not the skateboard with parts added; it is the next complete vehicle.

Two consequences worth stating, because they cut against instinct:

- **The UI is not last.** It arrives at the end of the bicycle (S9), because "watch a loop and answer its
  question" is a bicycle-shaped capability, not a finishing touch.
- **A slice may deliberately ship a worse version of something it will redo.** The skateboard has two
  tools; the car has nine. That is not waste — it is how you find out whether the tool interface is right
  while it costs one afternoon to change.

## What is not built here

The **socket transport itself** — the listener, the codec, the dispatcher, the HTTP transport, config
layering, logging, the plugin registry, health, and the API-key machinery — is built in
`go-backend-scaffolding`. Its service packages already carry zero `net/http` imports, so a socket
dispatcher calls the same services an HTTP handler does. See
[`ARCHITECTURE.md` §1a](ARCHITECTURE.md#1a-the-daemon-and-what-this-repository-specifies).

This repository specifies **Aeon**: the protocol's method table, the domain packages that register as
plugins, loop supervision and recovery, and the clients.

---

## Stage 0 — GARAGE

*You can build it and it starts.*

| # | Slice | Demo — what you can do when it lands |
| --- | --- | --- |
| **S1** | **Bootstrap** — the Go module on the scaffolding with unneeded services off, the Tauri shell, the Makefile, CI for six targets | `make dev` starts the daemon and opens the app, both reporting a version. `make build` produces binaries for six platforms. `make test` is green. |

S1 is the slice the whole plan rests on and the one most often skipped. It is also where the scaffolding's
`bootstrap.Register` list gets trimmed and `core.GlobalTenantID` becomes the only tenant.

---

## Stage 1 — SKATEBOARD

*It talks to a model, does one piece of work, and remembers.*

| # | Slice | Demo |
| --- | --- | --- |
| **S2** | **Protocol & streaming** — framing, the versioning rule, `ping`/`version`/`subscribe` | `aeon ping` round-trips over the socket. `aeon watch` tails server events live. The app shows *connected* with the negotiated protocol version. |
| **S3** | **Model layer** — canonical format, **two** adapters (Anthropic + OpenAI), streaming, three-state cost | `aeon ask "explain this repo's layout"` streams a real reply from **either** provider and prints the cost with its state — `known`, `partial`, or `unknown` **with a reason**. |
| **S4** | **Store** — SQLite, schema, migrations, transcripts | `aeon db info` prints the path, the tables and the applied migrations. A transcript written, the daemon restarted, and read back **byte-identical** — signed `thinking` blocks included. |
| **S5** | **One step, end to end** — the runtime, `read` and `bash`, transcript streamed over the protocol | `aeon step "summarise README.md and tell me what is missing"` → it reads the file, answers, and the transcript persists. `aeon step show <id>` replays it from the store. **This is the skateboard.** |

**Why two adapters in S3 and not one.** The canonical format exists because pass-through breaks at the
*second* provider. Building it against one adapter is exactly the mistake the v1 plan made — the format
looks fine until something with a different shape arrives. Two adapters is one extra file and it validates
the seam.

---

## Stage 2 — BICYCLE · complete under replay-only acceptance

*It asks me things, survives being killed, and I can watch it from an app.*

| # | Slice | Demo |
| --- | --- | --- |
| **S6** | **Suspend & the `question` tool** — durable questions, resume from persisted state | A step asks a question and **stops consuming anything**. `aeon answer` (or the app) resolves it and the step continues from where it was. |
| **S7** | **Crash recovery** — the three cases, re-run classes, the `bash` receipt | `kill -9` the daemon mid-step; restart; watch it recover and finish. Kill it *during a `bash`* and watch the step fail **honestly**, naming the tool. Then kill it *after* the command finished but before the write — and watch the receipt **recover the result** rather than throw the work away. |
| **S8** | **Blueprint & the two-step loop** — the format, its validator, `decide`, minimal `execute` | A two-lane blueprint runs two steps in order and stops at a gate. `aeon loop show` renders the ladder. Editing the blueprint YAML changes what runs, with no code change. |
| **S9** | **The app** — loop list, step timeline, the chat thread, question prompts, cost · **specified and built** | Everything in S6–S8, done **from the Tauri app** instead of the CLI: watch a loop run, answer its question inline, see cost in three honest states. **This is the bicycle.** [Replay-only demo verdict](S9-app/DEMO.md#replay-only-acceptance--2026-10-19). |

**S9 is the halfway point, on purpose.** By the end of the bicycle Aeon is genuinely useful for a narrow
job — run a scripted sequence of model steps against a repo, watch it, answer it, and lose nothing to a
crash. Nothing about method, agents or git yet.

---

## Stage 3 — CAR

*It builds real software and opens real pull requests.*

| # | Slice | Demo |
| --- | --- | --- |
| **S10** | **Policy** *(added by renumbering; built 2026-10-21)* — seven policy types behind one evaluator, deny-wins that names the allow it overrode, the authorship rule, the age cap | `aeon policy add` is the only authoring surface; a broad `allow` cannot hide a `deny`, and an agent cannot write its own permission. Walked in [`S10-policy/DEMO.md`](S10-policy/DEMO.md). |
| **S11** | **Permissions & the tools** *(built 2026-10-23, loop `aeon-s11-permissions`)* — the guard built from **this repository's own agent definitions** (the committed corpus measures 38 blocks · 367 rules · **51** distinct patterns · 245 deny / 115 allow / 7 `ask`, counted 2026-10-23 — properties are asserted, counts never are), `ask` as a third action reaching a human, and **ten** registered tools (`write`/`edit`/`glob`/`grep`/`webfetch`/`todo` added; `write_file` survives as a hidden alias) | A step attempts `git push`; an `ask` rule **suspends it for a human**. Walking the slice corrected its own script: **an approval is not an allow** — the answer reaches the *model*, the frozen rule asks again, and nothing spawns; running it takes an **`allow`**, and then the order is verdict → receipt → spawn with a token bound to the exact command bytes. A denied command is proven **never to have spawned**. Walked in [`DEMO.md`](DEMO.md) §S11 — 13 steps, 12 findings. |
| **S13** | **Agents** *(built 2026-10-25, solo pass T1–T11)* — the handler registry: 25 hand-rewritten bodies, drop records down to `git-loop`'s 372 lines, the closed `by-tech-tag` resolver wired at dispatch, and one narrow historical retry of a missing-handler block, decided by the daemon | **Mechanically walked**: the elixir refusal stops the loop naming the tag and the ten available tags, no generic handler runs, and the resolver hands `golang-engineer` down to the model layer (`unknown route`, never `unknown handler`). **The Go the specialist writes awaits a provider credential on the walk box** — the exact commands are in [`S13-agents/UI-QA-DEMO.md`](S13-agents/UI-QA-DEMO.md). Walked in [`S13-agents/DEMO.md`](S13-agents/DEMO.md) — corrections the walk made are marked in-file. |
| **S16** | **Git & forge actions** *(built 2026-10-26, solo pass T1–T13)* — the closed nine-action code layer: check-then-act idempotency with the probe that **cannot create** and a call log proving no side channel, the phantom-merge guard whose tree outranks the forge's report, the one receipt-first action that reconciles **by id, never by body**, templates where an undeclared slot dies at load, and `pr_summary` where shape refuses stubs while prose quality stays **honestly unchecked** | A loop branches, commits, and opens a **real pull request** with a description a human would have written. Run it twice: nothing double-applies. **Walked 2026-10-26 against real repositories with bare origins** — the unattended denies, the ask (`effect: outward — other people will see this`), the squash/original ancestor pair, `98 valid checks, 0 invalid, 0 unverifiable` under `--strict`, and the junk refusal with the tree staying dirty are all LIVE; **the GitHub-API half is OWED with its exact re-walk commands** (no token on the walk box — recorded, never glossed). Walked in [`S16-git-forge/DEMO.md`](S16-git-forge/DEMO.md) — 11 findings, six fixed by the walk; the app panel (QAC-79) reads asks verbatim and **never answers them**. |
| **S17** | **The full ladder** *(built 2026-10-26, solo pass T1–T10)* — two-level counters with the loop ceiling, redo edges that carry the reason AND the attempt (a bare retry is refused at the door), per-item ladder state with no single phase status, verify's three outcomes where a check that could not run is the THIRD state, the merge gate reading review STATE (threads never needed, never resolved), and the append-only unresolved-comments record | **A real feature, specified as a task, built by Aeon, reviewed, and merged. This is the car.** Walked 2026-10-26 **LIVE against the real daemon** (scripted provider — the repo's own replay pattern — real git, local bare origin): the ladder table plus the ONE gate question verbatim, the counters `t1 1/2 · siblings 0/2` with `total_failed_gates 1/10`, the redo input carrying `redo of "verify": sent back — unverified: the linter was killed before it ran`, attended escalation waiting on `["continue","abort"]` AND unattended `collapsed to block (loop is unattended)`, the `aborted` terminal — **the walk earned its keep: F-δ, the handoff reject's reason was dropped at the runtime, so §2 was structurally unreachable live; fixed red-first, the L32 guard never moved**. **6 findings F-α…F-ζ** in-file (F-α: **no production writer seeds `st.Tasks`** — approve→tasks(0)→done with nothing built; recorded, not invented). **The model/GitHub halves are OWED with their exact re-walk commands** (no credential, no token on the walk box — recorded, never glossed). Walked in [`S17-full-ladder/DEMO.md`](S17-full-ladder/DEMO.md); the app panel (QAC-93) reads the gate as text and **never answers it** — the counters render from the daemon's rows, and owed says owed. |

**S17 is the first slice whose demo is a product claim rather than a capability.** Everything before it
shows a mechanism working; S17 shows the method working.

---

## Stage 4 — SPACE SHUTTLE

*Anyone can use it, from anywhere, with anything.*

| # | Slice | Demo |
| --- | --- | --- |
| **S18** | **Provider breadth** *(T1–T10 built & walked 2026-10-26 — **BUILT**)* — the OpenAI-compatible path as **flags on the one openai adapter** (M13 survived: two provider ids, three wire files, every difference a flag row or a named finding), three capability states with a **probe only the operator runs** (M32), cost `unknown` **with a reason** and attribution **by route** over one transcript, and the A→B→A round trip whose resume and recovery honour the loop's **current** route (Q6, answered from the code: the assumption existed twice) | Aeon runs against Ollama on a laptop with no cloud key — `unknown (no-price-for-route)`, never `$0.00`, LIVE. A loop suspended on Anthropic is **resumed on OpenAI** — the three legs are LIVE-OWED (no keys on the walk box) and pinned by their gates, the store read byte-identical after every leg. Walked in [`S18-providers/DEMO.md`](S18-providers/DEMO.md) — 9 findings, one of them a real defect the walk caught in T8's deferred-switch plumbing. |
| **S19** | **The narrow surface** *(T1–T8 built & walked 2026-10-27 — **BUILT**)* — **one action table, two mountings**: seven actions (six reads, one trigger) projected identically onto MCP-stdio and keyed HTTP, each naming its socket method (asserted against S2's own table) and what it **cannot** do; the boundary statement rendered from the same data and riding every field a model reads; questions readable, **never answerable, never creatable** — the keyed attempt gets a 404 with the person-sentence and the alternative (N16, N6); the CI trigger pins `attended=0` and `allowOutward` is the one lever, **per loop at start, never a key**; four error kinds with four codes, classified by type identity — and a slow **or abandoned** caller leaves loop timing unchanged (4.4 ms → 4.0 ms, live) | Drive a loop from **Claude Code** over MCP (server `✔ Connected` on the real client; the model-display leg live-owed at quota, re-run command named). Trigger from **CI** over HTTP with the key out of band; attempt `kill` from either and find the capability **absent, legibly**. `aeon narrow list` diffs byte-empty across mountings. Walked in [`S19-narrow-surface/DEMO.md`](S19-narrow-surface/DEMO.md) — **7 findings, two of them real defects the walk caught on the wire** (a reasoned 404 the framework router pre-empted; typed refusals answering 503-"retry"), killed in the walk's commit and pinned live. |
| **S22** | **Retrieval** *(T1–T9 built & walked 2026-10-29 — **BUILT**)* — embeddings, hybrid search, context resolution | *"Have we decided this before?"* answered from past briefs and handoffs. A step arrives with its context already resolved and needs fewer `grep` calls to start. |
| **S23** | **First run & polish** — `aeon init`, provider presets, `doctor`, error shape, the README | A stranger on a clean machine, holding one API key, reaches a running loop in **under five minutes** — timed, by someone who has not read the design. |

---

## Dependency order, and where it is not forced

```
S1 ─▶ S2 ─▶ S3 ─▶ S4 ─▶ S5          skateboard
                          │
                          ├─▶ S6 ─▶ S7
                          └─▶ S8 ────┴─▶ S9    bicycle
                                            │
                          S11 ─▶ S13 ─▶ S16 ─┴─▶ S17   car
                                                   │
                              S18  S19  S22  S23 ◀─┘   shuttle
```

- **S1 blocks everything.** Nothing can be demonstrated before something runs.
- **S6 and S8 are independent** — suspension and blueprints touch different code — so they can run
  concurrently in two worktrees. S9 needs both.
- **S18–S23 are independent of each other.** Order them by what you need first; S22 is the only genuinely
  optional slice in the plan, and the product must work fully without it.
- **S11 before S13** because a handler's `bash` calls the policy evaluator.

## What each slice folder contains

```
SN-name/
  design.md     the approved design: context, decisions, architecture, out-of-scope
  brief.md      the implementation contract: DoD, invariants, investigation, self-check
  plan.md       the task index in dev-loop-protocol schema
  tasks/T*.md   one full spec per task: Context, What to do, Validation
  DEMO.md       the demo script — what to run, what you should see, what would mean it failed
```

`DEMO.md` is new and it is the point of the vertical decomposition. A slice is not done because its tests
pass; it is done when someone else can follow `DEMO.md` and see the thing work.

## Where the reasoning came from

The [`docs/_archive/aeon-v1-horizontal/`](../_archive/aeon-v1-horizontal/) folders hold the earlier horizontal decomposition — thirteen slices of
TypeScript specs. **Never seed a loop from them.** Their designs, numbered decisions and *measured* findings
are the source material for these slices, and [`docs/_archive/aeon-v1-horizontal/README.md`](../_archive/aeon-v1-horizontal/README.md) says what to
mine from each. The QA suites in [`../test_suites/`](../test_suites/) are **not** superseded: they test
behaviour rather than language, and these slices amend them in place.

## Adding a configuration page (S72 convention)

One Configuration page is **two moves, and nothing else**: create
`aeon/ui/src/features/config/pages/<id>.ts` calling `registerConfigPage({ id, label, group, order, … })`
with a lazy `render` (copy `pages/agents.ts` — the newest example), and append **one line**
`import "./<id>";` to `features/config/pages/registerAll.ts`. No edit to `routes.ts`, `router.tsx`, or
`PageLayout.tsx` — the registry, not a shared file, is how pages appear (CF2, so parallel worktrees
append different lines and never conflict). A page whose module nobody imports simply does not exist,
which is the point. Details: [S72 design](S72-configuration-shell/design.md) (CF2/CF3) and
[T4 Notes](S72-configuration-shell/tasks/T4.md) (what every page really does, walked).

## Status

### → **[STATUS.md](STATUS.md) is the index: what is built, what is next.**

Kept there rather than here so there is **one** place to update, and so this file can stay about what the
slices *are*. **[Its at-a-glance board](STATUS.md#at-a-glance--every-slice)** is the current, per-slice
build state — don't re-duplicate specific counts or "Built: S#–S#" claims here; they go stale the moment
the next slice ships and this paragraph is exactly how that happened before.

[`SEEDING.md`](SEEDING.md) has the loop sequence, and still applies.

**The socket transport is a request to the scaffolding, not work in this plan.**
[`REQUEST-localrpc-service.md`](REQUEST-localrpc-service.md) specifies what Aeon needs from
`go-backend-scaffolding`: a `localrpc` service carrying JSON-RPC 2.0 over a unix socket or named pipe, with filesystem
permissions and peer credentials as the whole access-control model. It is written for someone who has never heard of
Aeon, states what we do **not** need in order to bound the work, and asks five questions about their codebase rather than
answering them.

**Four real protocols are already expressed as blueprints.** [`blueprints/`](blueprints/) holds `dev-loop`, both halves of
`development-map`, and **`proposal-brief` — the protocol that produced every document in this directory.** Three genuinely
different shapes: one iterates tasks and merges, one has a mode that creates no artefacts and has no gates, and one has **no
code action anywhere** and terminates in a document. And **`conversational-map`, expressed only in part** — its user-triggered phases are not a blueprint's
shape at all, which is the format's **boundary** rather than a gap in it.

The exercise found **eight places S8's format could not express something** plus one finding about S16's single forge identity,
each argued in that directory's README, and resolving them is a deliverable of S8's T12.

**Beyond the alpha**, [`ROADMAP.md`](ROADMAP.md) §2 records Stage 5: history, roadmap, continuous work, memory and triage,
webhooks and bug triage — plus **Stage 5, the fleet**, where a roadmap and a project history turn Aeon from something that
runs a loop into something that runs a project. Those are sketches, not specs.

**What is not yet done is the building.** Nothing in `aeon/` exists; these are seventeen briefs, plans and task sets for the
loop to execute. S17's N4 deferred dogfooding to *"after S23's first-run work"*, so running this ladder on Aeon's own
repository is the first thing after the first thing.
