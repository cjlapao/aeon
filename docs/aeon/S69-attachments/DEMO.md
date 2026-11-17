# S69 demo — one file, put once, riding forever

Two lanes: the scripted bench (no daemon) and the CLI/ssocket walk.

## Bench lane — the three receipt states, no daemon

1. Open the bench's **Agent chat** page, Scenarios mode, scenario
   **Gallery**. Message `g9` shows the states side by side: `screenshot.png
   · image/png · 18.0 KB` with a real preview image under the bubble (the
   scripted read door answers), `quarterly-report.pdf · application/pdf ·
   1.0 MB` as a chip (the window does not embed pdfs), and `gone.png` as
   the VISIBLY BROKEN chip: "could not be read — @attachments/deadbeef-
   gone.png at revision 900 is no longer in this loop's store". No blanks,
   no fake icons.
2. Type a message with two files attached in the composer — the action
   log prints `attachment.put shot.png · 8 bytes (scripted)` once per
   file, then `loop.message → "…" · 2 attachment(s)`; the posted bubble
   carries the chips. Name one file `gone.png` and its chip goes broken
   the same honest way. The person's words in the bubble are EXACTLY what
   was typed.
3. What it does NOT show: real bytes over a real socket (next lane), and
   real image decoding of the fixture pixel.

## Socket/CLI lane — the human's terminal path

From `aeon/` with `XDG_STATE_HOME` at a throwaway and a resting loop
(`aeon loop start --blueprint talk "…"`, let it rest):

```
aeon loop message <id> "read my chart" --attach chart.png --attach notes.md
  put chart.png · @attachments/<sha8>-chart.png rev 1 · image/png · 41127 bytes · <sha8>…
  put notes.md · @attachments/<sha8>-notes.md rev 1 · text/plain · 312 bytes · <sha8>…
  loop <id> · turn started with 2 attachment(s)
```

4. The receipts printed are the STORE's facts (its size, its sha, the
   type its magic bytes proved). Attach `fake.png` whose bytes are really
   a JPEG and the command REFUSES before sending: the sentence names the
   claim and the sniff, and NO message was posted.
5. `aeon loop show <id>`'s thread rows carry the receipt JSON; the chat
   window shows the chips and previews from the same fields.
6. Delete the revision's bytes (test seam) and start the turn again: the
   turn RUNS — with the words and the attachments that remain, and the
   absence speaks (a notes sentence on the step for the conversation
   half, its own block in the steered transcript, the broken chip in the
   window).

Unrun at record time: the live-provider walk (the model actually receiving
the base64) — fixtures pin the request shapes; named, not glossed.
