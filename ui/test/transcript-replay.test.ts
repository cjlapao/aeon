import { describe, expect, it } from "vitest";
import { chatItemsFromLoopTranscriptPage, type LoopTranscriptMessageLike } from "../src/components/agent-chat";

const msg = (id: number, role: string, createdAt: string, content: LoopTranscriptMessageLike["content"]): LoopTranscriptMessageLike =>
  ({ id, stepId: 7, agent: "brainstormer", phase: "capture", role, createdAt, content });

describe("a replayed transcript keeps what the live chat showed", () => {
  const page = [
    msg(1, "user", "2026-11-07T14:00:00.000Z", [{ kind: "text", text: "improve AGENTS.md" }]),
    msg(2, "assistant", "2026-11-07T14:00:01.000Z", [{ kind: "tool_use", id: "t1", name: "read", input: { path: "a.md" } }]),
    msg(3, "user", "2026-11-07T14:00:03.500Z", [{ kind: "tool_result", toolUseId: "t1", content: "ok" }]),
    msg(4, "user", "2026-11-07T14:00:09.000Z", [{ kind: "text", text: "Reminder: your todo list has 3 open item(s) (a, b, c). Update it, or say why you are leaving them." }]),
  ];
  const items = chatItemsFromLoopTranscriptPage(page);

  it("opens a step with the orchestrator's dispatch divider", () => {
    expect(items[0]).toMatchObject({ kind: "divider", tone: "orchestrator", text: "Orchestrator → step capture · dispatched brainstormer" });
    // The step's brief is the orchestrator's, never "you": it repeated once per agent and sat between their turns.
    expect(items[1]).toMatchObject({ kind: "divider", text: "Brief for brainstormer: improve AGENTS.md", detail: "improve AGENTS.md" });
    expect(items.some((i) => i.kind === "text" && i.agent === "you")).toBe(false);
  });

  it("gives a call the time it took, from its own rows", () => {
    expect(items.find((i) => i.kind === "tool")).toMatchObject({ status: "done", durationMs: 2500 });
  });

  it("draws the runtime's reminder as a reminder, never as the person steering", () => {
    expect(items.some((i) => i.kind === "steer")).toBe(false);
    expect(items.at(-1)).toMatchObject({ kind: "divider", text: "reminder sent while working: 3 todo item(s) open" });
  });
});
