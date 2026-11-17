# Design — S9 The app (Stage 2 · Bicycle)

**Date:** 2026-09-06
**Status:** approved (design); not yet implemented
**Stage:** 2 · Bicycle, fourth of four — **the halfway point of the plan**
**Depends on:** S1 (the window), S5, S6, S7, S8. Needs all of Stage 2's mechanisms to render.
**Inherits:** U1–U10 from [`docs/_archive/aeon-v1-horizontal/10-ui-chat`](../../_archive/aeon-v1-horizontal/10-ui-chat/design.md). New decisions start at **U11**.

---

## Context

S1 built the window sixteen slices early, deliberately: *"so its integration is never a surprise."* It renders the
**daemon's** version over the socket and shows three connection states. Every Rust-toolchain, socket-path and
build problem was met alone, back when nothing else was moving.

This slice is where it becomes the product. And the thesis is narrow enough to state in one line.

### The app is where every three-state answer in the system survives or dies

Four independent places in Aeon refuse to collapse an uncertain answer into a certain one:

| What | States |
| --- | --- |
| **Cost** (S3) | `known` · `partial` — a floor · `unknown`, with a reason |
| **The `bash` receipt** (S7) | ran · did not run · **outcome unknown** |
| **Blueprint validation** (S8) | valid · invalid · **could not check**, naming the slice |
| **Liveness** (this slice, U13) | current · **stale** · disconnected |

That is [P31](../ARCHITECTURE.md#8-product-decisions), and a UI is exactly where it gets thrown away — because
"unknown" renders as `—`, and `—` becomes `$0.00`, and nobody notices. Every one of those designs paid a cost to
keep uncertainty visible, and the app is the last mile where that investment is either delivered or quietly
discarded.

So U17 is a rule with a test behind it: **an unknown is never rendered as a zero, a dash, or an absence.**

### The web layer cannot speak a unix socket

A browser has no unix-socket API, which is why the Tauri shell stopped being packaging and became the UI (S1's
T5, superseding U1). The Rust side owns the connection; the web layer asks it.

The naive shape is one Tauri command per protocol method. That produces **a second copy of S2's method table,
written in Rust, that drifts** — and P25 was explicit that Aeon has *two* surfaces, not three. So there is one
generic bridge (U11), and the closed method table stays in exactly one place.

### The toolkit is `@cjlapao/ui-kit`

v1 already depends on `^0.2.0`, so the port below brings components built on it. ~200 components, and the
app shell (`SideMenuLayout`, `SplitView`, `Panel`, `Tabs`), the thread (`TimelinePanel`), the blueprint
shape (`TreeView`, `ConnectionFlow`) and the stat tiles come for free.

**Two things it cannot express yet, and they are this slice's thesis** — `StepStatus` has no `not-needed`
or `skipped` (D13 needs both, rendered distinctly), and there is no socket-liveness three-state
(`ConnectionState` is for flow diagrams). **T4 settles both before writing the renderers**, by extending
the kit or composing locally — never by reusing the nearest existing type.

### What actually ports from v1, and what does not

U10 says *"port v1's `src/api/` hook layer rather than rewriting it — it is tested, and none of its problems are
the ones this slice is solving."* **The line is drawn in the wrong place**, and `packages/loop-state-ui` says so:

| Module | Ports? | Why |
| --- | --- | --- |
| `components/*` — `Markdown`, `PhaseTimeline`, `LoopCard`, `TaskSteps`, `TaskLifecycle`, `RelativeTime`, `RecoveryPanel`, `ConnectionPill` | **Yes** | Presentational, tested, and coupled only to `api/types`, `api/lifecycle`, `api/frontmatter` — not to a transport |
| `api/types.ts`, `api/lifecycle.ts`, `api/frontmatter.ts` | **Types no, logic yes** | The pure helpers port. The **types are regenerated** from S2's contract (U12) rather than hand-carried |
| `platform/{index,tauri,web}.ts` | **Yes** | This is U7 already built — a capability check with a browser fallback |
| `api/client.ts`, `api/connection.tsx` | **No** | `fetch` and `EventSource` against `loop-state-api`. The transport is now a socket behind a Rust bridge |
| `api/useLabels.tsx` | **Re-point** | It fetches, and most components import it. It is the one shallow coupling that reaches everywhere |

So the reusable half is the **components plus the pure helpers**, and the part U10 named as the thing to keep is
precisely the part that cannot be. Correcting that is worth more than the advice was.

**And the app does not use HTTP at all.** P25's narrow HTTP surface is S19, for CI and MCP. The app is a socket
client, so S19 is not a dependency of the bicycle.

## Goal

Everything in S6–S8, done from the app instead of the CLI: start a loop and watch it run, answer its question
inline without knowing which mechanism asked, read a step's transcript, see cost in three honest states, and be
told when what you are looking at is stale.

## Decisions

U1 is superseded (S1). U2–U9 stand, with U3's SSE becoming socket subscriptions and U8's HTTP becoming
JSON-RPC. New:

| # | Decision | Why |
| --- | --- | --- |
| U11 | **One generic RPC bridge**, not one Tauri command per method: `rpc(method, params)` plus one subscription event channel | S2's method table is closed and lives in one place. A per-method Rust command is a second copy that drifts, and P25 says two surfaces, not three |
| U12 | **TypeScript types are generated from S2's protocol contract**, checked in, and CI fails if regenerating them produces a diff | Hand-maintained types are exactly the drift the conformance fixtures exist to prevent — and in a UI, drift shows as a blank panel rather than an error |
| U13 | **Liveness is rendered, not hidden.** Data whose freshness cannot be confirmed is shown as **stale**, with how long since it was confirmed | A UI showing a loop `running` when the socket died forty seconds ago is lying, and it is the most believable lie in the product |
| U14 | **The app never distinguishes a step question from a gate** | L11's whole point: a human answering something should not have to know which mechanism asked. The distinction is a daemon concern |
| U15 | **The webview loads only bundled assets**, with a locked CSP and no remote origin | The generic bridge (U11) is safe because the app *is* the human, who is allowed to stop a loop. It stops being safe the instant arbitrary content can render in that webview |
| U16 | **The app has no filesystem access to the store.** Everything comes over the bridge | U8 said the UI never *writes* to the store; a second *reader* is just as bad, because it is a second set of assumptions about a schema that is not a shared contract |
| U17 | **An unknown is never rendered as a zero, a dash, or an absence.** It renders as an explicit statement carrying its reason, and a test asserts it for every three-state value in the system | Four designs paid to keep uncertainty visible. The app is where that is delivered or discarded, and discarding it is invisible |
| U18 | **The thread is per loop** and holds chat messages, questions and step summaries in one ordered stream | The chat is how a person interacts with the system, not a conversation with a model. One stream per loop is what makes "what happened, and what does it want" a single read |
| U19 | **No optimistic rendering of anything**, generalising U9 | The daemon is the truth and every mutation is followed by an event. A local edit is a second source of truth, and the two will disagree exactly when it matters |

## Architecture

```
aeon/ui/
  src-tauri/
    bridge.rs        rpc(method, params) → one socket round trip        (U11)
    subscribe.rs     one event channel, daemon events → webview
    conn.rs          the connection, reconnection, and liveness clock   (U13)

  src/
    proto/           GENERATED from S2's contract — do not edit          (U12)
    lib/rpc.ts       the typed client over the bridge
    lib/live.ts      subscription state + staleness                      (U13)
    lib/uncertain.tsx  the three-state renderers                         (U17)
    platform/        ported from v1 (U7)
    components/      ported from v1, re-typed
    screens/
      Loops.tsx        the list, and starting one
      Loop.tsx         the ladder, cost, the thread
      Step.tsx         a step's transcript, tools, manifest
      Blueprints.tsx   validation, in three states
      System.tsx       connection, version, the recovery report
```

### The bridge (U11)

```rust
#[tauri::command]
async fn rpc(method: String, params: serde_json::Value) -> Result<serde_json::Value, RpcError>
```

One command. It does not know the method list, so it cannot disagree with it.

Subscriptions are one channel: the Rust side holds the socket subscription and emits every event to the webview
as `aeon://event`, with the daemon's `seq`. The web layer filters. **`seq` is what makes a gap detectable** — S4
made it the `event` table's rowid, and a filtered subscriber legitimately sees gaps, so the web layer tracks the
last `seq` it processed and re-reads on reconnect rather than assuming continuity.

### Liveness (U13, U17)

```
connected + events flowing        → current
connected, no event within N      → current (nothing is happening; that is not staleness)
socket dropped                    → STALE, showing "last confirmed 40s ago"
reconnecting                      → STALE
reconnected                       → re-read, then current
```

The distinction that matters: **silence is not staleness.** A loop waiting at a gate emits nothing for hours and
is perfectly current. Staleness is *the connection being unconfirmable*, not the absence of events — and getting
that backwards produces a UI that cries wolf until people stop reading it.

On reconnect the app **re-reads** rather than resuming from its last `seq` alone, because a gap it cannot bound is
a gap it must not paper over.

### The thread (U18, U4, U14)

One ordered stream per loop:

```
  ┌ step  survey            ok      SURVEY.md      $0.0312
  ├ ask   "Which package should I start with?"          ← a step question (S6)
  │       [ internal/store ] [ internal/runtime ] [ … ]
  ├ you   internal/store
  ├ step  survey            ok      (resumed)
  ├ ask   "Approve the survey and continue?"            ← a gate (S8)
  │       [ approve ] [ revise ] [ abort ]
```

**Both `ask` rows look identical**, and that is U14 delivered: the first resumes a step, the second advances a
loop, and a human answering has no reason to care. The app sends `answer` with the question id; the daemon
dispatches on `kind`.

Questions render **inline in the thread** (U4), never as a modal — a modal detaches the decision from the context
needed to make it.

**No optimistic render** (U9, U19): the answer appears when the daemon says it landed. `answer` refuses a second
answer, and an optimistic render would show an accepted answer that was rejected — leaving someone believing the
step resumed on their input.

## The demo

See [`DEMO.md`](DEMO.md): start a loop from the app and watch the ladder fill in; answer a step question and a
gate from the same thread without being told which is which; open a step's transcript; **pull the socket out and
watch the UI go honest** rather than stale-but-confident; see a `partial` cost read as a floor; validate a
blueprint and see its three states; and watch a `kill -9` recovery report land in the app.

## Testing

| What | How |
| --- | --- |
| The bridge is generic | A test asserting `src-tauri/` contains **no protocol method names** — grep for the method table. The moment one appears, U11 is broken |
| Generated types | Regenerate in CI; a diff fails the build (U12) |
| Staleness | A fake bridge that stops responding: the UI shows stale with an age. **And a separate test that silence with a live socket stays `current`** — the wolf-crying case |
| Reconnect re-reads | Drop and restore: a re-read happens, and a `seq` gap does not silently persist |
| **Cost never flattens** | A snapshot over all three states: `unknown` renders its reason, `partial` reads as a floor ("at least $X"), and **the string `$0.00` never appears for an unknown** (U17, U6) |
| Validation never flattens | `unverifiable` renders distinctly from both `valid` and `invalid`, naming its slice |
| Recovery never flattens | A step failed by S7 renders the tool name and the receipt state |
| Questions are indistinguishable | A step question and a gate render **identically** except for their text and options (U14) |
| No optimistic answer | A rejected `answer` leaves the thread unchanged; the option set stays live (U9, U19) |
| No store access | A test asserting the web layer and Rust shell open no database and no file under the state directory (U16) |
| CSP and origin | The built app's CSP is asserted; no remote origin is loadable (U15) |
| Ported components | v1's existing component tests pass against regenerated types — the port changed a type import, not a behaviour |

The two rows worth reading twice are **"cost never flattens"** and **"silence stays current."** The first is the
whole thesis, and the second is the mistake that makes the first one's warning worthless.

## Out of scope

- **No diffs and no approval UI.** U5's rule — approval flows through the question primitive, never a separate
  mechanism — is a rule waiting for **S16**. There is nothing to diff until then.
- **No permission prompts.** **S11.** A step attempting `git push` and the app asking is S11's demo, not this one.
- **No agent picker.** **S13.** `agent:` resolves to one of two handlers.
- **No blueprint builder.** Never promised for the alpha. U18's manifest is what would make one tractable.
- **No HTTP.** The app is a socket client; S19's narrow surface is for CI and MCP.
- **No retrieval panel.** S22.
- **No code signing or notarisation.** S23, with distribution.
- **No multi-machine or remote daemon.** One daemon per machine (P29). Connecting to a daemon elsewhere is not in
  the alpha.

## Amendments to earlier slices

| Where | Change | Status |
| --- | --- | --- |
| `docs/_archive/aeon-v1-horizontal/10-ui-chat` U10 | **Corrected**: the reusable half is the components plus the pure helpers; the transport layer it named as the thing to keep is the one part that cannot be | **landed with this design** |
| S1 | Confirmed, not changed: the window, the three connection states and the socket-path display are the foundation this builds on |  — |

## Open questions

None. Whether v1's `loop-state-ui` package is edited in place or the app is built out at `aeon/ui/` is settled by
S1's B5: **`aeon/ui/`**. v1's package is read and ported from, never moved.
