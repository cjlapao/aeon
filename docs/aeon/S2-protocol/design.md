# Design — S2 Protocol & streaming (Stage 1 · Skateboard)

**Date:** 2026-09-06
**Status:** approved (design); not yet implemented
**Stage:** 1 · Skateboard — *it talks to a model, does one piece of work, and remembers*
**Depends on:** S1. **And on the scaffolding's socket transport existing** — assert that, do not assume it.

> Decision ids are prefixed **T** (transport-independent protocol). See the
> [prefix registry](../ARCHITECTURE.md#11-decision-prefix-registry).

---

## Context

**The protocol is the product boundary**, which is why it is the second slice and not the ninth.

Aeon's differentiator is that one engine serves every surface — start a loop in the CLI, watch it in an
editor, answer it in the app — and that promise is only as good as the contract between them. LSP won
because its protocol was *stable*: editor vendors could implement against it once. A protocol changed every
release gives you a VS Code extension pinned to v3 talking to a daemon shipping v5, which is the failure
mode this slice exists to prevent.

So the design question is not "what methods do we need" — it is **how does this evolve for years without
breaking clients**, and everything below follows from that.

Two things are deliberately *not* here. The **socket transport** is `go-backend-scaffolding`'s (§1a): the
listener, the codec, the dispatcher. And the **method table is nearly empty** — `initialize`, `ping`,
`version`, `subscribe`, `unsubscribe`. Every domain method arrives with the slice that needs it, which is the
vertical decomposition applied to the protocol itself.

One consequence shapes the demo. At S2 there are no loops, no steps and no store schema, so there is nothing
for a subscriber to receive. Rather than defer subscriptions to a slice that has domain events, **the daemon
emits its own lifecycle events** (T8) — a client connecting is a real event, and watching one terminal while
another runs `aeon ping` exercises the whole subscription path for real.

## Goal

A versioned JSON-RPC 2.0 protocol over the scaffolding's socket, with subscriptions, that a Go client, a
Rust client and a TypeScript client can all speak — and that can gain methods for the next fifteen slices
without any of them breaking.

## Decisions

| # | Decision | Why |
| --- | --- | --- |
| T1 | **JSON-RPC 2.0**, not a bespoke framing | Client libraries exist in Go, Rust and TypeScript, so a client is a dependency rather than a parser. And it already specifies request/response correlation, an error object and notifications — the three things a bespoke protocol re-derives badly |
| T2 | **One integer `protocolVersion`**, negotiated at `initialize`. Additive changes do not bump it; removing a method or changing a field's meaning does | A semantic version invites arguing about what a minor is. An integer plus a written rule about what counts as additive is checkable |
| T3 | **Clients must ignore unknown fields.** Stated in the protocol document, tested in the conformance suite | It is the single property that makes additive evolution safe, and it is the one clients get wrong silently |
| T4 | **Subscriptions, not server push.** A client subscribes with a filter and receives only what it asked for | One daemon multiplexes every project on the machine (§1a). An editor open on one repository must not receive every event from every other, and push would make it filter traffic it never wanted |
| T5 | **Subscriptions are per-connection and die with it.** No durable subscriptions | A durable subscription is server-held state keyed to a client that may never come back. Reconnection is a cursor's job, not a subscription's |
| T6 | **Every event carries a monotonic `seq`, scoped to its stream, and `subscribe` accepts `after`** | It is what makes "did I miss anything" answerable rather than guessed. The store's `event` table is already ordered and scoped, so the cursor is a key rather than a new mechanism |
| T7 | **`initialize` is mandatory and first.** Any other method before it is an error | Version negotiation must precede anything version-dependent, and a client that skips it is a client that breaks on the next version |
| T8 | **The daemon emits lifecycle events from this slice** — `daemon.started`, `client.connected`, `client.disconnected`, `subscription.created` | Subscriptions can then be built, demonstrated and tested before any domain events exist, instead of being written blind and first exercised at S5 |
| T9 | **Go types are the source of truth; a committed JSON conformance fixture set is the cross-language contract** | Three languages consume this. Generated types would be convenient; the fixtures are what actually prevents drift, because a generated type can still be used wrongly |
| T10 | **Errors are JSON-RPC error objects with Aeon codes in a documented range**, and `error.data` carries the three-part shape: what happened, what it means, what to do | The three-part shape is C9, arriving early because error text is the interface when something goes wrong and retrofitting it means revisiting every method |
| T11 | **The protocol package knows nothing about sockets** | The same method table serves the socket now and, narrowed, HTTP and MCP-stdio at S19. A protocol that imports a listener cannot be mounted twice |
| T12 | **Peer credentials are read at accept and logged** — uid and pid where the platform allows | Filesystem permissions are the control (P26); knowing *which* process connected is what makes an audit line useful |

## Architecture

```
aeon/internal/protocol/          knows nothing about transports (T11)
  version.go       PROTOCOL_VERSION, negotiation, the additive-change rule
  method.go        the method table: name → handler signature
  types.go         request/result types for every method
  errors.go        the Aeon code range and the three-part error data (T10)
  events.go        event kinds, the seq cursor, filters
  subscribe.go     per-connection subscription registry (T5)
  conformance/     committed JSON fixtures — the cross-language contract (T9)

aeon/internal/daemon/            wires protocol onto the scaffolding's transport
  serve.go         accept loop, peer credentials (T12), lifecycle events (T8)
```

### `initialize`, and what negotiation actually decides

```
→ {"jsonrpc":"2.0","id":1,"method":"initialize",
   "params":{"clientName":"aeon-cli","clientVersion":"0.1.0","protocolVersion":1}}

← {"jsonrpc":"2.0","id":1,
   "result":{"protocolVersion":1,"daemonVersion":"0.1.0","capabilities":["subscribe"]}}
```

The client states the version it **needs**; the daemon answers with the version it will **speak**.

- Client needs a version the daemon does not have → **refuse**, naming both. A daemon that silently
  downgrades a client that asked for v5 will fail later, somewhere else, with a confusing error.
- Client needs an older version the daemon still supports → the daemon answers with that older version and
  behaves accordingly. This is the case that keeps an old editor extension working, and it is the whole
  point of negotiation.
- `capabilities` is a list of optional feature names rather than a bitfield or a version implication, so a
  feature can be added without any version change (T2).

### Subscriptions

```
→ subscribe {"filter":{"projectRoot":"/home/me/repo","kinds":["loop.*","step.*"]},"after":1420}
← {"subscriptionId":"s-7f3a","from":1421}

← notification: {"jsonrpc":"2.0","method":"event",
                 "params":{"subscriptionId":"s-7f3a","seq":1421,"kind":"step.started", ...}}

→ unsubscribe {"subscriptionId":"s-7f3a"}
```

**The filter is the point** (T4). `projectRoot` scopes to one repository, `kinds` to what the client renders.
An editor open on one repo asks for that repo; the app's dashboard asks for everything; `aeon watch` asks for
whatever it was told. Without a filter every client receives every event on the machine and discards most of
it, which on a busy machine is most of the traffic.

**`after` is how a reconnecting client finds out what it missed** (T6). It passes the highest `seq` it saw;
the daemon replays from there and says where it started. Nothing about the subscription is durable (T5) — the
durability is in the store's ordered event log, and the cursor is a key into it.

`kinds` accepts a trailing `*` and nothing else. A glob dialect in a filter is a parser and a security
question; a prefix match is neither.

### Versioning, stated as a rule rather than a hope

The rule matters more than the mechanism, so it is written where an implementer will read it:

**Additive — does not bump `protocolVersion`:** a new method; a new **optional** field in params or result;
a new event kind; a new capability; a new error code.

**Breaking — bumps it:** removing or renaming a method or field; making an optional field required; changing
a field's type or meaning; changing an error code's meaning; changing an event's `seq` semantics.

T3 is what makes the additive column safe: **a client that rejects unknown fields turns every additive change
into a breaking one.** So the conformance suite includes a fixture with a field no current client knows, and
a client that fails it is not conformant.

### Errors

JSON-RPC's own codes stay reserved. Aeon uses `-31000` to `-31999`, documented per code, and `error.data`
carries:

```json
{"code":-31004,"message":"no pending question",
 "data":{"what":"loop 'auth-refactor' has no unanswered question",
         "means":"it is running, or it already got its answer",
         "todo":"check `aeon loop show auth-refactor`; a second answer is refused deliberately",
         "fatal":false}}
```

Three parts and a fatal marker (T10, C9). It arrives in S2 rather than S23 because error text is the
interface when something goes wrong, and adding the shape later means revisiting every method that already
returns errors.

## The demo

See [`DEMO.md`](DEMO.md): `aeon ping` round-trips; `aeon version` shows the negotiated protocol version;
`aeon watch` in one terminal shows `client.connected` when another terminal runs `aeon ping`; a client
requesting an unsupported version is refused by name; and the app shows the negotiated version rather than
its own constant.

## Testing

| What | How |
| --- | --- |
| Handshake | `initialize` succeeds; any other method before it errors |
| Negotiation up | a client needing a higher version is refused, naming both |
| Negotiation down | a client needing an older supported version gets it, and behaves accordingly |
| Unknown fields | a request with an unknown field succeeds; a result with one is accepted by the client |
| Subscription filter | a subscriber to project A receives nothing from project B |
| Kind filter | `loop.*` receives `loop.started`, not `step.started` |
| Cursor | `after: n` replays from `n+1` and reports `from` |
| Per-connection lifetime | dropping the connection removes the subscription; the registry returns to empty |
| Two subscriptions, one connection | both receive their own filtered stream, tagged distinctly |
| `seq` monotonic | never repeats, never goes backwards, no gaps within a stream |
| Lifecycle events | connecting a second client produces `client.connected` on the first's subscription |
| Error shape | every Aeon error has all three parts and a fatal marker |
| Conformance fixtures | every fixture round-trips through the Go implementation |
| Transport independence | `internal/protocol` imports nothing transport-related, asserted by grep |
| Peer credentials | the accept log names uid and pid where the platform allows |

## Out of scope

- **The socket transport.** `go-backend-scaffolding`'s (§1a). This slice's first task **asserts it exists**.
- **Any domain method.** No loops, steps, questions or cost. Each arrives with its slice.
- **The HTTP and MCP surfaces.** S19 mounts the narrowed table; T11 is what makes that possible.
- **Generated Rust and TypeScript types.** T9 defers codegen; the fixtures are the contract. The named
  trigger for revisiting: a third client, or the first drift bug.
- **Authentication.** Filesystem permissions (P26). The HTTP surface's API keys are S19.
- **Durable subscriptions or server-side replay buffers.** T5, T6.

## Consequences for later slices

| Slice | Consequence |
| --- | --- |
| S3–S5 | Each adds methods and event kinds **additively**, without touching `protocolVersion` |
| S4 | The store's `event` table is the cursor's backing; `seq` must come from it rather than a counter in memory |
| S9 | The app subscribes with a filter rather than receiving everything — the reason T4 exists |
| S19 | Mounts a narrowed table on HTTP and MCP-stdio; possible only because of T11 |
| all | A slice that bumps `protocolVersion` must say why in its design, against the rule above |

## Open questions

None. Codegen for Rust and TypeScript is deferred with a named trigger rather than left open (T9).
