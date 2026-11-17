# Brief — S22 Retrieval

**Implements:** [`design.md`](design.md) · **Demo:** [`DEMO.md`](DEMO.md)
**Inherits:** V1–V12 from [`docs/_archive/aeon-v1-horizontal/11-retrieval`](../../_archive/aeon-v1-horizontal/11-retrieval/design.md). New decisions begin at
**V13**.
**Stage:** 4 · Space shuttle, third of four. Depends on S4, S5, S18. **Independent of S19, S23.**
**Amended 2026-10-26 for P34** (`ARCHITECTURE.md`): *"A capability that is not reachable through the daemon
surface is not finished."* As originally written, `search` was a native tool only (T6) and reindex status
and triggering were CLI-only (`cmd/aeon/index.go`, T8) — a human in the app had no door onto either. D15/V21
below are the amendment, mirroring S34's `mcp.status`/`mcp.check` pattern exactly: a read method for
availability/staleness, an operator-triggered action method for reindex, and `search` itself gains a socket
twin alongside the agent tool.

> **Template deviations from `proposal-brief-protocol`, stated rather than silent:**
> **(a)** Implements a design, not a proposal — §7 records where this brief overrides it.
> **(b)** §14 is the demo contract, present in every slice of this plan.
> **(c)** **This is the only optional slice in the plan**, and D1 is unusual: it requires the whole product's test suite to
> pass with this slice's package **deleted**. §12's rules are shaped around keeping that true.

---

## 1. How to use this brief

> **Constant. Copy verbatim.**

This is a **brief, not a plan.** It states what must be true when the work is done, what must not change along
the way, and where to look to find out the rest. It contains no task breakdown and no implementation code.

1. **Read** the design and the demo, then §7 for where this brief overrides the design.
2. **Investigate** — §10 names what to read. Do not plan before you have those answers.
3. **Plan and decompose** — §12 gives the rules your task sequence must obey.
4. **Self-check** — §13, against your finished plan, before dispatching anything.

Where this brief states a decision, it is settled. Where it poses a question, answer it from the code.

**On the self-check:** every answer needs an artifact — a command's output, an enumeration, a quoted line.

---

## 2. Definition of Done

| # | Check |
| --- | --- |
| **D1** | **With `internal/retrieval` deleted, the whole product's test suite passes** and `search` is absent from the tool registry. **Not degraded — absent** (V13). This is the optionality claim, and it is only true if it is tested this way. |
| **D2** | **Empty is distinguishable from absent.** With retrieval present and the index empty, `search` **exists** and reports `index-empty` rather than returning nothing. |
| **D3** | **Four availability reasons**, and **no `extension-missing` state exists** — asserted by grep, since S4's DB18 makes it impossible (V19). |
| **D4** | **Keyword-only says so.** With no embedding route, results arrive **and the response states it is keyword-only** (V9). |
| **D5** | **A manifest entry records how it was resolved** — `declared`, or `retrieved` **with its query** (V14). Asserted on the stored manifest. |
| **D6** | **A resumed step does not re-resolve.** The retrieval spy is at **zero** on resume and on recovery (K13, K14, S7). |
| **D7** | **`step_message` is never indexed** (V15). Asserted over the index's contents after a step with tool output, **and** by grep over the indexer's inputs. |
| **D8** | **Results are `file:line` ranges and carry no chunk text** (V4). |
| **D9** | **A model or dimension mismatch refuses to query, reports a reindex cost, and reindexes nothing** (V17). |
| **D10** | **A reindex never delays a step**, and the index **reports its staleness** (V18). |
| **D11** | **Embedding cost reports through S18's three states**; an unmetered embedding route is `unknown` with a reason (V10, V16, M30). |
| **D12** | **The two corpora never merge.** A history query returns no code and a code query returns no history (V2). |
| **D13** | **Hybrid fusion works both ways**: a structural query is won by FTS5, a conceptual one by vectors, and both appear in fused results (V3). |
| **D13a** | **The benefit is measured** (V20): the same step run with and without resolved context, and **the resolved run makes fewer `grep`/`read` calls**, counted. For the only optional slice in the plan, this is the criterion for keeping it — *"if it is absent the feature is not earning its cost."* |
| **D14** | **`make test` and `make lint` green**, `make fmt` leaves no diff. |
| **D15** | **Search and reindex management are daemon methods, not only a native tool and a CLI** (P34). `retrieval.status {projectRoot}` answers availability (the same four reasons, V19) and per-corpus staleness (V18) — read-only, the app's door onto what `aeon index status` shows. `retrieval.reindex {projectRoot, corpus}` triggers a reindex, locked exactly like S34's `mcp.check` and S18's `routes.probe`: operator-triggered only (no silent reindex, N8, now enforced identically on both doors), bounded, async (D10 unchanged), single-flight per corpus/root. `search.query {projectRoot, corpus, query}` is the socket twin of the `search` tool — same resolver function, same `file:line`-only shape (D8), so a human exploring the app can search the project the way an agent step can. |

**D1, D5 and D6 are the three that matter for the optionality claim; D15 is the one that matters for P34.**
D1 is the optionality claim. D5 is what keeps a guessed context honest. D6 is what stops retrieval quietly
making a resumed step behave differently from the one that suspended. D15 is what keeps this slice's own
surface from becoming the next S22-shaped entry on a future S37's retrofit list.

**D1–D13 must each exist as named tests**, D1 additionally as a CI configuration that builds without the package.

D14 is a standing condition, never a task's sole acceptance criterion (R1).

---

## 3. The problem

`README.md` says *"S22 is the only genuinely optional slice, and the product must work fully without it."* That is a
load-bearing claim, and **memory is the thing most likely to become quietly load-bearing.**

**Three things this slice must get right.**

**Absence is not degradation.** V9 already covers degradation — no embedding route means keyword-only, said out loud.
Absence is different: **an empty result is a claim about the repository.** A handler that searches and finds nothing
concludes there is nothing. So absent means the tool does not exist (V13) — the same reasoning S19's N14 applied to
operational control, arriving somewhere else.

**V7's differentiator composes awkwardly.** Retrieval feeding context declaration is the prize, and it was named before
declaration was built. Now K13 freezes the manifest and K14 forbids re-resolution — so retrieval feeds it **once**. Which
creates a problem the tool half does not have: **a retrieved manifest is a guess**, and the step should know (V14).

**V1's evidence is stale.** It verified a Node API; S4's DB18 statically links the extension, so `extension-missing` cannot
happen and the availability model has **four** reasons (V19).

## 4. Goal

*"Have we decided this before?"* answered from past briefs and handoffs, and a step arriving with its context already
resolved — with the product working **fully** when none of it is there.

## 5. Shape and the seam

```
                                internal/retrieval/   ← DELETABLE (D1)
loop writes files ──▶ async reindex (V18) ──▶  index (vec + FTS5, per root)
                                                 │                │
step declaration ──▶ resolve ──▶ manifest entry: retrieved + query (V14)
                                                 │                │
                     search tool ──▶ file:line ranges, never text (V4)
                     registered ONLY when built (V13)             │
                                                                   │
            retrieval.status / retrieval.reindex / search.query ──┘
            same resolver + index, app's door (D15, V21, P34)
```

### The seams

**The package boundary is the optionality boundary.** If `internal/retrieval` is gone, `search` is not registered and
nothing else changes. That is what makes D1 testable rather than aspirational, and it means **no other package may import
this one except through a narrow interface** the rest of the system can satisfy with a no-op.

**Retrieval feeds the manifest once.** K13's freeze and K14's no-re-resolution are not negotiable, and D6 asserts them with
a spy rather than a comment.

**Embedding is a route** (V16). Which means the cost path, the three states, the unmetered case and the provenance are all
S18's, already built. Nothing here reimplements them.

**Indexing is triggered by the loop, not watched** (V8). *"The loop already knows it wrote files"*, and a watcher is *"a
daemon, a permissions problem and a source of phantom reindexes."*

## 6. Locked decisions

**Inherited:** V1 (`sqlite-vec` in the store's database — conclusion only, see V19), V2 (two corpora, never merged), V3
(hybrid vector plus FTS5), V4 (`file:line`, never chunk text), V5 (complements `grep`, never replaces it), V6 (model id and
dimension stored; mismatch forces reindex), V7 (retrieval can feed context declaration), V8 (content hash per chunk;
loop-triggered incremental reindex), V9 (no provider means keyword-only, said out loud), V10 (embedding cost through the
three states), V11 (symbol-aware where cheap, line-window otherwise), V12 (index per project root).

**New:** V13 (absent means the tool does not exist), V14 (manifest records its resolution), V15 (nothing indexed that no
redaction pass has seen), V16 (embedding is a route), V17 (mismatch refuses with a cost estimate), V18 (async reindex; the
index reports staleness), V19 (four availability reasons; V1's evidence superseded), V20 (the benefit is measured, from the
QA suite's QAV-09), **V21 (search and reindex each have a daemon method, locked the same way S34's `mcp.check`
and S18's `routes.probe` are: reindex never fires without an explicit `retrieval.reindex` call — no auto-fire
on `retrieval.status`, on a page open, or on a search that finds a stale index; `search.query` and the
`search` tool call the identical resolver function, so a divergence between what the app returns and what a
step's `search` call returns is a bug, not a feature).**

## 7. Overrides to the design

| # | Design says | This brief requires |
| --- | --- | --- |
| O1 | The package is deletable | **CI must build and test at least one configuration without it.** A deletability claim nobody builds is a claim that stops being true the first time someone adds an import, and it will be discovered by a user rather than a test. |
| O2 | A manifest entry records `retrieved` with its query | It must also record **the score**. A model deciding whether to trust a retrieved file benefits from knowing it was the fourth result at 0.61 rather than the first at 0.93, and the score is already computed. |
| O3 | A mismatch reports a reindex cost | The estimate must be labelled **an estimate**, with its basis — file and chunk counts. A figure presented as exact for work not yet done is the same class of error as a confident total over a `partial` cost. |
| O4 | The index reports its staleness | Staleness must be **per corpus**, not global. Code churns and history does not; one number would report the history corpus as stale because a source file changed. |
| O5 | The design does not say what happens when an embedding call fails mid-index | The indexed portion **stands**, the failure is recorded per chunk, and a retry resumes rather than restarting. A large index that must restart on any failure is one that never completes on a flaky connection. |
| O6 | Retrieval feeds the manifest at declaration | If resolution **returns nothing**, the step proceeds with its declared context and the manifest **says retrieval returned nothing** — it is not an error, and it is not silence. A step whose retrieval found nothing behaves differently from one where retrieval was never asked. |
| O7 | `search` complements `grep` (V5) | The tool's **description must say so**, in the words a model reads. V5's stated reason is *"so a model does not reach for similarity when it wants a definition"* — which only works if the description carries it. |
| O8 | Two corpora | A query must **name its corpus explicitly**; there is no default. A default corpus is a query whose scope depends on a setting the caller did not state, and V2's whole point is that the scopes differ. |

## 8. Non-goals

- **N1 — No language-aware parser per language.** V11: a heuristic plus a line window is most of the benefit; a parser per
  language is a project.
- **N2 — No filesystem watcher.** V8.
- **N3 — No external vector database.** V1 and P10: one install.
- **N4 — No reranking model.** Hybrid fusion is the ranking; a reranker's value here is unmeasured and it costs a model
  call per query.
- **N5 — No cross-project search.** V12.
- **N6 — No transcript indexing, ever** (V15). Not behind a flag, not for a trusted project. **Permanent.**
- **N7 — No re-resolution on resume or recovery** (D6). Also permanent: it would make a manifest meaningless.
- **N8 — No silent reindex** (V17).
- **N9 — No new cost state.** S18's three are complete; V16 reuses them.

### Standing non-goals

- **Never index `step_message`** (N6).
- **Never write outside `aeon/`**, except `docs/` and `docs/test_suites/`.
- **Never point an automated test at a live embedding provider.** Recorded fixtures, always.
- **Never run `git clean`.**

## 9. Repo invariants

**I1 — `internal/retrieval` is deletable** (D1, O1), and CI proves it.

**I2 — No package imports `internal/retrieval` except through an interface with a no-op implementation.**

**I3 — Retrieval feeds the manifest once, at declaration** (D6, N7).

**I4 — `step_message` is never indexed** (D7, N6).

**I5 — Results carry no chunk text** (D8, V4).

**I6 — There is no `extension-missing` state** (D3, V19).

**I7 — A query names its corpus** (O8).

**I8 — Embedding cost and its states are S18's, not reimplemented** (N9).

**I9 — Public API changes ship with their tests in the same task.**

**I10 — Search and reindex are reachable from the daemon surface, not only `cmd/aeon/index.go` and the
native tool** (P34, D15, V21). `retrieval.status`, `retrieval.reindex` and `search.query`'s socket handlers
call the same underlying functions the CLI and the `search` tool call — one implementation per capability,
multiple doors.

---

## 10. Investigation brief

### 10.1 What to read

1. **`docs/_archive/aeon-v1-horizontal/11-retrieval/design.md`** — V1–V12 in full, and the correction note on V1.
2. **`../S4-store/design.md`** — S25, the static linking, and the **four** availability reasons it already enumerated.
3. **`internal/runtime/compile.go` and the manifest** (S5) — where V14's provenance goes, and what K13's freeze means in
   code.
4. **`internal/runtime/resume.go`** (S6) and **`recover.go`** (S7) — the two paths D6 must prove do not re-resolve.
4a. **`docs/test_suites/aeon-retrieval.md`** — QAV-01 to QAV-09, **read before planning.** QAV-09 supplied V20's
   measured-benefit criterion, QAV-05 and QAV-06 already cover the mismatch cases sharply, and QAV-01's subject —
   `sqlite-vec` absent — is now impossible under S25 and has been rewritten.
5. **`internal/model/cost.go`** and S18's route handling — V16 and V10 reuse both.
6. **`internal/tools/class.go`** (S7's T2) — `search`'s re-run class, and the committed list it joins.
7. **The `sqlite-vec` Go bindings' API** — enough to write the migration; S25 means no load step.

### 10.2 The patterns to copy

- **S18's route mechanism** for the embedding model (V16). Do not build a second provider path.
- **S9's three-state renderers** for availability and staleness.
- **S11's committed-fixture discipline** for the tool registry, so `search`'s conditional registration is asserted.
- **Discriminated results over errors** (S4's DB19) — an availability reason is a domain outcome.

### 10.3 Confirm each from the code

| # | Question | Where |
| --- | --- | --- |
| Q1 | Does S4's migration ledger accommodate the vec and FTS5 tables, and does fresh-equals-migrated still hold with a virtual table? | `internal/store/migrate.go`. **A virtual table may not compare equal under S4's normalised `sqlite_master` check** — if so, that check needs a stated exception, not a weakening |
| Q2 | How does the tool registry register tools today, and can a tool be conditionally absent? | `internal/tools/`, `internal/handler/registry.go`. D1 depends on it |
| Q3 | Where exactly is the manifest frozen, and what shape does an entry have? | `internal/runtime/`. V14 adds fields to it |
| Q4 | Do resume and recovery share a context path, or two? | `internal/runtime/`. D6 must cover **both** |
| Q5 | Does S18's route type accommodate an embedding model, or does it assume a chat model? | `internal/model/route.go`. **If it assumes chat, say so** — V16 needs the same route mechanism, not a parallel one |
| Q6 | What does S11's committed re-run class list say about `search`? | `internal/tools/testdata/rerun_classes.json`. It was declared absent; it becomes present here |
| Q7 | How does the loop know it wrote files (V8), and is there a hook? **Shared with S30 Code Graph's own §7 Q2** — same question, same likely location; whichever slice is built first should answer it at the tools/daemon layer, not inside its own package, so the other can subscribe to the identical signal. | `internal/action/git_actions.go`, `internal/tools/write.go` |

### 10.4 Project-specific questions

| # | Question |
| --- | --- |
| Q8 | What exactly does the history corpus index? Produce the list, and for each, **what stood between the content and the index** (V15). |
| Q9 | What is the chunking heuristic, and what is its line-window fallback (V11)? |
| Q10 | How is the reindex cost estimated (O3), and what is its basis? |
| Q11 | What does a resolution that returns nothing record (O6)? |

---

## 11. Behavioural contract

- With the package deleted, the suite passes and `search` is absent (D1, I1, O1).
- Empty is `index-empty`, distinguishable from absent (D2).
- Four availability reasons; no `extension-missing` (D3, I6).
- Keyword-only announces itself (D4).
- A manifest entry records `declared` or `retrieved` **with query and score** (D5, O2).
- Retrieval feeds the manifest once; resume and recovery re-resolve nothing (D6, I3, N7).
- `step_message` is never indexed (D7, I4, N6).
- Results are `file:line`, no text (D8, I5).
- A mismatch refuses, prices a reindex **as an estimate with its basis**, and reindexes nothing (D9, O3).
- Reindex is async, never delays a step, and staleness is **per corpus** (D10, O4).
- An embedding failure mid-index leaves the indexed portion standing and resumes (O5).
- Embedding cost through S18's three states; unmetered is `unknown` with a reason (D11, I8).
- Corpora never merge; a query **names its corpus** (D12, O8, I7).
- Hybrid fusion works both ways (D13).
- A resolution returning nothing is recorded, not silent (O6).
- `search`'s description says it complements `grep` (O7).
- `retrieval.status`, `retrieval.reindex` and `search.query` exist as daemon methods, calling the same
  functions the CLI and the `search` tool call (D15, I10, P34). `retrieval.reindex` never fires without an
  explicit call — no auto-fire anywhere (V21, N8 extended to this door).

---

## 12. How to decompose

- **R1** — every task is gated by at least one **named test**.
- **R2** — a task that adds behaviour adds its test in the same task.
- **R3** — a file in more than one task's `files_expected` needs a stated reason.
- **R4** — **the deletability test comes early, not last.** D1 is the slice's claim, and a build configuration added at the
  end will find imports accumulated across eight tasks. Add it with the first task that creates the package.
- **R5** — **the index and its availability model are one task.** The four reasons are properties of the index's state, and
  a model added afterwards is one shaped by whatever the index happened to do.
- **R6** — **context resolution is its own task** (V7, V14), and it comes after the corpora exist. It is the differentiator
  and the place K13's freeze is either honoured or broken.
- **R7** — **the `search` tool is its own task**, because its conditional registration is D1's other half.
  **Its socket twin (`search.query`) ships in the same task** (D15) — the resolver function is one thing
  with two callers, and splitting them across tasks risks two implementations.
- **R13** — **`retrieval.status`/`retrieval.reindex` ship in the same task as the index/availability model**
  (R5) — the read method is a thin wrapper over state that task already owns.
- **R8** — no task over roughly 300 changed lines or 12 files without justification.
- **R9** — task files contain no deliberation: no "hmm", "let me", "on reflection", "I think", "not sure",
  "FIXME". `TODO(engineer):` in a code skeleton is the one permitted form.
- **R10** — a task carries no open question.
- **R11** — the final task walks `DEMO.md` and amends the QA suites.
- **R12** — **no task edits `docs/_archive/aeon-v1-horizontal/11-retrieval/design.md`** (V1's correction is already landed) or builds a second
  provider path (I8).

## 13. Pre-dispatch self-check

### A. Mechanical

| # | Run | Pass condition |
| --- | --- | --- |
| A1 | `grep -rniE "\bhmm\b\|\blet me\b\|\bon reflection\b\|\bFIXME\b\|\bI think\b\|\bnot sure\b" tasks/` then `grep -rn "TODO" tasks/ \| grep -v "TODO(engineer)"` | Both empty (R9, R10) |
| A2 | List every task with `est_changed_lines` and file count | Nothing over ~300 lines without justification (R8) |
| A3 | List every task with the **test name** that gates it | Every task has one (R1) |
| A4 | List every file in more than one task's `files_expected` | Each justified (R3) |
| A5 | Name the task that adds the **no-retrieval build configuration** | It must be the first task creating the package (R4, O1) |
| A6 | Paste Q8's list: what the history corpus indexes, and what stood between each item and the index | **A missing list blocks the plan.** V15 is a property, and the list is where it is applied |
| A7 | Paste Q1's answer | **If a virtual table breaks S4's fresh-equals-migrated check, the answer is a stated exception, not a weakened check** |
| A8 | Paste Q4's answer | Whether resume and recovery share a context path. **D6 must cover both** |
| A9 | Paste Q5's answer | Whether S18's route type fits an embedding model. **A parallel provider path is forbidden** (I8) |
| A10 | `grep -rn "retrieval" ` over every planned non-retrieval package | Only through the no-op interface (I2) |
| A11 | `make fmt` then `git status --porcelain` | No output |

### B. Scope

| # | Check | Ref |
| --- | --- | --- |
| B1 | Confirm no task writes a per-language parser | N1 |
| B2 | Confirm no task adds a filesystem watcher | N2 |
| B3 | Confirm no task adds an external database | N3 |
| B4 | Confirm no task adds a reranker | N4 |
| B5 | Confirm no task enables cross-project search | N5 |
| B6 | **Confirm no task indexes `step_message`, under any flag** | N6, I4 |
| B7 | **Confirm no task re-resolves on resume or recovery** | N7, I3 |
| B8 | Confirm no task reindexes without asking | N8 |
| B9 | Confirm no task adds a cost state or a second provider path | N9, I8 |
| B10 | Quote the `out_of_scope` lines covering N1–N9 | §8 |

### C. Acceptance

| # | Check | Ref |
| --- | --- | --- |
| C1 | Map each of D1–D13 to a task and a named test | §2 |
| C2 | For D1, confirm the test **builds without the package** and CI runs that configuration | D1, O1 |
| C3 | For D2, confirm empty and absent are asserted as **different** observations | D2 |
| C4 | For D5, confirm query **and score** are recorded | D5, O2 |
| C5 | For D6, confirm the spy is at zero on **both** resume and recovery | D6, Q4 |
| C6 | For D7, confirm **both** assertions — index contents and indexer inputs | D7 |
| C7 | For D9, confirm the reindex spy is at **zero** on a mismatch | D9 |
| C8 | For D10, confirm staleness is asserted **per corpus** | D10, O4 |
| C9 | Confirm no test calls a live embedding provider | §8 |

### D. Ordering

| # | Check | Ref |
| --- | --- | --- |
| D1 | Confirm the no-retrieval build configuration lands with the first package task | R4 |
| D2 | Confirm the index and availability model are one task | R5 |
| D3 | Confirm context resolution is its own task, after the corpora | R6 |
| D4 | Confirm the `search` tool is its own task | R7 |

### E. Structure

| # | Check | Ref |
| --- | --- | --- |
| E1 | Confirm nothing imports `internal/retrieval` except through the no-op interface | I2 |
| E2 | Confirm the manifest is fed once | I3 |
| E3 | Confirm no indexer input includes `step_message` | I4 |
| E4 | Confirm no result carries chunk text | I5 |
| E5 | Confirm no `extension-missing` state exists | I6 |
| E6 | Confirm `retrieval.status`, `retrieval.reindex` and `search.query` are registered socket methods, each calling the same function its CLI/tool counterpart calls | D15, I10, P34 |
| E7 | Confirm `retrieval.reindex` never fires without an explicit call | V21 |

### F. Project-specific

| # | Check |
| --- | --- |
| F1 | Paste Q8's list with what stood between each item and the index (V15). |
| F2 | State the chunking heuristic and its fallback (Q9, V11). |
| F3 | State how the reindex cost is estimated and on what basis (Q10, O3). |
| F4 | State what a resolution returning nothing records (Q11, O6). |
| F5 | State `search`'s re-run class and confirm the committed list is updated (Q6). |
| F6 | State how the loop signals that it wrote files (Q7, V8). |
| F7 | State how the `grep`/`read` call-count comparison is made (V20, D13a) — which step, run how, counted where. |

## 14. The demo contract

> **Present in every slice of this plan.** A slice is not done when its tests pass. It is done when someone who
> did not build it can follow `DEMO.md` and watch the thing work.

[`DEMO.md`](DEMO.md) is a deliverable, and the final task (R11) walks it and fixes whatever does not match.

**§1 takes the slice away before showing what it does**, and that ordering is the point. `make test` must pass with the
package gone, and `search` must be **absent from the tool list** — not present and empty. **An empty result is a claim
about the repository**, and a handler that searches for something and finds nothing concludes there is nothing.

**§3 is the differentiator, and its second half is the one that matters.** A manifest showing `retrieved` with its query is
useful; **suspending and resuming and getting an identical manifest** is what proves K13's freeze survived contact with
retrieval. If a resumed step re-resolved, it could get a different context from the one that suspended, and the manifest
would stop meaning anything.

**§5 must refuse and price, not answer or spend.** Answering across two embedding spaces gives *"plausible, wrong
results — the worst failure a retrieval system has"*; reindexing unasked spends real money. Both halves.
