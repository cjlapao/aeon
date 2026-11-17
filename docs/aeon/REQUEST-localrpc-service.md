# Feature request — a `localrpc` service for `go-backend-scaffolding`

**To:** whoever maintains `github.com/cjlapao/go-backend-scaffolding`
**From:** the Aeon project, which depends on this scaffolding as a pinned module
**Date:** 2026-09-07
**Status:** a request, sized and specified. Nothing here is built.

---

## What we are asking for

**A service that accepts local IPC connections and dispatches JSON-RPC 2.0 over them** — a unix domain socket on POSIX,
a named pipe on Windows, behind one API.

Two pieces, and the split matters:

| Piece | Whose |
| --- | --- |
| **The listener** — socket or pipe, its lifecycle, permissions, peer credentials, connection management | **Yours.** Generic |
| **JSON-RPC 2.0 framing and dispatch to registered method handlers**, plus a server→client notification channel | **Yours.** Generic |
| The method table and the domain handlers | **Ours.** Aeon-specific, built on top |

We are asking for the two generic pieces. We will not ask you to know anything about our methods.

## Why this is generic, not a favour

**This is the shape a language server has**, and it is the shape any daemon-plus-CLI pair has. A service that speaks
JSON-RPC over a local socket is what lets:

- a CLI and a long-running daemon share one code path, with no port and no HTTP;
- an **editor extension** talk to the same daemon, the way LSP clients do, without the daemon knowing what an editor is;
- a desktop app's native shell reach a backend on the same machine **without opening a port at all**.

That last one is why we need it: a browser cannot speak a unix socket, so our desktop app's Rust shell owns the
connection and the web layer asks it. Nothing is served, nothing listens on TCP, and there is no CORS or origin to
reason about.

**You already have the adjacent piece.** `internal/websocket` handles long-lived connections, and `Runnable`'s own
documentation names *"the HTTP listener, background workers, job consumers, websocket hubs"* as its use cases. This is
the fourth item in that list, on a different transport.

---

## How it fits your existing contract

We read `internal/system/interfaces/interfaces.go`. A `localrpc` service satisfies the plugin contract without any
change to it:

```go
func (s *LocalRPCServicePlugin) Name() string                  { return "localrpc" }
func (s *LocalRPCServicePlugin) Init(ctx context.Context) error // resolve the path, prepare the directory, clear a stale endpoint
func (s *LocalRPCServicePlugin) Serve(ctx context.Context) error // start the accept loop in a goroutine, return
func (s *LocalRPCServicePlugin) HealthCheck(ctx context.Context) bool // the listener is live AND the endpoint has the right mode
func (s *LocalRPCServicePlugin) IsEnabled() bool
func (s *LocalRPCServicePlugin) Dependencies() []system_interfaces.Dependency
```

Three things about that mapping, since they are the parts most easily got wrong:

**`Serve` must not block, and this one genuinely does not need to.** Bind in `Init`, accept in a goroutine from `Serve`,
return immediately — which is what your contract requires and what a listener wants anyway, because binding is the part
that can fail and it should fail during initialisation.

**`HealthCheck` has something real to check**, which your comment asks for: *"implement a real probe when there is
something meaningful to check (a DB ping, **a live connection**, …)."* Here it is two things — the listener is accepting,
**and** the endpoint still has the mode it was created with. The second matters because a socket whose permissions were
widened by something else is a socket that is no longer doing its job.

**It should implement `Runnable` and not `CapabilityProvider`.** See [access control](#access-control-and-the-one-thing-to-please-not-do)
below — this service has no claims to declare, and giving it some would be actively wrong.

---

## Requirements

Each with its reason, because a requirement without one gets optimised away later.

### Transport

| # | Requirement | Why |
| --- | --- | --- |
| T1 | **One API over a unix domain socket (POSIX) and a named pipe (Windows).** Callers do not branch on platform | A consumer that branches gets one platform tested |
| T2 | **The endpoint path is resolved, not configured blindly** — `$XDG_RUNTIME_DIR` then documented fallbacks on POSIX; the pipe namespace on Windows | Every consumer would otherwise re-derive it slightly differently, and a socket at a path nobody expects presents as "the daemon is not running" |
| T3 | **Mode `0600` on the endpoint, and on its parent directory** | It is the access control. See below |
| T4 | **Refuse to start if a live process holds the endpoint; clear it if the holder is gone** | A stale socket after a crash otherwise blocks every subsequent start, and the message *"address already in use"* sends someone hunting for a process that does not exist |
| T5 | **Concurrent clients**, each with independent in-flight requests | A CLI, an app and an editor at once is the normal case, not the exotic one |
| T6 | **Graceful shutdown**: stop accepting, drain in-flight work, close, remove the endpoint | A half-closed endpoint is T4's problem on the next start |
| T7 | **A client disconnecting cancels that client's in-flight work**, through the handler's `context.Context` | Otherwise a cancelled CLI leaves the daemon doing work nobody will read, and it is the handler that must find out |

### Framing and dispatch

| # | Requirement | Why |
| --- | --- | --- |
| F1 | **JSON-RPC 2.0**: requests, responses, and **notifications** | It is specified, it is what LSP uses, and it means an editor client is a known quantity rather than a bespoke integration |
| F2 | **One framing, documented.** We suggest newline-delimited JSON — a conforming JSON encoder emits no raw newline inside a string, so it is unambiguous and trivially debuggable with `nc` | `Content-Length` framing (LSP's) is also fine. **What matters is that it is stated**, because a consumer guessing wrong fails on the first multi-line payload |
| F3 | **Handlers register by method name**, receiving a context and raw params, returning a result or an error | Keeps your side ignorant of every consumer's domain, which is what makes it reusable |
| F4 | **Errors map to JSON-RPC error objects** with a code, and a consumer can supply its own codes | *"Something failed"* over an RPC boundary is a support ticket. A code is a branch |
| F5 | **Server→client notifications on an open connection**, addressed per connection | This is the subscription channel. Filtering, cursors and replay are the consumer's; **the channel is yours** |
| F6 | **A notification stream carries a monotonic sequence number** | A consumer that reconnects needs to know whether it missed something. Whether it can *bound* the gap is its problem; knowing one exists is the transport's |
| F7 | **Per-connection notification lifetime** — closing a connection ends its subscriptions, with no cleanup call required | A subscription that outlives its connection is a leak that only appears under real use |
| F8 | **No batching required.** If JSON-RPC batch arrives, a clear error is fine | We do not need it, and half-implemented batching is worse than none |

### Observability

| # | Requirement | Why |
| --- | --- | --- |
| O1 | **Connection count and per-connection state**, readable from the service | We surface it in a `doctor` command, and *"is anything connected"* is the first diagnostic question |
| O2 | **The endpoint path in the health report** | It is the first thing anyone debugging a failed connection needs, and it is derived rather than configured (T2) |
| O3 | **No payload contents in logs at default level** | Our payloads carry source code and model output. A log that helpfully includes them is a log we cannot ship |

---

## Access control, and the one thing to please not do

**The access control for a local endpoint is filesystem permissions plus peer credentials. That is the whole model.**

| # | Requirement | Why |
| --- | --- | --- |
| A1 | **Mode `0600`, owned by the running user** (T3) | Nothing else on the machine can open it. This is not a layer *in front of* auth; it **is** the auth |
| A2 | **Verify peer credentials on accept** — `SO_PEERCRED` on Linux, `LOCAL_PEERCRED` on macOS and BSD, the client token on Windows — and refuse a connection whose uid is not the running user's | Belt and braces on A1, and it makes a misconfigured mode fail closed rather than open |

**The thing to please not do: do not route this through the claim, role or tenant system.**

That system is excellent and it is the right answer for HTTP, where a request arrives from an unknown party over a
network. **A local socket at mode `0600` has already established who is calling** — the kernel did it — and adding a
claim check on top produces two access-control models where one is authoritative and the other is decoration.

The failure that concerns us is not that the check would be wrong. It is that a **second** model invites a future
consumer to configure it — and a socket whose access depends on a claim configured somewhere is a socket whose
permissions no longer mean what they say.

If a consumer needs per-caller authorisation on this transport, that is the consumer's to add above the dispatch layer,
where it is visibly theirs.

---

## What we do **not** need

Stated because it bounds the work, and because each of these would be a reasonable thing to assume we wanted.

- **No HTTP.** You already have it, and we use it separately for a deliberately narrower surface.
- **No TLS.** It is a local endpoint; the kernel is the channel.
- **No authentication beyond peer credentials** (above).
- **No multi-tenancy.** One user, one daemon, one endpoint. `core.GlobalTenantID` is the right tenant here and it never
  varies.
- **No remote or network transport.** If it can be reached from another machine it is not this service.
- **No service discovery.** The path is resolved (T2), never advertised or scanned for.
- **No JSON-RPC batching** (F8).
- **No schema validation of params.** Handlers own their own shapes; a validator here would be a second owner of a
  contract that already has one.

---

## Open questions for you

We have deliberately not answered these, because they are about your codebase rather than our requirements.

1. **Does `internal/events` already provide a fan-out shape that F5's notification channel should build on**, or should
   this service own its own? We can work with either; we would rather not create a second event mechanism if one fits.
2. **Config shape.** What keys would you want — enabled, an endpoint-path override, a max-connection count? We would
   suggest that **the path override exists but is empty by default**, so T2's resolution is the normal path and an
   override is a deliberate act.
3. **Does `internal/osutils` want the path resolution** (T2), given it already owns `MachineID()` and platform
   concerns? It would fit there, and a `localrpc` service that reaches into it is better than two places knowing where
   a runtime directory lives.
4. **Ordering.** Does anything need to bind *before* other services initialise? For us, yes — we want the endpoint
   accepting connections **before** slow start-up work runs, so the daemon is inspectable while it is busy. Your
   two-pass `Init` then `Serve` gives us that already, provided `Serve` ordering is topological as documented.
5. **Windows.** Is Windows a target you test, or best-effort? Our requirement is that the API not branch (T1); if the
   pipe implementation lands later than the socket one, that is fine and we would rather know.

---

## What we will build on top

So you can see the shape of the consumer, and where our side stops:

- **A closed method table**, versioned with one integer, with an additive-change rule — clients ignore unknown fields
  rather than breaking.
- **Subscriptions with filters and a cursor**, using F5's channel and F6's sequence number. We track the last sequence
  we processed and re-read on reconnect, because a filtered subscriber legitimately sees gaps.
- **Conformance fixtures** for every method and notification, as JSON — so a second client in another language can be
  written against the contract rather than against our Go.
- **A narrow projection of that table onto HTTP**, using your existing HTTP service, for CI and for editor integrations
  that cannot open a socket.

**None of that needs you to know about it.** If F3's registration and F5's channel exist, the rest is ours.

---

## Sizing, honestly

The listener, its lifecycle and peer credentials are a few hundred lines and are mostly platform detail — the sort of
thing that is fiddly rather than hard, and where the fiddliness is worth centralising exactly once.

The JSON-RPC layer is smaller than it sounds: framing, a dispatch map, correlation of concurrent requests on one
connection, and a per-connection notification writer.

**The parts that will take longest are the ones that always do**: the stale-endpoint case (T4), graceful drain (T6), and
disconnect cancellation propagating into handlers (T7). Those three are also the three that, left out, produce bugs
found in production rather than in tests — which is our reason for naming them as requirements rather than as nice to
have.
