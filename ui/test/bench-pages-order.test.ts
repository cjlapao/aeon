import { describe, expect, it } from "vitest";
import { BENCH_PAGES } from "../src/app/routes";

// S34 T5 / S35 T8, TB11/TD11: the menu position is load-bearing — the
// order IS the authorization story (Policy → Agents → MCP → Proposals,
// the desk last because it is about all three others' knobs). A later
// edit that reorders or separates these four breaks nothing else visibly
// (the dispatch branches in TestBench.tsx key off `id`, not position), so
// this is the one place that regression would be silent without a test.
describe("BENCH_PAGES — the trust bench's position", () => {
  it("places agents, mcp and proposals immediately after policy, in that order (TestBenchPagesOrderIsPolicyAgentsMcpProposals)", () => {
    const ids = BENCH_PAGES.map((p) => p.id);
    const policyIndex = ids.indexOf("policy");
    expect(policyIndex).toBeGreaterThanOrEqual(0);
    expect(ids.slice(policyIndex, policyIndex + 4)).toEqual(["policy", "agents", "mcp", "proposals"]);
  });

  it("marks exactly the four Trust pages with group: 'trust', nothing else", () => {
    const trustIds = BENCH_PAGES.filter((p) => "group" in p && p.group === "trust").map((p) => p.id);
    expect(trustIds).toEqual(["policy", "agents", "mcp", "proposals"]);
  });
});
