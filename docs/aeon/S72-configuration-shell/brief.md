# Brief — S72 Configuration shell

Written 2026-11-06. Prefix **CF**. Stage 5 · Fleet, UI. **Depends on:** nothing unbuilt. It re-houses UI that S43–S62 already built; it adds no daemon method, no migration and no Rust.

## 1. Why

S43–S62 each shipped a management UI, and none of them has a home. The result today:

- **Twelve sections are stacked on the Settings page.** `src/features/settings/SettingsScreen.tsx` renders `DaemonSettingsPanel`, `BackupsSection`, `SystemRecordsPanel`, `PolicySection`, `McpSection`, `ToolsSection`, `GatesSection`, `EnvironmentsSection`, `AutomationSection`, `TriageSection`, `MemorySection`, `SystemSection` and `ContinuousSection` one after another, under the appearance controls. There is no navigation inside it. The page is a long scroll, and a person cannot go to "Policy" without finding it by eye.
- **Agent management has no product page at all.** Its UI is the 922-line bench page `src/features/test-bench/pages/AgentManagementPage.tsx`, reachable only through the debug Test bench.
- **Several slices' UI exists only as bench pages** (mailbox, skills, learnings, merge queue, async workers, parallel agents). They are debug tools, not places a person manages anything.
- **The secondary menu is unused for this.** The shell (`src/app/PageLayout.tsx`) already builds a contextual panel per rail section (`models`, `bench`, `blueprints`, `projects`). Configuration is the section that needs one most.

This slice gives configuration a place: one rail entry, one panel of pages grouped by concern, one route per page. It is a shell and a re-homing. It does not rewrite any section's internals.

## 2. What it delivers

1. A rail entry **Configuration** in the bottom `App` group, route `/config/:page?`.
2. A secondary panel listing configuration pages in groups, derived from a self-registering page registry (the same convention as the bench registry, so parallel agents add pages without editing shared files).
3. The existing Settings sections moved, unchanged, to one page each under that panel.
4. An **Agents** page extracted from the bench's agent management page into a product feature, with the bench page importing it back (one implementation).
5. Rows for management slices whose UI is not built yet, shown disabled and labelled, so the menu keeps its shape as pages land.
6. Settings reduced to what it says it is: appearance, language and tones.

## 3. What it does not do

- It does not change what any section does. A section that is broken or shallow today is still broken or shallow after this slice. T4 records what each page really does, as findings, so the next slices have a true list. Fixing them is their work.
- It does not add daemon methods. Every page keeps talking to the daemon only through the socket (P34), exactly as its section does today.
- It does not touch the Test bench except the one import in task T3.
- It does not move Flows, Roadmap, History, Projects, Components or Models. Those are workspace views, or already have their own place.
- It does not redesign any visual style. Pages keep the kit's existing panels.

## 4. Acceptance

1. The rail shows **Configuration**; selecting it opens `/config` with a secondary panel; the first ready page is selected.
2. Every Settings section listed in section 1 is reachable as its own page at `/config/<id>`, renders with the same props it received on Settings, and passes its existing tests unmodified.
3. `/settings` shows appearance, language, tones and a link to Configuration. None of the thirteen stacked sections remains on it.
4. `/config/agents` renders the agent management UI from a product feature folder; the bench page renders the same component.
5. A disabled row is never clickable and says why ("planned" or "not wired").
6. With no daemon connection every Configuration page shows the shell's existing not-connected state, never an empty or zero page.
7. Browser walk (T4): every page opened once against a running daemon; findings recorded.
8. No file written by this slice passes 1000 lines (SIZE1).

## 5. Risks

- **R1 Double headers.** Each section renders its own title panel. Wrapping it in a page frame with a second title duplicates the heading. T2 decides per section; it must not edit the section to fix this.
- **R2 Shared files.** `routes.ts`, `PageLayout.tsx` and `router.tsx` are touched by other work. T1 keeps each edit small, and every later page is added through the registry, never by editing those files again.
- **R3 A test pins Settings content.** `test/i18n.test.tsx`, `test/product-shell.test.tsx` and `test/DaemonSettingsPanel.test.tsx` mention the Settings screen. T2 updates the assertions that moved, and records each change.
- **R4 Hidden state in Settings.** Some sections may read props only `SettingsScreen` passes (`canWrite`). T2 passes the identical props from the new route.
