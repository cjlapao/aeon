import { describe, expect, it } from "vitest";
import { laneAggregate, summaryLine, type StepSummary } from "./stepSummaries";
import { withLoopRuns } from "./sessionFromLoop";
import { deriveSession } from "../../components/agent-chat";
import type { LoopDetail } from "../loops/pages/LoopScreen";

const sum = (o: Partial<StepSummary>): StepSummary => ({ stepId: 1, loopId: "L", lane: "plan", agent: "a", status: "done", turns: 1, toolCalls: 2, toolErrors: 0,
  inputTokens: 10, outputTokens: 5, reasoningTokens: 0, wallMs: 100, costState: "known", costUsd: 0.5, firstEvent: 1, lastEvent: 2, ...o });

describe("step summaries", () => {
  it("adds a lane's steps up and keeps the cost state honest", () => {
    const a = laneAggregate([sum({ stepId: 1 }), sum({ stepId: 2, costState: "unknown", costUsd: undefined }), sum({ stepId: 3, lane: "other" })], "plan")!;
    expect(a).toMatchObject({ steps: 2, turns: 2, toolCalls: 4, costUsd: 0.5, costState: "partial" });
    expect(laneAggregate([sum({ costState: "unknown", costUsd: undefined })], "plan")!.costState).toBe("unknown");
    expect(laneAggregate([], "plan")).toBeUndefined();
  });

  it("says the work in one line, with failures", () => {
    expect(summaryLine({ turns: 3, toolCalls: 7, toolErrors: 1 })).toBe("3 turns · 7 calls (1 failed)");
    expect(summaryLine({ turns: 1, toolCalls: 1, toolErrors: 0 })).toBe("1 turn · 1 call");
  });

  it("restores agents the event window never saw, from the summaries", () => {
    const loop = { id: "L", lanes: [] } as unknown as LoopDetail;
    const session = withLoopRuns(deriveSession([]), loop, [sum({ stepId: 4, agent: "investigator", status: "done" }), sum({ stepId: 9, agent: "project-planner", status: "running" })]);
    expect(session.runs.map((r) => [r.agent, r.state])).toEqual([["investigator", "done"], ["project-planner", "running"]]);
  });
});
