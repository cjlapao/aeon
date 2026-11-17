import { chatItemsFromEvents, deriveSession, type ChatEventLike } from "../src/components/agent-chat";
import type { LoopDetail } from "../src/features/loops/pages/LoopScreen";

// A loop in flight, shared by the loop page's tests: survey running with two
// agents, a gate next, then summarise.

// --- a loop in flight: survey running with two agents, a gate next, then summarise -------------

export const loop = (over: Partial<LoopDetail> = {}): LoopDetail => ({
  id: "loop-1", slug: "two-step", blueprint: "two-step", goal: "Survey", root: "/repo", status: "running",
  position: { lane: "survey" },
  lanes: [
    { id: "start", status: "ok", structure: "direct", steps: [{ id: "start", kind: "model-step", status: "ok", stepId: 10, wallMs: 400, cost: { state: "known", usd: 0.0001 } }] },
    { id: "survey", status: "running", structure: "direct", produces: ["SURVEY.md"],
      steps: [{ id: "survey", kind: "model-step", status: "running", stepId: 11, wallMs: 14000, cost: { state: "known", usd: 0.0011 } }] },
    { id: "approve", status: "not-started", structure: "direct", steps: [{ id: "approve", kind: "gate", status: "not-started" }] },
    { id: "summarise", status: "not-started", structure: "direct", steps: [{ id: "summarise", kind: "model-step", status: "not-started" }] },
  ],
  ...over,
});

let n = 0;
export const ce = (kind: string, payload: Record<string, unknown>, at?: string): ChatEventLike => {
  n += 1;
  return { seq: n, kind, at: at ?? new Date(Date.UTC(2026, 9, 25, 10, 0, 0) + n * 1000).toISOString(), payload };
};

export const events = (): ChatEventLike[] => [
  ce("loop.step.dispatched", { loopId: "loop-1", lane: "survey", step: "survey", stepId: 11, agent: "surveyor" }),
  ce("loop.step.dispatched", { loopId: "loop-1", lane: "survey", step: "survey", stepId: 12, agent: "linter" }),
  ce("model.delta", { stepId: 11, agent: "surveyor", tool: "thinking", text: "Weighing two approaches.\nSecond line." }),
  ce("step.tool.start", { stepId: 11, agent: "surveyor", tool: "read", summary: "go.mod", toolUseId: "t1" }),
  ce("step.tool.end", { stepId: 11, agent: "surveyor", tool: "read", summary: "go.mod", toolUseId: "t1", result: "read go.mod" }),
  ce("step.tool.start", { stepId: 12, agent: "linter", tool: "bash", summary: "gofmt", command: "gofmt -l .", toolUseId: "t2" }),
  ce("step.tool.end", { stepId: 12, agent: "linter", tool: "bash", summary: "gofmt", toolUseId: "t2", result: "ok", output: "" }),
  ce("step.tool.start", { stepId: 11, agent: "surveyor", tool: "write_file", summary: "SURVEY.md", toolUseId: "t3" }),
  ce("step.tool.end", { stepId: 11, agent: "surveyor", tool: "write_file", summary: "SURVEY.md", toolUseId: "t3", path: "SURVEY.md", linesAdded: 18, result: "wrote" }),
];
export const derive = (evs = events()) => ({ items: chatItemsFromEvents(evs), session: deriveSession(evs) });

