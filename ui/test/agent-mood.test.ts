import { describe, expect, it } from "vitest";
import { agentMood, type ChatItem } from "../src/components/agent-chat";

// S76: the mood an agent's avatar wears, read off what the chat knows.

const run = (state: "running" | "stopped" | "done") => ({ agent: "brainstormer", stepId: 1, state });
const session = (state: "running" | "stopped" | "done", loopStatus: "unknown" | "running" | "waiting" | "finished" | "blocked" | "idle" = "running") =>
  ({ runs: [run(state)], loopStatus });
const tool = (extra: Record<string, unknown> = {}): ChatItem =>
  ({ id: "t", agent: "brainstormer", kind: "tool", tool: "read", status: "running", ...extra }) as ChatItem;

describe("agentMood", () => {
  it("is idle with no run at all", () => {
    expect(agentMood("brainstormer", [], { runs: [], loopStatus: "unknown" })).toBe("idle");
  });
  it("works while a step runs, and thinks while the last thing it did was thinking", () => {
    expect(agentMood("brainstormer", [tool()], session("running"))).toBe("working");
    const thinking = { id: "k", agent: "brainstormer", kind: "thinking", text: "hm", streaming: true } as ChatItem;
    expect(agentMood("brainstormer", [tool(), thinking], session("running"))).toBe("thinking");
  });
  it("is stuck while a question it asked is unanswered, and works again once answered", () => {
    const asked = { id: "q", agent: "brainstormer", kind: "question", questions: [{ id: "1", prompt: "which?", kind: "clarify" }] } as ChatItem;
    expect(agentMood("brainstormer", [asked], session("done", "waiting"))).toBe("stuck");
    const answered = { ...asked, questions: [{ id: "1", prompt: "which?", kind: "clarify", answer: "A" }] } as ChatItem;
    expect(agentMood("brainstormer", [answered], session("running"))).toBe("working");
  });
  it("is stuck on an awaiting approval and on an open gate, and when the loop is blocked", () => {
    const approval = tool({ status: "awaiting", approval: { id: "9", prompt: "run it?", kind: "permission" } });
    expect(agentMood("brainstormer", [approval], session("running"))).toBe("stuck");
    const gate = { id: "g", kind: "gate", title: "ok?", options: ["yes"], state: "awaiting" } as ChatItem;
    expect(agentMood("brainstormer", [gate], session("done", "waiting"))).toBe("stuck");
    expect(agentMood("brainstormer", [], session("done", "blocked"))).toBe("stuck");
  });
  it("is idle at rest, done when its loop finished", () => {
    expect(agentMood("brainstormer", [], session("done", "idle"))).toBe("idle");
    expect(agentMood("brainstormer", [], session("stopped", "waiting"))).toBe("stuck");
    expect(agentMood("brainstormer", [], session("done", "finished"))).toBe("done");
  });

  it("only the agent the loop is parked on waits; earlier agents rest, and a running one keeps working", () => {
    const runs = [
      { agent: "a", stepId: 1, state: "done" as const },
      { agent: "b", stepId: 2, state: "done" as const },
      { agent: "planner", stepId: 3, state: "done" as const },
    ];
    expect(agentMood("planner", [], { runs, loopStatus: "waiting" })).toBe("stuck");
    expect(agentMood("a", [], { runs, loopStatus: "waiting" })).toBe("idle");
    expect(agentMood("b", [], { runs, loopStatus: "waiting" })).toBe("idle");
    const stopped = [{ agent: "a", stepId: 1, state: "done" as const }, { agent: "b", stepId: 2, state: "stopped" as const }, { agent: "c", stepId: 3, state: "running" as const }];
    expect(agentMood("b", [], { runs: stopped, loopStatus: "waiting" })).toBe("stuck");
    expect(agentMood("c", [], { runs: stopped, loopStatus: "waiting" })).toBe("working");
  });
});
