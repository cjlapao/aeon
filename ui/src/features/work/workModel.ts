// workModel — the Work workspace's pure rules: the component tree, the tab
// list, and where each old address now lives. No React, no wire calls, so
// every rule here is testable without a daemon.
//
// A "project" is a root component (parentId empty). Everything below a root
// is a component, to any depth. The workspace shows that one tree; a scope is
// either one node of it or "all projects" (no node).

import type { RpcMethods } from "../../proto/contract";

export type WorkComponent = Omit<RpcMethods["component.list"]["result"]["components"][number], "parentId"> & { parentId?: string };
export type WorkLoop = RpcMethods["loop.list"]["result"]["loops"][number];

/** The hub's tabs, in reading order. The id is the URL segment. */
export const WORK_TABS = ["overview", "loops", "plan", "knowledge", "automation", "guardrails", "files", "history"] as const;
export type WorkTab = (typeof WORK_TABS)[number];

export function parseTab(raw: string | undefined): WorkTab {
  return (WORK_TABS as readonly string[]).includes(raw ?? "") ? (raw as WorkTab) : "overview";
}

/** Loop statuses that mean "a person is needed" — the daemon's words, grouped. */
export const NEEDS_YOU_STATUSES: readonly string[] = ["waiting", "blocked"];
export const RUNNING_STATUSES: readonly string[] = ["running", "waiting"];

export interface TreeNode {
  component: WorkComponent;
  children: TreeNode[];
  depth: number;
}

/** Roots first, each root's children beneath, every level sorted by name. */
export function buildTree(components: readonly WorkComponent[]): TreeNode[] {
  const byParent = new Map<string, WorkComponent[]>();
  const ids = new Set(components.map((component) => component.id));
  for (const component of components) {
    // A row whose parent is not in the list is shown as a root: folding changes what is drawn, never what is held.
    const parent = component.parentId && ids.has(component.parentId) ? component.parentId : "";
    byParent.set(parent, [...(byParent.get(parent) ?? []), component]);
  }
  const make = (parent: string, depth: number): TreeNode[] =>
    (byParent.get(parent) ?? [])
      .slice()
      .sort((a, b) => a.name.localeCompare(b.name))
      .map((component) => ({ component, depth, children: make(component.id, depth + 1) }));
  return make("", 0);
}

/** Every node of the tree, parents before children. */
export function flatten(nodes: readonly TreeNode[]): TreeNode[] {
  return nodes.flatMap((node) => [node, ...flatten(node.children)]);
}

/** The ids of a component and everything beneath it. */
export function subtreeIds(nodes: readonly TreeNode[], id: string): Set<string> {
  const found = flatten(nodes).find((node) => node.component.id === id);
  return new Set(found ? flatten([found]).map((node) => node.component.id) : []);
}

/** From the root down to the node: the breadcrumb's data. Empty when the id is unknown. */
export function pathTo(components: readonly WorkComponent[], id: string): WorkComponent[] {
  const byId = new Map(components.map((component) => [component.id, component]));
  const path: WorkComponent[] = [];
  let current = byId.get(id);
  const seen = new Set<string>();
  while (current && !seen.has(current.id)) {
    seen.add(current.id);
    path.unshift(current);
    current = current.parentId ? byId.get(current.parentId) : undefined;
  }
  return path;
}

/** The roots a scope covers, for filtering rows that carry `componentRoot`. */
export function scopeRoots(components: readonly WorkComponent[], tree: readonly TreeNode[], id: string, includeSub: boolean): Set<string> {
  if (id === "") return new Set(components.map((component) => component.root));
  const ids = includeSub ? subtreeIds(tree, id) : new Set([id]);
  return new Set(components.filter((component) => ids.has(component.id)).map((component) => component.root));
}

export interface ScopeCounts {
  running: number;
  needsYou: number;
}

/** Per component: loops running and loops that need a person, counted over the component's own root. */
export function countByRoot(loops: readonly WorkLoop[]): Map<string, ScopeCounts> {
  const out = new Map<string, ScopeCounts>();
  for (const loop of loops) {
    const entry = out.get(loop.componentRoot) ?? { running: 0, needsYou: 0 };
    if (RUNNING_STATUSES.includes(loop.status)) entry.running += 1;
    if (NEEDS_YOU_STATUSES.includes(loop.status)) entry.needsYou += 1;
    out.set(loop.componentRoot, entry);
  }
  return out;
}

/** A node's own counts plus everything beneath it, so a collapsed parent still shows what is inside. */
export function rollup(node: TreeNode, counts: ReadonlyMap<string, ScopeCounts>): ScopeCounts {
  const own = counts.get(node.component.root) ?? { running: 0, needsYou: 0 };
  return node.children.reduce(
    (sum, child) => {
      const inner = rollup(child, counts);
      return { running: sum.running + inner.running, needsYou: sum.needsYou + inner.needsYou };
    },
    { ...own },
  );
}

export function workPath(componentId: string, tab: WorkTab = "overview"): string {
  if (componentId === "") return tab === "overview" ? "/work" : `/work/all/${tab}`;
  return tab === "overview" ? `/work/${encodeURIComponent(componentId)}` : `/work/${encodeURIComponent(componentId)}/${tab}`;
}

/**
 * Old address → new address. The old list pages are gone from the rail; a
 * bookmark or a link inside a daemon message must still land somewhere true.
 * `root` is the old `?root=` filter, resolved to a component id by the caller.
 */
export function legacyTarget(routeId: string, rootId: string): string | undefined {
  switch (routeId) {
    case "loops": return workPath(rootId, "loops");
    case "projects": return workPath("", "overview");
    case "components": return workPath(rootId, "overview");
    case "history": return workPath(rootId, "history");
    case "roadmap":
    case "goals": return workPath(rootId, "plan");
    default: return undefined;
  }
}
