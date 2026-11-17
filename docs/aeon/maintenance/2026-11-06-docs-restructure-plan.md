# Plan — restructure `docs/aeon`

Written 2026-11-06. A plan only: nothing in this file has been executed. The operator decisions it needs are listed in section 8.

## 1. What is wrong, measured on 2026-11-06

- **73 slice folders and 17 top-level files**, 12,665 lines at the top level alone.
- **Three files are over the 1000-line limit (SIZE1):** `DEMO.md` 4,530, `STATUS.md` 1,574, `SLICES.md` 1,238. The operator deferred these on 2026-11-02, and since then agents have kept appending to them.
- **Five documents all claim to track progress:** `STATUS.md`, `SLICES.md`, `ROADMAP.md`, `TASKS.md`, `PLAN.md`. `SLICES.md` was declared the only maintained tracker on 2026-10-31. The other four now carry a banner saying their progress claims may be stale, but they are still the largest files in the folder and still the first thing a reader opens.
- **Finished one-off documents sit beside living ones:** `organization_layers_spec.md` (consumed by S38), `REQUEST-localrpc-service.md` (answered in S2), `Parallel.md` (a merge-order note for slices that are done), `AUTONOMY-GAPS.md` (a parking document; S55–S63 are built), `UI-workflow-tracker.md`, and the `UI-chat-panels/` folder (a slice-shaped folder with no number).
- **The living rules are mixed in with history.** `CONFIGURATION-ORDER.md`, `MANAGEMENT-PATTERNS.md`, `SEEDING.md` and `ARCHITECTURE.md` are what a builder needs today. Nothing in the folder layout says so.
- **Slice folders are uneven.** 39 of 73 have a `DEMO.md`. Some built slices still carry a `SKETCH.md`. S41 has only a `design.md`. S43 and S44 each have two folders (`-extra`), which the tracker explains but the folder names do not.
- **Links are the main risk.** Inbound references by file name: `DEMO.md` 441 files, `STATUS.md` 127, `ROADMAP.md` 82, `TASKS.md` 74, `PLAN.md` 42, `AUTONOMY-GAPS.md` 22, `SEEDING.md` 16, `VISION.md` 15. Most are in task files, Go comments and `AGENTS.md`. A plain rename breaks hundreds of them.

## 2. Target shape

```
docs/aeon/
  README.md                 the map: start here, living rules, slices, history
  ARCHITECTURE.md           living: the principles every slice argues from
  VISION.md                 living: the picture
  SLICES.md                 index only: stage table and links
  slices/                   one tracker file per stage (stage-0 … stage-5), each under 1000 lines
  DEMO.md                   index only: ordered list of demo sections and links
  demo/                     the cumulative demo body, one file per stage group
  guides/                   living how-to documents
    configuration-order.md
    management-patterns.md
    seeding.md
    workflow-tracker.md
    adding-a-bench-page.md  (extracted from README)
  history/                  frozen; every file starts with a banner naming its replacement
    roadmap.md  tasks.md  plan.md  status-log-*.md
    autonomy-gaps.md  requests/  sketches/
  maintenance/              dated one-off notes (already exists)
  blueprints/               unchanged
  S1-bootstrap/ … S72-…/    one folder per slice, normalised in section 5
```

Rules for the shape:

1. **A path that many files reference stays, and becomes an index.** `DEMO.md`, `SLICES.md`, `STATUS.md` keep their paths. This is the only way to avoid rewriting 600+ references in one change.
2. **A path with few references moves, and its links are rewritten by script.** `ROADMAP.md`, `TASKS.md`, `PLAN.md`, `AUTONOMY-GAPS.md` and the loose files fall in this group.
3. **Nothing is deleted without a replacement or a reason.** Superseded material goes to `history/` with a banner; git history keeps the rest.
4. **Every file under 1000 lines.** Split by cohesion (one topic per file), not by slicing at a line count.

## 3. Phases

Each phase is its own change, merged before the next starts. Phases 2–4 are scripted and mechanical.

### P0 — Inventory and a link checker (no content moves)

- Add `aeon/scripts/check-doc-links.sh` (or extend an existing guard): resolves every relative Markdown link and every bare `NAME.md` reference in `docs/`, `aeon/AGENTS.md`, `AGENTS.md`, task files and Go/TS comments, and reports the unresolved ones.
- Record the baseline count of unresolved references today. The rule for every later phase: the count never rises.
- Produce `maintenance/docs-inventory-2026-11-06.md`: one row per file with lines, kind (living / tracker / history / slice), inbound reference count, and its destination in section 2.

### P1 — Change the rules before the files (prevents re-mess)

Agents append to the hot files because the instructions tell them to. Fix the instructions first, or every later phase is undone within a week.

- `docs/aeon/README.md` "Updating the Aeon documents" and `aeon/AGENTS.md` §4a/§4b: completion is recorded in the slice's task Notes and one row of the stage tracker file. The cumulative demo is extended by adding a file under `demo/` and one index line. Nobody appends narrative to `STATUS.md`, `ROADMAP.md`, `TASKS.md` or `PLAN.md`.
- Root `AGENTS.md` SIZE1 section: remove the "deferred docs debt" paragraph once P2 lands.
- Announce a short window: no agent edits `DEMO.md`, `STATUS.md` or `SLICES.md` between the P1 merge and the P2 merge. Agents with open worktrees rebase onto P2 and re-apply their appends to the new location.

### P2 — Split the three hot files, keeping their paths

- **`DEMO.md` (4,530 lines, 55 sections).** Body moves to `demo/` grouped by stage (about five files); each section keeps its heading text so existing anchors in slice `DEMO.md` files and task Notes still make sense. `DEMO.md` becomes: how to run, the convention, and an ordered list of sections linking into `demo/`. Where a slice folder already has a `DEMO.md` with the same walk, the cumulative section is reduced to a one-paragraph summary plus a link, not duplicated.
- **`SLICES.md` (1,238 lines, about 900 table rows).** One file per stage under `slices/`. `SLICES.md` keeps the legend, the rule text and a stage summary table with links.
- **`STATUS.md` (1,574 lines).** Superseded by `SLICES.md` (README already says so). The at-a-glance board is dropped, because `SLICES.md` carries the same facts and is maintained. The narrative sections move to `history/status-log-*.md`, split by stage and each under 1000 lines. `STATUS.md` becomes a 15-line pointer: where the tracker is, where the history is. The path stays because 127 files cite it.

### P3 — Retire the historical planning documents

- `ROADMAP.md`, `TASKS.md`, `PLAN.md` → `history/`, each with a top banner naming `SLICES.md` and the slice folders as the replacement. Before moving `ROADMAP.md`, extract anything that is still a live decision and not yet in `ARCHITECTURE.md` or a slice design (its policy, triggers and agents sections, decisions table, amendments table). Each extracted item gets a home or is explicitly marked "now in S10 design".
- Rewrite inbound links with the P0 script's `--rewrite` mode, then re-run the checker.
- `VISION.md` stays; check its stage descriptions against reality and fix or mark stale lines. No restructure.

### P4 — Clear the root

| File | Action |
| --- | --- |
| `organization_layers_spec.md` | → `S38-org-layers/source-spec.md` (S38 consumed it) |
| `REQUEST-localrpc-service.md` | → `history/requests/` (answered, S2) |
| `Parallel.md` | → `history/parallel-merge-order.md` (decision D3: archive) |
| `AUTONOMY-GAPS.md` | → `history/autonomy-gaps.md` with a table at the top: each gap, the slice that closed it, and any gap still open (those open rows move into `SLICES.md` notes or a new slice stub) |
| `CONFIGURATION-ORDER.md`, `MANAGEMENT-PATTERNS.md`, `SEEDING.md` | → `guides/` (living; update their status tables, which are stale — `CONFIGURATION-ORDER.md` still marks S45–S50 as planned) |
| `UI-WORKFLOW-TRACKER.md` | → `guides/workflow-tracker.md` |
| `UI-chat-panels/` | stays at the root as an unnumbered folder (decision D4); add a one-line `README.md` saying it belongs to no slice and listing what built it, if known |

### P5 — Normalise the slice folders

Standard set per slice: `brief.md`, `design.md`, `plan.md`, `tasks/`, and `DEMO.md` once built. Optional: `investigation.md`, `source-spec.md`.

- `SKETCH.md` in built slices → `history/sketches/<slice>.md`.
- Add a one-line `README.md` to the folders whose name does not say what they are (`S43-extra-seed-process`, `S44-extra-component-tree`): which slice it extends and in what order they merged.
- S41 (design only) and S21–S23 (specced, unbuilt): confirm state in `slices/` and add a banner in the folder if the design is abandoned.
- Do not rename any slice folder. Worktree branches, task files and code comments cite them.

### P6 — The new map

- Rewrite `README.md` as the entry point: four sections (start here, living rules, slices, history), under 150 lines. Extract its "Adding a bench page" and worktree notes to `guides/`.
- Update the doc map table in `aeon/AGENTS.md` and root `AGENTS.md` to the new paths.
- Add `docs/aeon/` to the repo's doc guard so a new file over 1000 lines fails CI.

### P7 — Verify

- Link checker: zero new unresolved references, and the P0 baseline reduced (the existing broken ones fixed or listed).
- `find docs/aeon -name '*.md' | xargs wc -l` shows nothing over 1000. Record the numbers.
- Read three random slice folders and the new `README.md` cold: can a builder find the rules, the tracker and the slice in under a minute?

## 4. Tasks and order

| Task | Phase | Parallel? |
| --- | --- | --- |
| T1 | P0 checker, inventory | first |
| T2 | P1 rules | after T1; **freeze window opens** |
| T3 | P2 `DEMO.md` split | after T2 |
| T4 | P2 `SLICES.md` split | parallel with T3 (different files) |
| T5 | P2 `STATUS.md` retire | parallel with T3/T4 |
| T6 | P3 ROADMAP/TASKS/PLAN, VISION check | after T3–T5; freeze window closes at the end of T5 |
| T7 | P4 root clear | parallel with T6 (different files); touches inbound links, so run the checker after both |
| T8 | P5 slice normalisation | after T7 |
| T9 | P6 map, P7 verify | last |

## 5. Risks

- **R1 Concurrent edits.** Other agents are working in 10+ worktrees and write to the hot files. Mitigation: P1 first, a stated freeze window, short phases, and P2 as three small merges instead of one large one.
- **R2 Link rot in code comments.** Many references are in Go and TS comments (`see STATUS.md`). The checker covers `aeon/` source, and the scripted rewrite handles moves. References to `STATUS.md` and `DEMO.md` do not change because those paths stay.
- **R3 Losing a live decision inside a historical file.** Mitigated by the extract-before-move step for `ROADMAP.md` and `AUTONOMY-GAPS.md`, reviewed by a person.
- **R4 Anchors.** Links of the form `DEMO.md#s38-…` break when a section moves. The checker validates anchors; the index keeps old anchors alive by listing each section with its old heading.
- **R5 Scope creep into rewriting content.** This plan moves, splits, labels and indexes. It does not rewrite slice designs.

## 6. Out of scope

- `docs/superpowers/`, `docs/specs/`, `docs/test_suites/`, `docs/_archive/`, `docs/legal.md`, `docs/loop-ui-data-contract.md`. Same treatment later if wanted.
- Renaming slice folders or slice numbers.
- Editing any slice's brief, design or tasks beyond a banner.

## 7. Done when

`docs/aeon/` has no file over 1000 lines; one tracker is named in `README.md` and it is `SLICES.md`; the four historical planning documents are under `history/` with banners; the root holds `README`, `ARCHITECTURE`, `VISION`, `SLICES`, `DEMO`, `STATUS` (pointer) and the folders; and the link checker reports no new unresolved references.

## 8. Operator decisions (answered 2026-11-06)

- **D1** Drop `STATUS.md`'s at-a-glance board: **yes**. `SLICES.md` is the one tracker.
- **D2** Keep a pointer stub at `STATUS.md`: **yes**, permanently, because 127 files cite the path.
- **D3** `Parallel.md` and `AUTONOMY-GAPS.md`: **archive** under `history/`, do not delete.
- **D4** `UI-chat-panels/`: **leave it unnumbered**, not belonging to any slice.
- **D5** Freeze window on `DEMO.md`, `STATUS.md` and `SLICES.md`: **accepted**, under a day.
