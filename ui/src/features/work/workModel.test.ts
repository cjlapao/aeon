import { describe, expect, it } from "vitest";
import { buildTree, countByRoot, flatten, legacyTarget, parseTab, pathTo, rollup, scopeRoots, subtreeIds, workPath, type WorkComponent, type WorkLoop } from "./workModel";

const c = (id: string, name: string, root: string, parentId?: string): WorkComponent => ({ id, name, root, slug: id, parentId } as WorkComponent);
const comps = [c("a", "aeon", "/w/aeon"), c("ui", "ui", "/w/aeon/ui", "a"), c("kit", "kit", "/w/aeon/ui/kit", "ui"), c("d", "daemon", "/w/aeon/d", "a"), c("h", "helper", "/w/helper")];
const loop = (root: string, status: string) => ({ id: root + status, componentRoot: root, status } as unknown as WorkLoop);

describe("workModel", () => {
  it("builds the tree sorted, children under parents", () => {
    const tree = buildTree(comps);
    expect(tree.map((n) => n.component.id)).toEqual(["a", "h"]);
    expect(tree[0]?.children.map((n) => n.component.id)).toEqual(["d", "ui"]);
    expect(flatten(tree).find((n) => n.component.id === "kit")?.depth).toBe(2);
  });
  it("shows an orphan as a root instead of dropping it", () => {
    expect(buildTree([c("x", "x", "/x", "missing")]).map((n) => n.component.id)).toEqual(["x"]);
  });
  it("finds the subtree and the path", () => {
    const tree = buildTree(comps);
    expect([...subtreeIds(tree, "ui")].sort()).toEqual(["kit", "ui"]);
    expect(pathTo(comps, "kit").map((x) => x.name)).toEqual(["aeon", "ui", "kit"]);
    expect(pathTo(comps, "nope")).toEqual([]);
  });
  it("scopes roots: all, one, with sub components", () => {
    const tree = buildTree(comps);
    expect(scopeRoots(comps, tree, "", false).size).toBe(5);
    expect([...scopeRoots(comps, tree, "ui", false)]).toEqual(["/w/aeon/ui"]);
    expect([...scopeRoots(comps, tree, "ui", true)].sort()).toEqual(["/w/aeon/ui", "/w/aeon/ui/kit"]);
  });
  it("counts running and needs-you and rolls them up", () => {
    const counts = countByRoot([loop("/w/aeon/ui", "running"), loop("/w/aeon/ui/kit", "waiting"), loop("/w/aeon/d", "blocked")]);
    const aeon = buildTree(comps)[0]!;
    expect(rollup(aeon, counts)).toEqual({ running: 2, needsYou: 2 });
  });
  it("parses tabs and builds paths", () => {
    expect(parseTab("loops")).toBe("loops");
    expect(parseTab("zzz")).toBe("overview");
    expect(workPath("", "overview")).toBe("/work");
    expect(workPath("", "loops")).toBe("/work/all/loops");
    expect(workPath("ui", "plan")).toBe("/work/ui/plan");
  });
  it("maps every old list address", () => {
    expect(legacyTarget("loops", "ui")).toBe("/work/ui/loops");
    expect(legacyTarget("roadmap", "")).toBe("/work/all/plan");
    expect(legacyTarget("components", "ui")).toBe("/work/ui");
    expect(legacyTarget("system", "")).toBeUndefined();
  });
});
