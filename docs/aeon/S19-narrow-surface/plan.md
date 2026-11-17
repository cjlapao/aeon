---
slice: S19-narrow-surface
title: The narrow surface
stage: 4
stage_name: Space shuttle
position: 2 of 4
brief: docs/aeon/S19-narrow-surface/brief.md
design: docs/aeon/S19-narrow-surface/design.md
demo: docs/aeon/S19-narrow-surface/DEMO.md
depends_on: [S2-protocol, S8-blueprints, S11-permissions, S16-git-forge]
independent_of: [S18-providers, S22-retrieval, S23-first-run]
task_count: 8
---

# Plan — S19 The narrow surface

P25 settled the shape three stages ago: the socket carries the full protocol, HTTP carries a **narrow enumerated
projection** shared with MCP-stdio. This builds the narrow half.

**Most of this slice's decisions are about what it refuses to build**, and two of those refusals are permanent rather than
deferred: no operational control (R6), and no answering questions (N16).

## Task sequence

| Task | Deliverable | Gate |
| --- | --- | --- |
| **T1** | The action table, derived from S2's methods, **plus the boundary document** | Nothing matches operational control, by enumeration |
| **T2** | Auth: the key, out of band, never logged | An address without a key refuses to start |
| **T3** | The stdio mounting | Claude Code can list and trigger |
| **T4** | The HTTP mounting, off by default | Set equality with stdio, both directions |
| **T5** | Questions: read, never answer, never create | Asserted over the whole table |
| **T6** | `allowOutward` per loop, and CI's limits by composition | An outward action denies end to end |
| **T7** | Degradation, scoping, and error discrimination | A slow caller never delays a loop |
| **T8** | Demo walk, QA suite | §2's third question: absent **and** legible |

## Ordering rationale

- **T1 first** (R4). Both mountings project the table, and building a mounting first means the table is shaped by
  whichever came first. **The boundary document is in this task** (R5) — N14 is absence *plus* legibility, and legibility
  bolted on afterwards is legibility nobody wrote a test for.
- **T2 second** (R7), because T4 cannot start without it and because O1's out-of-band requirement and I6's never-logged
  requirement are both easy to get wrong and neither is about the action set.
- **T3 before T4** (R6) — the mountings are separate tasks, and **the set-equality test lives with T4** because it cannot
  be written until both exist.
- **T5 after both mountings**, so the "over the whole table" assertion covers both surfaces.
- **T6 after T3** — it needs a trigger action to trigger with, and its property holds by composition with S16 rather than
  by anything this slice decides.
- **T7 after the mountings**, since degradation and error shape are properties of a live surface.
- **T8 last.**

## Reality alignment — checked 2026-10-27, before T1

The same audit S18 ran, run here: every assumption in this plan and the eight task files, against the tree.
Six findings; none changes a decision, all change what the tasks *type*.

- **F-1 · The boundary sketch names a verb that does not exist.** The design's boundary statement says
  *"Instead: `aeon loop stop <id>` from a terminal, or the app."* There is no such CLI verb — `aeon loop`
  takes start/show/create/list/write/read, `aeon step` takes show/verify/list/run. `step.stop` exists only as
  a **socket method**, and it has a real human-side consumer: the app stops loops from
  `StepScreen`/`ChatView`/the loop-workspace. So the statement names what actually exists — **the app**, and
  the socket (`step.stop` over the `0600` path, which is exactly the surface a *human* holds and a sandboxed
  agent does not, P26). This is S18's F-6 recurring; the check-before-promising rule now applies to
  boundary text as much as to demo commands.
- **F-2 · S2 already anticipated this slice.** `protocol.Table.PublicMethods()` (method.go:603) is documented
  *"the methods a narrowed surface may mount… S19's HTTP and stdio surfaces take this, not Methods"*, and
  `Table.Methods()` says *"for `--print-methods` and tests"*. T1's `TestEverySocketMethodExists` asserts
  against `protocol.NewTable(sessions, subs, events).PublicMethods()` — the real table, constructed in-test,
  no copy, C4 satisfied exactly as S2 designed. All six read/trigger methods this slice projects
  (`loop.list`, `loop.show`, `step.show`, `blueprint.validate`, `store.awaiting`, `loop.start`) are in it.
  **Q1 answered early: no gap, no method added** (N8 holds untested-and-unneeded).
- **F-3 · The HTTP listener question (Q3) is answered by framework v0.10.1, and the pin is the work.**
  `api` is registered (`bootstrap/register.go:117` `svc.Register(api.New(svc))`) and pinned off
  (`ForceOff("api")`, :71). The consumable seam as measured: services implementing
  `interfaces.RouteContributor` (`HTTPRoutes() []api_models.Route`) are discovered by the api plugin's
  `Serve` pull, routes carry `SecurityRequirement`, and the auth pre-middleware calls the consumer-supplied
  four-method `TokenValidator` (`ValidateAPIKey` is ours; Bearer and APIKey header types are mutually
  exclusive — the narrow surface speaks **X-API-Key**). `ActivityRecorder` tolerates nil (the framework's own
  nildegrade tests prove it). **The pin is not deleted — it is inverted by explicit choice:** default stays
  `ForceOff("api")`; only `serve --http <address> --key…` passes `ForceOn("api")` to `system.Initialize`,
  so "off by default" stays a code property, and `TestAPIIsPinnedOff` in `register_test` evolves into a
  **connection-attempt** test (D7: asserted by attempting a connection, not by reading config).
- **F-4 · `aeon mcp` exists; it is the client.** `cmd/aeon/mcp.go` carries `list` and `call` (S12's client
  verbs, consuming third-party servers) and `internal/mcp` is that client layer — pinned reachable only from
  `cmd/aeon` by `absent_test.go`. **T3 adds `aeon mcp serve`**: a stdio JSON-RPC *server* that lives in
  `internal/narrow` (its own framing — narrow must not import `internal/mcp`, and the daemon still imports
  neither) and calls the daemon as a socket client, like every other verb.
- **F-5 · T6's file list predates the tree.** `internal/loop/attended.go` does not exist. Attendedness is
  `LoopStartParams.Attended *bool` (protocol/blueprint_loop.go:33), **settable at start and pinned** (S11
  G14) — which answers Q4 directly: a caller can only set it at start, and the narrow trigger ignores the
  body entirely and forces `attended=false`. `allowOutward` exists nowhere; the work is one new protocol
  field, one store column (migration **0031 `loop.allow_outward`**), and the deny sites in
  `internal/loop/execute.go` + `escalate.go`, which today deny `outward`/`irreversible` unconditionally when
  unattended — the composition T6 proves driven, not read.
- **F-6 · Two fixtures corrected.** `docs/test_suites/aeon-mcp.md` numbers **QAN-nn** (T8 continues that
  sequence, amending not rewriting). And **Claude Code IS installed** on this box
  (`~/.local/bin/claude`), so §2's *"verified against a real client"* — T3's tool-list boundary claim and
  T8's absence-and-legibility question — is walkable here, not owed.

## Files

| File | Tasks | Reason for overlap |
| --- | --- | --- |
| `internal/narrow/actions.go` | T1 | The single registration point (O7) |
| `internal/narrow/boundary.go`, `aeon/docs/NARROW.md` | T1 | Absence plus legibility, together (R5) |
| `internal/narrow/auth.go` | T2 | — |
| `internal/narrow/stdio.go` | T3 | — |
| `internal/narrow/http.go` | T4, T7 | T4 mounts it; T7 adds error discrimination and bounding. Split by concern |
| `internal/narrow/questions_test.go` | T5 | — |
| `internal/loop/attended.go` | T6 | *(superseded by F-5: no such file — the change lands at the deny sites in `loop/execute.go`/`escalate.go`, one protocol field, and migration 0031)* |
| `cmd/aeon/serve.go` | T2, T4 | T2 adds the key's plumbing; T4 adds `--http`. Both flags, disjoint |
| `cmd/aeon/doctor.go`, `narrow.go` | T4 | O8's startup and doctor lines; `aeon narrow list` |
| `docs/test_suites/aeon-mcp.md` | T8 | Amended, never rewritten |

## The two permanent refusals

Worth restating in the plan, because a later slice will be tempted:

**No operational control** (R6, N5). Not behind a flag, not with a stronger key, not for a trusted caller. It is a
security position, not a roadmap item: *"handing an agent the means to start loops is handing it a way around every bound
placed on one."*

**No answering questions** (N16, N6). A gate exists to reach a human; an API key is not a human. S11 already denies an
unattended `ask` rather than auto-answering, and a credential does not change that.

## What this slice does not do

**No MCP client** (N17 — out of the alpha; S13's `DEFERRED` records the demand) · no operational control, **ever** · no
answering or creating questions · no streaming over HTTP · no multi-tenant auth · no daemon discovery · no effect-class
decisions (S16's) · **no new socket method** — a gap there is a finding about S2.
