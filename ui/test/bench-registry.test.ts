import { describe, expect, it, beforeEach } from "vitest";
import { __resetBenchRegistryForTests, benchPages, getBenchPage, registerBenchPage } from "../src/features/test-bench/lib/benchRegistry";

// A new bench page registers itself by calling registerBenchPage at its own
// module scope instead of a slice editing routes.ts's array, TestBench.tsx's
// import list, AND TestBench.tsx's dispatch switch — three shared-file edits
// collapsed to zero, so two worktrees adding a page never touch the same
// line of the same file. See docs/aeon/README.md's "Adding a bench page".

beforeEach(() => __resetBenchRegistryForTests());

describe("benchRegistry", () => {
  it("registers a page and makes it findable by id", () => {
    registerBenchPage({ id: "widget", label: "Widget", path: "/bench/widget", order: 100, needsConnection: true, render: () => null });
    const found = getBenchPage("widget");
    expect(found?.label).toBe("Widget");
    expect(found?.path).toBe("/bench/widget");
  });

  it("returns undefined for an id nothing registered", () => {
    expect(getBenchPage("nope")).toBeUndefined();
  });

  it("refuses a second registration under the same id", () => {
    registerBenchPage({ id: "widget", label: "Widget", path: "/bench/widget", order: 100, needsConnection: true, render: () => null });
    expect(() =>
      registerBenchPage({ id: "widget", label: "Widget Two", path: "/bench/widget2", order: 101, needsConnection: true, render: () => null }),
    ).toThrow(/widget/);
  });

  it("lists every registered page sorted by its own order field, not registration order", () => {
    registerBenchPage({ id: "second", label: "Second", path: "/bench/second", order: 20, needsConnection: true, render: () => null });
    registerBenchPage({ id: "first", label: "First", path: "/bench/first", order: 10, needsConnection: true, render: () => null });
    expect(benchPages().map((p) => p.id)).toEqual(["first", "second"]);
  });

  it("carries an optional trust group through unchanged", () => {
    registerBenchPage({ id: "widget", label: "Widget", path: "/bench/widget", order: 1, group: "trust", needsConnection: true, render: () => null });
    expect(getBenchPage("widget")?.group).toBe("trust");
  });
});
