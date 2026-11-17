# S41 — the remote workspace listener (deferred)

This note exists only because this piece is explicitly **not** being built now. The app-side
work it's deferred from — a `Local`/`Remote` workspace concept in the app, switching between
daemons, a Rust-side TCP client — is being implemented directly, without a separate design
document, per standing guidance that in-progress work doesn't need written planning unless
asked. This note is that exception: a record of what was deferred and why, for whoever picks it
up.

## What's deferred

A daemon-side network listener that projects the **entire** JSON-RPC method table over the
network — full parity with the unix socket, not S19's eight-action enumerated surface
(`internal/narrow/actions.go`). The app's Rust shell gets a TCP client built against this
contract now, so it's ready the moment the listener exists, but nothing on the daemon side
answers it yet, and there is no live way to test it end-to-end until this slice is built.

## Why it's its own slice, not a quick addition

S19's own design treats an enumerated, reviewed action table as a safety boundary — each entry
states what it *cannot* do. A full-parity listener deliberately gives that up: every method
`localrpc` serves, including writes S19's authors kept off the narrow surface on purpose,
becomes reachable over the network, protected only by whatever auth this slice designs. That's
a materially different security posture from anything built so far, and it deserves the same
kind of reviewed, task-by-task treatment S19 itself got (eight tasks), not a bolt-on to the app
work.

## Assumed wire contract

Stated so the app's Rust-side TCP client has a concrete target to code against. These are
assumptions, not commitments made on behalf of whoever designs this slice for real:

- Same framing as the unix socket: newline-delimited JSON-RPC 2.0, one frame per line, full
  duplex, one connection per client.
- TCP, plausibly TLS later — not decided here.
- A bearer token or API key, sent at or immediately after `initialize`, not during the TCP
  handshake — so the same liveness/reconnect logic applies regardless of how auth ends up being
  carried.
- Full method parity: every method `localrpc` serves today, not an enumerated subset. This is
  the one point treated as a hard requirement rather than a guess — it's the stated reason full
  parity was chosen over reusing S19's narrow surface in the first place.

## What this slice still needs, that this note doesn't answer

Its own threat model, its auth scheme (API key vs. something richer), whether/how it reuses the
framework's `api` service the way S19 does or opens its own listener, what's logged, and how an
operator is meant to reason about "everything is now reachable" the way S19's `Cannot` lines let
them reason about a narrow one.
