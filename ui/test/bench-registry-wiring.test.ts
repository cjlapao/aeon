import { describe, expect, it, beforeEach, vi } from "vitest";

// routes.ts merges the legacy BENCH_PAGES literal with whatever the
// registry holds at the moment routes.ts itself is first evaluated — so a
// page registered by pages/registerAll.ts's explicit imports (one line per
// page, never an array entry/import/switch-branch trio) shows up in the
// real menu with zero further edits. Each test gets a FRESH module graph
// (vi.resetModules) so routes.ts's one-time BENCH_PAGES computation can be
// observed under a registry primed before vs. after that import.

beforeEach(() => {
  vi.resetModules();
});

describe("routes.ts BENCH_PAGES merges the registry", () => {
  it("still contains every legacy page when the registry is empty", async () => {
    const { BENCH_PAGES } = await import("../src/app/routes");
    const ids = BENCH_PAGES.map((p) => p.id);
    expect(ids).toContain("status");
    expect(ids).toContain("policy");
    expect(ids).toContain("proposals");
  });

  it("includes a page registered before routes.ts is first imported", async () => {
    const { registerBenchPage } = await import("../src/features/test-bench/lib/benchRegistry");
    registerBenchPage({ id: "fixture-merge-probe", label: "Fixture", path: "/bench/fixture-merge-probe", order: 5000, needsConnection: true, render: () => null });

    const { BENCH_PAGES, isBenchPage } = await import("../src/app/routes");
    const ids = BENCH_PAGES.map((p) => p.id);
    expect(ids).toContain("fixture-merge-probe");
    expect(isBenchPage("fixture-merge-probe")).toBe(true);
  });

  it("a registry page carrying group: 'trust' renders in BENCH_PAGES with that field intact", async () => {
    const { registerBenchPage } = await import("../src/features/test-bench/lib/benchRegistry");
    registerBenchPage({ id: "fixture-trust-probe", label: "Fixture Trust", path: "/bench/fixture-trust-probe", order: 5001, group: "trust", needsConnection: true, render: () => null });

    const { BENCH_PAGES } = await import("../src/app/routes");
    const found = BENCH_PAGES.find((p) => p.id === "fixture-trust-probe");
    expect(found && "group" in found ? found.group : undefined).toBe("trust");
  });
});
