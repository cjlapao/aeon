import { afterEach, describe, expect, it } from "vitest";
import { act, cleanup, fireEvent, render, screen, waitFor } from "@testing-library/react";
import { AgentChat, chatItemsFromEvents, type ChatEventLike } from "../src/components/agent-chat";
import { useLoopWorkspace } from "../src/features/loop-workspace/useLoopWorkspace";
import { useTodoPanel } from "../src/features/loop-workspace/todo/useTodoPanel";
import type { LiveState } from "../src/lib/live";
import { installFakeDaemon, resetFakeDaemon } from "./helpers";

// S75 T6: the loop's todo list is the chat's own To-dos panel (the `taskList`
// slot above the composer), loaded when the loop opens and followed live.

let seq = 0;
const ev = (kind: string, payload: Record<string, unknown>): ChatEventLike =>
  ({ seq: ++seq, kind, at: new Date(Date.UTC(2026, 10, 7, 11, 0, seq)).toISOString(), payload });
const at = "2026-11-07T11:00:00Z";
const current = { state: "current", endpoint: "/s", daemonVersion: "1", protocolVersion: 2, generation: 1 } as const satisfies LiveState;
const list = (rows: [string, string, string][]) => rows.map(([id, content, status]) => ({ id, content, status }));

afterEach(() => { cleanup(); resetFakeDaemon(); });

describe("the reminder", () => {
  it("draws as a neutral divider, never as a person's message", () => {
    const items = chatItemsFromEvents([ev("step.todo.nudged", { stepId: 27, reason: "completion", summary: "scope,write" })]);
    expect(items[0]).toMatchObject({ kind: "divider", tone: "neutral" });
    expect((items[0] as { text: string }).text).toContain("2 todo item(s) open");
  });
});

function Probe() {
  const ws = useLoopWorkspace("loop-1", current);
  const panel = useTodoPanel("loop-1", ws.todos, true, ws.setTodos);
  return <div>
    <span data-testid="has-todos">{ws.todos ? `${ws.todos.items.length}:${String(ws.todos.active)}` : "none"}</span>
    <AgentChat items={[]} readOnly={false} taskList={panel} composer={{ onSend: () => {} } as never} />
  </div>;
}

const loopShow = { id: "loop-1", slug: "chat-1", root: "/repo", status: "waiting", blueprint: "chat", lanes: [], position: { lane: "" } };
const baseResponses = { "loop.show": loopShow, subscribe: { subscriptionId: "s1", from: 0 }, "loop.transcript": { messages: [], hasOlder: false, hasNewer: false } };

describe("the To-dos panel", () => {
  it("is the chat's own task panel, filled from loop.todos before any event", async () => {
    installFakeDaemon({ responses: { ...baseResponses, "loop.todos": {
      active: true, revision: 2, items: list([["a", "write it", "in_progress"], ["b", "ship it", "pending"], ["c", "read it", "done"]]),
      counts: { pending: 1, inProgress: 1, done: 1 } } } });
    render(<Probe />);
    const panel = await screen.findByRole("region", { name: "To-dos" });
    expect(panel.textContent).toContain("1 in progress · 1 pending");
    const marks = Array.from(panel.querySelectorAll("li")).map((li) => li.getAttribute("data-status"));
    expect(marks).toEqual(["in_progress", "pending", "completed"]);
    expect(screen.getByTestId("agent-chat-panels")).toBeTruthy();
  });

  it("follows todo.updated and rests closed with the closer's name", async () => {
    const daemon = installFakeDaemon({ responses: { ...baseResponses, "loop.todos": {
      active: true, revision: 1, items: list([["a", "x", "pending"]]), counts: { pending: 1, inProgress: 0, done: 0 } } } });
    render(<Probe />);
    await screen.findByRole("region", { name: "To-dos" });
    act(() => daemon.emit({ subscriptionId: "s1", seq: 50, kind: "todo.updated", at, payload: {
      loopId: "loop-1", step: 3, revision: 2, active: false, closedBy: "all-done",
      items: list([["a", "x", "done"]]), counts: { pending: 0, inProgress: 0, done: 1 } } }));
    // Everything done and closed: the bar goes away rather than resting open on a wall of ticks.
    await waitFor(() => expect(screen.queryByRole("region", { name: "To-dos" })).toBeNull());
    expect(screen.queryByTestId("todo-panel-close")).toBeNull();
  });

  it("closes the list through the person's door", async () => {
    const daemon = installFakeDaemon({ responses: { ...baseResponses,
      "loop.todos": { active: true, revision: 1, items: list([["a", "x", "pending"]]), counts: { pending: 1, inProgress: 0, done: 0 } },
      "loop.todos.close": { active: false, revision: 2, items: list([["a", "x", "pending"]]), counts: { pending: 1, inProgress: 0, done: 0 }, closedBy: "person" } } });
    render(<Probe />);
    fireEvent.click(await screen.findByTestId("todo-panel-close"));
    await waitFor(() => expect(daemon.methods()).toContain("loop.todos.close"));
    expect(daemon.paramsOf("loop.todos.close")).toEqual({ id: "loop-1" });
    await waitFor(() => expect(screen.getByTestId("chat-task-note").textContent).toBe("closed · person"));
  });

  it("is absent, not empty, on a daemon without the method", async () => {
    installFakeDaemon({ responses: baseResponses });
    render(<Probe />);
    await waitFor(() => expect(screen.getByTestId("has-todos").textContent).toBe("none"));
    expect(screen.queryByRole("region", { name: "To-dos" })).toBeNull();
  });
});

describe("the item being worked on", () => {
  it("shows a spinner on the in-progress row and not on the others", async () => {
    installFakeDaemon({ responses: { ...baseResponses, "loop.todos": {
      active: true, revision: 1, items: list([["a", "doing it", "in_progress"], ["b", "later", "pending"]]),
      counts: { pending: 1, inProgress: 1, done: 0 } } } });
    render(<Probe />);
    const panel = await screen.findByRole("region", { name: "To-dos" });
    expect(panel.querySelectorAll('[data-testid="chat-task-spinner"]')).toHaveLength(1);
    expect(panel.querySelector('li[data-status="in_progress"] [data-testid="chat-task-spinner"]')).toBeTruthy();
  });
});
