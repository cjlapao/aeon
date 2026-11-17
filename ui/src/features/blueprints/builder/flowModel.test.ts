import { describe, expect, it } from "vitest";
import { addLadderStep, blankLadderLane, blankLane, changeStepKind, describeCondition, diagnosticsByLane, insertLane, laneOfPath, moveLane, removeLadderStep, removeLane, removeQuestion, renameLadderStep, renameLane, renameQuestion, rejectTo, uniqueName, updateStep, validId, FlowModel, judgeMove, judgeRungMove } from "./flowModel";

// ── flowOps.test.ts ─────────────────────────────────────────────
const flow = (): FlowModel => ({
  id: "demo", schemaVersion: 1, version: 1, questions: { go: { prompt: "Go?", answers: ["approve", "reject"] } }, onAbort: "plan",
  lanes: [
    { id: "plan", step: { kind: "model-step", agent: "planner" } },
    { id: "gate", step: { kind: "gate", question: "go" }, onReject: { goto: "plan", counter: "rounds", max: 2, then: "escalate" } },
    { id: "build", iterate: "tasks", rework: "implement", ladder: [{ id: "implement", kind: "model-step", agent: "dev" }, { id: "check", kind: "model-step", agent: "qa", onReject: { goto: "implement", counter: "c", max: 1, then: "block" } }] },
  ],
});

describe("lane edits", () => {
  it("inserts and moves lanes without touching the original", () => {
    const m = flow();
    const inserted = insertLane(m, 1, blankLane("review", "model-step"));
    expect(inserted.lanes.map((l) => l.id)).toEqual(["plan", "review", "gate", "build"]);
    expect(m.lanes).toHaveLength(3);
    expect(moveLane(inserted, 1, 3).lanes.map((l) => l.id)).toEqual(["plan", "gate", "build", "review"]);
  });

  it("removing a lane takes every pointer at it along", () => {
    const next = removeLane(flow(), "plan");
    expect(next.onAbort).toBeUndefined();
    expect(next.lanes.find((l) => l.id === "gate")?.onReject?.goto).toBeUndefined();
    // The loop's bound stays: it said how often, and that is still true of the lane.
    expect(next.lanes.find((l) => l.id === "gate")?.onReject?.max).toBe(2);
  });

  it("renaming a lane keeps every pointer at it pointing at it", () => {
    const next = renameLane(flow(), "plan", "outline");
    expect(next.lanes[0]!.id).toBe("outline");
    expect(next.lanes[1]!.onReject?.goto).toBe("outline");
    expect(next.onAbort).toBe("outline");
  });
});

describe("steps and ladders", () => {
  it("changing a step's kind keeps its id, condition and reject but not the old kind's fields", () => {
    const step = { id: "x", kind: "model-step" as const, agent: "dev", produces: ["a.md"], when: { field: "mode", op: "eq", value: "poc" }, onReject: { then: "block" } };
    const gate = changeStepKind(step, "gate");
    expect(gate).toEqual({ id: "x", kind: "gate", question: "", when: step.when, onReject: { then: "block" } });
    // A code action cannot carry a reject edge (only the verify run may).
    expect(changeStepKind(step, "code-action").onReject).toBeUndefined();
  });

  it("updating a step drops what is emptied", () => {
    const next = updateStep(flow(), "plan", null, { agent: "", produces: [] });
    expect(next.lanes[0]!.step).toEqual({ kind: "model-step" });
  });

  it("adds, renames and removes ladder rungs with the lane's rework and the rungs' rejects following", () => {
    let m = addLadderStep(flow(), "build", 2);
    expect(m.lanes[2]!.ladder!.map((s) => s.id)).toEqual(["implement", "check", "step"]);
    m = renameLadderStep(m, "build", 0, "code");
    expect(m.lanes[2]!.rework).toBe("code");
    expect(m.lanes[2]!.ladder![1]!.onReject?.goto).toBe("code");
    m = removeLadderStep(m, "build", 0);
    expect(m.lanes[2]!.rework).toBeUndefined();
    expect(m.lanes[2]!.ladder![0]!.onReject?.goto).toBeUndefined();
  });

  it("a ladder lane starts with one rung", () => {
    expect(blankLadderLane("build")).toEqual({ id: "build", iterate: "tasks", ladder: [{ id: "step-1", kind: "model-step", agent: "" }] });
  });
});

describe("reject edges", () => {
  it("a loop back gets a bound and a place to go when it runs out; a skip ahead needs neither", () => {
    const m = flow();
    expect(rejectTo(m, "build", "plan")).toEqual({ goto: "plan", counter: "build_retries", max: 2, then: "escalate" });
    expect(rejectTo(m, "plan", "build")).toEqual({ goto: "build" });
  });
  it("no target clears the jump and keeps the rest", () => {
    expect(rejectTo(flow(), "gate", "", { goto: "plan", then: "block" })).toEqual({ then: "block" });
    expect(rejectTo(flow(), "gate", "", { goto: "plan" })).toBeUndefined();
  });
});

describe("questions", () => {
  it("renaming a question keeps the gates that ask it asking it; removing one empties them", () => {
    expect(renameQuestion(flow(), "go", "proceed").lanes[1]!.step!.question).toBe("proceed");
    expect(removeQuestion(flow(), "go").lanes[1]!.step).toEqual({ kind: "gate", question: "" });
  });
});

describe("conditions and diagnostics", () => {
  it("writes a condition as a sentence", () => {
    expect(describeCondition({ all: [{ field: "mode", op: "neq", value: "poc" }, { any: [{ field: "flags.a", op: "eq", value: true }, { not: { field: "tasks.count", op: "gt", value: 3 } }] }] }))
      .toBe("mode is not poc and (flags.a is true or not (tasks.count is above 3))");
    expect(describeCondition({ field: "" })).toBe("");
  });
  it("finds a diagnostic's lane by id or by position", () => {
    const m = flow();
    expect(laneOfPath("lanes[gate].step.question", m)).toBe("gate");
    expect(laneOfPath("lanes[2].id", m)).toBe("build");
    expect(laneOfPath("on_abort", m)).toBeUndefined();
    const { byLane, flow: own } = diagnosticsByLane([
      { state: "invalid", kind: "unknown-agent", path: "lanes[plan].step.agent", message: "x" },
      { state: "invalid", kind: "unknown-abort-handler", path: "on_abort", message: "y" },
      { state: "valid", kind: "ok", path: "lanes[plan]", message: "" },
    ], m);
    expect(Object.keys(byLane)).toEqual(["plan"]);
    expect(own).toHaveLength(1);
  });
});

describe("names", () => {
  it("makes a free name and checks the grammar", () => {
    expect(uniqueName("step", ["step", "step-2"])).toBe("step-3");
    expect(validId("plan-2")).toBe(true);
    expect(validId("Plan")).toBe(false);
  });
});

// ── dropRules.test.ts ───────────────────────────────────────────
const dropFlow = (): FlowModel => ({
  id: "d", schemaVersion: 1, version: 1, questions: {},
  lanes: [
    { id: "plan", step: { kind: "model-step", agent: "a" } },
    { id: "gate", step: { kind: "gate", question: "q" }, onReject: { goto: "build", then: "escalate" } },
    { id: "build", step: { kind: "model-step", agent: "b" } },
  ],
});

describe("where a lane may be dropped", () => {
  it("a move that changes nothing is fine to ask about", () => {
    expect(judgeMove(dropFlow(), 1, 1)).toEqual({ ok: true });
  });
  it("refuses to turn a forward rejection into an unbounded loop", () => {
    const v = judgeMove(dropFlow(), 2, 0); // build goes first: gate -> build now points back
    expect(v.ok).toBe(false);
    expect(!v.ok && v.reason).toContain("“gate” would send the work back to “build”");
  });
  it("allows it once the edge is bounded", () => {
    const m = dropFlow();
    m.lanes[1]!.onReject = { goto: "build", counter: "gate_retries", max: 2, then: "escalate" };
    expect(judgeMove(m, 2, 0)).toEqual({ ok: true });
  });
  it("does not blame a move for a loop that was already unbounded", () => {
    const m = dropFlow();
    m.lanes[0]!.onReject = { goto: "plan", then: "block" };
    expect(judgeMove(m, 0, 1)).toEqual({ ok: true });
  });
});

describe("where a rung may be dropped", () => {
  const ladder = () => [
    { id: "implement", kind: "model-step" as const, agent: "dev" },
    { id: "check", kind: "model-step" as const, agent: "qa", onReject: { goto: "fix", then: "block" } },
    { id: "fix", kind: "model-step" as const, agent: "dev" },
  ];
  it("refuses to turn an on-to rejection into an unbounded loop", () => {
    const v = judgeRungMove(ladder(), 2, 0);
    expect(v.ok).toBe(false);
    expect(!v.ok && v.reason).toContain("“check” would send the item back to “fix”");
  });
  it("allows a move that keeps every rejection forward", () => {
    expect(judgeRungMove(ladder(), 0, 1)).toEqual({ ok: true });
  });
});
