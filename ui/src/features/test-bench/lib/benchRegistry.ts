import type { ReactNode } from "react";

/**
 * A bench page, registered by its own file rather than appended to a
 * shared array — see `docs/aeon/README.md`'s "Adding a bench page" and
 * `AGENTS.md` §3. Deliberately holds no dependency back onto `pages/` or
 * `app/routes.ts`: `pages/registerAll.ts` is the only file that imports
 * page modules for their registration side effect, so this file can never
 * become part of a circular import.
 */
export interface BenchPageDef {
  id: string;
  label: string;
  path: string;
  /** Menu position among bench pages, lower first. Legacy pages (pre-registry) keep their own hand-ordered array; this only orders pages added through the registry. */
  order: number;
  group?: "trust";
  /** False for a page that renders usefully with no daemon connection (e.g. local-only state). Defaults to true. */
  needsConnection: boolean;
  /**
   * S64: the render receives the bench's session facts. `connected` is
   * the one a listening page needs — a page that subscribes to the
   * daemon's event stream (the async-worker ledger does) must not
   * install a listener while the bench session is down, and must re-read
   * when it returns. Pre-S64 pages declare no parameter and compile
   * untouched: a page asks only when it listens.
   */
  render: (ctx: { connected: boolean }) => ReactNode;
}

const registry = new Map<string, BenchPageDef>();

/** Called once, at a page module's own top level — never from routes.ts or TestBench.tsx. */
export function registerBenchPage(def: BenchPageDef): void {
  if (registry.has(def.id)) {
    throw new Error(`bench page "${def.id}" is already registered — two files must not share an id`);
  }
  registry.set(def.id, def);
}

/** Every registered page, sorted by its own `order` field. */
export function benchPages(): BenchPageDef[] {
  return [...registry.values()].sort((a, b) => a.order - b.order);
}

export function getBenchPage(id: string): BenchPageDef | undefined {
  return registry.get(id);
}

/** Test-only: a fresh registry per test, so one test's registration can't leak into another's. */
export function __resetBenchRegistryForTests(): void {
  registry.clear();
}
