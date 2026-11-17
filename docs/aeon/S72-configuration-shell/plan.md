# Plan — S72 Configuration shell

Rules CF1–CF10 are in [`design.md`](design.md). Four tasks, strictly sequential: each one needs the previous one merged.

| Task | What | Depends on |
| --- | --- | --- |
| [T1](tasks/T1.md) | Route, rail entry, registry, panel, `ConfigScreen`, page frame, planned rows. No real page yet: one throwaway fixture page proves the machinery | none |
| [T2](tasks/T2.md) | Register the twelve existing sections as pages; shrink `SettingsScreen`; update the tests that pinned the old layout | T1 |
| [T3](tasks/T3.md) | Extract agent management from the bench page into `features/agents`; register the Agents page; bench imports it back | T1 |
| [T4](tasks/T4.md) | Browser walk of every page, findings table, STATUS and README updates | T2, T3 |

T2 and T3 touch different files and may run in parallel after T1 merges. T4 waits for both.

## Open questions the tasks answer

- **Q1** Does the kit's `SideMenuItem` support a disabled link? Answered in T1 by reading the kit's types and one render test.
- **Q2** Which sections render their own title panel (`frame: "bare"`)? Answered in T2, per section.
- **Q3** Which planned rows are truly absent (no product UI anywhere)? Answered in T1 by searching `src/features`.
- **Q4** Which kit icon name is used for the rail entry? Answered in T1.

## Out of scope, recorded for later

- Fixing anything T4 finds.
- An overview page at `/config` with cards per group. The redirect to the first ready page is enough for now.
- Moving Models and Settings into the Configuration panel.
- Relocating `useFact`/`FactCell` out of `test-bench/lib`.
