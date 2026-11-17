import { matchPath } from "react-router-dom";
import type { SideMenuItem } from "../components/kit";
// Side effect: every page registerAll imports calls registerBenchPage at
// its own module's top level. Must be imported before benchPages() below
// reads the registry — plain static ESM imports always evaluate before
// the importing module's own subsequent statements, so this is a language
// guarantee, not a timing assumption.
import "../features/test-bench/pages/registerAll";
import { benchPages } from "../features/test-bench/lib/benchRegistry";
// Side effect, same convention (S72 CF2): every Configuration page registers
// itself in its own file; this import is what makes those registrations run.
import "../features/config/pages/registerAll";

export type RouteId = "home" | "work" | "loops" | "projects" | "history" | "components" | "component" | "loop" | "loopClassic" | "step" | "blueprints" | "roadmap" | "goals" | "system" | "settings" | "models" | "modelNew" | "modelEdit" | "config" | "bench";
export type RouteAccess = "open" | "compatible-daemon";
export interface AppRoute {
  id: RouteId;
  path: string;
  title: string;
  access: RouteAccess;
  menu?: { label: string; icon: string; group: "workspace" | "app" };
  /** "workspace" pages are full-bleed (no page padding or width cap) and may use the header slot. Default "page". */
  layout?: "page" | "workspace";
}

/** The route registry is the navigation and future access-policy inventory. */
export const APP_ROUTES: readonly AppRoute[] = [
  { id: "home", path: "/", title: "Home", access: "open", menu: { label: "Home", icon: "Dashboard", group: "workspace" } },
  // The one workspace: the component tree in the side menu, a hub of tabs per component. It replaces
  // the Loops, Projects, Components, History, Roadmap and Goals rail rows; those addresses stay valid and
  // redirect here (`legacyTarget`), so no link ever breaks.
  { id: "work", path: "/work/:component?/:tab?", title: "Work", access: "open", menu: { label: "Work", icon: "Folder", group: "workspace" } },
  { id: "loops", path: "/loops", title: "Loops", access: "open" },
  // S38 T4 — the two tiers, both addressable. /projects is the GROUP list
  // (S38's new tier, OL1); /components is what /projects used to be: the
  // workspace-metadata screen, renamed with the tier it names. /components/:id
  // is the deep link the group page walks into.
  { id: "projects", path: "/projects", title: "Projects", access: "open" },
  { id: "components", path: "/components", title: "Components", access: "open" },
  // S53 T5 — Projects → History: the fifth surface of the history slice (the
  // socket, the CLI, the agent's two reads, the records arm, and this view).
  // Read-only by construction: what the daemon can show, does not show, and
  // cannot say, with the person's export as the one door that writes nothing.
  { id: "history", path: "/history", title: "History", access: "open" },
  { id: "component", path: "/components/:id", title: "Component", access: "open" },
  { id: "loop", path: "/loops/:id", title: "Loop", access: "open", layout: "workspace" },
  { id: "loopClassic", path: "/loops/:id/classic", title: "Loop (classic)", access: "open" },
  { id: "step", path: "/steps/:id", title: "Step", access: "open" },
  { id: "blueprints", path: "/blueprints", title: "Flows", access: "open", menu: { label: "Flows", icon: "Blueprint", group: "workspace" } },
  // S26 T6 — the forward Gantt: history occupies the past, this occupies
  // the future, and the present is wherever a loop is actually running
  // (the loop surfaces own that). A view; the doors are the bench tab's.
  { id: "roadmap", path: "/roadmap", title: "Roadmap", access: "open" },
  // Goals — the units of "something needs doing": filed by a brainstorm or a
  // person, decided by a person, each owning its documents and its tasks.
  { id: "goals", path: "/goals", title: "Goals", access: "open" },
  { id: "system", path: "/system", title: "System", access: "open", menu: { label: "System", icon: "Cog", group: "app" } },
  // S72 — Configuration: one entry for the management pages (CF1). It is not
  // an App-group rail row (the group is System, Test bench): the entry rides
  // the menu's footer section, the stack that carries the daemon-status LED
  // (PageLayout.tsx's `footer`); pages live in the registry (features/config),
  // never in this file.
  { id: "config", path: "/config/:page?", title: "Configuration", access: "open" },
  { id: "models", path: "/models", title: "Models", access: "open" },
  { id: "modelNew", path: "/models/new", title: "Add model", access: "open" },
  { id: "modelEdit", path: "/models/:id/edit", title: "Edit model", access: "open" },
  { id: "settings", path: "/settings", title: "Settings", access: "open" },
  { id: "bench", path: "/bench/:page?", title: "Test bench", access: "compatible-daemon", menu: { label: "Test bench", icon: "Bug", group: "app" } },
];

// LEGACY_BENCH_PAGES is frozen as the literal `BenchPage` id union's own
// source — never add a new page here. A page added from here on lives in
// its own file under features/test-bench/pages/, registers itself
// (registerBenchPage), and gets ONE line in pages/registerAll.ts; see
// BENCH_PAGES below, which merges this literal with whatever the registry
// holds. docs/aeon/README.md's "Adding a bench page" has the full
// convention; AGENTS.md §3 has the reasoning.
const LEGACY_BENCH_PAGES = [
  { id: "status", label: "Status", path: "/bench" },
  { id: "providers", label: "Providers", path: "/bench/providers" },
  { id: "ask", label: "Ask", path: "/bench/ask" },
  { id: "chat", label: "Chat", path: "/bench/chat" },
  { id: "store", label: "Store", path: "/bench/store" },
  { id: "step", label: "Step", path: "/bench/step" },
  { id: "recovery", label: "Recovery", path: "/bench/recovery" },
  { id: "blueprints", label: "Blueprints", path: "/bench/blueprints" },
  // S24 T8 — the backlog bench, after the loop-mechanics tabs and before
  // the trust bench: a queue is where intentions wait before they are loops.
  { id: "queue", label: "Queue", path: "/bench/queue" },
  // S26 — the roadmap bench (the user-requested CRUD): the forward Gantt's
  // lab door, right after its sibling — the queue holds intentions that
  // ARE loop.start payloads; the roadmap holds intentions not yet shaped.
  { id: "roadmap", label: "Roadmap", path: "/bench/roadmap" },
  // S29 T8 — the triage bench (the user-requested CRUD): the bug backlog's
  // lab door, right after the roadmap — a validated, prioritized bug hands
  // off to a roadmap item, so the two benches sit beside each other.
  { id: "triage", label: "Triage", path: "/bench/triage" },
  // S34 T5 / S35 T8 — the Trust group (TD11): the order IS the
  // authorization story — Policy → Agents → MCP → Proposals, the desk
  // last because it is about all three others' knobs. `group: "trust"`
  // is the only field PageLayout.tsx reads to decide where the group
  // header renders — derived from this array, never a hard-coded index.
  { id: "policy", label: "Policy", path: "/bench/policy", group: "trust" },
  { id: "agents", label: "Agents", path: "/bench/agents", group: "trust" },
  { id: "mcp", label: "MCP", path: "/bench/mcp", group: "trust" },
  { id: "proposals", label: "Proposals", path: "/bench/proposals", group: "trust" },
  { id: "automation", label: "Automation", path: "/bench/automation" },
  { id: "files", label: "Files", path: "/bench/files" },
  { id: "agent-chat", label: "Agent chat", path: "/bench/agent-chat" },
  { id: "notifications", label: "Notifications", path: "/bench/notifications" },
  { id: "demos", label: "Demos", path: "/bench/demos" },
] as const;
export type BenchPage = (typeof LEGACY_BENCH_PAGES)[number]["id"];

/**
 * Every bench page, legacy plus registered. Rendered by PageLayout.tsx's
 * menu and walked by TestBench.tsx's generic fallback branch. NOT
 * `as const` — a registry entry's `id` is a plain `string`, so this array's
 * element type is intentionally wider than `BenchPage` (which stays sourced
 * from LEGACY_BENCH_PAGES alone, above). Widening `BenchPage` itself to
 * cover registry ids would need them to be compile-time literals, which a
 * dynamically self-registering page cannot offer — a deliberate, simple
 * tradeoff: `isBenchPage` is the one runtime gate everything already
 * trusted before this change, and it is what actually extends to cover
 * registry pages (unchanged logic, now checking the wider array).
 */
export const BENCH_PAGES: readonly { id: string; label: string; path: string; group?: "trust" }[] = [
  ...LEGACY_BENCH_PAGES,
  ...benchPages(),
];
export const isBenchPage = (value: string): value is BenchPage => BENCH_PAGES.some((page) => page.id === value);

/** A route's address with its optional parameters taken off (`/work/:component?/:tab?` → `/work`): a matcher detail, never a URL to link to. */
export const menuPath = (path: string): string => path.replace(/\/:[A-Za-z]+\?/g, "") || "/";

export const MENU: SideMenuItem[] = [
  { slug: "work", type: "group", label: "Workspace" },
  ...APP_ROUTES.filter((route) => route.menu?.group === "workspace").map((route) => ({
    slug: route.id, type: "link" as const, label: route.menu!.label, path: menuPath(route.path), icon: route.menu!.icon,
  })),
];

/** Which layout the route at `pathname` wants. Unmatched paths get the ordinary page. */
export function layoutFor(pathname: string, routes: readonly AppRoute[] = APP_ROUTES): "page" | "workspace" {
  const route = routes.find((r) => matchPath({ path: r.path, end: true }, pathname));
  return route?.layout ?? "page";
}
