# S67 plan — pickable truth in one hook

| Task | Deliverable | Proves |
| --- | --- | --- |
| T1 | `ComposerChip` + `chips` row in `MessageComposer` (both variants), exported | the receipt row is BASE furniture, so the future home composer inherits it |
| T2 | `useSkillPicker` (rows/chips/panel/weave/`steerWith`/`messageWith`) + the hosts' spread (`LoopWorkspaceView`, `AgentChatPage` ×2) | `/` and the plus menu open the catalog; invalid/shadowed listed-disabled; the send weaves and clears on success |
| T3 | vitest gates at parity + the record | no new red anywhere the slice touched |

## The order and why

1. Row first: its testids pin what every later assertion greps for.
2. Hook second: all catalog meaning (which rows, which chips, which
   sentence) lands where the catalog already lives; hosts are spread
   lines. The hosts and the harness test are written from the same
   spread, so the test cannot drift from production the way the
   first draft's hand-wrapped hosts did.
3. Gates last, with the base measured by stashing the slice, not from
   memory.

## Done means

9 new tests green; suite failures = main's pre-existing set exactly
(12, same files); tsc error list diff to base = empty; every unrun
check named in T3.
