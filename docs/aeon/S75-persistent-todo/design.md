# S75 — design

Rules are numbered TD1… so tasks can cite them.

## 1. What exists today (read 2026-11-07)

- `internal/tools/todo.go`: `Todo()` returns a tool whose list lives in a closure per `Toolset` build, so it lives one step. A package guard (`todo_test.go`, an AST walk) forbids package variables and any import that could reach the store: W20. Items are `{id, content, status}`, status `pending | in_progress | done`, bounded at 200 items, 256 B per id, 4 KiB per content, 256 KiB per call. `set` replaces the whole list; every result renders the full list plus a counts line.
- The tool is registered but not in the default grant; an agent's handler names it.
- The chat (`components/agent-chat`) draws a tool as `kind: "tool"` with the call's summary; there is no case for a list.
- The runtime already has a place that appends a user turn between model calls (`MailboxDrain`, `loop.go` ~line 322) and a place that decides a step's outcome (`complete`, `blocked`, `failed`).
- Tools reach daemon state only through init-time seams (`SetSearchResolver`, the graph service); `internal/tools` must not import the store.

## 2. The rules

- **TD1 The list is the loop's, in the store.** Table `loop_todo` (one row per item: loop, component root, position, item id, content, status, updated at, last step) and `loop_todo_list` (loop, revision, opened at, closed at, closed by). One active list per loop; closed lists stay as history and cascade away with the loop. The migration is additive.
- **TD2 The tool keeps its shape and gains a seam.** `tools.SetTodoStore(TodoStore)` (an init-time func-seam, the `SetSearchResolver` shape) with `Load(loop) → items` and `Replace(loop, step, items) → revision`. The tool loads the active list at its first call in a step, applies `set`/`list`, writes through the seam, and still returns the full render. W20's AST guard is amended by name: the store is reached only through the interface, the package still imports no store and holds no package variable.
- **TD3 Unwired is said.** With no seam the tool behaves exactly as today and its result SUMMARY (what the chat shows; the transcript render stays byte-identical) ends with the fixed sentence `(not persisted: this daemon keeps the list for this step only)`. Absent is not zero (P31).
- **TD4 Active until closed.** A list is active while it has at least one item that is not `done`. When a `set` leaves every item `done` the list closes (`closed_by: "all-done"`) and the result says so. A `set` on a loop with no active list opens a new one. An empty `set` (the documented way to clear) closes it (`closed_by: "cleared"`). The person closes by hand (`closed_by: "person"`, TD8).
- **TD5 One event.** `todo.updated {loop, step, revision, items, counts, active, closedBy?}` on every write and every close, so a live client never parses a tool result to learn the list. It is a protocol-notified kind and a replayable durable view, like the other step events.
- **TD6 One read.** `loop.todos {loop}` answers `{active: bool, revision, items, counts, closedAt?, closedBy?}` for the active list or, when none is active, the most recent closed one with `active: false`; a loop that never had a list answers `{active: false, items: []}` with a statement, never an error. `aeon loop todos <loop>` is its CLI twin.
- **TD7 Given back to the agent.** When a step is compiled for a loop with an active list, the compile appends a section `Open todo list` to the SYSTEM half (instruction, S68 B2), rendered with the tool's own renderer so the model sees the form it writes. The section ends with one fixed sentence naming the rule: update the list as you work, mark items done when they are, say so if you drop one. The `step.started` note says `todo: 3 open of 5`.
- **TD8 The person closes it.** `loop.todos.close {loop}` is a human-door write (the person at the endpoint, not an agent), refused for an agent actor; it closes the active list with `closed_by: "person"` and emits `todo.updated`. An agent can only close by completing or clearing.
- **TD9 The nudge.** The runtime counts model calls since the last `todo` call in this step (or since the step began). When an active list has an open item and the count reaches `todo.nudge_after` (default 8, settable, 0 turns the nudge off), it appends one user turn with the fixed sentence `Reminder: your todo list has N open items (…ids). Update it, or say why you are leaving them.` and resets the count, so the nudge repeats at most once per window. It emits the step event `step.todo.nudged {reason, open ids}` so the chat can show it as a divider, never as the person's words.
- **TD10 The completion guard.** When a step's handoff arrives as `ok` (or `done`) and the loop has an active list with open items, the runtime does not complete the step the first two times: it appends the TD9 reminder worded for completion and calls the model again. The third time it completes, with the outcome kept and the fact recorded as a `step.todo.nudged` event with reason `closed-with-open`. A step that ends `failed` or `blocked` is not guarded (an honest stop needs no reminder). Lanes of type `converse` (the chat turn) are exempt: a chat turn ends every time it answers and the list is meant to outlive it.
- **TD11 Bounds are inherited.** 200 items, 256 B ids, 4 KiB content, 256 KiB per call. The nudge and the reminder sentences are fixed strings in one place, so the transcript's reminder is greppable.
- **TD12 Whole-list replace stays.** `set` replaces the list; the store keeps the revision so a concurrent writer is detected (`Replace` takes the revision it read and refuses a stale one, naming it). Two parallel agents writing one loop's list is S32's to solve; this slice records the refusal and does nothing cleverer.

## 3. The UI

- **TD13 The panel, not a card.** The list is the chat's own To-dos panel: the `taskList` slot `AgentChat` already owns above the composer, with the daemon's `done` shown as the panel's `completed`. No card in the stream and no new strip. (First built as a tool-row card and a pinned strip; replaced the same day on the operator's correction.)
- **TD14 Closing and resting.** While the list is active the panel heading carries the person's Close list (`loop.todos.close`). A closed list stays in the panel, saying `closed · <closer>`, until a new list replaces it.
- **TD15 Loaded, not replayed.** Opening a loop asks `loop.todos` once and then follows `todo.updated`. The strip never waits for a new tool call to know the list exists.
- **TD16 The nudge is a divider.** `todo.nudged` draws as a neutral divider ("reminder sent: 3 todo items open"), distinct from a person message and from the agent's text.

## 4. Questions answered by default (change them by saying so)

- **Q1** The completion guard exempts `converse` lanes. Recommendation taken: yes, exempt.
- **Q2** Nudge default is 8 model calls. Recommendation taken; the setting exists.
- **Q3** Only the person closes by hand; an agent cannot close another's list. Taken.
