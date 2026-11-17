# S2 Protocol & streaming — the demo

**Stage 1 · Skateboard.** *First of four. This one proves the boundary every client will speak across.*

By the end of S1 something ran. This demo shows that something can be **talked to**, that the conversation
is versioned, and that a client can subscribe to a filtered stream of what the daemon is doing.

There are no loops, steps or stored state yet — so the events you will watch are the daemon's own.

> **Walked 2026-10-15 on `linux/amd64`, every step including the raw-socket ones.** Eleven predictions were
> wrong and are recorded in §Findings. **Three were real defects in the binary and were fixed rather than
> documented** — most pointedly a pid rendered as `1.130661e+06`, which is unusable for the one purpose a
> pid has.
>
> **§8 was walked too**, on a virtual display: the window opened, React called the Tauri command, the Rust
> shell handshook with the Go daemon, and the daemon logged `client=aeon-app/0.0.0 protocol=1`. That log
> line is the proof — the three toolchains meeting is now observed rather than assumed.

**Before you start.** Use the dev endpoint so nothing here touches your real daemon:

```sh
export LOCALRPC_ENDPOINT_PATH=/tmp/aeon-demo/run/rpc.sock
export XDG_STATE_HOME=/tmp/aeon-demo/state
mkdir -p /tmp/aeon-demo/run /tmp/aeon-demo/state
chmod 700 /tmp/aeon-demo/run /tmp/aeon-demo/state
make build        # writes dist/aeon
```

---

## 1. It answers

```
./dist/aeon serve &       # or `make dev-daemon`
./dist/aeon ping
```

**You should see:**

```
pong · 0.1ms
```

**It failed if** it hangs. A hang means the socket was accepted (S1 proved that) but nothing is reading the
JSON-RPC framing — which is the seam between the scaffolding's transport and Aeon's protocol, and the one
thing this slice is really about.

---

## 2. The handshake is real, and mandatory

```
./dist/aeon version
```

**You should see** the client's build, then the daemon's, with the protocol version **marked as
negotiated**:

```
aeon 0.0.0-dev
  built   2026-10-15T05:54:53Z
  commit  ae10fff
  go      go1.26.6 linux/amd64
  daemon  0.0.0-dev
  protocol  1 (negotiated)
```

**The word "negotiated" is the point.** A bare number reads as a constant, and a constant proves nothing
about whether a handshake happened. Try it against a stopped daemon and the daemon lines are replaced by
`not connected — …` **naming the path it tried**.

Now prove the handshake is not decoration. Send a raw request that skips it:

```sh
printf '{"jsonrpc":"2.0","id":1,"method":"ping"}\n' \
  | socat -T3 STDIO,ignoreeof UNIX-CONNECT:$LOCALRPC_ENDPOINT_PATH
```

> **`ignoreeof` is not decoration either.** `socat -t2 -` and `nc -U -q2` half-close the socket at stdin
> EOF and **lose the reply** — you get empty output and exit 0, which looks exactly like a daemon that
> ignored you. Measured in S1; see `../S1-bootstrap/TESTSPEC.md` §5.3.

**You should see** a refusal naming the method that was too early:

```json
{"jsonrpc":"2.0","id":1,"error":{"code":-31001,"message":"connection is not initialized",
 "data":{"what":"method \"ping\" was called before `initialize` on connection conn-3",
         "means":"version negotiation has not happened, so the daemon does not know which protocol version to speak",
         "todo":"call `initialize` first, with the protocol version your client needs",
         "fatal":true}}}
```

**Read the `data` block.** Three parts and a fatal marker (T10). That shape is in the protocol from the
start because error text *is* the interface when something goes wrong, and adding it later means revisiting
every method.

---

## 3. Version negotiation refuses

Ask for a version the daemon does not have. **Flags come after the verb**, because each verb parses its own:

```
./dist/aeon ping --protocol-version 99
```

**You should see** a refusal naming **both** numbers, rendered in three parts, exiting non-zero:

```
aeon: protocol version not supported  (-31002)
  what   client needs protocol version 99; this daemon speaks 1 (oldest supported: 1)
  means  the client and the daemon are from incompatible releases; nothing further on this connection will work
  todo   upgrade whichever side is older — the message names both version numbers
  fatal  yes — this connection is unusable
```

**It failed if** it connects anyway. A daemon that silently serves a client asking for v99 will fail later,
somewhere else, with a confusing error — which is exactly the class of bug versioning exists to prevent.

### The downgrade case cannot be demonstrated yet, and that is worth knowing

The design's other half — *a client needing an older supported version is served that version* — is what
keeps an editor extension working across a daemon upgrade. **It cannot be shown here**, because
`MinSupportedVersion` and `PROTOCOL_VERSION` are both 1: there is no older version to ask for.

It is not untested. `negotiate(want, min, max)` is a pure function with the bounds injected, exercised at
`min=1,max=2` across six cases — because testing it through the constants would be a `t.Skip`, and **a
skipped test is not a passing one**. This section becomes demonstrable at the first version bump.

```
./dist/aeon ping --protocol-version 1      # the oldest supported: served, not refused
```

---

## 4. Unknown fields are ignored, not rejected

```sh
printf '{"jsonrpc":"2.0","id":1,"method":"initialize","params":{"clientName":"demo","clientVersion":"0","protocolVersion":1,"somethingFromTheFuture":true}}\n' \
  | socat -T3 STDIO,ignoreeof UNIX-CONNECT:$LOCALRPC_ENDPOINT_PATH
```

**You should see** a normal successful result:

```json
{"jsonrpc":"2.0","id":1,"result":{"protocolVersion":1,"daemonVersion":"0.0.0-dev","capabilities":["subscribe"]}}
```

This looks like a trivial check and it is the **single property that makes the next fifteen slices
possible** (T3). Every one of them adds methods, fields and event kinds without bumping the protocol
version. A client that rejects a field it does not recognise turns every one of those additions into a
breaking change — silently, at the client, months later.

`internal/protocol/conformance/forward-compat.json` is the standing test of this, and its README says why
deleting it would be a mistake.

---

## 5. Subscriptions: watch one terminal from another

Terminal **A**:

```
./dist/aeon watch
```

**You should see** it print its subscription and wait:

```
watching s-9a22e3b7 · kinds=* project=* · from seq 11
  Ctrl-C to stop
```

**That line exists because a client that silently waits is indistinguishable from one that subscribed to
nothing** — which is the failure an entire error code is spent avoiding (O1).

Terminal **B**:

```
./dist/aeon ping
```

**Back in terminal A you should see** the daemon's own lifecycle events arrive live:

```
    11  subscription.created   connId=conn-7 filter={} from=11 subscriptionId=s-9a22e3b7
    12  client.connected       clientName=aeon-cli clientVersion=0.0.0-dev connId=conn-8 peerPid=1161255 peerPidAvailable=true peerUid=3002 protocolVersion=1
    13  client.disconnected    connId=conn-8
```

**The first line is your own subscription.** The demo did not predict that and it is correct: a
subscription being created is a daemon event like any other, and a watcher with no filter asked for
everything.

**`peerPid` is real and usable** — that is S1's peer credentials arriving in the protocol (D12). `peerUid`
is checked by the transport before a handler ever runs; a connection from another user is refused and
closed (P26).

That is the whole subscription path working — filter, delivery, sequence numbers — with no loops or steps
in existence (T8). The alternative was writing subscriptions blind now and first exercising them at S5.

### Now the filter

```
./dist/aeon watch --kinds 'loop.*'
```

**You should see** it subscribe and then receive **nothing at all** when you run `./dist/aeon ping` in
terminal B, because `client.connected` is not a `loop.*` event.

**It failed if** you see client events. One daemon serves every project on the machine (§1a), so a client
that receives everything and discards most of it is receiving, on a busy machine, mostly other people's
work.

And a wildcard that cannot mean what you intended is **refused**, not silently matched:

```
./dist/aeon watch --kinds '*.started'
# aeon: invalid subscription filter  (-31010)
#   what   kind pattern "*.started" has a `*` that is not a single trailing one; …
```

---

## 6. The cursor answers "did I miss anything"

With `watch` running, note the highest `seq` you saw. Stop it. Run `ping` twice. Then:

```
./dist/aeon watch --after 3
```

**You should see** it report where it resumed and then deliver everything from while you were away:

```
watching s-734c2f85 · kinds=* project=* · from seq 4
  Ctrl-C to stop

     4  client.disconnected    connId=conn-1
     5  client.connected       …
     6  client.disconnected    connId=conn-2
```

**It failed if** events are missing, or if any `seq` repeats or goes backwards.

### Two `seq` numbers reach you, and only one is a cursor

A notification carries **both**:

```
{"jsonrpc":"2.0","method":"event","seq":1,            ← the TRANSPORT's, per connection
 "params":{"seq":12, ...}}                             ← AEON's, per stream
```

| | |
| --- | --- |
| envelope `seq` | the transport's per-connection delivery counter. It advances even when a notification is dropped for a slow reader, so you can **detect** loss. **It restarts at reconnection.** |
| `params.seq` | Aeon's stream-scoped ordinal. **This is the cursor** `--after` takes — and since S4 it is the `event` table's rowid, so it **survives a daemon restart**. |

Use `params.seq`. The envelope's restarts exactly when a cursor is needed; Aeon's does not. The two are
complementary: a dropped count says *a gap exists*, and `--after` is how you recover from it — across a
reconnection and, since S4, across a restart.

### And a cursor older than what the daemon still holds

```
./dist/aeon watch --after 1
# watching … · from seq 2
```

If the daemon no longer retains that far back, `from` comes back **higher than your cursor + 1** and
`watch` says so explicitly, naming how many events are missing. **Reporting where you actually resumed is
the difference between a gap and a silent gap.**

---

## 7. Subscriptions die with the connection

```
./dist/aeon watch --kinds 'client.*' &
sleep 2
./dist/aeon debug subscriptions
```

```
1 subscription, 2 connections initialised

SUBSCRIPTION CONN       KINDS                    NEXT SEQ
s-15c354ff   conn-6     client.*                 18
```

Now stop it — **`kill %1`, not `pkill -f 'aeon watch'`**, which matches your own shell's command line and
will kill your terminal. (Measured. Twice.)

```
kill %1
./dist/aeon debug subscriptions
```

**You should see** an empty registry:

```
no subscriptions (1 connection initialised)
```

**It failed if** the subscription is still listed. A subscription outliving its connection is server-held
state keyed to a client that may never return — a slow leak on a daemon meant to run for weeks.

**`debug subscriptions` exists so this can be shown rather than only tested.** A registry that empties is a
claim; a command that prints it empty is a demonstration. It is marked a *debug* method, so S19's narrowed
HTTP surface will not mount it — a debug method on a public surface is operational control by another name.

---

## 8. The app shows the negotiated version

```
make dev
```

**You should see** a window reporting the **daemon's** version and the **negotiated** protocol version:

```
Aeon
daemon    connected · daemon 0.0.0-dev · protocol 1 (negotiated)
          endpoint /tmp/aeon-demo/run/rpc.sock
```

Stop the daemon and reopen: it goes to **not connected**, showing the socket path it tried — and **no
protocol number at all.**

**It failed if** a protocol number is present while the daemon is down. That would mean the app is
displaying a build-time constant and the negotiation it claims to have done never happened — and it would
pass a naive check while the whole protocol layer was broken. The TypeScript type makes it
*unrepresentable*: `protocolVersion` exists only on the connected variant.

**How this was verified without looking at a window.** The daemon's own log is the proof:

```
client connected: conn=conn-9 client=aeon-app/0.0.0 protocol=1 uid=3002 pid=1169115
client disconnected: conn=conn-9
```

`client=aeon-app` is the window. React called the Tauri command, Rust opened the socket, negotiated, and
closed. **In dev you will see two** — React StrictMode double-invokes effects — and both close cleanly,
which is also worth seeing.

---

## What this demo does **not** show

- **No model is called.** S3.
- **Nothing is stored — ~~and `seq` starts over on a restart~~. S4 CHANGED THIS.** *(amended 2026-10-15,
  by S4's T8.)*

  At S2 this step told you to restart the daemon and watch `seq` start over, because the counter lived in
  memory. **It no longer does.** S4's `event` table is the allocator: `seq` is an `AUTOINCREMENT` rowid, so
  it survives a restart, and a cursor held from before one resumes where it left off:

  ```
  ./dist/aeon watch --after 29
  # watching s-8642fb40 · kinds=* project=* · from seq 30
  #     30  daemon.started          ← a DIFFERENT daemon process from the one that emitted 29
  ```

  The step is rewritten rather than deleted, because the fact that it once reset is useful history for
  anyone reading the two slices in order — and because a limitation fixed in one place and still documented
  in another is worse than either. S4's demo §4 is the same check from the other side.

  What is still true at S2 **in isolation**: a daemon built with no store keeps the in-memory counter. That
  mode exists for tests, and `aeon serve` cannot produce it — the store opens before the daemon is built.
- **Retention is bounded, and is now a cache.** 1024 events in memory, a guess. At S2 a cursor older than
  that resumed at the oldest retained seq and **reported the gap**. With S4's store attached it is served
  from the table instead and there is no gap to report; the honest-gap behaviour remains the fallback if
  that read fails.
- **No loop, step, question or cost exists.** There is nothing to subscribe to but the daemon itself.
- **No HTTP and no MCP.** The same method table serves both at S19; nothing here mounts them, and
  `internal/protocol` imports nothing transport-related so that it can.
- **Windows cannot serve.** Found while writing the client: the framework's named-pipe transport is
  `ErrPlatformNotImplemented`, deliberately deferred upstream. A Windows build runs `version` and
  `--vectors` and cannot run `serve`. Recorded in `../../../docs/BUILD.md`.

The skateboard is S5. This is its steering.

---

## Findings from the walk

**Recorded rather than quietly fixed, except 9, 10 and 11 — those were defects in the binary.**

| # | Finding | Resolution |
| :--: | --- | --- |
| 1 | §1 said `./dist/aeon-linux-amd64 serve`; `make build` writes `dist/aeon` | **Demo corrected** (the same stale name S1's walk found) |
| 2 | §1 predicted `pong · 0.8ms`; a unix socket answers in **0.1ms** | **Demo corrected.** One decimal is kept so a sub-millisecond trip does not render as `0ms`, which reads as "did not happen" |
| 3 | §2 predicted one line, `aeon 0.1.0 · protocol 1 (negotiated) · daemon 0.1.0`. The real output is S1's multi-line block plus two daemon lines | **Demo corrected.** S1's Go-toolchain line is deliberately kept: cgo is on, so the toolchain is part of what the binary is |
| 4 | §2 and §4 used `nc -U /run/user/1000/aeon.sock` — **wrong path** (the namespace makes it `…/aeon/rpc.sock`) and **`nc -U` loses the reply** | **Demo corrected** to `socat -T3 STDIO,ignoreeof`, with the reason stated |
| 5 | §2's predicted error text was `"initialize required"` / `"ping was called before initialize"` | **Demo corrected** to the real text, which also names the connection. The shape was right |
| 6 | §3 wrote `aeon --protocol-version 99 ping`; **flags come after the verb** | **Demo corrected.** Each verb parses its own flags, which is also what keeps S1's "extra arguments are refused" true for the verbs that take none |
| 7 | §3's downgrade case **cannot be demonstrated**: min and max are both 1 | **Said so plainly**, with where it IS tested (`negotiate` with injected bounds, six cases) and when the demo becomes live |
| 8 | §5 predicted `subscribed s-7f3a · kinds: * · from seq 1`; the real line is `watching … · kinds=* project=* · from seq 11`, and a watcher **also receives `subscription.created` for its own subscription** | **Demo corrected.** The extra event is correct: a subscription being created is a daemon event, and a watcher with no filter asked for everything |
| 9 | `client.connected` rendered the pid as **`peerPid=1.130661e+06`** | **BUG, FIXED.** Decoding into `map[string]any` makes every JSON number a `float64`, and `%v` on a large one is exponential — so the one field whose entire purpose is to be copied into a `kill` command was unusable |
| 10 | The same payload printed **`filter=map[]`** — Go's map syntax on the wire's JSON | **BUG, FIXED.** The value arrived as JSON and a reader is thinking in JSON, so nested values render as JSON |
| 11 | `debug subscriptions` printed **"1 connections initialised"** | **BUG, FIXED.** Small, and the kind of small that makes output look unmaintained |
| 12 | Nothing in the demo mentioned that **two `seq` numbers** reach a client | **§6 extended.** A client using the envelope's number as a cursor would pass every test on one connection and silently lose events on the first reconnect |

**Two steps were added that the demo omitted**: the dev-endpoint setup (§Before you start), without which
the walk writes to the real store; and the `--after` case where the cursor is older than retention, which
is the only way to see `from` do its job.

**And one correction from the other direction**: §7's `pkill -f 'aeon watch'` — which the demo never said,
but which is the obvious thing to type — kills your own shell, because the pattern matches its command
line. It is now named in the demo, having been done twice during this walk.
