# Design — S69 attachments

## The one-line rule

**Bytes once, refs forever, blocks at the last moment.** Bytes cross the
socket at `attachment.put` and nowhere else; every stored artefact carries
references into frozen `@attachments/` revisions; base64 content blocks
exist only in the request assembled immediately before the adapter. Live
and replay agree **by construction** — the pinned revision cannot move —
which is why D17's byte-identity needs no discipline here.

## Why `loop_document` (ruling 1)

`WriteDocument`/`ReadDocument` already: gzip the bytes, digest the RAW
bytes, verify the digest on read ("the row is corrupt" is reported where
somebody would notice), and append immutable revisions. An attachment is a
document whose author is a door, not a person. The namespace
`@attachments/<sha8>-<sanitised name>` is reserved: `ResolveDocumentName`
refuses it on BOTH sides of the document door, so a receipt's pinned
revision can never be edited by somebody typing a `plan.md`, and the
document door cannot even read it — attachments are read through the
attachment door or not at all. Because the sha is in the name, the same
bytes answer the SAME revision (dedup) and different bytes are a DIFFERENT
frozen document: a `(document, rev)` reference can never move under a row.

## The either/or (M4 widened)

`Block` gains `Data`, `MediaType`, `Document`, `Rev`; `Validate` enforces
one shape per kind: **stored form** = `Document` + `Rev ≥ 1` (`Data` empty),
**wire form** = `Data` (`Document`/`Rev` empty), `MediaType` required
always, every foreign field refused by name. A reference reaching an
adapter is a bug upstream: the adapters COUNT it
(`attachmentDropped`, its own counter — request-direction drops never
blur into the response-direction `dropped` the (O1) step record reports)
and never half-send. The unknown-kind drop-and-COUNT of M4 is untouched;
only the kinds list in the error sentence widened.

## Receipts are the store's facts (ruling 2)

The client sends `{document, rev}` — the two facts a client can legitimately
know. The door reads the pinned revision and files `{name, document, rev,
mediaType, size, sha256}` from **what the store says**: the sanitised name,
the sniffed mediaType (the bytes' verdict), the stored size and digest. A
caller asserting "5 KB of png" asserts nothing. Empty lists are refused at
the door (`[]` is a statement somebody tried to attach and stopped; absent
is the honest third state) and the bounds (≤8, ≤40 MiB of STORED bytes —
resolved from the store, not from claims) refuse naming the bound. The
magic-byte verdict runs at put: declared vs sniffed, images/pdf exact,
text/* accepted against the text class; a lying extension is refused
naming both sides.

## The one expansion point (AT5)

`protocol.handleStep` expands, through a daemon-wired expander
(`Table.AttachmentExpand`), the prompt turn's and each history turn's
receipts into `model.Block` data blocks at compile time — `CompileInput.
PromptAttachments` and the history blocks fold into the same user messages
(words FIRST, B3 kept, blocks beside them). The steer drain expands at its
own boundary (`stepSteers.Pending` → `runtime.Steer.Attachments` →
`AppendUserTurn`/`steeredResults`): words and files enter the transcript as
ONE turn. Protocol never touches the store; the daemon owns the read; the
adapters see only wire-form blocks.

## Missing speaks (ruling 3)

At the DOOR a missing revision is refused (the reference was a lie — every
offender named, nothing filed). At the DRAIN — the row was written while
the revision lived — the absence rides instead of blanking: conversation
turns append one sentence per missing file to the step's NOTES (the opening
line prints them; nothing compiles from notes, so prompt bytes are
untouched), and a drained steer carries its absence as its own text block
beside the words. The UI renders the same state as a visibly broken chip
naming document and revision.

## Deviations (stated)

- **M4 widened to six kinds** — a stated doctrine change, argued with the
  operator before code; the four original kinds keep their exact
  discipline, and adapters' (M4) drop-and-COUNT pins still pin.
- **`capability.go` untouched**: it is the closed five wire-behaviour flag
  strings; documents-on-chat/completions is a fact about the WIRE FORMAT,
  so the adapter's refusal naming route-and-fix is the honesty mechanism —
  a per-route modality flag would be a category error and a new subsystem.
- **Image parts only on the user role** (both OpenAI wires); documents on
  any role at chat/completions REFUSE; responses carries both on user.
- **`input_file` filename fallback** chain Name → document tail → "attachment"
  (the tail rung pinned via a direct call — Validate's either/or means
  wire-form blocks never carry a Document).
- **`loop message` CLI verb is new** (none existed); projection decision:
  `loop.message` reaches the socket only — no MCP or HTTP surface projects
  it today (grepped) — so `attachment.put`/`attachment.read` follow it
  there and nowhere else; P34 holds (the CLI is a thin client of the
  socket doors, never the sole door).
- **Put result vs row receipt**: the put result echoes the operator's
  declared type (their claim, validated); the ROW files the sniff. A row
  is a statement about the bytes as they were proven.
