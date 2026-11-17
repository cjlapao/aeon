# BACKLOG.md — the deferred-findings backlog (S17 T8)

**Q10, answered here.** Where the file lives, and what an entry looks
like — the two facts every reader of a deferred finding needs.

## Where the file lives

The backlog lives in **the project the loop works on**, not in Aeon:

```
<project root>/docs/unresolved-pr-comments.md
```

`internal/loop/backlog.go`'s `BacklogPath` is the one definition of that
path; nothing else computes it.

**Why that name and not `docs/BACKLOG.md`:** v1 wrote deferred review
findings to `docs/unresolved-pr-comments.md`, and the person triaging
this file six months from now greps for the path they already know. The
S17 spec names this *document* `aeon/docs/BACKLOG.md`; the runtime file's
naming diverges deliberately, and the divergence is recorded (spec says
one thing, v1 muscle memory says another — the record is the tiebreaker
and the record says v1).

## What an entry looks like

One line per finding, appended, never rewritten:

```
- PR #42 · 2026-10-26 · build/review · n+1 overflow risk at pager.go:42 — non-blocking
```

Every entry names, in order:

| Part | Where it comes from | Why required |
| --- | --- | --- |
| `PR #N` | the **evidence row** the `forge.openPR` action recorded (Q7: numbers come from evidence, never from prose) | a finding nobody can attach to a PR cannot be triaged |
| `YYYY-MM-DD` | the date the review passed with the finding | the backlog is a history; an undated entry cannot be aged |
| `lane/step` | the position that produced it | tells the reader which ladder rung to revisit |
| the finding | the reviewer's own words (the review handoff's reason on an ok) | paraphrasing at write time is a second truth |

A file is created with a header the first time a finding is appended;
an ok that reports nothing appends nothing — a backlog full of empty
entries is noise that trains nobody to read it.

## The properties, and the tests that hold them

| Property | Test |
| --- | --- |
| Appended, never rewritten — prior bytes survive as a prefix | `TestBacklogIsAppendOnly` |
| Every entry names its PR and its date | `TestBacklogEntryNamesPRAndDate` |
| ok-with-findings appends; ok-without appends nothing | `TestEngineAppendsReviewFindings`, `TestEngineOkWithoutFindingsAppendsNothing` |
| A finding that cannot be recorded **blocks** — deferred, never lost | the `backlog-append` block in `advance.go` |

The append-only guarantee is an `O_APPEND` flag, not a discipline: the
only open in `backlog.go` is `O_APPEND|O_CREATE|O_WRONLY`, and there is
no rewrite path in the file.

## What is NOT here

No thread resolution. The loop **honours** unresolved threads where a
forge reports them (`ListUnresolvedThreads`, honoured at the merge gate)
and **never resolves** them — that is a write into someone else's
conversation, and nothing in the method needs it. The grep guard is
`TestThreadsAreNeverResolved`.
