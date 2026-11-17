# Brief — S2 Protocol & streaming

**Implements:** [`design.md`](design.md) · **Demo:** [`DEMO.md`](DEMO.md)
**Architecture:** [`../ARCHITECTURE.md`](../ARCHITECTURE.md) — §1a, §4 and §7 are binding.
**Stage:** 1 · Skateboard, first of four. **Depends on S1, and on the scaffolding's socket transport.**

> **Template deviations from `proposal-brief-protocol`, stated rather than silent:**
> **(a)** Implements a design, not a proposal — §7 records where this brief corrects it.
> **(b)** §14 is the demo contract, present in every slice of this plan.
> **(c)** §10's first question is whether a **dependency in another repository exists**. It is first because
> the honest answer may be "no", and the plan must say so rather than proceed as if it were yes.

---

## 1. How to use this brief

> **Constant. Copy verbatim.**

This is a **brief, not a plan.** It states what must be true when the work is done, what must not change
along the way, and where to look to find out the rest. It contains no task breakdown and no implementation
code — producing those is your job.

1. **Read** the design and the demo, then §7 for where this brief overrides the design.
2. **Investigate** — §10 names what to read. Do not plan before you have those answers.
3. **Plan and decompose** — §12 gives the rules your task sequence must obey.
4. **Self-check** — §13, against your finished plan, before dispatching anything.

Where this brief states a decision, it is settled. Where it poses a question, answer it from the code.

**On the self-check:** every answer needs an artifact — a command's output, an enumeration, a quoted line.

---

## 2. Definition of Done

| # | Check |
| --- | --- |
| **D1** | **`initialize` is mandatory.** Any other method on a connection that has not initialised returns error `-31001` with all three `data` parts and `fatal: true`. Proven by attempt, on a raw socket. |
| **D2** | **Negotiation refuses upward and serves downward.** A client needing a version the daemon lacks is refused **naming both numbers**; a client needing an older supported version gets that version and the daemon behaves accordingly. Two tests. |
| **D3** | **Unknown fields are ignored, not rejected** — in params and in results. A request carrying `somethingFromTheFuture` succeeds, and the Go client accepts a result carrying one. **This is the property that makes the next fifteen slices additive**, so it is tested in both directions. |
| **D4** | **A subscription's filter is honoured.** A subscriber scoped to project A receives nothing from project B; one scoped to `loop.*` receives `loop.started` and not `step.started`. Two tests, both by attempt. |
| **D5** | **`after` replays from the cursor and reports where it resumed.** No event is skipped, none is delivered twice, and `seq` never repeats or goes backwards within a stream. |
| **D6** | **Subscriptions die with the connection.** Drop it and the registry returns to empty — asserted on the registry, not inferred from delivery stopping. |
| **D7** | **Two subscriptions on one connection each receive their own filtered stream**, tagged by `subscriptionId`. |
| **D8** | **Daemon lifecycle events are emitted and observable**: connecting a second client produces `client.connected` on the first client's subscription. |
| **D9** | **Every Aeon error carries all three `data` parts and a fatal marker**, enumerated over the whole error table — a list, checked, not a sample. |
| **D10** | **Every conformance fixture round-trips** through the Go implementation, and one fixture contains a field no current code knows (D3's insurance). |
| **D11** | **`internal/protocol` imports nothing transport-related** — no `net`, no listener, no scaffolding transport package. Asserted by grep. It is what lets S19 mount the same table on HTTP and stdio. |
| **D12** | **Peer credentials appear in the accept log** — uid and pid where the platform allows, and a named absence where it does not. |
| **D13** | **The app reports the negotiated protocol version**, and shows **no** protocol number while disconnected. A number present with the daemon down means the app is displaying a constant and the negotiation never happened. |
| **D14** | **`make test` and `make lint` are green**, `make fmt` leaves no diff. |

D2, D3 and D11 are the three that matter, and none is about a feature. D2 and D3 are what let this protocol
live for years; D11 is what lets it be mounted three ways. A slice that ships working methods and fails these
has built something that will have to be rebuilt.

**D1–D13 must each exist as named tests.**

D14 is a standing condition from S1, never a task's sole acceptance criterion (R1).

---

## 3. The problem

**The protocol is the product boundary.** Aeon's differentiator is that one engine serves every surface, and
that promise is worth exactly what the contract between them is worth. LSP won because its protocol was
*stable* — implement once, keep working. A protocol changed every release produces an extension pinned to v3
against a daemon shipping v5.

So the question this slice answers is not "what methods do we need". It is **how does this evolve for fifteen
more slices and several years without breaking a client**, and the answers are T2 (one integer, with a written
rule about what is additive), T3 (clients ignore unknown fields) and T9 (a fixture set as the cross-language
contract).

Two things are deliberately absent. The **socket transport** is `go-backend-scaffolding`'s — this slice
speaks over it, and does not build it. And the **method table is nearly empty**: five methods, with every
domain method arriving in the slice that needs it.

One consequence shapes the work. There are no loops, steps or stored events yet, so a subscriber would have
nothing to receive. Rather than build subscriptions blind and first exercise them at S5, **the daemon emits its
own lifecycle events** (T8): a client connecting is a real event, and it makes the whole path testable now.

## 4. Goal

A versioned JSON-RPC 2.0 protocol over the scaffolding's socket, with filtered subscriptions and a cursor,
speakable by Go, Rust and TypeScript clients, that gains methods for fifteen slices without breaking any of
them.

## 5. Shape and the seam

```
                     internal/protocol/          ← knows nothing about transports (T11)
                       version · method · types
                       errors · events · subscribe
                       conformance/  (JSON fixtures — the cross-language contract)
                              ▲
                              │
       internal/daemon/serve.go  ── wires it onto the scaffolding's socket transport
                              │
      ┌───────────────────────┼───────────────────────┐
   aeon ping/version/watch   the Tauri shell     (S19: HTTP, MCP-stdio)
```

### The seams

**Upward — the method table.** Every later slice adds to it. Additively (T2), which is a constraint on how
those slices are written and a reason this brief's §12 requires the rule to be quotable.

**Downward — the scaffolding's transport.** This slice's first job is to **assert it exists** and to speak
over it. If it does not exist yet, say so plainly (§10 Q1) rather than implementing one — that would
duplicate the thing §1a says is built elsewhere.

**Sideways — the fixtures.** `conformance/` is the contract the Rust and TypeScript clients test against
(T9). Codegen is deferred, with a named trigger: a third client, or the first drift bug.

## 6. Locked decisions

- **T1** — JSON-RPC 2.0. **Do not invent a framing.**
- **T2** — one integer `protocolVersion`, with the additive/breaking rule as written.
- **T3** — clients ignore unknown fields, tested in both directions.
- **T4** — subscriptions with filters. **Not server push.**
- **T5** — per-connection lifetime. **No durable subscriptions.**
- **T7** — `initialize` first, always.
- **T9** — Go types are the source; the JSON fixtures are the cross-language contract. **No codegen in this slice.**
- **T11** — `internal/protocol` imports nothing transport-related.

## 7. Overrides to the design

| # | Design says | This brief requires |
| --- | --- | --- |
| O1 | `kinds` accepts a trailing `*` | Also **reject** any other wildcard form with a named error rather than treating it as a literal. A filter that silently matches nothing is a client that appears connected and receives nothing, which is the hardest bug in this slice to diagnose from the outside. |
| O2 | Events carry a monotonic `seq` | In this slice `seq` is **in-memory and resets on restart**, and the demo says so. S4 moves it onto the store's `event` table. **Name that in the code** — an in-memory cursor that looks durable is worse than one that admits it. |
| O3 | The error table uses `-31000` to `-31999` | Every code is **allocated in one file** with its three-part text, and a code is never reused for a different meaning even after a method is removed. Reuse breaks D9's promise years later, when nobody remembers. |
| O4 | Subscriptions are per-connection | Bound the number **per connection** and in total, and report the refusal. An unbounded registry on a daemon meant to run for weeks is a leak with a client-shaped cause. |
| O5 | `initialize` returns `capabilities` | It is a list of **strings**, and an unknown capability name in a client's expectations is ignored rather than an error. Otherwise `capabilities` becomes a version by another name, which is what T2 exists to avoid. |
| O6 | The design does not say what happens to a malformed frame | A frame that is not valid JSON, or not valid JSON-RPC, gets a JSON-RPC parse error and **the connection stays open**. Dropping the connection on one bad frame makes a client's minor bug look like a daemon crash. |

## 8. Non-goals

- **N1 — No socket transport.** `go-backend-scaffolding`'s (§1a). Assert it; do not build it.
- **N2 — No domain methods.** No loops, steps, questions, cost, tools. Each arrives with its slice.
- **N3 — No HTTP, no MCP.** S19 mounts the narrowed table; T11 is what makes that possible.
- **N4 — No generated Rust or TypeScript types.** T9. The fixtures are the contract.
- **N5 — No authentication.** Filesystem permissions (P26); API keys are S19's.
- **N6 — No durable subscriptions and no server-side replay buffer.** T5, T6.
- **N7 — No store schema.** S4. `seq` is in-memory here and says so (O2).
- **N8 — No UI capability beyond the version line.** S9.
  **Amended 2026-10-15: superseded.** A slice now ships the UI surface that exercises it
  (`aeon/AGENTS.md` §4b). S2's surface is the Status tab — the connection and the **negotiated** protocol
  version. S9 still builds the real app; what changed is that each slice no longer defers its own test
  surface to it.

### Standing non-goals

- **Never write outside `aeon/`** (P12), except `docs/` and `docs/test_suites/`.
- **Never edit `go-backend-scaffolding`.** It is a pinned dependency (S1's B1): read it, and raise what it needs as a request. Version changes follow S1's `UPSTREAM.md` upgrade procedure and must leave the registered set unchanged (B11).
- **Never run `git clean`.**

## 9. Repo invariants

**I1 — `internal/protocol` imports nothing transport-related** (D11, T11). Name the mechanism that catches a
violation. This is the invariant whose breach is invisible: everything works, and S19 then cannot mount the
table twice.

**I2 — One error-code registry** (O3). A code allocated in two places is a code that means two things.

**I3 — One place decides whether a connection has initialised.** A second check drifts, and the looser one
wins, which turns T7 into a suggestion.

**I4 — Every method's request and result type is in `types.go`**, not inline in a handler. The fixtures are
generated from and tested against those types.

**I5 — No test binds the real socket path** (inherited from S1's I6). Temp paths, so a test run never
collides with a developer's daemon.

**I6 — The subscription registry is the only place subscriptions live.** No per-handler bookkeeping — D6
asserts on the registry, and a second store of subscriptions would make that assertion meaningless.

**I7 — `make test` green is a standing condition.**

**I8 — Public API changes ship with their tests in the same task.**

---

## 10. Investigation brief

### 10.1 What to read

1. **`go-backend-scaffolding`, for the socket transport** — Q1 below. Read what it actually offers before
   planning anything that speaks over it.
2. **`aeon/internal/app/`, `appctx/`, `bootstrap/`** as S1 left them — the protocol layer registers as a
   service like anything else.
3. **`aeon/cmd/aeon/serve.go`** (S1 T3) — the accept path this slice hooks into, and the socket path
   resolution to reuse.
4. **`docs/_archive/aeon-v1-horizontal/13-cli/`** — `C9`'s three-part error shape, arriving here rather than at S23.
5. **`docs/_archive/aeon-v1-horizontal/09-server/`** — its SSE framing lessons, and R6's reasoning about what a surface must
   refuse, which S19 will need and this slice must not contradict.
6. **A JSON-RPC 2.0 library for Go**, and the specification itself for the error-code reservations.

### 10.2 The patterns to copy

- **The scaffolding's plugin/service shape**, so the protocol layer starts and stops with everything else.
- **`pkg/diagnostics`'s `Diagnostic`/`DiagnosticBag`**, which several superseded slices already assume.
- **Discriminated results over exceptions**, as every superseded slice does.
- **S1's temp-path test discipline.**

### 10.3 Confirm each from the code

> ## Q1–Q6 are ANSWERED. Read this before investigating.
>
> **Added 2026-10-15, after S1 shipped.** Six of these questions were written when the transport was a
> request to another project; it now exists, is in the tree at `framework/v0.10.1`, and S1 runs against it.
> **The answers are below with their sources. Confirm them, do not re-derive them** — and read
> `$(go env GOMODCACHE)/github.com/cjlapao/go-backend-scaffolding/framework@v0.10.1/localrpc/README.md`
> in full, because it provides more than this brief assumed.
>
> **The headline: `Notify` already exists, with per-connection sequence numbers and a drop counter.**
> Q7 was written on the assumption that this slice invents `seq`. It does not. Building a second
> sequencing mechanism over the top of this one is the single most expensive mistake available here.

| # | Answer | Source |
| --- | --- | --- |
| **Q1** | **It exists.** `framework/localrpc` — a *service*, not a `net.Conn`: `Register(method, Handler) error`, `Notify(connID, method, params) error`, `Connections() []ConnInfo`, `EndpointPath() string`, `Ready() bool`, `Shutdown(ctx)`. Unix socket on POSIX, named pipe on Windows, one API with no branching for callers. **This slice is not blocked.** | `localrpc/localrpc.go`, `localrpc/interfaces/interfaces.go` |
| **Q2** | **Full JSON-RPC 2.0, not just framing.** Newline-delimited frames, envelope validated, `id` echoed verbatim and never interpreted, notifications (no `id`) answered with nothing, panics recovered as `-32603` with no stack reaching the wire. **So Q4 is moot: add no JSON-RPC library.** S1 proved the dispatch end to end — `version` answers, an unknown method returns `-32601`. | `localrpc/service/conn.go`, S1 `TESTSPEC.md` §5.3 |
| **Q3** | **`ConnInfo` carries `PeerUID` and `PeerPID`**, and the service **refuses a connection from another uid** before a handler sees it. `peer_linux.go` and `peer_darwin.go` both report uid and pid; `peer_unsupported.go` is the fallback, and Windows uses `listener_windows.go` with a named pipe. **`PeerPID` is `0` where the platform does not report one** — so D12 names an absence rather than omitting it, exactly as required. S1's own `internal/endpoint/peer_linux.go` is separate: it reads `SO_PEERCRED` to name the *holder* of a contested endpoint. | `localrpc/interfaces/interfaces.go`, `localrpc/service/peer_*.go` |
| **Q4** | **Moot — add nothing.** See Q2. The handler signature is `func(ctx context.Context, params json.RawMessage) (any, error)`. Return a `*localrpc.Error` (via `localrpc.NewError`) for a specific code; any other error becomes `-32603` with its message. | `localrpc/interfaces/interfaces.go` |
| **Q5** | **Yes: `localrpc.Shutdown(ctx)`**, and the plugin's own lifecycle runs it with every other service. S1 observed `shutdown complete` and the endpoint removed. **Two orderings S1 paid for:** `Register` must be called **after** `svc.Start` or it fails with `localrpc: service is not running`; and `Ready()` is false until the serve phase completes, during which unknown methods answer **`-32002` ServerNotReady** rather than `-32601`. That distinction is already a three-state answer — do not collapse it. | `localrpc/plugin.go`, S1 `cmd/aeon/serve.go` |
| **Q6** | S1's accept path logs one line at INFO: `localrpc: accepting connections on <path>`, then `serve.go` prints `endpoint <path>` and `store <path>` on stdout (B10). **That is all.** Nothing logs a connection, a method call, or a peer — so D12 adds rather than extends, and nothing needs deduplicating. | S1 `cmd/aeon/serve.go`, walked in `S1-bootstrap/DEMO.md` |

#### What the transport already gives you, that this brief predates

| | |
| --- | --- |
| **Per-connection sequence numbers** | `ConnInfo.NotifySeq` is the last sequence sent on that connection. `Notify` advances it. |
| **A gap signal, not silent loss** | `NotifyBuffer` (default **256**) bounds the per-connection queue; when it is full the **oldest is dropped and `NotifyDropped` increments, while the sequence still advances** — so a consumer detects the gap and re-reads. This is the mechanism D-whatever-numbers-it must consume, not reinvent. |
| **Real bounds, not guesses** | `MaxConnections` **64**, `MaxFrameBytes` **1 MiB**, `NotifyBuffer` **256**. **Q8 asked for numbers and said to admit they were guesses — these are the defaults, and they are not guesses.** |
| **Eight error codes already defined** | `-32700` parse · `-32600` invalid request *(also: frame over the limit, connection closed)* · `-32601` method not found · `-32602` invalid params · `-32603` internal · **`-32002` server not ready** · **`-32001` connection limit** · **`-32000` batch unsupported**. |
| **Batches are refused whole** | `-32000`. Half-applied batching is not a thing this transport will do, so the protocol cannot offer it. |
| **The handler's context is the CONNECTION's** | Cancelled when the client disconnects. **It is the only way a handler learns nobody is waiting** — any handler doing real work must select on it. |
| **`Register` refuses a duplicate method** | Rather than silently shadowing the first. |
| **`ConnIDFromContext(ctx)`** | How a handler addresses a notification back to its own caller. |
| **`HealthCheck` re-verifies the endpoint's mode** | An endpoint whose permissions were widened by something else reports unhealthy rather than serving open. |
| **Params are NOT validated** | Published as **SEC-T6-005**: the transport validates the envelope only; `params` arrives as raw `json.RawMessage`. **Every handler validates its own.** |

**Q10 (the error table) narrows accordingly:** enumerate this slice's *domain* codes above the transport's,
and stay out of the reserved range `-32768…-32000`, which the transport already occupies down to `-32000`.

### 10.4 Project-specific questions

| # | Question |
| --- | --- |
| Q7 | **Amended 2026-10-15, and largely settled — read this rather than deciding it again.** `localrpc` provides a sequence number, but **it is not this slice's `seq` and cannot be.** Theirs is *per connection* and counts deliveries; T6's is *per stream* and orders events. T5 kills subscriptions with their connection and makes reconnection a cursor's job — and a per-connection counter **restarts at reconnect**, which is precisely when the cursor is needed. So **this slice does define its own stream-scoped `seq`**, and the remaining question is narrow: given there is no store at S2 (O2), where does it live, and what is the S4 change that replaces it? ⚠️ **The trap is using `ConnInfo.NotifySeq` as the cursor.** It would pass every test on one connection and silently lose events on the first reconnect. **The two are complementary, not competing:** `NotifyDropped` tells a client *a gap exists*, and T6's `after` is how it recovers — so a client that sees a non-zero drop count re-subscribes with the highest `seq` it saw. Say that in the design, because it is the one place the two numbers meet. |
| Q8 | **Amended 2026-10-15.** The transport's bounds are **64 connections, 1 MiB frames, 256 queued notifications** — defaults, not guesses (§10.3). So: are they right for Aeon, and what does this slice bound *on top* of them (subscriptions per connection, total subscriptions)? State those numbers, and for those, whether they are guesses. |
| Q9 | How are the conformance fixtures produced and checked — hand-written and asserted, or generated from the Go types and committed? Either works; a fixture nobody regenerates goes stale, so name the check. |
| Q10 | What is the full initial error table? Enumerate every code this slice can return, with its three parts (O3, D9). |
| Q11 | **An endpoint can be unlinked under a running daemon**, and neither end notices: the listener survives, `ss -lx` still shows it LISTEN, and every new client gets a connect failure indistinguishable from "no daemon running". **Observed during S1's verification, not hypothesised.** Decide whether this slice detects it — and if not, say so and say what a client is told. A liveness answer that cannot distinguish *gone* from *never there* is a two-state answer to a three-state question (P31). |

---

## 11. Behavioural contract

### 11.1 Connection and handshake

- `initialize` first; anything else returns `-31001`, `fatal: true` (D1).
- The client sends the version it needs; the daemon answers with the version it will speak.
- Needs a higher version → refuse, naming both (D2). Needs an older supported one → serve it (D2).
- `capabilities` is a list of strings; unknown names are ignored (O5).
- A malformed frame gets a parse error and **the connection stays open** (O6).
- Peer credentials are logged at accept, or their absence is named (D12).

### 11.2 Subscriptions

- `subscribe {filter:{projectRoot?, kinds?}, after?}` → `{subscriptionId, from}`.
- `kinds` matches exactly or by a single trailing `*`; any other wildcard is a named error (O1).
- `after: n` replays from `n+1` and reports `from` (D5).
- Notifications are `method: "event"` carrying `subscriptionId` and `seq`.
- Per-connection lifetime; the registry empties when the connection drops (D6, T5).
- Bounded per connection and in total, with the refusal reported (O4).
- Two subscriptions on one connection are independently filtered and tagged (D7).

### 11.3 Events

- Kinds in this slice: `daemon.started`, `client.connected`, `client.disconnected`,
  `subscription.created` (T8).
- `seq` is monotonic within a stream, never repeats, never regresses.
- **`seq` is in-memory and resets on restart in this slice** (O2), stated in the code and in the demo.

### 11.4 Errors

- Aeon codes in `-31000`–`-31999`, allocated in one file, never reused for a new meaning (O3, I2).
- Every error's `data` carries `what`, `means`, `todo` and `fatal` (D9).

---

## 12. How to decompose

- **R1** — every task is gated by at least one **named test**.
- **R2** — a task that adds behaviour adds its test in the same task.
- **R3** — a file in more than one task's `files_expected` needs a stated reason.
- **R4** — **the transport assertion is part of the first task** (Q1). If the transport is absent the slice
  stops there, and the plan must be able to say so without having built anything on the assumption.
- **R5** — the version rule and the error registry land **before** any method, because both are cross-cutting
  and retrofitting either means revisiting every handler.
- **R6** — subscriptions are their own task, and events another. They fail differently: one is filtering and
  lifetime, the other ordering and emission.
- **R7** — no task over roughly 300 changed lines or 12 files without justification.
- **R8** — the conformance fixtures are their own task, **after** the methods exist and **before** the clients.
- **R9** — task files contain no deliberation: no "hmm", "let me", "wait", "on reflection", "I think",
  "not sure", "FIXME". `TODO(engineer):` in a code skeleton is the one permitted form.
- **R10** — a task carries no open question.
- **R11** — the final task walks `DEMO.md` end to end and fixes what does not match.

## 13. Pre-dispatch self-check

### A. Mechanical

| # | Run | Pass condition |
| --- | --- | --- |
| A1 | `grep -rniE "\bhmm\b\|\blet me\b\|\bon reflection\b\|\bFIXME\b\|\bwait\b\|\bI think\b\|\bnot sure\b" tasks/` then `grep -rn "TODO" tasks/ \| grep -v "TODO(engineer)"` | Both empty (R9, R10) |
| A2 | List every task with `est_changed_lines` and file count | Nothing over ~300 lines or ~12 files without justification (R7) |
| A3 | List every task with the **test name** that gates it | Every task has one (R1) |
| A4 | List every file in more than one task's `files_expected` | Each justified (R3) |
| A5 | **Paste the answer to Q1** | The transport's interface, quoted from its source. **If it does not exist, the plan says the slice is blocked and stops** — a plan that assumes it blocks sixteen slices on a guess |
| A6 | `grep -rn "net\.\|Listen\|Conn" ` over your planned `internal/protocol` files | No hits (D11, I1) |
| A7 | Paste the full error table (Q10) — every code, its three parts, its fatal marker | The enumeration is the deliverable, not a sample. D9 asserts over the whole table, and a code added later without its parts fails it |
| A8 | Paste the answers to Q2, Q3, Q5 and Q7 | Four answers from the source. **Q3 must name an absence** where a platform gives no peer credentials, rather than omitting the platform |
| A9 | `make fmt` then `git status --porcelain` | No output |

### B. Scope

| # | Check | Ref |
| --- | --- | --- |
| B1 | Confirm no task implements a socket listener or framing | N1 |
| B2 | Confirm no task adds a loop, step, question, cost or tool method | N2 |
| B3 | Confirm no task mounts HTTP or MCP | N3 |
| B4 | Confirm no task generates Rust or TypeScript types | N4 |
| B5 | Confirm no task adds authentication | N5 |
| B6 | Confirm no task adds a durable subscription or a replay buffer | N6 |
| B7 | Confirm no task creates a store table | N7 |
| B8 | Quote the `out_of_scope` lines covering N1–N8 | §8 |

### C. Acceptance

| # | Check | Ref |
| --- | --- | --- |
| C1 | Map each of D1–D13 to a task and a named test | §2 |
| C2 | For D3, confirm **both directions** are tested — an unknown field in params, and one in a result the Go client reads | D3 |
| C3 | For D2, confirm two tests: refused upward naming both numbers, served downward | D2 |
| C4 | For D6, confirm the assertion is on the **registry**, not on delivery stopping | D6, I6 |
| C5 | For D5, state how "no gaps and no duplicates" is asserted over a run, not a single event | D5 |
| C6 | For D13, confirm the app shows **no** protocol number while disconnected | D13 |

### D. Ordering

| # | Check | Ref |
| --- | --- | --- |
| D1 | Confirm the transport assertion is in the first task | R4 |
| D2 | Confirm the version rule and error registry precede every method | R5 |
| D3 | Confirm subscriptions and events are separate tasks | R6 |
| D4 | Confirm the fixtures land after the methods and before the clients | R8 |

### E. Structure

| # | Check | Ref |
| --- | --- | --- |
| E1 | Confirm one error-code registry | I2 |
| E2 | Confirm one place decides whether a connection has initialised | I3 |
| E3 | Confirm one subscription registry | I6 |
| E4 | Confirm every request and result type is in `types.go` | I4 |

### F. Project-specific

| # | Check |
| --- | --- |
| F1 | State where `seq` comes from here, and the S4 change that replaces it (Q7, O2). |
| F2 | State the subscription bounds and that the numbers are guesses (Q8, O4). |
| F3 | State how the fixtures are produced and what stops them going stale (Q9). |
| F4 | Confirm a malformed frame leaves the connection open (O6). |
| F5 | Confirm an invalid `kinds` wildcard is a named error rather than a literal match (O1). |

## 14. The demo contract

> **Present in every slice of this plan.** A slice is not done when its tests pass. It is done when someone
> who did not build it can follow `DEMO.md` and watch the thing work.

[`DEMO.md`](DEMO.md) is a deliverable, and the final task (R11) walks it end to end and fixes whatever does
not match — recording **which** was wrong, the code or the demo, because editing the demo is faster and
therefore the default.

S2's demo has one property worth preserving: **several of its steps are raw `nc -U` requests.** That is
deliberate. A demo that only exercises Aeon's own client proves the client and the daemon agree, which is a
weaker claim than the protocol being correct — and the third-party clients this protocol exists for will not
be using Aeon's client library.
