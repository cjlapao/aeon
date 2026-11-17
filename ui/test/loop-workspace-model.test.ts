import { describe, expect, it } from "vitest";
import { chatItemsFromEvents } from "../src/components/agent-chat";
import {
  agentActivity, fileChanges, filesInScope, footprint, scopeToLoop, stepTelemetry, workflowFromLoop,
} from "../src/features/loop-workspace/workspaceModel";
import { ce, derive, events, loop } from "./loop-fixtures";
import type { AeonEvent } from "../src/lib/protocol";

// The loop page's pure derivations. Task 2 starts with the scoping rule; later
// tasks extend this file (workflow, telemetry, files, footprint).

let seq = 0;
const ev = (kind: string, payload: Record<string, unknown>): AeonEvent =>
  ({ subscriptionId: "s", seq: ++seq, kind, at: "2026-10-25T10:00:00Z", payload });

describe("scopeToLoop", () => {
  it("keeps the loop's own events and every event of a step it dispatched, and drops the rest", () => {
    const events = [
      ev("loop.started", { loopId: "loop-1" }),
      ev("loop.started", { loopId: "loop-2" }),
      ev("loop.step.dispatched", { loopId: "loop-1", stepId: 12, agent: "surveyor" }),
      ev("step.tool.start", { stepId: 12, tool: "read" }),
      ev("step.tool.start", { stepId: 99, tool: "bash" }),
    ];
    expect(scopeToLoop(events, "loop-1").map((e) => [e.kind, (e.payload as { stepId?: number }).stepId])).toEqual([
      ["loop.started", undefined], ["loop.step.dispatched", 12], ["step.tool.start", 12],
    ]);
  });

  it("also keeps steps the loop snapshot names, even when their dispatch event aged out of the ring", () => {
    const events = [ev("step.tool.end", { stepId: 7, tool: "read" }), ev("step.tool.end", { stepId: 8, tool: "read" })];
    expect(scopeToLoop(events, "loop-1", [7]).map((e) => (e.payload as { stepId: number }).stepId)).toEqual([7]);
  });
});

describe("workflowFromLoop", () => {
  it("shows the agents on a step, in the step's sub-steps, followed by its tool calls", () => {
    const { items, session } = derive();
    const survey = workflowFromLoop(loop(), session, items).steps.find((x) => x.id === "survey")!;
    expect(String(survey.meta)).toContain("2 agents");
    expect(survey.badge).toBe("RUNNING");
    expect(survey.subSteps!.map((x) => x.label)).toEqual(["surveyor", "linter", "read: go.mod", "bash: gofmt", "write_file: SURVEY.md"]);
    expect(survey.subSteps!.find((x) => x.label === "write_file: SURVEY.md")!.status).toBe("done");
  });

  it("draws a failed tool call as failed, not as paused waiting for help", () => {
    const { session } = derive();
    const failed = { id: "t-bad", kind: "tool", agent: "surveyor", tool: "search", summary: "q", toolUseId: "bad", stepId: session.runs.find((r) => r.agent === "surveyor")!.stepId, status: "error" } as never;
    const sub = workflowFromLoop(loop(), session, [failed]).steps.find((x) => x.id === "survey")!.subSteps!.find((x) => String(x.label).startsWith("search"))!;
    expect(sub.status).toBe("failed");
  });

  it("labels a gate lane Gate, and a gate the loop is waiting at reads as paused, not broken", () => {
    const { items, session } = derive();
    const waiting = workflowFromLoop(loop({ status: "waiting", position: { lane: "approve" }, gateAnswers: ["approve", "reject"] }), session, items);
    const gate = waiting.steps.find((x) => x.id === "approve")!;
    expect(gate.badge).toBe("Gate");
    expect(gate.status).toBe("blocked");
    expect(waiting.activeStepId).toBe("approve");
  });

  it("keeps a lane with no agent runs exactly as the existing adapter draws it", () => {
    const { items, session } = derive();
    const summarise = workflowFromLoop(loop(), session, items).steps.find((x) => x.id === "summarise")!;
    expect(summarise.status).toBe("not_started");
    expect(String(summarise.meta)).not.toContain("agent");
    // A step that has not started has no cost line to show, let alone "unknown — …".
    expect(String(summarise.meta ?? "")).not.toMatch(/cost|unknown/i);
  });
});

describe("agentActivity", () => {
  it("is the agent's latest thing, thinking by its first line and a tool by its call", () => {
    const { items } = derive();
    expect(agentActivity(items, "linter")).toBe("bash gofmt");
    expect(agentActivity(items, "surveyor")).toBe("write_file SURVEY.md");
    const thinkingLast = chatItemsFromEvents([...events(), ce("model.delta", { stepId: 11, agent: "surveyor", tool: "thinking", text: "Checking go.sum first.\nmore" })]);
    expect(agentActivity(thinkingLast, "surveyor")).toBe("Checking go.sum first.");
    expect(agentActivity(items, "nobody")).toBeUndefined();
  });
});

describe("stepTelemetry", () => {
  it("summarises one lane: status, cost, time, agents, produces, tools, files and the gate that follows", () => {
    const { items, session } = derive();
    const t = stepTelemetry(loop(), session, items, "survey");
    expect(t.status).toBe("running");
    expect(t.cost).toEqual({ state: "known", value: 0.0011 });
    expect(t.elapsedMs).toBe(14000);
    expect(t.activeAgents).toBe(2);
    expect(t.produces).toBe("SURVEY.md");
    expect(t.tools.map((x) => [x.tool, x.summary])).toEqual([["read", "go.mod"], ["bash", "gofmt"], ["write_file", "SURVEY.md"]]);
    expect(t.filesChanged).toEqual([{ path: "SURVEY.md", linesAdded: 18, linesRemoved: undefined }]);
    expect(t.nextGate).toEqual({ lane: "approve", prompt: "You'll be asked to review and approve SURVEY.md before step summarise is initiated." });
  });

  it("marks a call parked on an approval as gated, and has no next gate when none follows", () => {
    const parked = [...events(),
      ce("step.tool.start", { stepId: 11, agent: "surveyor", tool: "bash", summary: "go vet", toolUseId: "t9" }),
      ce("step.tool.end", { stepId: 11, agent: "surveyor", tool: "bash", toolUseId: "t9", result: "suspended: awaiting a permission answer" }),
      ce("question.asked", { id: 5, stepId: 11, agent: "surveyor", toolUseId: "t9", kind: "permission", body: "Allow?", options: ["deny", "approve"] })];
    const { items, session } = derive(parked);
    const t = stepTelemetry(loop(), session, items, "survey");
    expect(t.tools.at(-1)).toMatchObject({ tool: "bash", gated: true, status: "awaiting" });
    expect(stepTelemetry(loop(), session, items, "summarise").nextGate).toBeUndefined();
  });

  it("does not invent a cost or a time it was not given", () => {
    const { items, session } = derive();
    const bare = loop({ lanes: [{ id: "survey", status: "running", structure: "direct", steps: [{ id: "survey", kind: "model-step", status: "running", stepId: 11 }] }] });
    const t = stepTelemetry(bare, session, items, "survey");
    expect(t.cost?.state).not.toBe("known");
    expect(t.elapsedMs).toBeUndefined();
  });

  it("measures a running step's elapsed time from its first event when the daemon has not yet", () => {
    const { items, session } = derive();
    const bare = loop({ lanes: [{ id: "survey", status: "running", structure: "direct", steps: [{ id: "survey", kind: "model-step", status: "running", stepId: 11 }] }] });
    const first = Date.parse(items.find((i) => i.kind === "tool")!.at!);
    expect(stepTelemetry(bare, session, items, "survey", first + 9000).elapsedMs).toBe(9000);
  });
});

describe("filesInScope", () => {
  it("reads → read, a fresh write → created, an overwrite or a read-then-write → modified", () => {
    const evs = [...events(),
      ce("step.tool.start", { stepId: 11, agent: "surveyor", tool: "read", summary: "README.md", toolUseId: "r2" }),
      ce("step.tool.end", { stepId: 11, agent: "surveyor", tool: "read", summary: "README.md", toolUseId: "r2", result: "read" }),
      ce("step.tool.start", { stepId: 11, agent: "surveyor", tool: "write_file", summary: "README.md", toolUseId: "w2" }),
      ce("step.tool.end", { stepId: 11, agent: "surveyor", tool: "write_file", toolUseId: "w2", path: "README.md", linesAdded: 3, linesRemoved: 1, result: "wrote" }),
      ce("step.tool.start", { stepId: 11, agent: "surveyor", tool: "write_file", summary: "NOTES.md", toolUseId: "w3" }),
      ce("step.tool.end", { stepId: 11, agent: "surveyor", tool: "write_file", toolUseId: "w3", path: "NOTES.md", linesAdded: 4, result: "wrote" })];
    expect(Object.fromEntries(filesInScope(derive(evs).items))).toEqual({
      "go.mod": "read", "SURVEY.md": "created", "README.md": "modified", "NOTES.md": "created",
    });
  });
});

describe("footprint", () => {
  it("is cost per step and in total, and says when a total is only a floor", () => {
    const f = footprint(loop());
    expect(f.perStep.map((x) => [x.lane, x.costUsd])).toEqual([["start", 0.0001], ["survey", 0.0011], ["approve", undefined], ["summarise", undefined]]);
    expect(f.totalUsd).toBeCloseTo(0.0012, 6);
    expect(f.partial).toBe(false);
    const partial = footprint(loop({ lanes: [{ id: "a", status: "ok", structure: "direct", steps: [
      { id: "a", kind: "model-step", status: "ok", cost: { state: "known", usd: 0.002 } },
      { id: "b", kind: "model-step", status: "ok", cost: { state: "unknown" } }] }] }));
    expect(partial.totalUsd).toBe(0.002);
    expect(partial.partial).toBe(true);
  });
});

describe("fileChanges", () => {
  it("is every file the loop wrote, with its lines summed across writes", () => {
    const evs = [...events(),
      ce("step.tool.start", { stepId: 11, agent: "surveyor", tool: "write_file", summary: "SURVEY.md", toolUseId: "w9" }),
      ce("step.tool.end", { stepId: 11, agent: "surveyor", tool: "write_file", toolUseId: "w9", path: "SURVEY.md", linesAdded: 1, linesRemoved: 1, result: "wrote" })];
    expect(fileChanges(derive(evs).items)).toEqual([{ path: "SURVEY.md", linesAdded: 19, linesRemoved: 1 }]);
  });

  it("is empty when nothing was written", () => {
    expect(fileChanges([])).toEqual([]);
  });
});
