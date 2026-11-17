# S69 — attachments (bytes once, refs forever, blocks at the last moment)

## The ask, in the operator's words

"lets move s69" — then the standing ruling from the S68 decomposition:
attachments end-to-end, done "the propper way anthropic and openai do,
there are fields for that": capability content riding the person's turn,
visible in the window, never woven into words, never replayed as bytes on
every turn. The four rulings below were asked and answered (all Recommended)
before code.

## The rulings (asked, answered, binding)

1. **Bytes live in `loop_document`** under a reserved `@attachments/`
   namespace — no new byte table. The documents mechanism already stores
   bytes (gzip BLOB), verifies sha256 on read, and freezes revisions;
   that is exactly "store once, render from storage".
2. **A separate `attachment.put` door** — bytes cross the socket exactly
   once, at put; every later artefact (thread row, steer row, events, the
   bubble) carries RECEIPT REFERENCES.
3. **Missing revision at drain ⇒ send anyway, speak the absence** — a
   system-side sentence rides; the turn is never silently diminished and
   never forged into a blank (P31's missing-is-not-zero).
4. **`attachment.read` + image preview are in** — the bubble shows what was
   attached; the read is for RENDERING only, never for re-sending.

## What this slice is (ruling B of the A/B/C decomposition)

**B — attachments**: the model gains `image`/`document` block kinds (M4
widened to six, the same tagged-struct discipline); the store gains the
attachment door over `loop_document` plus receipt columns on
`chat_message`/`step_steer`; the socket gains `attachment.put`/
`attachment.read`; `loop.message`/`step.steer` take references and file
store-resolved receipts; the daemon's send-time expansion — the ONE join
point beside request compilation — is the only place a reference becomes
base64; the UI puts on send, clears on resolve, renders chips and previews,
and renders a missing file as a visibly broken chip speaking one honest
sentence; the CLI carries `loop message … --attach`.

Out of list (recorded in the spec, honoured): no new byte table, no base64
in the transcript, no re-send of read bytes, `loop.start` takes NO
attachments (the kickoff posts a message instead), HomeScreen gets no
attachment affordance (consistent with S68's quick-starter ruling), caps
fixed (10 MiB per put; ≤8 refs and ≤40 MiB stored bytes per message), and
the media type is whatever the bytes' magic prove, not what a caller claims.

Spec: `docs/superpowers/specs/2026-10-31-attachments-design.md`
(AT1–AT12, committed `385c0756`).
