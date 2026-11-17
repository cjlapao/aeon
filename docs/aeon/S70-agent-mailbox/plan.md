# Plan — S70 Agent↔agent mailbox

## What is settled before planning

Rulings MB1–MB8 (operator, 2026-10-31) and the acceptance tests AT1–AT15 are locked — see `design.md` and
the spec. The reuse surface is measured (`brief.md` §2). Three mechanical items are T1's to decide with
the code open (design.md's last section) — none is a product call.

## How we will do it

Same ritual as S68/S69, which means: build preamble + reindex park/restore for daemon suites; every gate
measured and dated in the task file's Notes; sweep reds proven pre-existing at the then-current main
before merge; vitest/tsc measured UNCONTENDED; fixtures digests move only with this task named (CR10);
one commit per task; records append, never rewrite.

## Task sequence

### T1 — The rows: `agent_message` + codec + caps (store only)

One timestamped migration file (never `schema.go`). `store.AgentMessage` + `AppendAgentMessage` (thread
= first id when thread param 0; `result_of` must name a row in the same thread and loop or the write
refuses naming both), `DeliverAgentMessage` (NULL→stamp exactly once; second attempt refuses naming the
row), `ThreadAgentMessages` / `LoopAgentMessages` readers (order by id; scope always slug+root). Caps at
the store door: 32 KiB body, 16/thread, 64 threads per loop lifetime — each refusal naming both sides
(AT6). No update/delete method exists (AT5 compiles a test that proves the surface has none).
Also lands here: the running/not-running callee predicate the runtime needs (does the target lane have a
live step?) — its own small reader over the step rows, no new state.
Files: `internal/store/agent_message.go` (+ `_test`), one migration. Gates: store suite ok at baseline.

### T2 — The tool + the runtime: parse, address check, delivery, half-split

`ask_agent` in `Registry()` (pin test → eleven, `DefaultGrant` untouched); `ParseAskAgent` closed
vocabulary (AT1 zero-row spy). The runtime seam: address check against the PINNED blueprint lanes
(stranger refusal names stranger + lane list, AT3); frozen-set judgement at the ask instant (AT4, D12);
8-asks-per-step cap; then MB4's two arms: boundary-drain (running callee — one composition point per
boundary, steer text byte-identical) and suspend/resume (not-running callee + wait — caller row
`suspended`, question-row pair, cold resume via `loop.CompleteSuspendedStep`, answer as own block via
`AppendUserTurn`; crash-replay test AT8; `wait:false` queue-back AT9). Callee dispatch through the S68
`ModelRunner(words, frame)` with the MB5 frame (AT11 shape test). AS5: stack-dump test covers the path.
Files: `internal/tools/ask_agent.go` (+ test), `internal/toolset/toolset.go`, runtime step/pull seams
(+ tests). Gates: tools/runtime/loop suites at baseline.

### T3 — The doors + events: `agent.messages`, event family, fixtures

`agent.messages {loopId, thread?}` on the socket (scope from the loop row; read-only — AT13 asserts NO
authoring method exists on any narrow surface). Events `agent.message.posted/.delivered/.answered`
published at the three state changes (D7 omitempty; AT15 byte-identity from captured fixtures).
Conformance fixtures: ask, reply, drain, absence (four new + any amended named with this task, CR10);
`make protogen`, `check-protogen`, guard green. Files: `internal/protocol/agent_message.go` (+ fixtures,
+ tests), daemon emit seams (+ s70 daemon tests). Gates: protocol/daemon at baseline (reindex parked).

### T4 — The window: `exchange` row kind + bench

`fromEvents.ts` maps the three events into an `exchange` transcript row (mapper validates every field —
a forged row draws nothing); `ChatStream`/`cards` render `from → to · body` with the existing-palette tone
and thread grouping; the honest-absence row for undeliverable threads (AT14); the bench scripts the
conversation (scripted wiring, no daemon) + gallery message showing exchange + absence states.
Files: ui (types/fromEvents/ChatStream/cards, bench page + scenarios, + vitest). Gates: full vitest
uncontended at baseline; tsc at baseline, zero owned.

### T5 — CLI + gates + records

`aeon loop messages <id> [--thread N]` thin client (+ flag tests; the human path walked in DEMO). Full
sweep with preamble + park/restore; sweep reds proven pre-existing at the then-current main at the root
checkout; vitest/tsc uncontended; records (`brief.md`/`design.md`/`plan.md`/`tasks/*` Notes dated),
SLICES row, cumulative DEMO §S70; single merge to main, push, post-merge smoke.

## Unrun by design (named up front, per §4a)

Live provider bytes (adapters/fakes pin the callee-turn request shapes); Tauri; Windows pipe;
real-desktop exchange walk (the scripted bench stands in).
