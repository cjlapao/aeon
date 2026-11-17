# Design — S72 Configuration shell

Rules CF1–CF10. Cited from `plan.md` and the task files.

## CF1 The route and the rail

Add a route to `APP_ROUTES` in `src/app/routes.ts`:

`{ id: "config", path: "/config/:page?", title: "Configuration", access: "open", menu: { label: "Configuration", icon: <kit icon>, group: "app" } }`

- Add `"config"` to the `RouteId` union.
- Pick the icon from the kit's `iconRegistry` (exported by `@cjlapao/ui-kit`). `Cog` is already System's icon; choose a different registered name and record it in the task Notes. Do not add an SVG to `src/icons` for this.
- `access` is `"open"`, like Settings. Pages show their own not-connected state (CF7).
- Order inside the `App` group: System, Configuration, Test bench.

## CF2 The registry

New file `src/features/config/lib/configRegistry.ts`, modelled on `src/features/test-bench/lib/benchRegistry.ts`. It must not import pages or `routes.ts` (no cycles).

```ts
export type ConfigGroup = "general" | "access" | "work" | "platform";
export interface ConfigPageDef {
  id: string;                 // path segment, kebab-case, unique
  label: string;
  group: ConfigGroup;
  order: number;              // lower first, within the group
  summary?: string;           // one line, shown as the disabled-row reason or tooltip
  status: "ready" | "planned";
  needsConnection: boolean;   // default true
  render?: (ctx: ConfigPageContext) => ReactNode;   // required when status is "ready"
}
export interface ConfigPageContext { controller: LiveController; live: LiveState; canWrite: boolean }
```

- `registerConfigPage(def)` throws on a duplicate id and on `status: "ready"` without `render` (a registration bug must fail loudly at import, not render blank).
- `configPages()` returns all definitions sorted by group order then `order`.
- `CONFIG_GROUPS` is the ordered list of groups with labels: `general` "General", `access` "Agents & access", `work` "Work", `platform` "Platform".
- Pages register from their own file. `src/features/config/pages/registerAll.ts` is the only file that imports page modules, one line per page. It is imported once by `routes.ts` for its side effect, the same way `registerAll` is for the bench.
- Page components load lazily (`React.lazy`) inside `render`, so Configuration does not enlarge the initial bundle.

## CF3 The pages and their groups

Order follows `docs/aeon/CONFIGURATION-ORDER.md`: the small load-bearing pieces first.

| Group | id | label | Source |
| --- | --- | --- | --- |
| general | `daemon-settings` | Daemon settings | `DaemonSettingsPanel` |
| general | `backups` | Backups | `BackupsSection` |
| general | `system-records` | System records | `SystemRecordsPanel` |
| access | `agents` | Agents | extracted in T3 |
| access | `policy` | Policy | `PolicySection` |
| access | `mcp` | MCP servers | `McpSection` |
| access | `tools` | Tools | `ToolsSection` |
| work | `gates` | Verification gates | `GatesSection` |
| work | `environments` | Environments | `EnvironmentsSection` |
| work | `automation` | Automation | `AutomationSection` |
| work | `triage` | Triage | `TriageSection` |
| work | `memory` | Memory | `MemorySection` |
| work | `continuous` | Continuous work | `ContinuousSection` |
| platform | `platform-status` | Platform status | `SystemSection` |

Planned rows (status `"planned"`, no `render`), registered in `src/features/config/pages/planned.ts` so removing one is a one-line change when its page lands: `blueprints` "Blueprints" (access: S48 management UI, if no section exists; T1 verifies by grep before listing it), `skills` "Skills" if no product UI exists. T1 confirms each by searching `src/features` and lists only rows that are truly absent.

The component names above are the real exports found in `SettingsScreen.tsx` on 2026-11-06. Each keeps its current props.

## CF4 The secondary panel

`PageLayout.tsx` gains `'config'` in `SectionId`, `SECTION_OF_ROUTE` (`config: 'config'`) and `SECTION_LABELS` ("Configuration"). In `panelItems`, the `config` branch builds items from `configPages()`:

- one `type: 'group'` item per group that has at least one page, then that group's links;
- every link carries `groupName` equal to its group item's slug. The kit only renders a group header when a link names it; adjacency alone renders nothing. `PageLayout.tsx` documents this in the bench branch. Reuse the same shape.
- `status: "planned"` rows are `disabled: true` links with `meta: "planned"`. Confirm the kit's `SideMenuItem` supports `disabled`; if it does not, render them without a `path` and with `meta`, and record the finding.
- Link slug is `config:<id>`, path `/config/<id>`.

`activePanelSlug` for `config` is `config:<page>`, defaulting to the first ready page when `:page` is absent. As the bench branch notes, pass an explicit slug even for unknown pages so the kit does not fall back to prefix matching.

The panel enables the kit's `search` for `config`, placeholder "Search configuration".

## CF5 The route body

New file `src/features/config/ConfigScreen.tsx`, rendered by `router.tsx`'s `case "config"`.

- Reads `:page` from the route. No page: `<Navigate replace to="/config/<first ready id>" />`.
- Unknown id or a `planned` id: a not-found state inside the shell (title, one sentence, link to the first ready page). Not a blank page, not a redirect that hides the typo.
- Ready page: a `ConfigPageFrame` (CF6) around `def.render({ controller, live, canWrite })`.
- Wraps the page in the existing `TabBoundary` (`src/components/TabBoundary.tsx`) so one page throwing does not blank the shell.

## CF6 The page frame

`ConfigPageFrame` renders: the page label as `h1`, the group label as a quiet breadcrumb line, then the page. If a section already renders its own title panel, the frame renders no `h1` for it (R1). Decide per page with a `frame: "titled" | "bare"` field on `ConfigPageDef` (default `"titled"`). T2 sets `"bare"` for the sections that carry their own header and records each choice. A section is never edited to make this work.

## CF7 Connection states

Pages with `needsConnection: true` (default) and no `live.state === "current"` render the shell's existing not-connected `Alert` (same wording and test id as `tab-needs-connection` in `TestBench.tsx`), not the page. This keeps one empty state across the product (TB12). `canWrite` is `live.state === "current"` plus the existing protocol-version rule: use `canWrite` as `router.tsx` already computes it for other screens.

## CF8 Settings shrinks

`SettingsScreen.tsx` keeps: header, Appearance, Language, Default tones, Live preview, and the "Other configuration" panel. It loses the thirteen section components and their imports. The "Other configuration" panel gains a prominent link to `/config`. `data-testid="product-settings"` stays.

## CF9 Agents extraction (T3)

Split `AgentManagementPage.tsx` (922 lines) by cohesion into `src/features/agents/` (component, list, detail, edit form, hooks). The bench file keeps its `registerBenchPage` call and renders the extracted `<AgentManagement />`. Each resulting file is under 1000 lines, and the line counts are recorded in the task Notes. The Configuration `agents` page renders the same component. It uses the shared `Fact`/`useFact`/`FactCell` helpers from `src/features/test-bench/lib/`; importing from there is allowed this once, because moving those helpers is out of scope and a later slice may relocate them.

## CF10 Tests

- `configRegistry.test.ts`: duplicate id throws; ready-without-render throws; ordering by group then order; planned rows carry no render.
- `ConfigScreen.test.tsx`: no `:page` redirects to the first ready page; unknown id and planned id show the in-shell not-found; a throwing page is caught by the boundary; disconnected shows the not-connected alert.
- `PageLayout` panel test (extend the existing product-shell tests): `/config/policy` highlights `config:policy`; groups render once, each above its own links; planned rows are disabled.
- One smoke render per migrated page with the fake daemon helper in `test/helpers`: the page mounts and shows its section's root test id. Each section's own tests stay as they are.
- `product.test.tsx` nav pins and the rail test gain "Configuration".
- `SettingsScreen` tests: appearance still present; none of the moved sections present; link to `/config` present.
