import type { StatusView } from "../settings/system/systemModel";
import type { WorkComponent } from "./workModel";

/**
 * Which of the daemon's indexes belong to the picked scope. Pure.
 *
 * Graph and search follow the REPOSITORY, not the component: a sub component inside its parent's
 * repository shares the parent's index. So a scope root matches an index root when they are equal,
 * when the index root is a folder above it (the longest such one wins), or when the index root sits
 * inside the scope. `shared` names the scope roots served by a parent's index, so the page can say so.
 */
type GraphRoot = NonNullable<StatusView["graph"]["roots"]>[number];

const inside = (child: string, parent: string): boolean => child === parent || child.startsWith(parent.endsWith("/") ? parent : `${parent}/`);

export function graphRootsFor<T extends { root: string }>(indexRoots: readonly T[], scope: ReadonlySet<string>, all: boolean): { roots: T[]; shared: string[] } {
  if (all) return { roots: [...indexRoots], shared: [] };
  const picked = new Set<string>();
  const shared: string[] = [];
  for (const wanted of scope) {
    const above = indexRoots.filter((r) => inside(wanted, r.root)).sort((a, b) => b.root.length - a.root.length)[0];
    if (above && above.root !== wanted) shared.push(wanted);
    if (above) picked.add(above.root);
    for (const r of indexRoots) if (inside(r.root, wanted)) picked.add(r.root);
  }
  return { roots: indexRoots.filter((r) => picked.has(r.root)), shared };
}

/**
 * The index root that serves one picked project: its own when it has one, else the longest folder above it (a sub
 * component inside a parent's repository shares the parent's index), else none: nothing has been indexed yet.
 */
export function indexRootFor<T extends { root: string }>(indexRoots: readonly T[], pickedRoot: string): T | undefined {
  return indexRoots.filter((r) => inside(pickedRoot, r.root)).sort((a, b) => b.root.length - a.root.length)[0];
}

/** Retrieval names its components by id, name or root depending on the corpus; match all three. */
export function retrievalComponentsFor<T extends { component: string }>(rows: readonly T[], scope: readonly WorkComponent[], all: boolean): T[] {
  if (all) return [...rows];
  const keys = new Set(scope.flatMap((c) => [c.id, c.name, c.root]));
  return rows.filter((r) => keys.has(r.component));
}

export type { GraphRoot };
