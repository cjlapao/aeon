import { describe, expect, it } from "vitest";
import type { ChatItem } from "../src/components/agent-chat";
import { withPastQuestions } from "../src/features/loop-workspace/replayQuestions";

const call = (tool: string, toolUseId: string): ChatItem =>
  ({ id: toolUseId, kind: "tool", tool, toolUseId, stepId: 4, agent: "brainstormer", status: "done", at: "2026-11-07T14:00:00Z" }) as ChatItem;

describe("withPastQuestions", () => {
  const past = [
    { id: 1, stepId: 4, kind: "clarify", prompt: "Which?", answers: ["A", "B"], answer: "A", toolUseId: "c1" },
    { id: 2, stepId: 4, kind: "clarify", prompt: "How?", answers: [], answer: "fast", toolUseId: "c1" },
    { id: 3, stepId: 4, kind: "permission", prompt: "Run it?", answers: [], answer: "yes", toolUseId: "c2" },
  ];

  it("draws a question call as its answered card, every question of the call in one block", () => {
    const out = withPastQuestions([call("glob", "g"), call("question", "c1")], past);
    expect(out[0]).toMatchObject({ kind: "tool", tool: "glob" });
    expect(out[1]).toMatchObject({ kind: "question", agent: "brainstormer", at: "2026-11-07T14:00:00Z" });
    const qs = (out[1] as Extract<ChatItem, { kind: "question" }>).questions;
    expect(qs.map((q) => q.answer)).toEqual(["A", "fast"]);
    expect(qs[0]?.options).toEqual(["A", "B"]);
  });

  it("keeps a permission call's row and gives it the decision", () => {
    const out = withPastQuestions([call("bash", "c2")], past);
    expect(out[0]).toMatchObject({ kind: "tool", tool: "bash", approval: { answer: "yes", kind: "permission" } });
  });

  it("leaves a call the store has no row for exactly as saved", () => {
    const rows = [call("question", "unknown")];
    expect(withPastQuestions(rows, past)).toEqual(rows);
    expect(withPastQuestions(rows, [])).toBe(rows);
  });
});
