# CORPUS.md — what the history corpus indexes, and what stood between

V15 is a property, not a list: **nothing is indexed that has not passed
through something that could have redacted it.** This file is where the
property is APPLIED — the full answer to Q8, so that a future corpus is
decided by the same test rather than case by case. `TestEveryIndexedSource-
HasARedactionStep` holds this file and `history.go`'s `IndexedHistorySources`
to the same standard: every source names what stood between it and the
index; a source without one FAILS.

## The list (Q8)

| Source | What stood between it and the index | Where it enters |
| --- | --- | --- |
| Brief artefacts — `brief-artefacts` (`produces:` files) | **written to a file a person reads and reviews** — the artefact gate (`advance.go`: a declared artifact not written is a rejection) means a human-facing document passed a human-facing gate | `IndexHistoryFile` |
| Plan / discovery artefacts — `plan-artefacts`, `discovery-artefacts` | **written to a file a person reads and reviews** — same door, same reason | `IndexHistoryFile` |
| Handoffs — `handoffs` | **a declared schema with stated field purposes (S13's O6)** — only `HandoffFieldNames` render, field name included; undeclared keys are refused by the renderer (the schema's boundary is the index's boundary) | `IndexHandoff` |
| Chat messages — `chat-messages` | **human-authored** — role must be `user`; a model turn can quote tool output verbatim and would be step_message arriving through a side door | `IndexChatMessage` |
| **`the per-step transcript table`** | **NOTHING.** Raw `bash` output, file contents, whatever a command printed. **Never indexed.** | — |

Every row's content additionally passes `redactSecrets` before any store
door or embed call — the credential-shape net under the review step, not a
replacement for it ("never indexed un-redacted" means both).

## Why `step_message` is permanent (N6)

Not behind a flag. Not for a trusted project. Not for a project whose
author asserts their transcripts are clean. **The author cannot know**: a
transcript holds whatever a command printed, and nobody reviewed a
command's output before it landed. A flag would be a promise nobody is in
a position to make. `TestNoFlagEnablesTranscriptIndexing` greps for any
configuration path, build tag, or environment variable that would enable
it — there is none and the test keeps it none. `history.go`'s block comment
says the same thing in the code, because the next person to want it will
look for the flag first.

## The two-way wall (V2, D12)

History and code never merge: different chunking (prose takes T2's
line-window path — the NORMAL path, not a degraded one; sections are
boundaries where a document has them), different staleness, different
value. Merging them would make *"search only past decisions"* impossible,
which is the demo's first question. Asserted both ways
(`TestHistoryQueryReturnsNoCode`: a history query returns no code AND a
code query returns no history — the corpora are two rows, two chunk
populations, and a query names one).

## Deciding a NEW corpus (the test a future task must pass)

1. Name what stands between the source and the index. "Nothing" means the
   corpus is refused (the `step_message` row).
2. It is a ROW in the corpus table (V2's kind column + a validator entry),
   not a schema change.
3. Its staleness is its own (V8's hash rule applies per corpus, V12's
   per-root holds).
4. Add it to `IndexedHistorySources` (or its corpus's equivalent) AND this
   file, in the same commit — the test compares the two.
