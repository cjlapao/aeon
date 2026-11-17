# NARROW — the narrow surface, and what it will never do

Aeon has **two transports and two surfaces** (P25). The daemon's socket carries the
full protocol, guarded by filesystem permissions on a `0600` path with peer
credentials (P26) — that is the human surface. The **narrow surface** is an
enumerated projection of it, mounted twice and identically: **stdio for editors**
(`aeon mcp serve`) and **HTTP for CI** (`aeon serve --http <address> --http-key-file <path>`).
One table decides both; there are no mounting-specific actions (N18).

This document is the human-side twin of the statement the surface carries itself
(`internal/narrow/boundary.go`), which every tool listing shows. If the two ever
disagree, the code's rendering is the truth and this file is the lagging mirror.

## The table

Actions are read-only except one. Each **projects** a socket method (N13 — derived,
never parallel: `actions_test.go` asserts each one against S2's own
`PublicMethods()`), and each states what it **cannot** do (N12 — enforced, not
decorative):

| Action | Projects | Kind | What it cannot do |
| --- | --- | --- | --- |
| `aeon_loop_list` | `loop.list` | read | reveal loops the daemon's access control would hide; return credentials; enumerate callers |
| `aeon_loop_show` | `loop.show` | read | expose the transcript; resolve a question the loop is asking; overstate cost |
| `aeon_step_show` | `step.show` | read | run, continue or steer the step; hide dropped thinking |
| `aeon_blueprint_validate` | `blueprint.validate` | read | save or alter a blueprint; start a loop with what it validates |
| `aeon_questions_pending` | `store.awaiting` | read | **answer** a question; **create** a question; say whom to ask |
| `aeon_questions_open` | `question.pending` | read | **answer** the question it shows; **create or alter** a question; choose whom to ask or notify anyone |
| `aeon_loop_start` | `loop.start` | trigger | **steer, pause, resume, retry or stop the loop it starts** — the trigger is one-way; answer gates; grant outward beyond the start request itself |

The fixture `internal/narrow/testdata/actions.json` must match the table byte-for-byte,
so widening the surface is always a visible diff (O7).

## What is absent — permanently

```
aeon has no operational control actions, by design.

  Available:  aeon_loop_start (trigger)
              aeon_loop_list, aeon_loop_show, aeon_step_show, aeon_blueprint_validate,
              aeon_questions_pending, aeon_questions_open
  ABSENT:     stop, kill, pause, resume, retry, answer, abort, cancel — not restricted, absent.

  Reason: handing an agent the means to start and stop loops is handing it a way
  around every bound placed on one.

  Instead: stop or answer a loop from the app, or over the daemon's socket
  (`step.stop`, `loop.answer`) — the socket is the human surface: filesystem
  permissions on a 0600 path, peer credentials (P26). This surface is not.
```

Two refusals are **permanent, not deferred** (plan.md):

- **No operational control — ever** (R6). Not behind a flag, not with a stronger
  key, not for a trusted caller. It is enforced by enumeration
  (`ForbiddenOperationalControl` as a token set over names and projected methods,
  plus one pinned trigger), so the synonym nobody thought to forbid fails the same
  door. The absence is *legible* (N14): the statement rides the tool listing, so a
  client finds the reason plus the alternative instead of an "unknown tool" that
  reads like a version mismatch.
- **No answering questions — ever** (N16). A gate exists to reach a human; an API
  key authenticates a caller and does not make it one. S11 set the precedent: an
  unattended loop *denies* an ask rather than auto-answering it. Reading is the
  point: `aeon_questions_pending` names the loops that wait, `aeon_questions_open`
  shows the question itself — its id, kind, prompt and declared answer set — so a
  monitor can report WHY a loop stopped; resolution happens where a human is. A
  keyed attempt at the obvious URL is a 404 carrying that reason and the
  alternative (`aeon answer <id> <value>`), on HTTP and inside the stdio tool
  error alike. The kind is visible to the MONITOR (the person deciding whether to
  go and answer); U14's rule — never distinguish the asker TO the person answering
  — is untouched, because the audience is different.

There **is** no `aeon loop stop` command — the terminal alternative the first draft
of this text promised was never built (S19 plan.md F-1). The app stops loops
(*StepScreen* / *ChatView*, over the socket); so can you, with any socket client.

## What a key is, and is not

The socket's access control is a filesystem mode on a path a port cannot carry, so
HTTP requires a key: **out of band only** — a file (`--http-key-file`) or
`AEON_HTTP_KEY`, never a flag value, never logged, constant-time compared. An
address without a key is a refusal to start, not a warning and never a generated
default (N15). Nothing binds unless someone typed an address.

A key buys the right to call the table above. It buys nothing beyond it: no key
level grants outward effects, no key answers gates, no key becomes a human.

## What a CI trigger is, and is not

`POST /v1/loops` (and the `aeon_loop_start` tool — one transform function behind
both, grep-pinned so neither mounting owns a private copy) **pins every triggered
loop unattended**: `attended` is forced to `false` in the dispatch no matter what
the body claimed. A CI job is not present in the sense a gate needs, and polling
every ten seconds does not make it so — attendedness is a declaration about a
human, and the transport cannot attest to one. The pin is *written*, not omitted:
the row says `0`, not `NULL`, so the never-declared third state cannot drift into
being the trigger's default.

The one outward lever is `allowOutward` — **in the start body, per loop, at the
one moment someone is deciding what THIS run may do.** It lifts `outward` for an
unattended loop and nothing else: `irreversible` keeps S16's ask (unattended →
deny at once), and when a loop that declared the flag reaches a merge anyway, the
block reason says the flag did not lift it. There is deliberately no field here
reaching merging, and no standing version of the grant anywhere — not on the key,
not on a caller, not in configuration — because a permanent grant is a grant whose
theft is unbounded. `TestNoKeyLevelGrantExists` reflects and greps for exactly
that absence.

## Where the guarantees live

| Property | Pinned by |
| --- | --- |
| No operational control, by enumeration over poisoned tables | `TestNoActionMatchesOperationalControl` |
| Every projection exists in S2's real table | `TestEverySocketMethodExists` |
| Every entry states its narrowing | `TestEveryEntryStatesWhatItCannotDo` |
| The table is the only registration point | `TestActionsIsTheOnlyRegistrationPoint` |
| The boundary redirects, names real doors only | `TestBoundaryNamesWhatToUseInstead` |
| Two mountings, one set — both directions | `TestStdioAndHTTPSetsAreEqual` (T4) |
| Trigger is one-way | `TestTriggerIsOneWay` |
| Questions never answerable — enumeration over poisoned tables, all three surfaces | `TestNoActionAnswersAQuestion` (T5) |
| Questions never creatable — same net, provenance not control (N6) | `TestNoActionCreatesAQuestion` (T5) |
| Both mountings refuse answering, separately asserted | `TestBothMountingsAgree`, `TestKeyedCallerCannotAnswerAGate` (T5) |
| The permanence is recorded where the flag-hunter looks | `TestPermanenceIsRecorded` (T5) |
| Nothing binds by default — asserted by connecting | `TestNoPortWithoutAnExplicitAddress` (T4) |
| Triggered loops pinned unattended — dispatch-side, both mountings separately | `TestHTTPTriggerPinsUnattended`, `TestStdioTriggerPinsUnattended` (T6) |
| One trigger transform, no per-mounting copy | `TestTriggerTransformIsOneDefinition` (T6) |
| No key-level or standing outward grant exists | `TestNoKeyLevelGrantExists` (T6) |
| Outward denies **driven end to end**, not by reading a policy table | `TestOutwardDeniesEndToEnd`, `TestOutwardRunsWhenOptedIn` (T6) |
| The opt-in is per loop — one key, two loops, two outcomes | `TestAllowOutwardIsPerLoop` (T6) |
| `allowOutward` does not grant `irreversible`, and says so in the block | `TestIrreversibleDeniesToo` (T6) |
| Polling does not attend a loop; the pin is data | `TestTriggeredLoopIsUnattended`, `TestAttendednessCannotBeInferredFromPolling` (T6) |
| Four error kinds, four codes; `refused` ≠ `not-found` ≠ transport | `TestFourErrorKindsAreDistinct`, `TestAbsentActionIs404NotRefused`, `TestRestartingIs503` (T7) |
| Daemon errors classified by TYPE, not by string resemblance | `TestDaemonErrorsAreClassifiedByType` (T7) |
| Every handler timed — enumerated over the mux, not sampled | `TestEveryHandlerHasATimeout` (T7) |
| A stalled writer is hijacked and closed; goroutines return to baseline | `TestAbandonedConnectionIsReaped` (T7) |
| The bound is for abandoned readers — a slow-but-alive reader gets its bytes | `TestSlowReaderStillGetsItsBytes` (T7) |
| A hung stdio caller never makes EOF unreturnable | `TestHungStdioCallerDoesNotPreventShutdown` (T7) |
| A read resolves within THIS daemon's store; the surface holds no data source | `TestReadsAreScopedToThisDaemon` (T7) |
| No error body carries the key — asserted on every path | `TestNoErrorBodyCarriesAKey` (T7) |
| A slow AND an abandoned caller leave loop timing unchanged (live) | `TestSlowCallerNeverDelaysALoop`, `TestNoHandlerHoldsAStoreLock` (T7) |

## S58 (2026-11-03): the inbox is not on this surface

S58's attention plane — the inbox (`inbox.ack · inbox.snooze · inbox.unack`),
the class table, the notification channels and the subscription — is **not**
on the narrow surface and never will be: acking, snoozing, or opening an
outbound receiver is exactly the operational decision (SV13) the narrow
surface exists to be unable to make (SV-D3 keeps the action set at nine;
`TestSupervisionWritesNeverReachOtherDoors` walks narrow, agent services, the
tool registry, the records catalog, the source text, and a step with every
tool granted, and each negative has a canary).
