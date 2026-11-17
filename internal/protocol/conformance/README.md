# `conformance/` — **this directory is the contract**

**32 fixtures.** Request/response pairs and notifications, as JSON, committed.

## Why these exist rather than generated types

Aeon's own client and daemon can agree with each other while both being wrong about the specification. They
share a codebase, a set of assumptions and an author — so a test of one against the other proves they match,
not that either is right.

**The clients this protocol exists for will not use Aeon's Go client:** a VS Code extension in TypeScript,
the Tauri shell in Rust, and whatever somebody else writes. These files are what those implementations test
against, and the reason they can be written without reading any Go.

Codegen for Rust and TypeScript is deferred (T9), with a named trigger: **a third client, or the first drift
bug.** Generated types are a convenience and can still be used wrongly; a fixture is a fact about the wire.

## Rules for reading them

| | |
| --- | --- |
| **Key order is not significant** | JSON objects are unordered. The test compares semantically, and asserting on byte equality would make every formatting change a failure. |
| **`description` is not part of the wire** | It is the fixture explaining itself. The test ignores it. |
| **Unknown fields must be ignored** | In params *and* in results (T3). See `forward-compat.json`. |
| **Aeon's codes are `-31000` to `-31999`** | JSON-RPC's own `-32768…-32000` stay reserved: `-32601` for an unknown method, `-32602` for bad params, `-32700` for a malformed frame. A transport-level condition keeps its transport code and carries no `data`. |
| **Every Aeon error carries three parts and a fatal marker** | `what` happened, what it `means`, what to `todo`. Error text is the interface when something goes wrong. |

## `forward-compat.json` — **do not delete it**

It carries `somethingFromTheFuture` in a request and `featureWeHaveNotWrittenYet` in a response. Neither is
in any Go type, so **the file looks like a mistake**, and the obvious instinct is to tidy it away.

It is the only standing test of T3, the single property that makes this protocol's evolution safe: *a client
that rejects an unknown field turns every additive change into a breaking one.* Deleting it removes the test
and nothing fails — which is precisely why this paragraph exists.

A client that cannot pass it is not conformant.

## The two `seq` numbers, which is the thing most likely to be got wrong

A notification arrives carrying **two** sequence numbers, and they are not the same thing:

```
{"jsonrpc":"2.0","method":"event","seq":17,          ← the TRANSPORT's, per connection
 "params":{"seq":1421, ...}}                          ← AEON's, per stream
```

| | |
| --- | --- |
| **envelope `seq`** | the transport's per-connection delivery counter. It advances even when a notification is dropped for a slow reader, so a client can detect loss. **It restarts at reconnection.** |
| **`params.seq`** | Aeon's stream-scoped event ordinal. **This is the cursor** `subscribe {after: n}` takes. |

**Use `params.seq`.** The envelope's number restarts exactly when a cursor is needed. The two are
complementary rather than competing: a dropped-notification count says *a gap exists*, and `after` is how
you recover from it.

## What `seq` does not yet survive

**At S2 `seq` is in memory and resets when the daemon restarts.** A client must not treat it as durable
across one. S4 moves it onto the store's `event` table, after which it is monotonic across restarts; the
limitation is recorded in `events.go` beside the counter itself.

## Adding a fixture

Every method and **every error code** needs at least one — a code without a fixture is a code no third-party
client can implement against. `conformance_test.go` fails when one is missing, and that check runs in CI, so
a fixture set nobody regenerates cannot quietly go stale.
