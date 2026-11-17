# Design — S70 Agent↔agent mailbox

Rulings MB1–MB8, with the reasoning each one owes. The spec
(`docs/superpowers/specs/2026-10-31-agent-mailbox-design.md`) carries the acceptance tests AT1–AT15 and
the industry citations; this file carries WHY, in the order the operator heard the questions.

## MB1 — The entrance is a registered tool, not a daemon queue and not a blueprint edit

The model is the one who wants to ask; every serious industry design agrees (OpenAI's docs say it in one
sentence: "Handoffs are represented as tools to the LLM"). A socket-only entrance would make the daemon
or a human post messages "on behalf of" an agent — the agents wouldn't actually be talking. A
blueprint-only entrance (extending the board) keeps asking a pre-declared act, which contradicts the
territory: the whole point is the un-predicted question. So: `ask_agent` in `tools.Registry()`, and the
S11 rule applies without negotiation — registered is not granted; `DefaultGrant()` untouched; the
operator grants via `tool_access` policy or the loop-start grants. The `question` tool is the shape
template: closed-vocabulary parse BEFORE anything persists, `Run` unreachable, the runtime owns the
effect. The frozen set judges the ask at the ask instant (D12) — `EvaluatedAt` required, never defaulted,
the same discipline `agentsvc.Invoke` already died once for not having.

## MB2 — Loop-scoped addressing; D8 becomes the mailbox's law

The loop is the conversation's boundary because replay, policy scope and the window already agree on it:
`to` must name an agent of the loop's own PINNED blueprint lanes. A stranger is refused naming the
stranger AND listing the lane's addressable agents (refusal doctrine: name the reason and the thing
refused, and what IS allowed). Never broadcast — the board's D8 rule ("never a broadcast, never
unaddressed") generalizes to the whole mailbox. Card-store agents outside the loop stay the board's
territory with its `relayed` provenance: provenance kinds stay singular facts, and opening
mailbox→cards would fork the chain-edge/cycle machinery across two doors.

## MB3 — Rows are the conversation; a thread is its own header

`agent_message`: `id, loop_slug, component_root, thread, from_agent, to_agent, body, expect_reply,
result_of (NULL until answered), delivered_at (NULL until drained), created_at`. No thread table: the
first ask's id IS the thread (a conversation is its own header — one less join, one more fact). Append-
only: no update/delete path compiles against it (rows are facts; `delivered_at` is set exactly once by
the drain, and a second delivery attempt refuses naming the row). Caps, each refusal naming both sides:
32 KiB body (steer's number), 16 messages per thread, 8 asks per step (a turn cannot spam), 64 threads
per loop LIFETIME (the self-review catch: an active-window cap starves a long-running loop — a loop
should outlive its conversations, not be blocked by them). Migration: one timestamped file under
`internal/store/migrations/` (never a `schema.go` edit — the S43-era rule).

## MB4 — Delivery REUSES TaskMode; there is never a second waiting path

`task.go` states the law this slice inherits: "A Task is a row, and an async Task is a SUSPENSION, not a
concurrency model... Nothing here starts a goroutine." The mailbox extends that shape, it does not fork
it:

- **Callee running** (live step on a lane): the row sits `delivered_at NULL` and drains at the callee's
  next turn boundary — the steer's mechanism (S39), one drain join composing a frame line ABOVE the pure
  words (never woven — B3; AT7 pins the words byte-identical).
- **Callee not running + `wait: true`**: the callee's turn is dispatched (S68 half-split per MB5); the
  CALLER's step row goes `suspended`, a question row names the wait (D16 pair — a wait a person cannot
  see is the failure the repo already outlawed); the answer resumes the caller COLD via
  `loop.CompleteSuspendedStep` — the same primitive `step.continue` uses — and arrives as its own block
  beside the caller's next words (S69's `AppendUserTurn` shape, never inside them). Restart between ask
  and answer replays from rows (AT8's crash test).
- **`wait: false`**: no suspension anywhere; the answer queues back at the caller's next boundary.
- Mode immutable at the ask (no promotion — task.go's own rule; a step whose transcript shape changes on
  timeout has a gap nothing explains). Zero goroutines (AS5): the package stack-dump test extends here.

## MB5 — The callee's turn is the S68 half-split, which is how S68's deviation CLOSES

S68 recorded a deviation: "`step.Skill` (agent-to-agent) carries no chosen lines (S70)." The honest fix
is not to attach chosen skills to agent↔agent turns (there is NO operator in such a turn — S68 B6 makes
chosen skills a per-turn operator act, so there is nothing to resolve); the honest fix is to make the
turn's SHAPE forbid them. System half: the frame names the caller, its lane, and the loop's goal; policy
still speaks last. Transcript half: ONLY the message's words. The frame builder has no channel for skill
lines — the absence is structural, not a forgotten flag (AT11 is a shape test, not a value test).
`step.Skill`'s own blueprint-static path is NOT reframed here (out-list): this slice changes only what
the mailbox dispatches.

## MB6 — The window shows the conversation; the steer is the honesty template

Events `agent.message.posted / .delivered / .answered` (the steer trio's shape, D7-clean payloads); a
new transcript row kind `exchange` rendering `from → to · body` with its own tone and thread grouping.
Why visible when S68/S69 made capability content system-side: those are things the person SENT; this is
a conversation that HAPPENED in their loop — hiding it would make the window blind to real work (the
operator's ruling: like steers, words visible). Undeliverable thread → the honest absence row (naming
thread and target), the ruling-3 discipline from S69. The mapper validates every field like S69's
`receiptList` — a forged row draws nothing.

## MB7 — Reads on the socket; the person reads, never authors

`agent.messages {loopId, thread?}` lists rows — scope from the loop row, never a flag (S69's rule
again). There is NO authoring method on the socket or CLI: the human's door to a loop is steer (S39), and
an agent's door is the tool. `aeon loop messages <id> [--thread N]` is the thin client (P34). Live and
reload read the same rows (S39's rule).

## MB8 — D7 byte-identity

Every new wire field `omitempty`; nil never marshals as `[]`/`{}`; a mailbox-free loop's recorded frames
are byte-identical pre/post (AT15 pins it from captured fixtures).

## Resolved 2026-10-31, by the operator (all Recommended answers)

Entrance (tool), scope (own lanes), thread (rows, one addressee), delivery (TaskMode as-is), window
(visible exchange), callee turn (S68 half-split). The thread-cap self-review catch (lifetime bound, not
active window) is recorded in MB3.

## Open items this design leaves to T1 — mechanical, not product calls

- Exact drain-join site for mailbox rows in the running-callee path (extend `steerDrainText`'s join to
  pull both queues, or a second named join beside it) — whichever keeps ONE composition point per
  boundary and keeps steer text byte-identical.
- Whether the suspend path files its own question row directly or via the same question-row constructor
  board uses — whichever keeps D16's pair in ONE place.
- The `exchange` row kind's exact tone token from the existing palette (no new colors without need).
