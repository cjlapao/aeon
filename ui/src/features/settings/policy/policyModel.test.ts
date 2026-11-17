import { describe, expect, it } from "vitest";
import {
  applyFilters,
  authorshipFor,
  buildAddParams,
  canSubmit,
  controlFor,
  describeDiff,
  emptyFilterDraft,
  groupByTarget,
  groupSeedRecordsByAgent,
  isExpired,
  seedPreviewRows,
  specFieldsFor,
  suggestedId,
  type PolicyDraft,
  type PolicyRow,
  type Vocabulary,
} from "./policyModel";

// Test fixtures MAY name the closed-set values (the grep guard's exemption)
// precisely so the tests prove the UI never has to: everything below is fed
// through as daemon data, never recalled by the production code.

const vocabulary: Vocabulary = {
  types: ["tool_access", "command_access", "route_access"],
  targetKinds: ["all-agents", "agent", "loop", "lane-type", "mcp"],
  sources: ["operator", "agent", "mcp_server", "import"],
  actions: ["allow", "deny", "ask"],
  authorship: [
    { source: "operator", action: "allow", allowed: true },
    { source: "operator", action: "deny", allowed: true },
    { source: "operator", action: "ask", allowed: true },
    {
      source: "agent",
      action: "allow",
      allowed: false,
      reason: "refused: an allow may only be authored by source \"operator\", not \"agent\" (PL1)",
    },
    { source: "agent", action: "deny", allowed: true },
    { source: "agent", action: "ask", allowed: true },
  ],
  specs: [
    {
      type: "tool_access",
      fields: [
        { name: "toolName", kind: "string", required: true, description: "the tool" },
        { name: "state", kind: "enum", required: true, enumValues: ["allow", "deny", "ask"], description: "verdict" },
      ],
    },
    {
      type: "command_access",
      fields: [
        { name: "pattern", kind: "string", required: true, description: "the command pattern" },
        { name: "limit", kind: "number", required: false, description: "a bound" },
      ],
    },
  ],
};

function row(overrides: Partial<PolicyRow> & { id: string; target: string }): PolicyRow {
  return { type: "command_access", action: "deny", source: "operator", notAfter: null, ...overrides };
}

function draft(overrides: Partial<PolicyDraft> = {}): PolicyDraft {
  return {
    type: "tool_access",
    targetKind: "agent",
    targetId: "golang-engineer",
    action: "deny",
    source: "operator",
    id: "ui.test.rule",
    expiry: "",
    spec: { toolName: "bash", state: "deny" },
    ...overrides,
  };
}

describe("controlFor maps spec fields", () => {
  it("enum → select with the descriptor's own values", () => {
    const control = controlFor(vocabulary.specs[0]!.fields[1]!);
    expect(control.control).toBe("select");
    expect(control.options).toEqual(["allow", "deny", "ask"]);
    expect(control.required).toBe(true);
  });

  it("string → text, with maxLength when the descriptor ships one", () => {
    const control = controlFor({ ...vocabulary.specs[0]!.fields[0]!, maxLength: 128 } as (typeof vocabulary.specs)[0]["fields"][0]);
    expect(control.control).toBe("text");
    expect(control.maxLength).toBe(128);
  });

  it("number → numeric", () => {
    expect(controlFor(vocabulary.specs[1]!.fields[1]!).control).toBe("numeric");
  });

  it("an unknown kind still renders (as text, marked), never vanishes", () => {
    const control = controlFor({ name: "weird", kind: "duration", required: false, description: "" });
    expect(control.control).toBe("text");
    expect(control.unknownKind).toBe(true);
  });
});

describe("canSubmit follows the matrix", () => {
  it("allow + a non-operator source → false, carrying the daemon's own reason", () => {
    const decision = canSubmit(draft({ action: "allow", source: "agent" }), vocabulary);
    expect(decision.ok).toBe(false);
    expect(decision.matrixBlocked).toBe(true);
    // The reason is the matrix cell's byte-for-byte — no UI re-derivation of PL1.
    expect(decision.refusal).toBe(vocabulary.authorship[3]!.reason);
  });

  it("deny + any source the matrix allows → true", () => {
    expect(canSubmit(draft({ action: "deny", source: "agent" }), vocabulary).ok).toBe(true);
    expect(canSubmit(draft({ action: "deny", source: "operator" }), vocabulary).ok).toBe(true);
  });

  it("allow + operator → true", () => {
    expect(canSubmit(draft({ action: "allow", source: "operator" }), vocabulary).ok).toBe(true);
  });

  it("a matrix cell the daemon never stated → disabled without an invented reason", () => {
    const decision = canSubmit(draft({ action: "allow", source: "import" }), vocabulary);
    expect(decision.ok).toBe(false);
    expect(decision.matrixBlocked).toBe(true);
    expect(decision.refusal).toBeUndefined();
  });

  it("required spec fields are completeness, not the matrix", () => {
    const decision = canSubmit(draft({ spec: { toolName: "bash", state: "" } }), vocabulary);
    expect(decision.ok).toBe(false);
    expect(decision.matrixBlocked).toBe(false);
    expect(decision.missing).toContain("spec.state");
  });
});

describe("authorshipFor", () => {
  it("finds the exact cell", () => {
    expect(authorshipFor(vocabulary, "agent", "deny")?.allowed).toBe(true);
    expect(authorshipFor(vocabulary, "agent", "allow")?.allowed).toBe(false);
    expect(authorshipFor(vocabulary, "import", "allow")).toBeUndefined();
  });
});

describe("buildAddParams", () => {
  it("empty expiry is null — never an empty string, never a sentinel (PL7)", () => {
    const params = buildAddParams(draft({ expiry: "" }));
    expect(params.notAfter).toBeNull();
  });

  it("a set expiry travels as the given instant", () => {
    const params = buildAddParams(draft({ expiry: "2030-01-02T03:04:05Z" }));
    expect(params.notAfter).toBe("2030-01-02T03:04:05Z");
  });

  it("sends exactly the draft as params — target {kind,id}, spec trimmed, blanks dropped", () => {
    const params = buildAddParams(draft({ spec: { toolName: " bash ", state: "deny", limit: "" } }));
    expect(params).toEqual({
      id: "ui.test.rule",
      type: "tool_access",
      target: { kind: "agent", id: "golang-engineer" },
      action: "deny",
      source: "operator",
      spec: { toolName: "bash", state: "deny" },
      notAfter: null,
    });
  });
});

describe("suggested id", () => {
  it("is deterministic from type+target (stable, never churning)", () => {
    const a = suggestedId("command_access", "agent", "Golang Engineer");
    const b = suggestedId("command_access", "agent", "Golang Engineer");
    expect(a).toBe(b);
    expect(a).toBe("command_access.agent.golang-engineer");
  });

  it("differs when type or target differ", () => {
    expect(suggestedId("tool_access", "agent", "a")).not.toBe(suggestedId("command_access", "agent", "a"));
    expect(suggestedId("tool_access", "agent", "a")).not.toBe(suggestedId("tool_access", "agent", "b"));
  });

  it("stays a valid-looking id: lowercase-safe and bounded", () => {
    const value = suggestedId("A B/C", "agent", "x".repeat(100));
    expect(value.length).toBeLessThanOrEqual(64);
    expect(value).toMatch(/^[a-z0-9._-]+$/);
  });
});

describe("groupByTarget keeps daemon order", () => {
  it("groups by first-seen target and keeps input order everywhere", () => {
    const rows = [
      row({ id: "1", target: "agent:b" }),
      row({ id: "2", target: "agent:a" }),
      row({ id: "3", target: "agent:b" }),
      row({ id: "4", target: "loop:z" }),
    ];
    const groups = groupByTarget(rows);
    expect(groups.map((g) => g.target)).toEqual(["agent:b", "agent:a", "loop:z"]);
    expect(groups[0]!.rows.map((r) => r.id)).toEqual(["1", "3"]);
  });
});

describe("applyFilters sends the filters as params", () => {
  it("an untouched draft sends only includeExpired", () => {
    expect(applyFilters(emptyFilterDraft())).toEqual({ includeExpired: true });
  });

  it("a chosen kind composes the CLI spelling kind:id", () => {
    expect(applyFilters({ ...emptyFilterDraft(), targetKind: "agent", targetId: "boss" }).target).toBe("agent:boss");
  });

  it("a chosen kind with no id sends the bare kind — the daemon parses and refuses, the UI never decides", () => {
    expect(applyFilters({ ...emptyFilterDraft(), targetKind: "all-agents" }).target).toBe("all-agents");
  });

  it("every filter travels; nothing filters rows", () => {
    const params = applyFilters({
      type: "command_access",
      targetKind: "loop",
      targetId: "ci",
      source: "operator",
      action: "deny",
      origin: "system",
      includeExpired: false,
    });
    expect(params).toEqual({
      type: "command_access",
      target: "loop:ci",
      source: "operator",
      action: "deny",
      origin: "system",
      includeExpired: false,
    });
  });
});

describe("isExpired", () => {
  it("marks past expiries, keeps null/no-expiry unmarked", () => {
    expect(isExpired("2000-01-01T00:00:00Z")).toBe(true);
    expect(isExpired("2999-01-01T00:00:00Z")).toBe(false);
    expect(isExpired(null)).toBe(false);
  });
});

describe("describeDiff", () => {
  it("identical before/after reads as no change", () => {
    const side = { target: "agent:bob", answers: [{ policyID: "p1", state: "deny", render: "denied by p1" }] };
    expect(describeDiff(side, { ...side })).toEqual({ changed: false });
  });

  it("a different after returns BOTH sides verbatim — never a collapsed effect", () => {
    const before = { target: "agent:bob", answers: [] as Array<{ policyID: string }> };
    const after = { target: "agent:bob", answers: [{ policyID: "p1", state: "allow", render: "allowed by p1" }] };
    const diff = describeDiff(before, after);
    expect(diff.changed).toBe(true);
    expect(diff.before).toBe(before);
    expect(diff.after).toBe(after);
  });
});

describe("specFieldsFor", () => {
  it("reads the descriptors the daemon sent for the type — none recalled", () => {
    expect(specFieldsFor(vocabulary, "command_access").map((f) => f.name)).toEqual(["pattern", "limit"]);
    expect(specFieldsFor(vocabulary, "route_access")).toEqual([]);
  });
});

describe("seed preview", () => {
  it("groups engine records by the agent key segment and tallies the daemon's states", () => {
    const states = groupSeedRecordsByAgent([
      { key: "corpus.alpha.aaa1", state: "applied" },
      { key: "corpus.alpha.aaa2", state: "removed" },
      { key: "corpus.beta.bbb1", state: "diverged" },
    ]);
    expect(states).toEqual([
      { agent: "alpha", counts: { applied: 1, removed: 1 } },
      { agent: "beta", counts: { diverged: 1 } },
    ]);
  });

  it("joins the run's per-agent counts with the engine states; nothing is dropped", () => {
    const rows = seedPreviewRows(
      [{ agent: "alpha", seeded: 3, unchanged: 1 }],
      [
        { agent: "alpha", counts: { applied: 4 } },
        { agent: "beta", counts: { removed: 2 } },
      ],
    );
    expect(rows.map((r) => r.agent)).toEqual(["alpha", "beta"]);
    expect(rows[0]!).toEqual({ agent: "alpha", seeded: 3, unchanged: 1, states: { applied: 4 } });
    expect(rows[1]!).toEqual({ agent: "beta", seeded: 0, unchanged: 0, states: { removed: 2 } });
  });
});
