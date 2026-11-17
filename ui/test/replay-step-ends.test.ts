import { describe, expect, it } from "vitest";
import type { ChatItem } from "../src/components/agent-chat";
import { withStepEnds } from "../src/features/loop-workspace/replayStepEnds";
import type { LoopDetail } from "../src/features/loops/pages/LoopScreen";

const loop = (steps: object[]) => ({ lanes: [{ id: "capture", status: "done", steps }] }) as unknown as LoopDetail;
const div = (id: string, at: string, highlights?: string[]): ChatItem => ({ id, kind: "divider", tone: "orchestrator", text: "d", at, highlights }) as ChatItem;
const text = (id: string, at: string): ChatItem => ({ id, kind: "text", agent: "brainstormer", text: "t", at }) as ChatItem;

describe("withStepEnds", () => {
  const history = [div("dispatch:7", "2026-11-07T14:00:00Z", ["brainstormer"]), text("a", "2026-11-07T14:00:05Z"), div("dispatch:8", "2026-11-07T14:01:00Z", ["linter"]), text("b", "2026-11-07T14:01:05Z")];
  const l = loop([
    { id: "s1", kind: "agent", status: "done", stepId: 7, outcome: "ok", endedAt: "2026-11-07T14:00:30Z" },
    { id: "s2", kind: "agent", status: "failed", stepId: 8, outcome: "failed", reason: "no go.mod", endedAt: "2026-11-07T14:02:00Z" },
  ]);

  it("closes each ended step where it ended, with the live wording and tone", () => {
    const out = withStepEnds(history, l, new Set());
    expect(out.map((i) => i.id)).toEqual(["dispatch:7", "a", "stepend:7", "dispatch:8", "b", "stepend:8"]);
    expect(out[2]).toMatchObject({ tone: "success", text: "brainstormer finished · ok" });
    expect(out[5]).toMatchObject({ tone: "danger", text: "linter finished · failed · no go.mod" });
  });

  it("draws nothing for a step the live stream covers or whose turns were not loaded", () => {
    expect(withStepEnds(history, l, new Set([7, 8]))).toEqual(history);
    expect(withStepEnds(history, loop([{ id: "s", kind: "agent", status: "done", stepId: 99, endedAt: "2026-11-07T14:00:30Z" }]), new Set())).toEqual(history);
  });
});
