import { describe, expect, it } from "vitest";
import type { ChatEventLike } from "../src/components/agent-chat";
import { syntheticLoop } from "../src/features/test-bench/syntheticLoop";

// The bench has no daemon to answer `loop.show`, so the full-page mode builds
// the loop's snapshot from the events a scenario has played so far.

const lanes = [{ id: "survey", kind: "model-step" as const }, { id: "approve", kind: "gate" as const }, { id: "summarise", kind: "model-step" as const }];
let n = 0;
const ev = (kind: string, payload: Record<string, unknown>): ChatEventLike => ({ seq: ++n, kind, at: "2026-10-25T10:00:00Z", payload });
const statuses = (evs: ChatEventLike[]) => Object.fromEntries(syntheticLoop(lanes, evs).lanes.map((l) => [l.id, l.status]));

describe("syntheticLoop", () => {
  it("starts with nothing run and the first lane current", () => {
    const loop = syntheticLoop(lanes, []);
    expect(statuses([])).toEqual({ survey: "not-started", approve: "not-started", summarise: "not-started" });
    expect(loop.position.lane).toBe("survey");
    expect(loop.blueprint).toBe("two-step");
  });

  it("a dispatched lane is running and carries its step id, so the page can attribute calls to it", () => {
    const evs = [ev("loop.step.dispatched", { lane: "survey", step: "survey", stepId: 71, agent: "surveyor" })];
    const loop = syntheticLoop(lanes, evs);
    expect(statuses(evs).survey).toBe("running");
    expect(loop.status).toBe("running");
    expect(loop.lanes[0]!.steps![0]).toMatchObject({ id: "survey", kind: "model-step", stepId: 71, status: "running" });
  });

  it("a call parked on a permission question makes the loop wait at that lane, until it is answered", () => {
    const dispatched = ev("loop.step.dispatched", { lane: "survey", step: "survey", stepId: 71, agent: "surveyor" });
    const asked = ev("question.asked", { id: 601, stepId: 71, kind: "permission", body: "Allow?", toolUseId: "t" });
    const waiting = syntheticLoop(lanes, [dispatched, asked]);
    expect(waiting.status).toBe("waiting");
    expect(waiting.position.lane).toBe("survey");
    const answered = syntheticLoop(lanes, [dispatched, asked, ev("question.answered", { questionId: 601, answer: "approve" })]);
    expect(answered.status).toBe("running");
  });

  it("a gate waits with its answers, and an answered gate and an advanced lane are done", () => {
    const evs = [
      ev("loop.step.dispatched", { lane: "survey", step: "survey", stepId: 71 }),
      ev("loop.advanced", { lane: "survey", outcome: "ok" }),
      ev("loop.waiting", { loopId: "l", questionId: 90, prompt: "Approve?", answers: ["approve", "reject"], lane: "approve", step: "approve" }),
    ];
    const waiting = syntheticLoop(lanes, evs);
    expect(waiting.status).toBe("waiting");
    expect(waiting.position.lane).toBe("approve");
    expect(waiting.gateAnswers).toEqual(["approve", "reject"]);
    expect(statuses(evs)).toMatchObject({ survey: "ok", approve: "waiting" });
    const after = [...evs, ev("loop.advanced", { lane: "approve", questionId: 90, answer: "approve", outcome: "approve" })];
    expect(statuses(after)).toMatchObject({ survey: "ok", approve: "ok" });
  });

  it("finished and blocked loops say so", () => {
    expect(syntheticLoop(lanes, [ev("loop.finished", { status: "done" })]).status).toBe("done");
    expect(syntheticLoop(lanes, [ev("loop.blocked", { reason: { kind: "step-failed", detail: "x" } })]).status).toBe("blocked");
  });
});
