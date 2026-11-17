import { describe, expect, it } from "vitest";
import type { AeonEvent } from "../../lib/protocol";
import { mergeEvents, statusFromEvents, stepsWithTurns, withoutLegacyDetail } from "./journal";

const ev = (seq: number, kind: string, payload: Record<string, unknown> = {}): AeonEvent => ({ subscriptionId: "s", seq, kind, at: "2026-11-15T10:00:00Z", payload } as AeonEvent);

describe("the journal helpers", () => {
  it("joins windows by seq, once each, oldest first", () => {
    const live = [ev(5, "a"), ev(6, "b")];
    const merged = mergeEvents(live, [{ seq: 4, kind: "z", at: "t" }, { seq: 5, kind: "a", at: "t" }, { seq: 7, kind: "c", at: "t" }]);
    expect(merged.map((e) => e.seq)).toEqual([4, 5, 6, 7]);
  });

  it("knows which steps the events carry whole turns for", () => {
    const covered = stepsWithTurns([ev(1, "step.tool.start", { stepId: 1 }), ev(2, "step.turn.completed", { stepId: 2 }), ev(3, "model.delta", { stepId: 3 })]);
    expect([...covered].sort()).toEqual([2, 3]);
  });

  it("leaves a legacy step's calls to the transcript but keeps everything of a covered step", () => {
    const events = [ev(1, "step.tool.start", { stepId: 1 }), ev(2, "step.tool.start", { stepId: 2 }), ev(3, "step.done", { stepId: 1 }), ev(4, "loop.advanced", { loopId: "l" })];
    const kept = withoutLegacyDetail(events, new Set([2])).map((e) => e.seq);
    expect(kept).toEqual([2, 3, 4]);
  });

  describe("the loop's true status", () => {
    const asked = ev(10, "question.asked", { id: 3, stepId: 1 });
    it("stays waiting while a question is owed", () => {
      expect(statusFromEvents("waiting", [asked])).toBe("waiting");
      expect(statusFromEvents("waiting", [asked, ev(11, "step.tool.start", { stepId: 2 })])).toBe("waiting"); // another agent working does not answer it
    });
    it("is running once every question is answered and the agent acted since", () => {
      expect(statusFromEvents("waiting", [asked, ev(11, "question.answered", { questionId: 3 }), ev(12, "step.tool.start", { stepId: 1 })])).toBe("running");
    });
    it("is still waiting right after the answer, before the agent acts", () => {
      expect(statusFromEvents("waiting", [asked, ev(11, "question.answered", { questionId: 3 })])).toBe("waiting");
    });
    it("leaves any other status alone", () => {
      expect(statusFromEvents("blocked", [asked, ev(12, "step.tool.start")])).toBe("blocked");
    });
  });
});

describe("appendLive", () => {
  const delta = (seq: number, text: string, step = 1, tool?: string) => ({ subscriptionId: "s", seq, kind: "model.delta", at: "t", payload: { stepId: step, iteration: 1, text, ...(tool ? { tool } : {}) } }) as never;
  const other = (seq: number, kind: string) => ({ subscriptionId: "s", seq, kind, at: "t", payload: { stepId: 1 } }) as never;

  it("folds a run of chunks into one event that keeps its first seq", async () => {
    const { appendLive } = await import("./journal");
    const out = appendLive([], [delta(1, "He"), delta(2, "llo"), delta(3, " world")], 100);
    expect(out).toHaveLength(1);
    expect(out[0]!.seq).toBe(1);
    expect((out[0]!.payload as { text: string }).text).toBe("Hello world");
  });

  it("starts a new event when the run changes, ignores repeats, and stays bounded", async () => {
    const { appendLive } = await import("./journal");
    const out = appendLive([delta(1, "a")], [delta(1, "a"), delta(2, "think", 1, "thinking"), other(3, "step.tool.start"), delta(4, "b")], 100);
    expect(out.map((e) => e.seq)).toEqual([1, 2, 3, 4]);
    expect(appendLive(out, [delta(5, "c")], 2).map((e) => e.seq)).toEqual([3, 4]);
  });
});

describe("a loop that was answered is running", () => {
  it("reads loop.resumed as the end of the wait, whatever else has or has not happened since", () => {
    const asked = ev(10, "question.asked", { id: 3, stepId: 1 });
    expect(statusFromEvents("waiting", [asked])).toBe("waiting");
    expect(statusFromEvents("waiting", [asked, ev(11, "loop.resumed", { loopId: "L1", reason: "answered" })])).toBe("running");
  });
});
