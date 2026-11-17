# Loop replay: a reopened loop shows everything a live one did

Status: implemented, not yet seen in the running app (started 2026-11-15). Owner: cjlapao. This document is the plan and the record; it is updated as each step lands.

## What was wrong

Six defects were reported from the loop page:

1. The cost only appeared after a page reload.
2. After a loop blocked or finished and was reopened, the step history in the inspector was missing.
3. A reopened chat lost agents, and the user's first message appeared again between the agents' turns.
4. The step rail kept the paused icon after the question was answered.
5. A blocked agent's reason showed on load and vanished after scrolling up and down.
6. Auto-approved calls were labelled "approved by you", the same as calls the person approved.

## The cause

The loop page builds its chat from the daemon's event stream. Every published event is already written to the `event` table, and a subscription from cursor 0 is served from that table. Three facts made that useless for reopening a loop:

- The table could not be read per loop. The `loop_slug` column was always empty (the daemon passed an empty string), so the only way to find one loop's events was to scan the whole daemon's history.
- A replay from cursor 0 returns the oldest 1000 events of the daemon (`DefaultEventLimit`, ordered by id ascending), across every loop, including every streamed text chunk. A loop that ran after the first 1000 events of the daemon's life got none of its events back. The page then kept only the last 1500 events it received.
- When the page fell back to the stored model transcript (`step_message`), that transcript holds the model's messages and nothing else: no costs, no tool timings, no approvals, no agent state, and the dispatch brief of each step looked like a message from the user.

Defects 1 and 6 had their own causes: a step's cost is only stamped when the step ends and the page re-reads the loop only on loop and question events; and an auto-approved call leaves no record of who approved it.

Defect 5 has a code cause in `useLoopWorkspace.ts`: loading older history ends with `merged.slice(0, HISTORY_WINDOW)`, which keeps the oldest turns and drops the newest ones, which is where a blocked reason lives.

## The design

One path for live and reopened: the page feeds both through the same reducer (`chatItemsFromEvents`). History is read from the journal, not rebuilt from the transcript.

### Step 1: the journal can be read per loop

- Migration adds `loop_id` and `step_id` columns to `event`, with an index on `(loop_id, id)`, and backfills them from the payloads already stored.
- The event backing fills those columns for every event that names a loop or a step. A step event resolves its loop through the step row; the result is cached.
- A new store read returns one loop's events in a window, by cursor (the event id), oldest first, with `before` and `after`, a limit, and a kind filter that leaves out `model.delta` by default.
- A new method, `loop.events`, serves that read. It is a read; it changes nothing.
- The subscription replay stops being the way a page gets history: the page reads `loop.events` for the latest window, pages backwards on scroll, and subscribes from the current head for live events.

### Step 2: record the missing details

- Every model turn publishes `step.turn.completed` with the turn's full text and thinking, tokens, cost, route, elapsed time, and the step's and the loop's running cost totals. Streamed `model.delta` rows of a step are deleted once its turn is journaled; the journal keeps the finals.
- Every tool call carries its arguments summary, full result (capped at 256 KB with a `truncated` flag), duration and exact error.
- Every approval is recorded with `approvedBy` (`person`, `auto`, `policy` or `rule`), the rule id and the time, including calls the loop's full access or pre-approved tools let through, which today leave no record.
- Every agent publishes a state change (running, waiting, blocked, idle) with its reason.
- A blocked reason is stored on the `loop.blocked` event, not rebuilt from live state.

### Step 3: the page

- Opening a loop reads the latest window from `loop.events`, then subscribes from the head. Scrolling up reads earlier windows and evicts from the far end only.
- Cost is read from each turn's payload as it arrives. No loop re-read is needed.
- A step's dispatch brief renders as a dispatch divider, never as "you". The person's own message renders once.
- The rail and header follow the loop status from the daemon, which reports `running` while the parked step runs, and per-agent state events.
- The approval chip says "approved by you" for a person and "approved by you, auto" for an automatic approval. Policy and rule approvals say so.

### Step 4: proof

- A replay test records a real loop's events, reopens the loop through `loop.events`, and compares the rendered items with the live ones.
- Loops that ran before the journal was addressable are backfilled by the migration as far as their stored payloads allow; older detail that was never stored is not invented. The page falls back to the transcript for those.

## Decisions taken

- Streamed chunks stay in the event table (it is append-only, so nothing is pruned), but `loop.events` leaves them out by default and every finished turn carries its whole text and thinking. A replay shows the finished text, not its typing.
- One tool result in the journal is capped at 32 KB with a flag saying it was cut (revised from 256 KB: the whole result is already in the step's transcript, so the journal copy only needs to be enough to show the call). The cap is `streamDetailCap` in `internal/runtime/loop.go`.

## Progress

Filled in as each step lands, with measured results.

### Step 1: done (2026-11-15)

- Migration `20261115150000_event_journal.sql`: `event.loop_id`, `event.step_id`, a backfill from stored payloads (a loop event by its `loopId`, a step event through the step's own row), and `idx_event_loop`.
- `EventBacking.Append` files every event under its loop and step; the step-to-loop lookup is cached per step.
- `store.ReadLoopEvents` reads one loop's journal as a window (latest, before, after), with a kind filter and an exclusion list. `loop.events` serves it, with `head` for the live join. Contract regenerated, fixture `conformance/loop-events/window.json`.
- Tests: window reads and the backfill (`internal/store/events_loop_test.go`), and attribution through the daemon's backing (`internal/daemon/event_journal_test.go`). Migration pins bumped to 66 files and 111 total, goldens regenerated.
- Finding that changes step 2: the event table is documented append-only (no update, no delete). Pruning streamed chunks after a turn would break that rule, so step 2 keeps the chunks and instead stops writing them when a turn is journaled with its full text, or caps the table through the retention path the policy events already use. The choice is recorded in step 2 below when made.

### Step 2: done (2026-11-15)

- New event `step.turn.completed`, emitted after every model call (also a failed one): whole text, whole thinking, usage, the turn's cost, the step's running cost, wall time, stop reason, tool-call count and route. The cost counter and a reopened chat both read it.
- `step.tool.end` carries `durationMs` (measured by the runtime between the call's own start and end events, so every end path reports it the same way) and `detail`, the call's result content for every tool, capped at 32 KB with `detailTruncated`.
- `step.policy.decided` carries `approvedBy`: `person` (the person answered the question), `auto` (full access or a pre-approved tool for the loop), `policy` (a rule allowed it outright). `question.answered` carries `answeredBy` and the question kind.
- The blocked reason was already on `loop.blocked`; it is in the journal now that the journal can be read.
- Finding: the step transcript is rewritten at every checkpoint (`DELETE` then `INSERT` of `step_message`), so every message's `created_at` is the time of the last checkpoint, not the time the message happened. That is why timings read from the transcript were wrong. The journal's events carry their own times; the transcript fallback is kept only for loops that ran before the journal.
- Tests: `internal/runtime/replay_events_test.go`, event fixtures `step-turn-completed.json` and `step-policy-decided-auto.json`, the daemon's event-order test and the exec golden updated.

### Step 3: done (2026-11-15)

- `useLoopWorkspace` reads `loop.events` for the latest window, subscribes from the daemon's head, and pages older windows without evicting anything. A daemon without the method falls back to the old way.
- The reducer draws `step.turn.completed` (once, never beside streamed chunks), a call's duration and result, and the approver. The chip says "approved by you" for a person, "approved by you, auto" for a standing pre-approval, "approved by a client" for a client; a rule allowing a call and a decline get no approval chip.
- Cost: the daemon saves the step's cost at every checkpoint, and the page re-reads the loop on each `step.turn.completed` and `step.done`, so the counter moves with every response.
- The loop's status is read from the events as well as the saved row: waiting stays waiting while a question is owed; once every question is answered and the agent has acted, it is running. The rail and header follow it. (The daemon's `loop.show` does the same for any client.)
- A step's brief renders as a quiet "Brief for <agent>" row under the dispatch divider, never as "you". The person's own message is the `loop.message.posted` event and renders once.
- The old transcript window no longer drops its newest turns when older ones load, which was the cause of the blocked reason disappearing on scroll.
- A loop that ran before the journal recorded turns keeps its transcript fallback; the journal's call rows for such a step are left out so a call is not drawn twice.

### Step 4: done (2026-11-15)

- `test/agent-chat-adapters.test.ts` ("replay equals live") feeds one loop's whole story to the reducer twice, once as the live stream with chunks and once as the journal without them, and requires the same visible chat.
- `test/loop-workspace.test.tsx` covers the hook: the journal read, the subscription from its head, the fallback on an older daemon, and the running status after an answered question.

### Known limits

- Per-agent state (running, waiting, blocked) is derived from the replayed events by the same reducer as live, not stored as its own event; a state the events do not show is not shown.
- A loop that ran partly before this change shows its older steps from the transcript (words and calls) and its newer steps from the journal.
- The event table keeps the streamed chunks forever (append-only). Its growth has not been measured on a long loop.
- Not seen in the running app yet: the daemon must be rebuilt and a loop run to see the new events.

## Step summaries (`loop.steps`)

The journal answers "what happened", but rebuilding every step's totals from it meant reading thousands of events, so
a reopened long loop knew only its newest steps. The daemon now keeps one row per step in `step_summary`: model turns,
tool calls and failures, tokens in/out/thinking, model time, and the step's cost with its state (`known`, `partial`
or `unknown`, never a bare zero). The row is updated by `Store.AppendEvent` as the step's `loop.step.dispatched`,
`step.turn.completed` and `step.tool.end` events are written; migration `20261116100000_step_summary.sql` backfills it
from the events already stored. The step's agent and status are not copied into the row; `loop.steps` joins them from
the step row, so a summary cannot disagree with the step about its own state.

- `loop.steps {id, before?, limit?}` returns the newest `limit` (default 50) summaries, newest first, with `hasOlder`;
  `before` is a step id and pages older steps. The loop's totals ride the first page only.
- `loop.events` accepts `stepId`, so one step's calls are one indexed read.
- The page holds the newest window and pages older steps on demand; the rail's per-step counts, the agents list and the
  inspector's "Work done" section read from the summaries. A step's tool calls are read from the journal narrowed to
  that step the first time its lane is opened.
