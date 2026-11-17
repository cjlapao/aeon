import { describe, expect, it } from "vitest";
import { BUILT_IN_MINIONS, minionNameFor } from "./minionIdentity";

describe("which minion an agent is", () => {
  it("is always one of the three built-ins, and always the same one for the same agent", () => {
    for (const slug of ["legal-advisor", "project-planner", "brainstormer", "x"]) {
      expect(BUILT_IN_MINIONS).toContain(minionNameFor(slug));
      expect(minionNameFor(slug)).toBe(minionNameFor(slug));
    }
  });

  it("spreads a real roster over pip, bop and noodle", () => {
    const roster = ["brainstormer", "intent-clarifier", "investigator", "legal-advisor", "product-manager", "project-manager",
      "project-planner", "proposal-author", "brief-author", "security-engineer", "debugger-engineer", "golang-engineer",
      "python-engineer", "rust-engineer", "technical-writer", "qa-test-spec", "memory-steward", "chat"];
    const seen = new Map<string, number>();
    for (const slug of roster) seen.set(minionNameFor(slug), (seen.get(minionNameFor(slug)) ?? 0) + 1);
    expect([...seen.keys()].sort()).toEqual(["bop", "noodle", "pip"]);
  });
});
