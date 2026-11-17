# Design — S7 Crash recovery (Stage 2 · Bicycle)

**Date:** 2026-09-06
**Status:** approved (design); not yet implemented
**Stage:** 2 · Bicycle, second of four
**Depends on:** S1–S6. **Independent of S8** — the two can run concurrently.
**Implements:** [`../ARCHITECTURE.md` §3a](../ARCHITECTURE.md#3a-crash-recovery-is-the-spine-with-a-different-trigger) and P27.

> Decision ids continue **K** (runtime) and **W** (tools). New runtime decisions start at **K29**, new tool
> decisions at **W16**.

---

## Context

**Almost all of this slice already exists**, which is the point of having sequenced it this way.

A suspended step exits its process and resumes cold from persisted state alone (S6). A crashed step is the same
problem reached by a different door: the process is gone, the store holds everything, and `ResumeStep` reconstructs
it. So there is **no recovery subsystem** — there is a scan, a decision, and the resume machinery S6 built.

What is genuinely new is one sharp problem. A suspension happens at a **clean point**: a `question` tool call, the
transcript consistent, everything flushed. A crash lands **anywhere** — including in the middle of a `bash`
command, at which point the record cannot say whether it ran.

That is the whole slice: **the record cannot say, so it must be made to.**

### The three cases, and how they are told apart

Recovery only considers steps whose `status` is **`running`**. That is why S5's T5 made `running`, `suspended` and
`orphaned` distinct values — a suspended step also has a `tool_use` with no result (the `question`), and without
the status distinction recovery would try to "recover" every loop that was correctly waiting for a human.

| Case | The transcript's tail shows | Recovery |
| --- | --- | --- |
| **A** | the last assistant turn's `tool_use` blocks **all** have results | `ResumeStep`. Identical to a suspension |
| **B** | no assistant turn at the tail — it ends on a user or tool message | The model call never committed and nothing executed. Re-issue it. Costs tokens twice; safe |
| **C** | an assistant turn with `tool_use` blocks lacking results | **Unknown.** A tool may have run, partly run, or never started |

### Case C, and the receipt's three states

A tool's re-run safety decides what happens (W16, P27):

| Class | Tools | Recovery |
| --- | --- | --- |
| `safe` | `read`, `glob`, `grep`, `webfetch` | re-run |
| `detectable` | `write` (same content converges), `edit` (exact-match, so a re-run **fails loudly** rather than corrupting), `todo` | re-run; a failure is information, not damage |
| `unsafe` | **`bash`**, alone | cannot re-run — the receipt decides |

**The receipt has three states, not two** — and the third is the one that earns it its place:

| Receipt state | Means | Recovery |
| --- | --- | --- |
| **absent** | the tool never started | re-run. Safe, because nothing happened |
| **present with a completion** | the tool ran and its result was captured; only the transcript write was lost | **append the captured result and continue.** Recovered |
| **present without a completion** | the tool started and the outcome is unknown | **fail the step**, naming the tool |

The middle row is why receipts are worth writing at all. Without them, a crash during the window between a
command finishing and its result reaching the transcript would fail a step whose work actually succeeded — and
with `go test ./...` that is minutes thrown away for nothing.

The third row is where guessing would be worse than failing. `git commit` run twice is not recoverable by
inspection, and a recovery that silently re-ran it produces a repository nobody can reason about. So the step
fails with a reason naming the tool, and the blueprint's redo edge decides (K33, S8).

## Goal

A daemon that starts, finds every step its predecessor was running, and either resumes it, re-issues its last
call, recovers its captured tool result, or fails it honestly — naming which.

## Decisions

| # | Decision | Why |
| --- | --- | --- |
| K29 | **Recovery is a scan of `status='running'` steps on daemon start.** `status` is what distinguishes a crashed step from a suspended one | A suspended step also has an unanswered `tool_use`. Without the status distinction, recovery would try to recover every loop correctly waiting for a human |
| K30 | **Case determination is a pure function** over the transcript's tail plus the receipts | It makes the hardest logic in the slice testable **without crashing anything** — a crash is expensive to stage and impossible to stage exhaustively |
| K31 | **The receipt has three states**: absent (re-run), present-with-completion (**recover the result**), present-without (fail naming the tool) | The middle state is the reason receipts exist: a crash between a command finishing and its result being written would otherwise discard work that succeeded |
| K32 | **The runtime writes receipts through an injected narrow `ReceiptWriter`**, not a store handle | K6 says the runtime persists nothing and opens no store. A one-method interface preserves that while letting the write happen where only the runtime knows a tool is about to act |
| K33 | **A step failed by recovery carries a distinguishable reason** naming the tool and the receipt state | S8's redo edges act on it, and "failed" without a cause is a loop that retries the same thing |
| K34 | **Recovery is idempotent and each step's outcome is committed atomically** | A crash *during* recovery is ordinary, and the next start must not double-recover |
| K35 | **Recovery runs automatically, is observable, and can be refused** with `--no-recover` | Automatic because that is the point; observable because a silent multi-minute recovery on start looks like a hang; refusable because debugging a recovery loop needs a way in |
| W16 | **Every tool declares a re-run class.** `bash` alone is `unsafe` | The class is a property of what the tool does, so it belongs on the tool. And there being exactly one unsafe tool is worth knowing: the receipt machinery guards one thing |
| W17 | **`edit` is `detectable`, not `unsafe`** | Its exact-match semantics mean a re-run finds the original string absent and **fails loudly** rather than applying a patch twice. Detectable failure is not damage |

## Architecture

```
aeon/internal/store/
  receipts.go     tool_receipt — a table, not a column (see §Amendments)

aeon/internal/runtime/
  recover.go      DetermineCase (pure — K30), RecoverStep
  receipt.go      the ReceiptWriter interface (K32)
  loop.go         + write a receipt around an unsafe tool

aeon/internal/tools/
  *.go            + RerunClass() on every tool (W16)

aeon/internal/daemon/
  recover.go      the start-up scan (K29, K34, K35)
```

### The receipt is a table, not a column

`S4-store/design.md` said the receipt would be "a `step_message` column". **That was wrong, and the ordering is
why.**

A receipt must exist **before** the tool runs, and the `step_message` row holding that tool's result does not
exist until after it finishes. A column on a row that is not there yet cannot record that something is about to
happen.

That line is corrected in S4's design, and `ARCHITECTURE.md` §3a — which was silent on storage shape — now states
the table and the reason, alongside the three receipt states this slice establishes.

So: a small purpose-built table, which also keeps the transcript exactly the canonical message list rather than
a message list with recovery metadata threaded through it.

```sql
CREATE TABLE tool_receipt (
  id           INTEGER PRIMARY KEY AUTOINCREMENT,
  step_id      INTEGER NOT NULL REFERENCES step(id) ON DELETE CASCADE,
  tool_use_id  TEXT NOT NULL,
  tool         TEXT NOT NULL,
  arg_digest   TEXT NOT NULL,          -- so a recovered result can be matched to its call
  started_at   TEXT NOT NULL,
  completed_at TEXT,                   -- NULL is the unknown state (K31)
  result       TEXT,                   -- the captured tool result, when completed
  is_error     INTEGER,
  UNIQUE (step_id, tool_use_id)
);
```

`arg_digest` exists so a captured result can be **matched to the call it belongs to**. Recovering a result onto a
different call would be worse than failing, and the digest makes that detectable rather than assumed.

### The scan

```
daemon start
  │
  ├── --no-recover ? → log the count of running steps and stop (K35)
  │
  └── for each step WHERE status='running':
         DetermineCase(transcript, receipts)     ← pure (K30)
             │
             ├── A → ResumeStep
             ├── B → re-issue the model call
             └── C → per unanswered tool_use, by re-run class (W16):
                       safe / detectable       → re-run
                       unsafe, no receipt      → re-run
                       unsafe, receipt+done    → append the captured result, continue
                       unsafe, receipt only    → fail the step, naming the tool (K33)
         │
         └── commit the outcome atomically (K34)
```

Recovery **never re-resolves context and never resets bounds** — the manifest carries the hashes, the iteration
count and the absolute deadline, exactly as a resume does. A step cannot escape its budget by crashing
repeatedly, which is the same reason S5 stored the deadline absolutely.

**One loop, three doors now.** `RunStep`, `ResumeStep`, and recovery — and recovery reaches `ResumeStep` rather
than the loop directly, so there is still exactly one loop and one resume path.

## The demo

See [`DEMO.md`](DEMO.md): `kill -9` between turns and watch it resume; kill mid-model-call and watch it re-issue;
kill during a `bash` and watch it **fail honestly**; kill after the command finished but before the write, and
watch the receipt **recover the result**; confirm a suspended step is **not** touched; and crash during recovery.

## Testing

| What | How |
| --- | --- |
| Case determination | **Pure-function tests over constructed transcripts** — every case, every receipt state, no crash staged (K30) |
| A | A step whose tail is complete resumes and finishes identically to one never interrupted |
| B | A tail with no assistant turn re-issues; the model is called again and nothing was executed twice |
| C, safe | An unanswered `read` is re-run |
| C, detectable | An unanswered `edit` is re-run, and a second application **fails loudly** rather than corrupting |
| C, unsafe, no receipt | Re-run, because nothing happened |
| C, unsafe, receipt + completion | **The captured result is appended and the step continues** — the receipt's reason to exist |
| C, unsafe, receipt only | The step **fails**, with a reason naming the tool and the receipt state |
| Digest mismatch | A captured result whose `arg_digest` does not match its call is **not** used; the step fails |
| Suspended untouched | A step with `status='suspended'` is not scanned, not resumed, not failed |
| Idempotence | Recovery run twice produces the same outcomes; a crash mid-recovery leaves the rest recoverable |
| Bounds | A recovered step's iterations and deadline come from the manifest |
| Receipts written | An unsafe tool's receipt exists **before** the process spawns, asserted by ordering |
| `--no-recover` | Reports the count and changes nothing |

**A real `kill -9` appears only in the QA suite.** Unit tests construct the states directly, which is what K30
is for — staging a crash at a precise instant is unreliable, and staging every instant is impossible.

## Out of scope

- **Blueprint redo edges.** S8 consumes K33's reason; this slice produces it.
- **The bash policy.** **S11.** A recovered `bash` is as unguarded as a fresh one.
- **Orphan reconciliation across machines.** `(pid, host)` identity was v1's answer to a detached-child design;
  the daemon owns loops now (P29), so a `running` step whose daemon is gone is simply a crashed step.
- **Re-run classes for tools that do not exist.** `write`, `edit`, `glob`, `grep`, `webfetch`, `todo` get their
  class **declared** here and enforced when they arrive at S11.
- **The app.** S9.
- **Compaction changes.** S6's compaction is unaffected: a recovered step compacts as any other.

## Amendments to earlier slices

| Where | Change | Status |
| --- | --- | --- |
| `../ARCHITECTURE.md` §3a | **Extended**, not corrected: it was silent on the receipt's shape and stated only two states. It now carries the three-state table, the reason the middle state earns the receipt its place, and the table-not-column rule | **landed with this design** |
| `S4-store/design.md` | Its "the receipt is a `step_message` column" line is **wrong** and is replaced | **landed with this design** |
| `S4-store/DEMO.md` | Its illustrative migration output named `0006_step_receipt`, a migration S4 does not create | **landed with this design** |
| S4 (code) | Gains the `tool_receipt` migration. Its `step.status` values already carry the distinction K29 needs | T1 |
| S5 | Every tool gains `RerunClass()`; `bash` alone returns `unsafe` |
| S6 | `ResumeStep` is unchanged — recovery reaches it rather than duplicating it |

## Open questions

None. The receipt's `arg_digest` algorithm is an implementation choice; what matters is that a mismatch is
detected rather than assumed.
