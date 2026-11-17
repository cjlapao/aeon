# S75 — the todo that lasts: persisted, shown, rehydrated, nudged

Written 2026-11-07. Stage 5 · Fleet. Depends on S11 (the `todo` tool), S68 (section destinations), S72/S39 (the loop workspace and the agent chat).

## The problem

On 2026-11-07 the brainstormer opened a chat turn with `todo · (2 argument(s))`. The person saw a gear icon and a count of arguments, never the list. Three separate gaps sit behind that line:

1. **The chat cannot show the list.** The tool's result carries the rendered checklist, but the chat draws every tool as a one-line row with its arguments. Nothing reads the list as a list.
2. **The list dies with the step.** `internal/tools/todo.go` keeps the list in a per-toolset closure on purpose (ruling W20: "the transcript is the record, no row of any table is ever written"). A chat is many steps, so the next turn starts with no list, and the model has no memory of it unless it happens to re-read its own transcript.
3. **Nothing keeps the agent honest.** A list the model wrote is a promise it can forget. Nothing reminds it that items are open, and a step can finish `ok` with half the list undone.

The operator's ruling (2026-11-07) reverses W20 for the loop: **a todo list stays active until it is closed, comes back when the chat is opened again, and the system nudges the agent about it.**

## What this slice is

1. **A persisted list per loop.** The list lives in the store, keyed by the loop (a chat conversation is a loop). Every `todo set` writes it. Every result still carries the full render, so the transcript still shows the list at each point in time.
2. **Active until closed.** A list is active while any item is not done. When every item is done it closes on its own and stays as history; the next `set` starts a new list. The person can close it by hand.
3. **Shown in the chat.** The `todo` call renders as a checklist card instead of an argument count, a pinned strip in the loop workspace shows the active list with its progress, and the list loads from the store when the chat is opened, before any new event arrives.
4. **Given back to the agent.** At the start of every step the open list is placed in what the model reads, so it resumes its own plan.
5. **Policed.** The runtime nudges the agent when it has gone several model calls without touching an open list, and refuses to let a step claim success while items are open (twice, then it records the honest outcome).

## What it is not

- It does not add a second tool. `todo` keeps its `set` / `list` actions and its bounds.
- It is not the task or goal system. Those are the organisation's records; this is one loop's working checklist.
- It does not make items assignable to agents or share one list between parallel agents (S32); a `set` still replaces the whole list. Recorded as a follow-up.

## Acceptance

1. A chat turn that calls `todo set` shows the checklist in the chat, with each item's status, not an argument count.
2. Close the app, reopen the chat: the strip shows the same list before the agent does anything.
3. Send a second message: the agent's step begins with the open list in its context (proved by the compiled system text in a test and by the model's next `todo` call continuing it).
4. After the configured number of model calls without a `todo` call and with items open, the transcript shows the runtime's reminder, once per window.
5. A blueprint step whose handoff says `ok` with open items is sent back twice with a reminder; the third time it completes and the outcome says it closed with open items.
6. Marking every item done closes the list; the strip says so; a new `set` opens a new list.
7. With the store seam absent (a hand-built daemon), `todo` works as it does today and its result says the list is not persisted.
