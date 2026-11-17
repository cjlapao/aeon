import { describe, expect, it } from "vitest";
import { flowGraphNodes } from "./FlowGraph";
import type { FlowModel } from "./flowModel";

const flow: FlowModel = {
  id: "demo", schemaVersion: 1, version: 1, questions: { go: { prompt: "Go?", answers: ["approve", "reject"] } },
  lanes: [
    { id: "plan", step: { kind: "model-step", agent: "planner", produces: ["PLAN.md"] } },
    { id: "gate", step: { kind: "gate", question: "go" }, onReject: { goto: "plan", counter: "rounds", max: 2, then: "escalate" } },
    { id: "build", iterate: "tasks", ladder: [{ id: "implement", kind: "model-step", agent: "dev" }, { id: "check", kind: "model-step", agent: "qa", onReject: { goto: "implement", max: 1 } }] },
  ],
};

describe("the flow as a graph", () => {
  const nodes = flowGraphNodes(flow, { type: "lane", laneId: "gate" }, {});

  it("is one card per lane in run order, the picked one active", () => {
    expect(nodes.map((n) => n.id)).toEqual(["plan", "gate", "build"]);
    expect(nodes.map((n) => !!n.active)).toEqual([false, true, false]);
  });

  it("draws a rejection as a branch to its target, with the bound in the caption", () => {
    expect(nodes[1]!.failure).toEqual({ to: "plan", label: "to plan · up to 2 times · then escalate" });
    expect(nodes[0]!.failure).toBeUndefined();
  });

  it("walks a ladder's rungs as a chain and points a rung's rejection at its sibling", () => {
    const ladder = nodes[2]!;
    expect(ladder.childLayout).toBe("chain");
    expect(ladder.rows!.flat().map((r) => r.id)).toEqual(["build/implement", "build/check"]);
    expect(ladder.rows![1]![0]!.failure?.to).toBe("build/implement");
  });

  it("marks a lane the daemon calls invalid", () => {
    const bad = flowGraphNodes(flow, { type: "settings" }, { plan: [{ state: "invalid" } as never] });
    expect(bad[0]!.tone).toBe("rose");
  });
});
