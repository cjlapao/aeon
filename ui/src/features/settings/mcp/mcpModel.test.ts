import { describe, expect, it } from "vitest";
import {
  canSubmit,
  declareNeedsAllow,
  describeBeforeAfter,
  duplicateDraftFrom,
  emptyServerDraft,
  envNamesOnly,
  grantCellState,
  groupServers,
  isUrlTransport,
  mergeChanges,
  unclassifiedTools,
  type GrantView,
  type McpVocabulary,
  type ServerDetail,
  type ServerRow,
} from "./mcpModel";

const vocabulary: McpVocabulary = {
  transports: ["command-ish", "streamhttp"],
  rerun: ["safe", "detectable", "unsafe"],
  effect: ["near", "outward", "undoable"],
  grammars: { envName: "^[A-Z][A-Z0-9_]*$", server: "…", tool: "…" },
};

function draft(overrides: Partial<Parameters<typeof canSubmit>[0]> = {}) {
  return { ...emptyServerDraft(), name: "gh", tier: "public", transportType: "command-ish", command: "gh-mcp", ...overrides };
}

describe("envNamesOnly", () => {
  it("accepts a bare name", () => {
    expect(envNamesOnly("GITHUB_TOKEN")).toEqual({ ok: true, name: "GITHUB_TOKEN" });
  });
  it("refuses an assignment — a value would smuggle through the door", () => {
    const verdict = envNamesOnly("A=b");
    expect(verdict.ok).toBe(false);
    if (!verdict.ok) expect(verdict.key).toBe("settings.mcp.form.envName.value");
  });
  it("refuses leading whitespace (a pasted pair)", () => {
    expect(envNamesOnly(" X").ok).toBe(false);
  });
  it("refuses the empty entry", () => {
    expect(envNamesOnly("").ok).toBe(false);
    expect(envNamesOnly("  ").ok).toBe(false);
  });
});

describe("grantCellState", () => {
  function view(state: string, missingClasses?: string[]): GrantView {
    return { qualified: "gh.list", state, tier: "public", reason: "r", missingClasses };
  }
  it("maps the four states distinctly", () => {
    expect(grantCellState(view("available")).tone).toBe("green");
    expect(grantCellState(view("ask")).tone).toBe("amber");
    expect(grantCellState(view("unavailable")).tone).toBe("slate");
    expect(grantCellState(view("refused")).tone).toBe("rose");
  });
  it("carries the state word UNREWRITTEN", () => {
    expect(grantCellState(view("unavailable")).state).toBe("unavailable");
  });
  it("flags missing classes", () => {
    expect(grantCellState(view("unavailable", ["rerun"])).unclassified).toBe(true);
    expect(grantCellState(view("available")).unclassified).toBe(false);
  });
});

describe("declareNeedsAllow", () => {
  it("spots the MG7 moment — declared but the policy has not spoken", () => {
    const after = [
      { qualified: "gh.list", state: "available", tier: "public" },
      { qualified: "gh.create", state: "unavailable", tier: "public", reason: "refused: no allow rule matched gh.create" },
    ];
    expect(declareNeedsAllow(after, "gh.create")?.reason).toContain("no allow");
    expect(declareNeedsAllow(after, "gh.list")).toBeUndefined();
  });
});

describe("describeBeforeAfter", () => {
  const a: GrantView = { qualified: "gh.list", state: "available", tier: "public" };
  const b: GrantView = { qualified: "gh.create", state: "unavailable", tier: "public" };
  it("identical lists read NO CHANGE", () => {
    const diff = describeBeforeAfter([a, b], [a, b]);
    expect(diff.changed).toBe(false);
    expect(diff.added).toEqual([]);
    expect(diff.removed).toEqual([]);
    expect(diff.changedState).toEqual([]);
  });
  it("an added tool reads added", () => {
    expect(describeBeforeAfter([], [a]).added).toEqual(["gh.list"]);
  });
  it("a state flip reads changed, with both daemon words", () => {
    const flipped = { ...a, state: "ask" };
    const diff = describeBeforeAfter([a], [flipped]);
    expect(diff.changedState).toEqual([{ qualified: "gh.list", from: "available", to: "ask" }]);
  });
});

describe("canSubmit", () => {
  it("a sound draft passes", () => {
    const decision = canSubmit(draft(), vocabulary);
    expect(decision.ok).toBe(true);
    expect(decision.errors).toEqual([]);
  });
  it("unclassified tools are ALLOWED but FLAGGED (MC6 defaults nothing)", () => {
    const decision = canSubmit(draft({ tools: [{ name: "list_issues", rerun: "", effect: "" }] }), vocabulary);
    expect(decision.ok).toBe(true);
    expect(decision.flags).toEqual(["settings.mcp.form.tool.unclassified#0"]);
  });
  it("a half-declared class is an error, not a flag", () => {
    const decision = canSubmit(draft({ tools: [{ name: "t", rerun: "safe", effect: "" }] }), vocabulary);
    expect(decision.ok).toBe(false);
    expect(decision.errors).toContain("settings.mcp.form.tool.halfClassified");
  });
  it("a class outside the daemon's vocabulary is refused at the gate", () => {
    const decision = canSubmit(draft({ tools: [{ name: "t", rerun: "sometimes", effect: "near" }] }), vocabulary);
    expect(decision.errors).toContain("settings.mcp.form.tool.classOutsideVocabulary");
  });
  it("a private server needs an owner", () => {
    const decision = canSubmit(draft({ tier: "private" }), vocabulary);
    expect(decision.errors).toContain("settings.mcp.form.privateNeedsOwner");
    expect(canSubmit(draft({ tier: "private", agent: "tester" }), vocabulary).ok).toBe(true);
  });
  it("a public server forbids an owner", () => {
    const decision = canSubmit(draft({ agent: "tester" }), vocabulary);
    expect(decision.errors).toContain("settings.mcp.form.publicForbidsOwner");
  });
  it("the URL transport needs a url; the command transport needs a command", () => {
    expect(canSubmit(draft({ transportType: "streamhttp" }), vocabulary).errors).toContain("settings.mcp.form.needUrl");
    expect(canSubmit(draft({ command: "" }), vocabulary).errors).toContain("settings.mcp.form.needCommand");
  });
  it("an env entry smuggling a value blocks the submit", () => {
    expect(canSubmit(draft({ env: ["A=b"] }), vocabulary).errors).toContain("settings.mcp.form.envName.value");
  });
});

describe("isUrlTransport", () => {
  it("picks the vocabulary entry that names http — the section names none itself", () => {
    expect(isUrlTransport(vocabulary, "streamhttp")).toBe(true);
    expect(isUrlTransport(vocabulary, "command-ish")).toBe(false);
    expect(isUrlTransport(undefined, "streamhttp")).toBe(false);
  });
});

describe("groupServers / unclassifiedTools", () => {
  const row = (tier: string, classified: boolean): ServerRow =>
    ({
      agent: "", name: `s${tier}${classified ? "a" : "b"}`, tier, origin: "user", enabled: true, status: "unprobed",
      declaredBy: 0, updatedAt: "",
      transport: { type: "command-ish", command: "x", args: [], envNames: [] },
      tools: [{ name: "t", classified, rerun: classified ? "safe" : "", effect: classified ? "near" : "" }],
    }) as unknown as ServerRow;
  it("groups by tier preserving the daemon's order", () => {
    const groups = groupServers([row("public", true), row("private", true), row("public", false)]);
    expect(groups.map((g) => g.tier)).toEqual(["public", "private"]);
    expect(groups[0]?.rows).toHaveLength(2);
  });
  it("spots an unclassified tool by class fields, not by a UI guess", () => {
    expect(unclassifiedTools(row("public", false))).toHaveLength(1);
    expect(unclassifiedTools(row("public", true))).toHaveLength(0);
  });
});

describe("duplicateDraftFrom", () => {
  it("seeds the definition but NOT the name — the collision forces the new name", () => {
    const detail = {
      name: "gh", agent: "", tier: "public", enabled: true, origin: "system", status: "live", declaredBy: 2, updatedAt: "",
      transport: { type: "command-ish", command: "gh-mcp", args: ["--mcp"], envNames: ["GITHUB_TOKEN"] },
      tools: [{ name: "list", classified: true, rerun: "safe", effect: "near" }],
    } as unknown as ServerDetail;
    const copy = duplicateDraftFrom(detail);
    expect(copy.name).toBe("");
    expect(copy.enabled).toBe(false);
    expect(copy.env).toEqual(["GITHUB_TOKEN"]);
    expect(copy.tools).toEqual([{ name: "list", rerun: "safe", effect: "near" }]);
  });
});

describe("mergeChanges", () => {
  it("fresh first, ids deduplicated — no re-sort into a lie", () => {
    const old = [{ id: 2 }, { id: 1 }] as never[];
    const fresh = [{ id: 3 }, { id: 2 }] as never[];
    expect(mergeChanges(old, fresh).map((r) => r.id)).toEqual([3, 2, 1]);
  });
});
