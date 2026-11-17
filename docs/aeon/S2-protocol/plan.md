# Plan — S2 Protocol & streaming

> **For agentic workers:** written to the `dev-loop-protocol` `plan.md` + `tasks/{taskid}.md` schema.
> Each task's full spec is at `tasks/T{n}.md`.
>
> **Spec:** [`design.md`](design.md) · **Brief:** [`brief.md`](brief.md) · **Demo:** [`DEMO.md`](DEMO.md)

## Understanding

### What we understood

**The protocol is the product boundary**, which is why it is the second slice rather than the ninth. Aeon's
differentiator is one engine serving every surface — start a loop in the CLI, watch it in an editor, answer it
in the app — and that promise is worth exactly what the contract between them is worth.

LSP won because its protocol was *stable*: implement once, keep working. The failure mode is concrete and
common — a VS Code extension pinned to v3 talking to a daemon shipping v5 — so the question this slice answers
is not "what methods do we need". It is **how does this survive fifteen more slices and several years without
breaking a client.**

Three answers, and they are the slice:

- **One integer `protocolVersion`, with a written rule about what is additive.** A semantic version invites
  arguing about what a minor is; an integer plus a rule is checkable.
- **Clients ignore unknown fields.** This is the single property that makes the additive column safe. A client
  that rejects a field it does not recognise turns every one of the next fifteen slices' additions into a
  breaking change — silently, at the client, months later.
- **A committed JSON fixture set as the cross-language contract.** Three languages consume this protocol.
  Generated types would be convenient; the fixtures are what prevents drift, because a generated type can
  still be used wrongly.

Two things are deliberately absent. The **socket transport** is `go-backend-scaffolding`'s (§1a) — this slice
speaks over it and does not build it. And the **method table is nearly empty**: `initialize`, `ping`,
`version`, `subscribe`, `unsubscribe`. Every domain method arrives with the slice that needs it.

One consequence shaped the design. There are no loops, steps or stored events yet, so a subscriber would have
nothing to receive. Rather than build subscriptions blind and first exercise them at S5, **the daemon emits its
own lifecycle events**: a client connecting is a real event, and `aeon watch` in one terminal while another
runs `aeon ping` exercises filtering, delivery and sequencing for real.

### Probes to run and record before decomposing

| # | Probe |
| --- | --- |
| Q1 | **Does the scaffolding's socket transport exist, and what is its interface** — a `net.Conn`, a framed message stream, or a request/response callback? **If it does not exist, this slice is blocked: say so and stop.** Do not implement one |
| Q2 | Does it already speak JSON-RPC, or only frame messages? If the former, consume it rather than adding a library |
| Q3 | How does it expose peer credentials, and what is available on macOS and Windows? **Name an absence** rather than omitting a platform |
| Q4 | Which Go JSON-RPC library — or is a thin layer over the scaffolding's framing simpler? State the choice and the reason |
| Q5 | Does the service lifecycle give a shutdown hook that tears down subscriptions **before** the socket closes? D6 depends on that ordering |
| Q6 | What does S1's accept path already log, so peer credentials extend it rather than duplicate it |
| Q10 | **The full initial error table** — every code, its three parts, its fatal marker |

> Q1 is first for a reason: the honest answer may be "no". A plan that assumes the transport exists blocks
> sixteen slices on a guess, and the assumption would be invisible until someone tried to run the demo.

### How we will do it

**Cross-cutting rules before any method** (R5). The version rule and the error registry are retrofitted at the
cost of revisiting every handler, so they land first — and the error table is enumerated in full rather than
grown a code at a time, because D9 asserts over the whole table and a code added later without its three parts
fails it.

Then the handshake with negotiation **in both directions**. Refusing a client that asks for too much is the
obvious half; serving one that asks for less is the half that actually keeps an old editor extension working,
and it is the one that gets skipped.

Then subscriptions and events as **separate tasks** (R6), because they fail differently: subscriptions are
filtering and lifetime, events are ordering and emission. Merging them merges their test tables and hides
which half is wrong.

Then the fixtures — after the methods exist, before the clients (R8), because they are the contract the
clients are written against. Then the CLI, then the app, then the demo walk.

### What you will have at the end

A protocol a third party could implement against: five methods, filtered subscriptions with a cursor, a
versioning rule that makes fifteen slices of additions safe, an error shape that says what to do, and a
fixture set that keeps three languages honest.

## Objective

Build `internal/protocol` and wire it onto the scaffolding's socket, so every later slice adds methods
additively and every client speaks one contract.

## Tech tags in use

go, react-ts, docs

## Test suite

aeon-protocol

## Tasks

### T1 — Assert the transport, then the version rule and the error registry
- engineer: golang-engineer-loop
- tech: go
- depends_on: []
- est_changed_lines: ~290
- detail: tasks/T1.md
- success_criteria:
  - the scaffolding's transport interface is quoted in this task's notes; **if absent, the task stops and reports the slice blocked**
  - unit test `PROTOCOL_VERSION is an integer and the additive rule is documented beside it` passes
  - unit test `every error code in the registry has what, means, todo and a fatal marker` passes, over the whole table
  - unit test `error codes are unique and none is reused for a second meaning` passes
  - unit test `internal/protocol imports nothing transport-related` passes, asserted by grep
- files_expected: `aeon/internal/protocol/version.go`, `aeon/internal/protocol/errors.go`, `aeon/internal/protocol/types.go`, `aeon/internal/protocol/errors_test.go`

### T2 — `initialize`, `ping`, `version`: the handshake and negotiation both ways
- engineer: golang-engineer-loop
- tech: go
- depends_on: [T1]
- est_changed_lines: ~300
- detail: tasks/T2.md
- success_criteria:
  - unit test `any method before initialize returns -31001 with fatal true` passes
  - unit test `a client needing a higher version is refused, naming both numbers` passes
  - unit test `a client needing an older supported version is served that version` passes
  - unit test `an unknown field in params is ignored` passes
  - unit test `the Go client accepts a result carrying an unknown field` passes
  - unit test `an unknown capability name in the client's expectations is ignored` passes
  - unit test `a malformed frame returns a parse error and the connection stays open` passes
  - unit test `exactly one place decides whether a connection has initialised` passes, asserted by grep
- files_expected: `aeon/internal/protocol/method.go`, `aeon/internal/protocol/handshake.go`, `aeon/internal/daemon/serve.go`, `aeon/internal/protocol/handshake_test.go`

### T3 — Subscriptions: registry, filters, per-connection lifetime
- engineer: golang-engineer-loop
- tech: go
- depends_on: [T2]
- est_changed_lines: ~300
- detail: tasks/T3.md
- success_criteria:
  - unit test `a subscriber scoped to project A receives nothing from project B` passes
  - unit test `a subscriber to loop.* receives loop.started and not step.started` passes
  - unit test `an invalid kinds wildcard is a named error, not a literal match` passes
  - unit test `dropping the connection empties the registry` passes, asserted on the registry
  - unit test `two subscriptions on one connection are independently filtered and tagged` passes
  - unit test `the per-connection and total bounds are enforced and the refusal is reported` passes
  - unit test `there is exactly one subscription registry` passes, asserted by grep
- files_expected: `aeon/internal/protocol/subscribe.go`, `aeon/internal/protocol/filter.go`, `aeon/internal/protocol/subscribe_test.go`

### T4 — Events: kinds, `seq`, the cursor, lifecycle emission
- engineer: golang-engineer-loop
- tech: go
- depends_on: [T3]
- est_changed_lines: ~280
- detail: tasks/T4.md
- success_criteria:
  - unit test `seq is monotonic over a run with no gaps and no duplicates` passes, asserted over a sequence rather than one event
  - unit test `after n replays from n+1 and reports from` passes
  - unit test `connecting a second client produces client.connected on the first client's subscription` passes
  - unit test `daemon.started, client.connected, client.disconnected and subscription.created are all emitted` passes
  - unit test `seq is documented as in-memory and resetting on restart` passes, asserted against the source comment
  - the S4 change that replaces the in-memory counter is named in this task's notes
- files_expected: `aeon/internal/protocol/events.go`, `aeon/internal/daemon/lifecycle.go`, `aeon/internal/protocol/events_test.go`

### T5 — The conformance fixture set
- engineer: golang-engineer-loop
- tech: go
- depends_on: [T4]
- est_changed_lines: ~240
- detail: tasks/T5.md
- success_criteria:
  - every method has at least one request/response fixture pair, committed as JSON
  - unit test `every fixture round-trips through the Go implementation` passes
  - unit test `a fixture containing an unknown field is accepted` passes — D3's insurance
  - unit test `every error code has a fixture` passes
  - the mechanism that keeps fixtures from going stale is named and runs in CI
  - `aeon/internal/protocol/conformance/README.md` states that this directory is the contract the Rust and TypeScript clients test against
- files_expected: `aeon/internal/protocol/conformance/**`, `aeon/internal/protocol/conformance_test.go`

### T6 — The CLI: `ping`, `version`, `watch`, `debug subscriptions`
- engineer: golang-engineer-loop
- tech: go
- depends_on: [T5]
- est_changed_lines: ~290
- detail: tasks/T6.md
- success_criteria:
  - unit test `aeon ping reports a round-trip time` passes
  - unit test `aeon version reports the negotiated protocol version` passes
  - unit test `aeon watch subscribes, prints its subscription, and streams events` passes
  - unit test `aeon watch --kinds filters` passes
  - unit test `aeon watch --after resumes from the cursor` passes
  - unit test `aeon --protocol-version 99 is refused, naming both numbers` passes
  - unit test `aeon debug subscriptions lists the registry` passes
- files_expected: `aeon/cmd/aeon/ping.go`, `aeon/cmd/aeon/watch.go`, `aeon/cmd/aeon/debug.go`, `aeon/internal/protocol/client.go`, `aeon/cmd/aeon/watch_test.go`

### T7 — The app reports the negotiated version
- engineer: react-typescript-engineer-loop
- tech: react-ts
- depends_on: [T6]
- est_changed_lines: ~200
- detail: tasks/T7.md
- success_criteria:
  - unit test `the app reports the negotiated protocol version` passes, against a fake returning an unexpected value
  - unit test `the app shows no protocol number while disconnected` passes
  - unit test `the app performs initialize before any other call` passes
  - the Rust shell is still the only place that touches the socket
  - the app gains no capability beyond the version line
- files_expected: `aeon/ui/src-tauri/src/protocol.rs`, `aeon/ui/src/App.tsx`, `aeon/ui/test/**`

### T8 — Walk the demo, and the QA suite
- engineer: technical-writer-loop
- tech: docs
- depends_on: [T7]
- est_changed_lines: ~180
- detail: tasks/T8.md
- success_criteria:
  - `DEMO.md` is walked end to end on a clean checkout, including its raw `nc -U` steps
  - every mismatch is fixed and this task's notes record whether the code or the demo was wrong
  - `docs/test_suites/aeon-protocol.md` exists, `draft`, ids `QAP2-01` upward
  - the suite covers the handshake by raw socket, negotiation both ways, unknown-field tolerance, filters, the cursor across a reconnect, subscription teardown, and a third-party client speaking the fixtures
- files_expected: `docs/aeon/S2-protocol/DEMO.md`, `docs/test_suites/aeon-protocol.md`

## Priority order

**T1 and T5 are the deliverable**, and neither is a method.

T1 because the version rule and the error registry are cross-cutting: retrofitting either means revisiting
every handler that already exists, and the error table in particular has to be enumerated **in full** rather
than grown one code at a time — D9 asserts over the whole table, so a code added at S9 without its three
parts fails a test written now. T1 also carries the question that can stop the slice: whether the
scaffolding's transport exists at all. That belongs first, before anything is built on the assumption.

T5 because the fixtures are what make this protocol real for anyone who is not us. Aeon's own client and the
daemon can agree with each other while both being wrong about the specification — and the third-party clients
this protocol exists for will not use Aeon's client library. The fixture that contains a field no current code
knows is the smallest and most valuable file in the slice: it is the standing proof that additive evolution
works, and it is the one a future contributor will be tempted to "fix".

T2's second test is the one that matters and the one most likely to be skipped: **serving a client that asks
for an older version.** Refusing a client that asks for too much is obvious and gets written. Downgrading
correctly is what keeps an editor extension working across a daemon upgrade, which is the entire promise of
versioning, and nothing else in the slice exercises it.

T3 and T4 are separate because filtering and ordering fail differently, and because T3's real content is
negative — a subscriber must receive *nothing* from another project. A single daemon serves every project on
the machine, so a filter that silently matches everything looks like it works and quietly ships every
repository's activity to every client.

A plan ordered by visible progress would have written `ping` first, because it is the thing you can run. It
would then have retrofitted the error shape across five handlers and discovered at S9 that no client can be
written against the protocol without reading Aeon's own source.
