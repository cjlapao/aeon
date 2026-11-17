import type { ReactNode } from "react";
import type { LiveController, LiveState } from "../../../lib/live";

/**
 * S72 (CF2): the Configuration page registry.
 *
 * Modelled on `features/test-bench/lib/benchRegistry.ts` and holding the same
 * rule: this file must not import pages or `app/routes.ts`, so it can never
 * join a circular import. `pages/registerAll.ts` is the only file that imports
 * page modules for their registration side effect; `app/routes.ts` imports
 * that one file, and the panel and the screen read the live registry.
 */

/** Menu groups, in the order the panel lists them (S72 CF4). */
export type ConfigGroup = "general" | "access" | "work" | "platform";

/**
 * S72 CF6: whether the page frame draws the `h1`. A section that renders its
 * own title panel registers `"bare"` so the heading is not duplicated (R1);
 * the default is `"titled"`.
 */
export type ConfigPageFrameKind = "titled" | "bare";

/** What a ready page is handed to render with (S72 CF2). */
export interface ConfigPageContext {
  controller: LiveController;
  live: LiveState;
  canWrite: boolean;
}

/** A Configuration page, registered by its own file — never appended to a shared array. */
export interface ConfigPageDef {
  /** Path segment: kebab-case, unique across the registry. */
  id: string;
  label: string;
  group: ConfigGroup;
  /** Menu position within the group, lower first. */
  order: number;
  /** One line — the disabled-row reason on a planned row, a description elsewhere. */
  summary?: string;
  status: "ready" | "planned";
  /** Defaults to true. False only for a page that renders usefully with no daemon connection. */
  needsConnection?: boolean;
  /** Defaults to "titled". See ConfigPageFrameKind. */
  frame?: ConfigPageFrameKind;
  /**
   * Defaults to false. True claims the viewport's full height: the shell's
   * page region stops scrolling and hands the height down (layout → frame →
   * page), so the page owns its scrolling — a table page scrolls its own rows
   * under a sticky header instead of the page scrolling. The page must then
   * complete the chain itself (a `flex-1 min-h-0` content area); the layout
   * keeps `overflow-auto` as a safety valve for short states (a crash
   * boundary, a warning on a very short window).
   */
  fill?: boolean;
  /**
   * The Work address this page moved to. A moved page leaves the Configuration menu and its
   * old address redirects there, so a bookmark or a link inside a daemon message still lands.
   */
  movedTo?: string;
  /** Required exactly when status is "ready" — checked at registration (CF2). */
  render?: (ctx: ConfigPageContext) => ReactNode;
}

/** A def after registration: the defaults the interface leaves optional are resolved once, here. */
export type RegisteredConfigPage = ConfigPageDef & { needsConnection: boolean; frame: ConfigPageFrameKind; fill: boolean };

/** The group order the panel renders, with its labels (S72 CF2). */
export const CONFIG_GROUPS: readonly { id: ConfigGroup; label: string }[] = [
  { id: "general", label: "General" },
  { id: "access", label: "Agents & access" },
  { id: "work", label: "Work" },
  { id: "platform", label: "Platform" },
];

const registry = new Map<string, RegisteredConfigPage>();

/** Called once, at a page module's own top level — never from routes.ts or the screen. */
export function registerConfigPage(def: ConfigPageDef): void {
  if (registry.has(def.id)) {
    throw new Error(`config page "${def.id}" is already registered — two files must not share an id`);
  }
  // A registration bug must fail loudly at import, not render blank (CF2).
  if (def.status === "ready" && !def.render) {
    throw new Error(`config page "${def.id}" is ready but registers no render — a ready page must provide one`);
  }
  if (def.status === "planned" && def.render) {
    throw new Error(`config page "${def.id}" is planned but carries a render — remove the status or the page (CF10)`);
  }
  registry.set(def.id, { ...def, needsConnection: def.needsConnection ?? true, frame: def.frame ?? "titled", fill: def.fill ?? false });
}

/** Every page, sorted by group order then `order`; ids break ties so the sort is total. */
export function configPages(): RegisteredConfigPage[] {
  const groupRank = new Map(CONFIG_GROUPS.map((group, index) => [group.id, index]));
  return [...registry.values()].sort(
    (a, b) => (groupRank.get(a.group) ?? 0) - (groupRank.get(b.group) ?? 0) || a.order - b.order || a.id.localeCompare(b.id),
  );
}

/** The pages the Configuration menu and the jump box list: everything that has not moved into Work. */
export function menuConfigPages(): RegisteredConfigPage[] {
  return configPages().filter((page) => !page.movedTo);
}

export function getConfigPage(id: string): RegisteredConfigPage | undefined {
  return registry.get(id);
}

/** The page `/config` redirects to — the first ready one in panel order. */
export function firstReadyConfigPage(): RegisteredConfigPage | undefined {
  return configPages().find((page) => page.status === "ready");
}

/** Test-only: a fresh registry per test, so one test's registration can't leak into another's. */
export function __resetConfigRegistryForTests(): void {
  registry.clear();
}
