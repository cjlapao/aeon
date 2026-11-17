import { describe, expect, it } from "vitest";
import { graphRootsFor, indexRootFor, retrievalComponentsFor } from "./knowledgeScope";

const idx = [{ root: "/r/aeon" }, { root: "/r/helper" }];

describe("graphRootsFor", () => {
  it("keeps every index for all projects", () => {
    expect(graphRootsFor(idx, new Set(), true).roots).toHaveLength(2);
  });
  it("matches an equal root", () => {
    const r = graphRootsFor(idx, new Set(["/r/helper"]), false);
    expect(r.roots).toEqual([{ root: "/r/helper" }]);
    expect(r.shared).toEqual([]);
  });
  it("serves a sub component from its parent's repository and says so", () => {
    const r = graphRootsFor(idx, new Set(["/r/aeon/ui"]), false);
    expect(r.roots).toEqual([{ root: "/r/aeon" }]);
    expect(r.shared).toEqual(["/r/aeon/ui"]);
  });
  it("does not match a sibling that only shares a name prefix", () => {
    expect(graphRootsFor(idx, new Set(["/r/aeon-x"]), false).roots).toEqual([]);
  });
});

describe("retrievalComponentsFor", () => {
  const rows = [{ component: "c1" }, { component: "ui" }, { component: "other" }];
  const scope = [{ id: "c1", name: "ui", root: "/r/aeon/ui" }] as never;
  it("matches by id or name", () => expect(retrievalComponentsFor(rows, scope, false)).toHaveLength(2));
});

describe("indexRootFor", () => {
  it("is the project's own index, else the repository index above it, else nothing yet", () => {
    expect(indexRootFor(idx, "/r/helper")).toEqual({ root: "/r/helper" });
    expect(indexRootFor(idx, "/r/aeon/ui")).toEqual({ root: "/r/aeon" });
    expect(indexRootFor(idx, "/r/new-project")).toBeUndefined();
  });
  it("picks the deepest index when several contain the project", () => {
    expect(indexRootFor([{ root: "/r" }, { root: "/r/aeon" }], "/r/aeon/ui")).toEqual({ root: "/r/aeon" });
  });
});
