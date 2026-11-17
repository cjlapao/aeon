# SUPERVISION — the inbox, the channels, and what they never do

**Marked FOR HUMAN REVIEW.** This is the external-facing guide: what the
notification plane sends, to where, and how a receiver should treat it. It
states promises a receiver may rely on; a reviewer should read it against
`internal/notify/` and `docs/aeon/S58-supervision/design.md` before those
promises are quoted anywhere outside this repository.

Aeon's attention plane has two halves. The **inbox** is Aeon asking its
person — evaluated live from loop state, never a queue that can go stale.
The **channels** are that asking reaching outward — an HTTPS receiver the
person configured, with the person's secret, carrying a fixed envelope. The
person's machine stays the source of truth; the channel is a mirror of
*events*, not of data.

## The inbox

`inbox.list` / `inbox.summary` answer **from live state at read time** (SV1):
no inbox table, no cache, nothing to go stale. Sources (the closed census a
reviewer can enumerate in `internal/attention`): blocked loops, waiting
questions, watchdog firings, budget warnings and cap hits, stop requests an
agent recorded, finished loops — each with a **severity** (`info`, `warn`,
`critical`) and a stable **attention key** (source + subject + why), which
is what "seen" and "snoozed" attach to.

| Door | Who | What it may do |
| --- | --- | --- |
| `inbox.ack` | person | hide an item from the default view (never delete; counted) |
| `inbox.snooze` | person | hide until an instant, ≤ 7 days out; expiry computes at read |
| `inbox.unack` | person | undo either; marker rows, append-only |
| `mission.status` | person | per-root overview: counts, spend floor, next-ready, last movement |

Agents get **five read twins** (`inbox_list`, `inbox_summary`,
`mission_status`, `notify_channel_list`, `notify_class_list` — own-view,
`Mutates:false`, catalog family `supervision`) and **no write of any kind**:
no agent can ack, snooze, edit a class, or touch a channel. The proof is
`TestSupervisionWritesNeverReachOtherDoors` (six doors, each canaried).

## The channel envelope

A send is one HTTPS `POST`, `Content-Type: application/json`, signed, with
three headers: `X-Aeon-Signature` (hex HMAC-SHA256 over the exact body,
`crypto/subtle` compare on the receiver side), `X-Aeon-Delivery` (the
delivery id), `X-Aeon-Timestamp` (the `at` below, replayed for signature
verification). Body — **exactly these fields, ids, enums and person-written
templates only** (SV-D5: no log lines, no diffs, no prompt text, no
secrets — never):

| Field | Present | Meaning |
| --- | --- | --- |
| `v` | always | envelope version, `1` |
| `id` | always | delivery id — dedupe/idempotency key at the receiver |
| `at` | always | RFC3339 instant of the event |
| `event` | always | `test` · `inbox.digest` · or the event kind (e.g. `loop.finished`) |
| `kind` | events, test | the kind with its variant (`loop.blocked.cap-hit`); empty for the digest |
| `title`, `severity` | always | person-editable template output; severity is one of `info · warn · critical` |
| `deepLink` | most | an `aeon://` link — **navigation only, see below** |
| `rootSlug`, `loopSlug` | single-subject events | the loop/root it is about |
| `count` | digest | the one synthetic-with-content: a number (SV-D6) |

The HMAC secret is **by reference**: `<state>/aeon/credentials/notify-<channelId>.key`,
`0600` under a `0700` directory, written only through `notify.channel.*`
flows and the file store — never a database column, never an answer field,
never logged (SV7; the same mechanism S3's route keys use). Channels are
**born disabled**; enabling requires the secret to exist. The seed never
lays a channel — outward paths exist only because a person made them.

## What never leaves the machine

Everything except the seven envelope fields above: loop state, transcripts,
code, credentials, the store itself. The dispatcher itself never opens an
outbound path with zero channels configured — `TestOneChannelMeansExactlyThatHost`
and `TestZeroChannelsMakeNoConnectionAtTheTransport` (nettest, permanent)
hold the plane to C11's allowed list: an enabled channel's host joins the
list; nothing else connects (SV14/N1).

## SSRF rules (what the sender refuses)

- **HTTPS only.** No plaintext receiver.
- **No private or loopback addresses** — DNS answers and literal IPs are
  checked before connecting (`dial.go`), so a public name resolving to
  `127.0.0.1` or a LAN address is refused at the dial, and a rebinding TTL
  cannot slip through (the check is on the resolved address, at connect
  time). `allowPrivate` is an explicit per-channel operator escape hatch,
  off by default.
- **Redirects are never followed** (`NoRedirects`): a 302 that points
  inward is a refusal, not a hop.
- Timeouts on every request; a dead receiver **never blocks or slows a
  loop** (bounded queue, suppression logged, the loop never waits — SV-D8).

## Delivery outcomes

Every attempt, including every suppression, lands in the delivery log
(`notify.delivery.list`, append-only): `sent` · `failed` (transport or
non-2xx; attempts counted) · `secret-missing` (enable raced a deleted key)
· `suppressed-dedup` (same attention key inside 10 minutes) ·
`suppressed-disabled` · `suppressed-quiet` (the channel's quiet hours) ·
`suppressed-rate` (the per-channel rate budget) · `suppressed-overflow`
(the queue was full — the event is honestly *dropped*, never silently
retried forever). A digest event (`inbox.digest`, the hourly compensation
while the daemon was down) reports a **count**, never a replayed list.

## Deep links: three doors, never an answer

The only forms the app accepts: `aeon://inbox`, `aeon://loop/<id>`,
`aeon://root/<id>` (id `[A-Za-z0-9_-]{1,64}`). A link is a **place, never
an answer** (SV9): it can navigate, it cannot ack, stop, or answer — no
query, no fragment, no percent-escapes, no extra segments; anything else is
refused with one of five fixed words (`wrong-scheme unknown-door
bad-target oversize-target carries-extra`), and the raw URL is never
rendered. One fixture (`ui/test/fixtures/deeplinks.json`) drives BOTH
parsers — the Rust one that accepts the OS-opened link and the web one that
routes — so the two can never disagree.

## How to configure a receiver safely

1. **Verify the signature**: HMAC-SHA256 the raw body with the shared
   secret and compare in constant time (`crypto/subtle.ConstantTimeCompare`)
   against `X-Aeon-Signature` **before** parsing the body. A missing or
   mismatched signature is a discard, not a reject-with-detail.
2. **Check the timestamp**: accept `X-Aeon-Timestamp` only inside a small
   window (a few minutes); a stale-but-valid signature is a replay.
3. **Treat `deepLink` as navigation only**: render it as a link a human
   chooses to click; never fetch it, never execute it, never assume the
   loop still exists (links outlive loops — the app itself says "That loop
   no longer exists" when one is gone).
4. **Idempotency**: key on `id`; retries happen (backoff ladder), so an
   unprepared receiver sees duplicates.
5. The secret is **one channel's** — rotating it affects one receiver; the
   file's permissions (`0600`) are the last line, so keep the state
   directory yours alone.

## Where the proofs live

`TestSupervisionWritesAreAHumanDoor` + the six negatives +
`TestSupervisionGuardsHaveTeeth` + `TestOriginalNarrowRecordsGuardsAreByteIdentical`
(`cmd/aeon/supervision_guard_test.go`); the network permanents in
`internal/notify/dispatch_test.go` and
`internal/nettest/channel_connections_test.go`; the door shapes and
conformance fixtures in `internal/protocol/` and
`internal/protocol/conformance/notify/`. Slice record:
`docs/aeon/S58-supervision/`.
