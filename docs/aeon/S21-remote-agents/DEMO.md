# S21 Remote agents — the demo

**Stage 4 · Space shuttle.** *Fourth of six. This is the first time Aeon talks to a machine it does not own.*

Everything above the transport is S14's, unchanged. Everything here is about the four things the local case
got for free.

Runs against a **fake peer** in `testdata/` with its own certificate — never a real remote service.

---

## 1. Nothing is configured, and nothing is reachable

```
aeon remote list
```

**You should see the absence stated:**

```
no remote peers configured.
nothing outbound is reachable.
```

**It failed if it probed anything.** **Configured, never discovered** (RA3) — no `/.well-known` crawl, no
mDNS, no registry. The same discipline MC3 applied to MCP servers and AP10 to plugins, for the same
reason: **a peer that arrives without a config edit is a peer nobody decided on** — and under RA4 it is
also an egress nobody authorized.

---

## 2. Configure a peer, and watch an unpinned certificate refused

Configure the fake peer **without** its pin:

```
aeon remote add --id docs-peer --address wss://peer.example:8443
```

**You should see it refused at configuration time:**

```
REFUSED  docs-peer has no pinned identity
         a remote peer requires a pin. the system CA set alone is not sufficient (RA2).
```

**Why that is not paranoia:** a valid certificate proves TLS to *some* host. It does **not** prove this is
the peer you consented to. **Pinning is S15's signed-card argument one layer up** — *"is this really who
it claims"* needs an answer that is not trust.

Now add it properly, with a pin, and point it at a peer presenting a **valid certificate from a real CA
for the right hostname (`peer.example`, chain-valid to `ca.pem`, expires 2036 — `openssl verify -CAfile ca.pem peer.pem` says `OK`) — but not the pinned one:

```
aeon remote ping docs-peer
```

```
REFUSED  docs-peer: certificate is valid but does not match the pin
         pinned   SPKI sha256:9f2a…
         offered  SPKI sha256:41bd…
```

**It failed if that connected.** This is the test that distinguishes pinning from TLS.

---

## 3. There is no plaintext, and no exception for localhost

```
aeon remote add --id local-peer --address ws://localhost:9000 --pin …
```

```
REFUSED  plaintext is not supported. use wss:// (RA1)
         for a local agent, the unix socket is already better — see P26.
```

**It failed if a flag exists to allow it.** A plaintext option is the one somebody uses in production, and
the local case **already has a socket with filesystem permissions and peer credentials.** If a peer is
remote, it is remote.

---

## 4. A skill request is data leaving the machine

This is the genuinely new risk, and the step worth reading twice.

```
aeon policy add --type RouteAccess --target all-agents --action deny \
                --spec '{"route":"remote:*","state":"deny"}' --id no-code-leaves

aeon skill invoke docs-peer/summarise --params '{"path":"internal/store/schema.go"}'
```

**You should see it refused, with nothing sent and nothing read:**

```
refused: egress to docs-peer · RouteAccess "no-code-leaves" · target all-agents
         · source operator · no expiry
         the request was not assembled. no file was read.
```

**That last line is the decision** (RA4). The check happens **before the request is built**, not before it
is sent.

**Because a request assembled and then discarded has already read the files it was going to carry** — and
in a long-running daemon that assembly may well be logged. Both placements produce the same refusal
message; only one of them is actually safe.

**And this is why egress is a policy rather than a setting.** *"Never send code to a non-local route"* was
already sayable about models (S18's `RouteAccess`). The same sentence has to be sayable about agents,
authored by an operator, with a record.

---

## 5. Allow the egress, and the answer is a stranger's

Remove the deny, then:

```
aeon skill invoke docs-peer/summarise --params '{"path":"README.md"}'
aeon step show <step-id> --prompt | grep -A3 'docs-peer'
```

**You should see the result labelled:**

```
[untrusted — skill result from remote agent "docs-peer"]
  This file documents …
```

**Always, with no exception** (RA5). AS12 and AP9 taken to their conclusion: **first-party means reviewed
in our pull request**, and nothing on another machine qualifies — whatever it claims about itself.

Try the claim:

```
aeon remote list --provenance
```

```
docs-peer   remote   (peer claimed first-party — ignored)
```

---

## 6. An async remote Task suspends. There is no held connection

```
aeon skill invoke docs-peer/deep-analysis --params '{"path":"."}' --async
aeon loop show <loop>
```

**You should see the step suspended, and nothing waiting:**

```
step  9f31a0   SUSPENDED   awaiting remote task 7c2e (docs-peer/deep-analysis)
```

```
aeon debug goroutines | grep -c remote
0
aeon debug connections --outbound
none
```

**No goroutine and no open connection** (RA7).

**Why a held connection is not an option:** it dies with the daemon, and **P2 exists precisely so a
suspended step survives that.** So kill the daemon and bring it back:

```
# Ctrl-C, then
make dev
```

The step is still suspended, waiting. **The completion will arrive as an inbound call** — which is the next
step, and the reason it needs its own guard.

---

## 7. The callback is verified **and bound**

The peer completes the Task by calling our own narrow surface.

First, a correctly signed completion for a Task we never created:

```
# fake peer posts a completion with task id "deadbeef"
```

**You should see it refused:**

```
REFUSED  completion for unknown task "deadbeef"
         signature verified · no such task was created by this daemon
```

Then a correctly signed completion carrying **another** Task's id:

```
REFUSED  completion for task 7c2e came from docs-peer, which was not its callee
```

**It failed if either succeeded.**

**Read why carefully.** Verification proves *who sent it*. **Binding proves *what they were asked*.** A
callback that is authenticated but unbound is **a way to inject a skill result into a transcript** — a
fabricated answer that the loop then treats as the agent's work.

**That is exactly what S19's N6 refuses for questions** (*"a fabricated prompt, once answered, feeds a
value into a transcript as though the model had asked for it"*), and the same reasoning applies here.

Now the real one:

```
step  9f31a0   ok   remote task 7c2e complete
```

**And no second listener was opened** — that endpoint is S19's mounting (RA7, P25). Check:

```
aeon debug listeners
  socket   /run/user/1000/aeon/rpc.sock
  http     127.0.0.1:7777   (narrow surface, S19)
```

**Two, not three.**

---

## 8. A remote outage degrades. It does not hang

Point a peer at something that never answers:

```
aeon skill invoke slow-peer/anything --params '{}'
```

```
timeout: slow-peer did not answer within 30s
```

**And that is not an empty result.** Check:

```
aeon step show <step-id> | grep -A2 slow-peer
```

**You should see `timeout`, not `[]` and not "no results"** — MC9's discipline, which matters more here
because **remotely the failure is somebody else's outage**, on a schedule nobody controls.

**And it is not retried.** A retry doubles the wait and **the model cannot tell one outage from two.**

---

## 9. Cost you cannot see stays unknown

```
aeon loop chain <loop>
```

**You should see:**

```
loop research
└─ step analyse        project-planner-loop   $0.0180  (known)
   └─ task 7c2e        docs-peer (remote)     unknown — the peer reported no usage

   analyse total:  at least $0.0180   ← cannot be known while a child is unknown
```

**It failed if the remote task reads `$0.00`.**

**A remote agent's spend is not observable here.** Treating absence as zero — or inventing a number — is
**the precise failure the entire cost design exists to prevent**, and a remote peer is the easiest place
for it to reappear, because the field is genuinely empty.

**And the parent cannot be `known`** while a child is `unknown` (P32). An aggregate over uncertain values
is itself uncertain.

---

## What this demo does **not** show

- **No Aeon as a remote agent.** Asking a remote agent is this slice. **Aeon publishing its own card over
  HTTP for somebody else to call** is S19's surface and a different threat model — it would make us the
  thing being pinned.
- **No service mesh.** No discovery, no health-based routing, no fleet retries. A peer is a configured
  endpoint with a pinned identity.
- **No federation.** A chain through a remote peer is bounded by AS7's depth and cycle rules counting
  **our** hops. **What the peer does internally is not observable and is not our graph.**
- **No certificate authority.** If a peer wants a client certificate, it is configured like any credential
  — the environment or a path, never argv (C17).
- **No new policy type.** Egress is `RouteAccess` (S18); who may invoke what is still `SkillAccess` (S10).

The shuttle can reach other machines. **S22 gives it memory of its own, and S23 gets a stranger to a
running loop in five minutes.**

---

## Walked 2026-10-28 — the ledger

Walked against the **real fake peer** (`internal/remote/testdata/certs/` — the committed PKI) and a
**real daemon** (`aeon serve --http` with `AEON_REMOTE_PEERS` at a peers file whose key is a PATH, C17).
What the walk measured first-hand:

- **§7 LIVE through the narrow surface.** A completion signed by the peer's own HMAC key (an external
  byte-equivalent signer over the length-prefixed canonical — the server agreed with it) for task
  `deadbeef`: **404 `completion for unknown task "deadbeef" — signature verified · no such task was
  created by this daemon`** — the predicted sentence verbatim. Tamper one hex char → **401 "did not
  verify"**, store counters proven unmoved; unknown peer `attacker` → the same answer a bad signature
  gets (no enumeration); stale timestamp → refused at the window **after** verification (the order,
  observed).
- **§7's listener count: two, not three.** `ls -l` the endpoint (`srw-------`) + `ss -ltn` shows exactly
  one TCP listener. `aeon debug listeners` does not exist (F-4); the tools that do exist measured the fact.
- **§6 LIVE.** `aeon debug goroutines | grep -ci remote` → **0** on a real daemon.
- **§2's certificate is genuinely valid apart from the pin** — confirmed out-of-band, not on trust:
  `openssl verify -CAfile ca.pem peer.pem` → `OK`; `checkend 0` → not expired (2026→2036);
  cert-DER sha256 `9c198d52…`, SPKI sha256 `d8c8be1b…`, rotated key's SPKI `892d283a…` — a
  provably different pin, which is QARA-03's refusal made checkable.
- **§1's absence is stated at the only surface a peer ever sees** — see F-1/F-5.

## Findings

| # | What the walk found | Disposition |
| --- | --- | --- |
| **F-1** | `aeon remote list` does not exist — **no task in S21 built any `aeon remote` CLI verb**. The step's promise (the absence stated) is nevertheless kept where it matters: an unwired daemon's `remote.complete` answer says *"no remote peers are wired in this daemon — completions have nothing to bind to"* (measured LIVE, 503) | **Documented.** T5's precedent is the rule: Findings, not code — a CLI family nobody specified is invented by its own task, not smuggled into a docs walk (P34: the daemon surface is primary and it ANSWERS) |
| **F-2** | Same for `aeon remote add/ping`: the config-time refusals (unpinned, scheme, literal key bytes) exist and are gated — at the package level. No CLI mount presents them | **Documented** (as F-1); the package gates remain the witnesses (`TestUnpinnedPeerRefusedAtConfig` et al.) |
| **F-3** | **The outbound invoke mounting exists in no task.** `aeon skill invoke` is not a verb; `agent.invoke` refuses a `remote:` callee (AS2 — no signed card); no production code calls `EgressGate.Ask`/`Dialer.Call`. §4/§5/§8/§9 are walked at the package gates (T4's three-zero spies, T5's label gates, T8's cost table). The design's own gate forbids the fix here: **`invoke.go` and the rest of S14 are unedited** | **Priced, not softened.** The caller task carries: the routing decision, the assembler, the 4-line `ChainCheck` export (T7's note said "T9 carries" — the walk found T9 has no caller to mount it beside, and an export with zero callers edits S14's unedited list for dead code; it ships WITH its first consumer, which is the honest placement) |
| **F-4** | `aeon debug listeners` and `debug connections --outbound` do not exist | **Documented**; `ss -ltn` + `ls -l` measured the real fact (two listeners, `0600` socket); `debug goroutines` DOES exist and answered 0 (LIVE) |
| **F-5** | **The mount flattened the unwired-by-name answer.** `narrow/errors.go` rendered `ErrStoreUnavailable` as Message + retryHint and **dropped `Data.What`** — so T6's promise ("answers the absence by name") and BOTH S20 webhook wiring answers arrived at the peer as a generic store outage: a retry that can never help and a doctor pointer at a fine store. Found by posting to a real unwired daemon; the package-level tests pinned the handler's error and could not see the mount's rendering | **Fixed + pinned** (T9): the `What` now rides the 503 (measured verbatim after the fix) and `TestStoreUnavailableNamesWhatIsAbsent` guards the mount. A refusal names the thing refused, at every surface |
| **F-6** | §2's address said `peer.test`; the committed testdata certificate is `CN=peer.example` | **Rewritten to truth** (§2), with the openssl validity record added to the step |
| **F-7** | §7's wrong-callee and replay-of-a-LANDED-task legs could not be walked live: they need an awaiting async task, and only the unmounted caller (F-3) creates one. The unknown-task leg — the one that needs nothing — WAS walked live; a replay of a refusal was walked live (re-refuses identically: the cache guards landings, not verdicts) | **Documented**; package gates remain the witnesses (T6's three binding gates + counters); the live legs become amendments when F-3's caller ships |
| **F-8** | §9's `aeon loop chain <loop>` does not exist; the rollup surface that DOES exist is `agent.task.show` on the socket (live: `read the task: store: agent task 999: store: not found` — an honest miss, O4) | **Documented**; the §9 facts are pinned by T8 + S14's rollup gates (`unknown — peer-reported-no-usage`, parent never `known`) |

**The "does not show" section verified after T1–T8:** Aeon-as-remote-agent is still out — the inbound
surface receives COMPLETIONS only (verified and bound); nothing publishes a card, serves a skill, or
answers an ask from outside (N2 holds: greps find no serving path in `internal/remote`); no discovery
(N1 — `TestNothingIsDiscovered` greps peer.go's own bytes); no federation (N3 — the bound reads our
rows; a peer's self-reported internals cannot reach it, T7); no CA (N4 — credentials are env/path only,
`KeyRef` holds no bytes); no new policy type (RA4 — the closed set still holds seven,
`TestNoNewPolicyTypeWasAdded`).

