# Plan — five tasks, in the order they ran

- **T1 — model + adapters**: six block kinds (image/document added to the four),
  either/or Validate, the three adapters' wire shapes, chat/completions
  document refusal, the reference-at-adapter count.
- **T2 — the store**: the `@attachments/` door over `loop_document`
  (shared revision writer, caps, allow-list, magic-byte verdict, sha-dedup,
  pinned reads) and the receipt columns on `chat_message` / `step_steer`.
- **T3 — the doors and the spine**: `attachment.put`/`attachment.read` on the
  Memory interface; `loop.message`/`step.steer` resolve refs at the door and
  file store-resolved receipts; the ONE send-time expansion at the compile
  boundary (prompt turn + history) and the steer drain's own boundary;
  missing-speaks; five conformance fixtures + method-table/handshake widening;
  `make protogen`/`check-protogen`/`guard`.
- **T4 — the app**: `src/lib/attachments.ts` (put once, cached pinned reads,
  data URLs, human sizes), the composer's draft FILES forwarded through the
  skills wrapper, put-then-send inside `useChatActions` (field absent when
  zero; clear-on-resolve was already the composer's discipline), receipt
  chips + image previews + the visibly broken chip naming document+rev on
  both person-side surfaces, the scripted bench doors.
- **T5 — the terminal door, the gates, the record**: `aeon loop message`
  (none existed) with repeatable `--attach`; the projection decision
  (socket-only); every suite measured; the slice records; one commit; merge.

Two agents died at T4 in the harness's tool outage (one mid-scout, one at
context exhaustion); the third pass did it in-session. The scout from the
second (host line numbers, wrapper shapes) proved exact.
