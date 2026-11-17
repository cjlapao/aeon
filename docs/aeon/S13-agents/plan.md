---
slice: S13-agents
title: Agents
stage: 3
stage_name: Car
position: 2 of 4
brief: docs/aeon/S13-agents/brief.md
design: docs/aeon/S13-agents/design.md
demo: docs/aeon/S13-agents/DEMO.md
depends_on: [S8-blueprints, S11-permissions]
blocks: [S16-git-forge, S17-full-ladder]
task_count: 11
---

# Plan — S13 Agents

**Most of this slice is prose.** Twenty-five handler bodies rewritten by hand from 2,902 lines of originals, plus the
mechanical guards that are the only automatable check on them.

The guards come first (R4). Landing 25 rewrites before the check that validates them means re-reviewing 25 rewrites by
hand.

## Task sequence

| Task | Deliverable | Gate |
| --- | --- | --- |
| **T1** | The handler file format and loader; policy from S11's extractor | Policy byte-identical per agent |
| **T2** | The mechanical guards — `aeon handlers verify` — and the drop-record format | The check **fails** on an original body |
| **T3** | The handoff schema | A declared-but-absent field is a `reject` |
| **T4** | Rewrites 1–4: `golang`, `python`, `javascript`, `rust` | All four pass verify; `golang-engineer` writes working Go |
| **T5** | Rewrites 5–10: `sql`, `react-typescript`, `vue-typescript`, `websocket`, `tauri-react`, `tauri-vue` | Verify, plus one run each |
| **T6** | Rewrites 11–15: `code-reviewer`, `verifier`, `qa-engineer`, `qa-test-spec`, `debugger-engineer` | The three largest originals |
| **T7** | Rewrites 16–20: `project-planner`, `intent-clarifier`, `investigator`, `technical-writer`, `memory-steward` | Verify, plus one run each |
| **T8** | Rewrites 21–25: `docker`, `terraform`, `security`, `legal-advisor`, `ui-design` | Verify, plus one run each |
| **T9** | `git-loop`'s accounting — no handler, but its 372 lines dispositioned | Every line in one of three categories |
| **T10** | The resolver, its table, and registry wiring | Duplicate tags are a load-time error; unknown tags refuse |
| **T11** | Demo walk, QA suite | `go test ./...` passes on the demo's output |

**T4 is deliberately four**, not five (R5). The shape of one rewrite determines the shape of 25, and discovering the
shape is wrong on the twenty-second is expensive.

## Ordering rationale

- **T1 first** — nothing can be loaded, let alone verified, without the format.
- **T2 before any rewrite** (R4). Its gate is the **negative control**: the check must fail on an original. A guard
  that passes everything would let 25 unverified rewrites land.
- **T3 before the rewrites** because each body declares its handoff fields, and a schema invented after twenty-five
  bodies is a schema that fits none of them.
- **T4 small and first among the rewrites**, establishing the pattern. Its `golang-engineer` is also the demo's §1, so
  the highest-consequence rewrite is done while attention is freshest.
- **T5–T8 grouped by domain family** (R6), so a reviewer holds one kind of judgement at a time. T6 carries the three
  largest originals — 204, 287 and 293 lines — and is the batch most likely to lose something.
- **T9 after the rewrites** because `git-loop`'s disposition depends on knowing what the other 25 absorbed.
- **T10 after enough rewrites to resolve to** (R7). Wiring a resolver with two handlers proves nothing about a table of
  ten tags.
- **T11 last.**

## Files

| File | Tasks | Reason for overlap |
| --- | --- | --- |
| `internal/handler/load.go` | T1 | — |
| `internal/handler/verify.go`, `cmd/aeon/handlers.go` | T2 | — |
| `internal/handler/handoff.go` | T3 | — |
| `internal/handler/bodies/*.md` | T4–T8 | Disjoint sets of files; no body is touched by two tasks |
| `internal/handler/drops/*.md` | T4–T9 | Same, plus `git-loop.md` in T9 |
| `internal/handler/resolve.go` | T10 | — |
| `internal/handler/registry.go` | T10 | Grows from 2 entries to 25. One task, because it is a serialization point |
| `internal/handler/testdata/handlers.json` | T10 | The committed list S8 established |
| `internal/blueprint/validate.go` | T10 | Sole change: the resolver reserve list becomes a real check (D13) |
| `docs/test_suites/aeon-agents.md` | T11 | Amended, never rewritten |

**No task writes to `opencode/`.** It is read by T2's negative control and T4–T9's rewrites, and never modified (A10).

## The 25, by family

| Batch | Handlers |
| --- | --- |
| T4 | golang · python · javascript · rust |
| T5 | sql · react-typescript · vue-typescript · websocket · tauri-react · tauri-vue |
| T6 | code-reviewer · verifier · qa-engineer · qa-test-spec · debugger-engineer |
| T7 | project-planner · intent-clarifier · investigator · technical-writer · memory-steward |
| T8 | docker · terraform · security · legal-advisor · ui-design |

4 + 6 + 5 + 5 + 5 = **25**, and `git-loop` is the twenty-sixth original with no handler (A3, T9).

## The follow-up five — T12–T17 (added 2026-10-26, user instruction)

The user directed: *spec those 5 agents based on what we have in the
`opencode/agents/` folder — use and adapt those*. Five kinds have lanes in
shipped blueprints and no body: their rows validate `cannot verify` and
`loop.start` refuses those lanes. S13 named them (STATUS:823); the pass ships
them with **the slice's own mechanism** — body + drop record + registration +
the `futureAgents` flip — nothing new.

| Kind | Source (measured) | Lanes it serves (from the shipped files) | Corpus block? (measured 2026-10-26) |
| --- | --- | --- | --- |
| task-coordinator | `opencode/agents/task-coordinator.md` — 632 L | `development-map-direct` triage+brief; `conversational-map` triage | **no** — decide by guard behaviour, record the decision |
| proposal-author | `opencode/agents/proposal-author.md` — 244 L | `proposal-brief` proposal (`produces: proposal.md`) | **yes** — binds by name (A16) |
| brief-author | `opencode/agents/brief-author.md` — 275 L | `proposal-brief` brief (`brief.md`) + self-check | **yes** — binds by name (A16) |
| product-manager | `opencode/agents/product-manager.md` — 240 L | `development-map-pipeline` feature-issue (`produces: feature.md`) | **no** — nil recorded, not empty |
| project-manager | `opencode/agents/project-manager.md` — 201 L (NOT `project-manager-primary.md`, 323 L) | `development-map-pipeline` task-breakdown (`produces: tasks/`) | **no** — nil recorded, not empty |

Corpus truth measured from `internal/policy/testdata/corpus.json`, `agents[].agent`,
38 entries — measured, not recollected (the first extraction read the wrong key and
came back empty; the record names its own trap).

**Adaptation rules (A1's extension):** no `mode`/`variant`/`permission`/`patch`
front-matter ports · skills and `hotfix-protocol` are refused by the guard and
accounted in the drop record · subagent-dispatch is RITUAL (the blueprint
dispatches) · outward forge mechanics are BECAME MECHANISM (`forge.openIssue`
is an engine action) · every drop entry carries `L<n>` references.

**Facts the tests chase:** the negative control — **corrected at T12,
2026-10-26, by measurement:** the plan's "26 → 31" was wrong. The frozen
26 is `load_test.go`'s count of `*-loop.md` ORIGINALS (S13's own 25 plus
git-loop); the five follow-up sources are PRIMARY documents, never in
that set, so the frozen count STAYS 26 and each follow-up task re-runs
that glob to prove it. The five originals are instead asserted by name
in `followup_test.go` (`TestVerifyFailsOnTheFollowUpOriginals`, named
not globbed — a glob widens silently): measured findings per source
task-coordinator 6 / proposal-author 5 / brief-author 4 /
product-manager 3 / project-manager 3, each ≥3 with a skill and a ritual
finding among them, as the guards actually say; the S9
historical retry case uses `task-coordinator` as its missing name — T12's flip
flips that fixture red **correctly** (reseed with a never-shipping name; the
historical case becomes eligible — its own QA line); `futureAgents` reaches
**empty and stays** — the branch is the third state's mechanism for the next
follow-up; **F-α is disclosed not papered** — these bodies make authoring lanes
runnable while `st.Tasks` still has no production writer, so lane-runnable is
not loop-complete, and no task here fakes the seeding.

## What this slice does not do

Git or forge actions (S16) · blueprints for the 16 primaries (S17) · deleting anything under `opencode/` (A10) ·
`pr_summary` (S16) · `search` (S22) · new tools · **spawn, abort, or subtree cost accounting** (A1 — there is no tree) ·
an agent authoring UI.
