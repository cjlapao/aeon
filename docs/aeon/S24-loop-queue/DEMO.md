# Demo — S24 Loop queue (walked 2026-10-29)

Walked for real against two **live daemons** on a throwaway `XDG_STATE_HOME`
(`/tmp/s24t9*`, never the real store — AGENTS §6 made executable), the binary
built from this checkout with the `sqlite_fts5` tag, components registered
through the app's own door (`component.create` over the socket — there is no
CLI verb for it, and the walk used the door the app uses). Raw socket calls
went over plain JSON-RPC frames after `initialize`, the socat form of §6 in
python. (The predictive version of this file — written before the slice, per
§4a — is kept in git history; where it was wrong is exactly the Findings
table's content.)

**The model lane, named (§4b requires it):** NO provider was configured —
a throwaway state has no routes. That turned the walk into the *failure*
walk it should have been: materialized loops ran far enough to reach their
first step, the engine blocked them (`aeon loop show` → `two-step@2 blocked,
survey blocked`), and every one of the queue's halt-and-resolve behaviours
then had to hold under real terminal events, not scripted ones. Success
starts and completed sizing are pinned by the daemon gates (T3/T4/T5 use
in-process lanes) and are named below as **not shown here**.

## The fixture

* `repoA` — component `ledger-a`, brand new: zero queue items ever (the
  cold-start case CG4 names).
* `repoB` — component `ledger-b`, with a prior `loop start` loop (warmup),
  which the lane-less engine blocked — the walk did not need its success.

## The walk (quoted output, real run)

**1. Settings, before anyone decided anything.**

```
$ aeon queue settings --component /tmp/s24t9b/repoA
queue_advance=ask · sizing_mode=manual · component=/tmp/s24t9b/repoA
$ aeon queue settings --component /tmp/s24t9b/ghost
queue_advance=ask · sizing_mode=manual · component=/tmp/s24t9b/ghost · UNREGISTERED — no component owns this root; these are the slice's declared defaults, never stored facts
```

The registered read carries the defaults as **stored column facts** (the
0041 DEFAULTs); the unregistered root reads the SAME pair and says it is
declaring, not remembering (P31's third state — the prediction had no such
state, F-3).

**2. Append on the cold start.**

```
$ aeon queue append --component … --blueprint two-step --goal "first pass" --tech go
1 queued · position 1 · pending · two-step
appending started nothing — queue.advance (or the auto chain on a terminal) is the explicit door (CG4)
$ aeon queue list --component …
#1 · pos 1 · pending    · two-step · first pass · tech=go
```

Nothing started — the daemon noticed nothing, because there is nothing to
notice (CG4, observed, not assumed).

**3. Size, manually: the fact is a range plus its basis (D8).**

```
$ aeon queue set-size 1 --min 1 --max 3 --basis "one handler, one door"
#1 · ready · size 1–3 (one handler, one door)
```

**4. The explicit door (CG4) — there is no CLI verb; the socket IS the door (F-5).**

```
→ queue.advance {"componentRoot":"/tmp/s24t9b/repoA"}
{"jsonrpc":"2.0","id":1,"result":{"started":true}}
# later: #1 · pos 1 · blocked · two-step · first intent · size 1–3 (one handler, one door) · loop=loop-dmhhkcpai7vp
```

`started: true`, the row became `running` and gained a `loop_slug`
(`loop-dmhhkcpai7vp`) through the ordinary `loop.start` door; then the
lane-less engine blocked the loop and the reactor moved the item to
`blocked` — and raised the question in the **existing** surface:

```
sqlite> SELECT kind, body, options FROM question;
gate | queued item "first intent" is blocked — the loop reported itself blocked; the queue is halted until a person says otherwise (cancel the item to let the next one run) | ["advance","hold"]
```

**5. CG2 — any `blocked` halts, unconditionally. This step found the slice's bug (F-1).**

With `#1 blocked` and `#2 ready` (sized `2–5`), queue-advance `auto`:

```
BEFORE (unfixed binary):  → queue.advance {"started":true}   — item 2 STARTED OVER THE BLOCK
AFTER  (fixed):           → queue.advance {"started":false}  — and #2 stays ready
```

The prediction (step 7) had blocked-halt as a *ceiling-exhaustion* case
never actually forced; the walk reached `blocked` honestly and the explicit
door walked straight past it — `NextReadyQueueItem` carried only the D2
one-running clause, and no advancer ever asked about `blocked`. The fix is
one `NOT EXISTS (… status = 'blocked')` in each of the two selection doors
(`NextReadyQueueItem`, `FirstPendingQueueItem` — the sizing dispatch halts
with the queue), so all four callers — the explicit door, the auto chain,
the answered-ask chain, the sizing chain — inherit the halt from the one
read that answers "what may start". Pinned by
`TestTheSelectionDoorsHaltOnAnyBlocked` (store) and
`TestQueueAdvanceDoorHaltsOnAnyBlocked` (the seam), each with its positive
control.

**6. Resolve by cancel, never delete (D12) — and the door opens again.**

```
$ aeon queue remove 1
#1 · cancelled — removed means cancelled, never deleted (D12): the row stays and `list` still shows it
→ queue.advance {"started":true}        # the halt was real; its resolution is too
#1 · pos 1 · cancelled · two-step · first intent · size 1–3 (one handler, one door) · loop=loop-dmhhkcpai7vp
#2 · pos 2 · blocked   · two-step · second intent · size 2–5 (two doors, one survey) · loop=loop-dmhhknov8lqi
```

The cancelled row keeps its size, its slug and its place on the board —
`list` still shows it, which is the point (D12). Item 2 then blocked the
same honest way and raised its own `gate` question (second row above).

**7. Skill mode: the hand-written size is refused BY NAME (D9).**

```
$ aeon queue settings --component … --sizing-mode skill
queue_advance=auto · sizing_mode=skill · component=/tmp/s24t9b/repoA
$ aeon queue append --component … --blueprint two-step --goal "unsized third"
3 queued · position 3 · pending · two-step
$ aeon queue set-size 3 --min 1 --max 1 --basis "hand-written"
aeon: the queue refused the act  (-31700)
  what   /tmp/s24t9b/repoA sizes by skill: report_size is the only writer of size (D9) — a hand-written size for item 3 is refused by name
  means  nothing moved — the row keeps the state it had
```

The refusal is named, three-part, and the row kept its state (predicted
byte-for-byte). It shows `pending`, not `sizing` — the prediction had it
`sizing` at append (F-4): `sizing` begins when the door **dispatches** the
sizing run; appending declares, it does not begin. And with `#2` blocked,
the walk's own fix is what kept that sizing dispatch from starting (step 5's
`FirstPendingQueueItem` clause, exercised by the live board, not only by the
gate).

## Findings

| # | Prediction | What the walk found | Disposition |
| --- | --- | --- | --- |
| F-1 | Any `blocked` halts the queue (CG2, step 7) | The explicit `queue.advance` door **started the next ready item over a blocked row** — the halt existed nowhere in the selection read; D2's one-running clause was its only `NOT EXISTS` | Fixed at the store door (both selection reads), pinned by 2 new gates with positive controls, re-walked live (`started:false`, then `started:true` after the D12 cancel) |
| F-2 | Step 3: `advance` → running and "a real loop starts" | No provider in the throwaway state: the loop DID start (slug, `running`) and the engine blocked it at the first step within seconds | Honest — the failure path walked with real terminal events; success starts named not-shown (T3 gates are the witness) |
| F-3 | Step 1: defaults "read back exactly as written at project creation" | The settings are component columns with SQL DEFAULTs (0041); an **unregistered** root answers the same pair with an UNREGISTERED banner — a third state the prediction didn't know (P31) | Recorded; the walk quotes both states |
| F-4 | Step 6: an item appended under `skill` "shows `sizing`, not `pending`" | It shows `pending`; `sizing` begins when the advance door **dispatches** the sizing run — append declares, it does not begin (D7) | Recorded; the status machine (T2) is right, the prediction was wrong |
| F-5 | Step 3: "`aeon queue advance` (or the CLI's explicit equivalent)" | There is **no CLI verb** — by the CLI's own spec (T6 lists six verbs); the socket verb is the door, the bench page has its button | Recorded; the walk spoke raw JSON-RPC frames |
| F-6 | (appendix) unregistered root | `queue append` at a root no component owns refuses with a raw store sentence: `store: append queue item <root>: FOREIGN KEY constraint failed` inside `Data.what` — it names the act and the root, but it is SQL, not the house's named refusal | Recorded, not softened; a named pre-check at the Append door is a candidate follow-up (T2's surface) |
| F-7 | Step 4's answered-ask chain (question → `approve` → materialize) | Not reachable without a lane (needs a loop reaching a terminal), and no CLI verb lists questions (`aeon answer STEP_ID ANSWER` answers, the app lists) | Named **not shown**; the daemon's answered-ask gate (T4) is the witness |

## What this walk does not show

* **A successful loop to `done`** (and so step 4/5's `approve`→materialize
  leg): no provider was configured, by design (§5's tests-never-real rule);
  the reactor's done-transition and the ask-chain gates are the witnesses.
* **A completed sizing run**: `report_size` landing a range end-to-end is
  T5's E2E gate with its recorded lane; the walk showed the append-time
  state, the dispatch halt (F-1's clause) and the named refusal (D9).
* **The UI half** (`QueuePage.tsx`): headless here — no Tauri system
  libraries, the same note S11's record carries forward. The 14 vitest gates
  (T8, clean-tree-verified) are the witness.
* **LoopCeiling-forced blocking** (the prediction's route to `blocked`):
  unnecessary — the lane-less engine produced genuine `blocked` loops
  naturally, so the number "10" never had to be manufactured.
