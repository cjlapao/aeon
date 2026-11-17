import { describe, expect, it } from "vitest";
import {
  applyDisplayOrder,
  buildSurfaceFilter,
  buildToolsFilter,
  capabilityMeaning,
  capabilityPill,
  doorChips,
  driftSummary,
  driftTone,
  emptySurfaceFilter,
  emptyToolsFilter,
  filterOptions,
  groupByState,
  linkFor,
  stateBadge,
  surfaceCountChips,
  surfaceDoorOptions,
  surfaceKindOptions,
  surfaceFilterIsEmpty,
  toolsFilterIsEmpty,
  type ByAgentResult,
  type CatalogVocabulary,
  type SurfaceCounts,
  type ToolRow,
} from "./toolsModel";

/**
 * The pure half of the Tools/Surface section (S47 T10). Every fixture below is a
 * STAND-IN for a daemon answer — the capability, family, state and door words that
 * appear here are test data on purpose, because `toolsRpc.guard.test.ts` forbids
 * them in the production sources. The canary tests at the end are the reason this
 * file is longer than the assertions alone: a pure function that returns the same
 * thing for every input passes an assertion and proves nothing.
 */

function toolRow(overrides: Partial<ToolRow> = {}): ToolRow {
  return {
    access: "read",
    address: "workspace.read.read",
    category: "workspace",
    available: true,
    capabilities: ["workspace_read"],
    classified: true,
    conditional: false,
    description: "Reads a file under the workspace root.",
    family: "workspace",
    schemaHash: "sha256:0a",
    governed: ["tool_access"],
    name: "read",
    origin: "builtin",
    rerunClass: "safe",
    rowCurrent: true,
    schema: {},
    source: "system",
    version: 1,
    ...overrides,
  };
}

const vocabulary: CatalogVocabulary = {
  capabilities: [
    { name: "workspace_read", meaning: "Reads bytes under the step's workspace root through the one resolver." },
    { name: "spawn_process", meaning: "Starts a process, and only behind a policy-issued allow token." },
  ],
  families: ["workspace", "acme"],
  accesses: ["read", "write"],
  sources: ["system"],
  rerunClasses: ["safe", "detectable", "unsafe"],
};

const counts: SurfaceCounts = {
  byDoor: { socket: 9, public: 6, narrow: 3, agentService: 9 },
  byKind: { read: 6, write: 0, control: 0, diagnostic: 3 },
  total: 9,
  unfiltered: 185,
};

// --- the pills and badges ---------------------------------------------------------

describe("capabilityPill", () => {
  it("sends the capability's own word and never a prettified one", () => {
    expect(capabilityPill("workspace_read").text).toBe("workspace_read");
    expect(capabilityPill("network_egress").text).toBe("network_egress");
  });

  it("gives distinguishable tones, and a stable one for the same word", () => {
    const words = ["workspace_read", "workspace_write", "spawn_process", "network_egress", "asks_human", "messages_agent", "org_records_read", "index_read"];
    const tones = new Set(words.map((w) => capabilityPill(w).tone));
    expect(tones.size).toBeGreaterThan(3);
    expect(capabilityPill("spawn_process").tone).toBe(capabilityPill("spawn_process").tone);
  });

  it("is a function of the word and not a constant", () => {
    // The canary: a function returning one tone for every input would pass the
    // set-size test above only by accident. Hashing into ten tones means SOME
    // pair can collide, so the claim checked here is the one that is always true:
    // among the sample, at least one word does not get the first word's tone.
    const words = ["workspace_read", "workspace_write", "spawn_process", "network_egress", "asks_human", "messages_agent", "org_records_read", "index_read"];
    const tones = words.map((w) => capabilityPill(w).tone);
    expect(tones.some((tone) => tone !== tones[0])).toBe(true);
    expect(tones.every((tone) => tone === tones[0])).toBe(false);
  });
});

describe("stateBadge", () => {
  it("keeps the daemon's state text verbatim", () => {
    expect(stateBadge("granted_denied").text).toBe("granted_denied");
  });

  it("renders the five states as five distinguishable badges", () => {
    const five = ["granted_allowed", "granted_denied", "granted_ask", "granted_no_rule", "not_granted"];
    const tones = new Set(five.map((s) => stateBadge(s).tone));
    expect(tones.size).toBe(5);
  });

  it("colours a policy action too, because the drawer shows those beside it", () => {
    expect(stateBadge("allow").tone).toBe("green");
    expect(stateBadge("deny").tone).toBe("rose");
  });

  it("comes back neutral for a state the rules cannot place, never green", () => {
    // P31's direction of travel: an unknown reads as unknown, not as permission.
    expect(stateBadge("").tone).toBe("slate");
    expect(stateBadge("quux").tone).toBe("slate");
  });
});

describe("driftTone", () => {
  it("puts the loudest colour on the disagreement, not on the absent row", () => {
    expect(driftTone("differs")).toBe("rose");
    expect(driftTone("missing_row")).toBe("orange");
    expect(driftTone("retired")).toBe("slate");
    expect(driftTone("something_else")).toBe("slate");
  });
});

// --- the grouping -----------------------------------------------------------------

describe("groupByState", () => {
  function row(agent: string, tool: string, state: string): ByAgentResult["tools"][number] {
    return { agent, tool, state, dispatchable: true };
  }

  it("groups by the state the daemon sent, in the order it sent them", () => {
    const groups = groupByState([row("atlas", "bash", "granted_denied"), row("atlas", "read", "granted_allowed"), row("atlas", "edit", "granted_denied")]);
    expect(groups.map((g) => g.state)).toEqual(["granted_denied", "granted_allowed"]);
    expect(groups[0]?.rows.map((r) => r.tool)).toEqual(["bash", "edit"]);
  });

  it("keeps the daemon's order inside a group rather than sorting it", () => {
    const groups = groupByState([row("a", "zulu", "granted_allowed"), row("a", "alpha", "granted_allowed")]);
    expect(groups[0]?.rows.map((r) => r.tool)).toEqual(["zulu", "alpha"]);
  });

  it("answers an empty answer with no groups", () => {
    expect(groupByState([])).toEqual([]);
  });
});

// --- the door chips ---------------------------------------------------------------

describe("doorChips", () => {
  it("chips the doors the entry says it is on, with the narrow action's own name", () => {
    const chips = doorChips({ doors: { socket: true, public: true, narrow: "aeon_loop_show", agentService: false, toolTwin: "loop_list" } });
    expect(chips.map((c) => c.id)).toEqual(["socket", "public", "narrow", "toolTwin"]);
    const narrow = chips.find((c) => c.id === "narrow");
    expect(narrow?.value).toBe("aeon_loop_show");
    expect(narrow?.tone).toBe("violet");
    expect(chips.find((c) => c.id === "toolTwin")?.value).toBe("loop_list");
  });

  it("renders an unknown narrow flag as unknown, never as absent", () => {
    // The case P31 exists for: the seam was never wired, so nobody can say.
    const chips = doorChips({ doors: { socket: true, public: false, narrow: "unknown", agentService: false } });
    const narrow = chips.find((c) => c.id === "narrow");
    expect(narrow?.value).toBe("unknown");
    expect(narrow?.tone).toBe("amber");
    expect(chips.map((c) => c.id)).not.toContain("public");
  });

  it("carries the two badges as chips of their own", () => {
    const chips = doorChips({ doors: { socket: true, public: false, narrow: "", agentService: false }, humanDoorOnly: true, debug: true });
    expect(chips.map((c) => c.id)).toEqual(["socket", "humanDoorOnly", "debug"]);
  });

  it("says nothing about doors the entry does not claim", () => {
    expect(doorChips({ doors: { socket: false, public: false, narrow: "", agentService: false } })).toEqual([]);
    expect(doorChips({})).toEqual([]);
  });
});

// --- the links --------------------------------------------------------------------

describe("linkFor", () => {
  it("routes a policy reference to where rules are authored", () => {
    expect(linkFor({ kind: "policy", id: "cmd.7" })).toEqual({ to: "/settings?policy=cmd.7", labelKey: "settings.tools.drawer.links.policy" });
  });

  it("routes an MCP tool to its server", () => {
    expect(linkFor({ kind: "mcp_server", id: "acme" })?.to).toBe("/settings?server=acme");
  });

  it("returns nothing for a reference it does not know, rather than a plausible route", () => {
    // A link that goes nowhere reads as a fact about the target, so the honest
    // answer to an unknown kind is no link at all.
    expect(linkFor({ kind: "loop", id: "l-1" })).toBeUndefined();
    expect(linkFor(undefined)).toBeUndefined();
  });

  it("escapes the identifier it puts in a query", () => {
    expect(linkFor({ kind: "policy", id: "a b&c" })?.to).toBe("/settings?policy=a%20b%26c");
  });
});

// --- the drift banner's data ------------------------------------------------------

describe("driftSummary", () => {
  it("is quiet and names the version when nothing drifts", () => {
    const summary = driftSummary({ counts: { conditionalUnwired: 2, mcp: 2, system: 72 }, drift: [], seed: { appliedVersion: 3, availableVersion: 3, set: "tools.core", state: "applied" } });
    expect(summary.kind).toBe("current");
    expect(summary.version).toBe(3);
    expect(summary.set).toBe("tools.core");
    expect(summary.lines).toEqual([]);
  });

  it("carries every drift line with the field names the daemon reported", () => {
    const summary = driftSummary({ counts: { conditionalUnwired: 0, mcp: 0, system: 3 }, drift: [{ name: "bash", state: "differs", fields: ["capabilities", "rerun_class"] }], seed: { appliedVersion: 3, availableVersion: 4, set: "tools.core", state: "partial" } });
    expect(summary.kind).toBe("drift");
    expect(summary.lines).toEqual([{ name: "bash", state: "differs", fields: ["capabilities", "rerun_class"] }]);
  });

  it("says unknown when the door has not answered, rather than current", () => {
    // A screen that reads "catalog current" because the read failed is the exact
    // lie this banner is here to prevent.
    expect(driftSummary(undefined).kind).toBe("unknown");
  });
});

// --- the filters ------------------------------------------------------------------

describe("the filter round trip", () => {
  it("sends nothing for a filter nobody touched", () => {
    expect(buildToolsFilter(emptyToolsFilter())).toEqual({});
    expect(buildSurfaceFilter(emptySurfaceFilter())).toEqual({});
    expect(toolsFilterIsEmpty(emptyToolsFilter())).toBe(true);
    expect(surfaceFilterIsEmpty(emptySurfaceFilter())).toBe(true);
  });

  it("sends only what was chosen, with the tri-state available as a boolean", () => {
    expect(buildToolsFilter({ family: "workspace", source: "", capability: "workspace_read", access: "", available: "false", q: "read" })).toEqual({ family: "workspace", capability: "workspace_read", available: false, q: "read" });
    expect(buildToolsFilter({ family: "", source: "", capability: "", access: "", available: "true", q: "" })).toEqual({ available: true });
    expect(buildSurfaceFilter({ kind: "read", door: "", humanDoorOnly: "true", q: "" })).toEqual({ kind: "read", humanDoorOnly: true });
    expect(toolsFilterIsEmpty({ family: "x", source: "", capability: "", access: "", available: "", q: "" })).toBe(false);
  });
});

describe("filterOptions", () => {
  it("offers exactly the spellings the daemon sent", () => {
    const options = filterOptions(vocabulary);
    expect(options.families).toEqual(["workspace", "acme"]);
    expect(options.sources).toEqual(["system"]);
    expect(options.capabilities).toEqual(["workspace_read", "spawn_process"]);
    expect(options.rerunClasses).toEqual(["safe", "detectable", "unsafe"]);
  });

  it("offers nothing before the vocabulary door has answered", () => {
    // The selects render empty and disabled rather than offering a guess typed
    // beside the door that owns the list.
    expect(filterOptions(undefined)).toEqual({ families: [], sources: [], capabilities: [], rerunClasses: [], accesses: [] });
  });

  it("reads the surface kinds and doors out of the counts, every bucket including the empty ones", () => {
    expect(surfaceKindOptions(counts)).toEqual(["read", "write", "control", "diagnostic"]);
    expect(surfaceDoorOptions(counts)).toEqual(["socket", "public", "narrow", "agentService"]);
    expect(surfaceKindOptions(undefined)).toEqual([]);
  });

  it("counts every bucket the daemon named, beside the filtered total", () => {
    const chips = surfaceCountChips(counts);
    expect(chips.find((c) => c.label === "read")?.value).toBe(6);
    expect(chips.find((c) => c.label === "write")?.value).toBe(0);
    expect(chips.find((c) => c.label === "socket")?.value).toBe(9);
    expect(surfaceCountChips(undefined)).toEqual([]);
  });
});

describe("capabilityMeaning", () => {
  it("returns the daemon's sentence for a capability it named", () => {
    expect(capabilityMeaning(vocabulary, "spawn_process")).toBe("Starts a process, and only behind a policy-issued allow token.");
  });

  it("returns nothing rather than inventing a sentence", () => {
    expect(capabilityMeaning(vocabulary, "unheard_of")).toBe("");
    expect(capabilityMeaning(undefined, "workspace_read")).toBe("");
  });
});

// --- the display order ------------------------------------------------------------

describe("applyDisplayOrder", () => {
  it("keeps the daemon's reading order, which is not alphabetical", () => {
    const rows = [toolRow({ name: "zulu" }), toolRow({ name: "alpha" }), toolRow({ name: "mike" })];
    expect(applyDisplayOrder(rows).map((r) => r.name)).toEqual(["zulu", "alpha", "mike"]);
  });

  it("drops a repeated name, which is a rendering bug rather than a fact", () => {
    const rows = [toolRow({ name: "read" }), toolRow({ name: "read", family: "other" }), toolRow({ name: "edit" })];
    expect(applyDisplayOrder(rows).map((r) => r.name)).toEqual(["read", "edit"]);
  });

  it("does not drop anything when no name repeats — the canary that the dedupe is real", () => {
    const rows = [toolRow({ name: "read" }), toolRow({ name: "edit" })];
    expect(applyDisplayOrder(rows)).toHaveLength(2);
    expect(applyDisplayOrder([])).toEqual([]);
  });
});
