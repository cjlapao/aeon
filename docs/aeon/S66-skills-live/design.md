# S66 design — watching the catalog, telling everyone about it

## The watcher (`internal/skills/watch.go`)

fsnotify (v1.10.1, now a direct dep — `ev.Op.Has(...)` is the v1.10 API)
over `Roots()` plus each existing top-level skill folder. Rules, each
with a test:

- **One refresh per burst.** Every event arms a debounce timer
  (`DefaultWatchDebounce`, 500 ms real time); the fire calls
  `refreshOnWatch`: `Scan(true)`, diff against the previous cache by
  `name:source`, and announce only when the diff is non-empty. A
  refresh that finds nothing announces nothing — an event stream that
  chatters is as untrustworthy as one that is silent.
- **New folders join, dead folders drop.** A `Create` under a watched
  dir that is itself a directory joins the watch (so `mkdir newskill`
  then writes inside it are seen); `Remove`/`Rename` unwatch.
- **Dot trees are ours, not the operator's.** `.staging-*`/`.trash-*`
  live inside the managed root; watching them lets the installer chase
  its own writes. Skipped at watch-time AND they never parse as skills.
- **A baseline exists before the first event.** `StartWatch` scans
  synchronously first; left to the event path, the FIRST real addition
  would be silently consumed as the baseline — the exact silence the
  slice exists to delete (measured in tests before the fix).
- **Honesty travels.** `Scan` hands every caller a shallow copy
  stamped with `Watching`/`WatchError`, so `skills.list`, the CLI line
  and the `skills.changed` payload all state the same watch truth.
  Missing roots at boot ⇒ `Watching:false` + the reason; partial
  blindness ⇒ `Watching:true` + the sentence naming the blind root.

## The seam (`internal/runtime/skills_inject.go`)

`SetSkillsProvider(fn)` — the policy canonicalizer's shape: init-time,
one production call, nil means the compile is byte-identical to its
pre-S66 self. The compile appends the section LAST (the handler's own
ordering note: task first), as `CategoryInjectedContext` (the
two-category vocabulary at `context.go` stays whole). A panicking
provider is NOT recovered — `runstep_test`'s guard says construction
lives outside the panic boundaries; a provider panic is a daemon bug
and dies loudly.

The marker rides `StepRequest.SkillsInjected` → `state.emit` stamps it
onto `step.started` (`runtime.StepSkills{Count,Names,Truncated,Marker}`)
→ `protocol.stepEventPayload` forwards `skills` additively. Frozen
manifests are untouched: the marker is what the operator was TOLD,
carried by the event, not a fact resume must re-derive.

The daemon owns the WORDS (`daemon/skills_wire.go`): one renderer
builds the section text (framing sentence, one-line descriptions,
`read <path>/SKILL.md`), the caps (60 entries / 32 KiB, truncation
spoken), and the single `Marker` sentence. The service's `Scan(false)`
is the only clock; a fault logs `inject skipped…` and injects nothing
(a broken catalog makes no offer, and the step still runs).

## The announcement

`daemon.New` binds `opts.Skills.SetEventEmit(...)` to `table.OnEvent
(protocol.KindSkillsChanged, ev)` — late-bound because the event door
is being built around the service. Payload = `skills.ChangeEvent`
(added/removed/changed names + watching/watchError), daemon-wide (the
catalog belongs to the install, not a project), fixture-pinned.
`serve` starts the watch for the daemon's ctx lifetime and logs the
state ("watching both roots…" / "NOT watching — <reason>").

## The UI

- `fromEvents`: `step.started` with `skills.count>0` → one `injection`
  ChatItem (`Base`, not agent-scoped; the offer belongs to the daemon
  and survives the agent zoom). `ChatStream` renders `InjectionRow` —
  compact, mono, tooltip = the daemon's marker verbatim.
- `useSkills`: one `subscribe {kinds:["skills.changed"]}` per mount;
  every announced diff re-reads the catalog. A daemon without
  subscribe leaves the manual doors working — silence is absence of
  news, not a fault.
- `SkillsPage`: the three watch states rendered (on / blind-with-the-
  daemon's-reason / unknown-on-an-old-daemon), the CLI list line
  mirrors it.
