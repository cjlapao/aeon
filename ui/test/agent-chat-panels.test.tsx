import { afterEach, describe, expect, it, vi } from "vitest";
import { cleanup, fireEvent, render, screen, within } from "@testing-library/react";
import { AgentChat, ChatComposer, ChatPanels, type ChatPanel, type ChatPersistentPanel, type ChatTask } from "../src/components/agent-chat";

afterEach(() => { cleanup(); vi.restoreAllMocks(); });
const persistent: ChatPersistentPanel[] = [
  { id: "goal", title: "Ongoing goal", summary: "Build panels", content: "Full goal details" },
  { id: "queued", title: "Queued message", summary: "Run tests", content: "Full queued message" },
  { id: "note", title: "Note", summary: "Check context", content: "Full context" },
];

describe("chat panels", () => {
  it("reserves most of the message stream when the available height shrinks", () => {
    let composerTop = 440;
    vi.spyOn(HTMLElement.prototype, "clientHeight", "get").mockReturnValue(600);
    vi.spyOn(HTMLElement.prototype, "getBoundingClientRect").mockImplementation(function (this: HTMLElement) {
      return { top: this.classList.contains("chat-composer-anchor") ? composerTop : 0,
        bottom: 600, left: 0, right: 800, width: 800, height: 600, x: 0, y: 0, toJSON: () => ({}) };
    });
    render(<AgentChat items={[]} composer={{ onSteer: vi.fn() }} panels={persistent.slice(0, 2)} />);
    const dock = screen.getByTestId("agent-chat-composer").parentElement!;
    const budget = () => Number.parseFloat(dock.style.getPropertyValue("--chat-panels-height"));
    expect(budget()).toBeLessThanOrEqual(600 * 0.35);
    expect(budget()).toBeCloseTo((440 - 16) * 0.45);
    composerTop = 300;
    fireEvent(window, new Event("resize"));
    expect(budget()).toBeCloseTo((300 - 16) * 0.45);
  });

  it("renders nothing without supplied panels", () => {
    const { container } = render(<ChatPanels />);
    expect(container.innerHTML).toBe("");
  });

  it("shows only one temporary picker, selects enabled rows, and dismisses it", () => {
    const onSelect = vi.fn();
    const onPickerDismiss = vi.fn();
    const picker: ChatPanel = { id: "files", title: "Files", items: [
      { id: "readme", label: "README.md", description: "Project guide", onSelect },
      { id: "secret", label: "Unavailable file", disabled: true, onSelect },
    ] };
    const { rerender } = render(<ChatPanels picker={picker} onPickerDismiss={onPickerDismiss} />);
    fireEvent.click(screen.getByRole("button", { name: /README.md/ }));
    fireEvent.click(screen.getByRole("button", { name: "Unavailable file" }));
    expect(onSelect).toHaveBeenCalledTimes(1);
    fireEvent.click(screen.getByRole("button", { name: "Close Files" }));
    expect(onPickerDismiss).toHaveBeenCalledTimes(1);
    rerender(<ChatPanels picker={{ id: "agents", title: "Agents", items: [] }} />);
    expect(screen.queryByRole("region", { name: "Files" })).toBeNull();
    expect(screen.getByText("No items to display.")).toBeTruthy();
    expect(screen.queryByRole("region", { name: "Chat panels" })).toBeNull();
  });

  it("keeps up to two persistent rows compact and expands their full content", () => {
    render(<ChatPanels panels={persistent.slice(0, 2)} />);
    expect(screen.queryByRole("region", { name: "Chat panels" })).toBeNull();
    const toggle = screen.getByRole("button", { name: /Ongoing goal/ });
    expect(toggle.getAttribute("aria-expanded")).toBe("false");
    expect(document.getElementById(toggle.getAttribute("aria-controls")!)?.hidden).toBe(true);
    fireEvent.click(toggle);
    expect(toggle.getAttribute("aria-expanded")).toBe("true");
    expect(document.getElementById(toggle.getAttribute("aria-controls")!)?.hidden).toBe(false);
    expect(screen.getByRole("button", { name: /Queued message/ }).getAttribute("aria-expanded")).toBe("false");
  });

  it("stacks three persistent panels and can expand the deck", () => {
    render(<ChatPanels panels={persistent} />);
    expect(screen.getByRole("region", { name: "Chat panels" })).toBeTruthy();
    fireEvent.click(screen.getByRole("button", { name: "Expand panels" }));
    expect(screen.getByRole("button", { name: /Ongoing goal/ })).toBeTruthy();
    expect(screen.getByRole("button", { name: /Queued message/ })).toBeTruthy();
    expect(screen.getByRole("button", { name: /Note/ })).toBeTruthy();
    fireEvent.click(screen.getByRole("button", { name: "Stack panels" }));
    fireEvent.click(screen.getByRole("button", { name: /next/i }));
    expect(screen.getByRole("region", { name: "Queued message" })).toBeTruthy();
  });

  it("updates task counts and exposes collapse and status labels", () => {
    const tasks: ChatTask[] = [
      { id: "a", label: "Inspect files", agent: "Explorer", status: "in_progress" },
      { id: "b", label: "Run tests", agent: "Reviewer", status: "pending" },
      { id: "c", label: "Load context", status: "completed" },
      { id: "d", label: "Connect provider", status: "failed" },
    ];
    const { rerender } = render(<ChatPanels taskList={{ tasks }} />);
    expect(screen.getByText("1 in progress · 1 pending")).toBeTruthy();
    expect(screen.getByLabelText("Completed")).toBeTruthy();
    expect(screen.getByLabelText("Failed")).toBeTruthy();
    const toggle = screen.getByRole("button", { name: /To-dos/ });
    expect(toggle.getAttribute("aria-expanded")).toBe("false");      // always folded to begin with
    fireEvent.click(toggle);
    expect(toggle.getAttribute("aria-expanded")).toBe("true");       // opening is the person's
    rerender(<ChatPanels taskList={{ tasks: tasks.map((task) => ({ ...task, status: "completed" })) }} />);
    expect(screen.queryByRole("button", { name: /To-dos/ })).toBeNull();   // everything done: the bar is gone
  });

  it("preserves persistent panels when a picker opens or closes, and omits both without a writable composer", () => {
    const onPickerDismiss = vi.fn();
    const props = { items: [], composer: { onSteer: vi.fn() }, taskList: { tasks: [{ id: "t", label: "x", status: "pending" as const }] }, panels: persistent.slice(0, 2), onPickerDismiss };
    const { rerender } = render(<AgentChat {...props} picker={{ id: "files", title: "Files", items: [] }} />);
    expect(within(screen.getByTestId("agent-chat-panels")).getAllByRole("region", { name: "To-dos" })).toHaveLength(1);
    fireEvent.keyDown(screen.getByLabelText("message"), { key: "Escape" });
    expect(onPickerDismiss).toHaveBeenCalledTimes(1);
    rerender(<AgentChat {...props} />);
    expect(screen.queryByTestId("agent-chat-picker")).toBeNull();
    expect(screen.getByRole("button", { name: /Ongoing goal/ })).toBeTruthy();
    rerender(<AgentChat {...props} readOnly />);
    expect(screen.queryByTestId("agent-chat-panels")).toBeNull();
    rerender(<AgentChat {...props} composer={undefined} />);
    expect(screen.queryByTestId("agent-chat-panels")).toBeNull();
  });
});

describe("composer special-key callbacks", () => {
  it("prefers the longest trigger and switches back when a slash is deleted", () => {
    const onSpecialKeyChange = vi.fn();
    render(<ChatComposer onSteer={vi.fn()} onSpecialKeyChange={onSpecialKeyChange} />);
    const input = screen.getByLabelText("message");
    fireEvent.change(input, { target: { value: "/" } });
    expect(onSpecialKeyChange).toHaveBeenLastCalledWith({ key: "/", query: "", start: 0, end: 1 });
    fireEvent.change(input, { target: { value: "//" } });
    expect(onSpecialKeyChange).toHaveBeenLastCalledWith({ key: "//", query: "", start: 0, end: 2 });
    fireEvent.change(input, { target: { value: "See //review" } });
    expect(onSpecialKeyChange).toHaveBeenLastCalledWith({ key: "//", query: "review", start: 4, end: 12 });
    fireEvent.change(input, { target: { value: "/" } });
    expect(onSpecialKeyChange).toHaveBeenLastCalledWith({ key: "/", query: "", start: 0, end: 1 });
    fireEvent.change(input, { target: { value: "//" } });
    fireEvent.change(input, { target: { value: "// " } });
    expect(onSpecialKeyChange).toHaveBeenLastCalledWith(null);
  });

  it("matches custom multi-character triggers independent of their declaration order", () => {
    const onSpecialKeyChange = vi.fn();
    render(<ChatComposer onSteer={vi.fn()} specialKeys={["::", ":", ""]} onSpecialKeyChange={onSpecialKeyChange} />);
    fireEvent.change(screen.getByLabelText("message"), { target: { value: "::loop" } });
    expect(onSpecialKeyChange).toHaveBeenLastCalledWith({ key: "::", query: "loop", start: 0, end: 6 });
  });

  it("reports triggers, search text and ranges, and switches keys", () => {
    const onSpecialKeyChange = vi.fn();
    render(<ChatComposer onSteer={vi.fn()} onSpecialKeyChange={onSpecialKeyChange} />);
    const input = screen.getByLabelText("message");
    for (const key of ["/", "@", "$"]) {
      fireEvent.change(input, { target: { value: key } });
      expect(onSpecialKeyChange).toHaveBeenLastCalledWith({ key, query: "", start: 0, end: 1 });
    }
    fireEvent.change(input, { target: { value: "Read @Agent" } });
    expect(onSpecialKeyChange).toHaveBeenLastCalledWith({ key: "@", query: "Agent", start: 5, end: 11 });
    fireEvent.change(input, { target: { value: "Read @Agent " } });
    expect(onSpecialKeyChange).toHaveBeenLastCalledWith(null);
  });

  it("dismisses bare triggers on space and suppresses reopening after Escape", () => {
    const onSpecialKeyChange = vi.fn();
    render(<ChatComposer onSteer={vi.fn()} onSpecialKeyChange={onSpecialKeyChange} />);
    const input = screen.getByLabelText("message");
    fireEvent.change(input, { target: { value: "@" } });
    fireEvent.change(input, { target: { value: "@ " } });
    expect(onSpecialKeyChange).toHaveBeenLastCalledWith(null);
    fireEvent.change(input, { target: { value: "/" } });
    fireEvent.keyDown(input, { key: "Escape" });
    expect(onSpecialKeyChange).toHaveBeenLastCalledWith(null);
    const count = onSpecialKeyChange.mock.calls.length;
    fireEvent.change(input, { target: { value: "/test" } });
    expect(onSpecialKeyChange).toHaveBeenCalledTimes(count);
    fireEvent.change(input, { target: { value: "/test @" } });
    expect(onSpecialKeyChange).toHaveBeenLastCalledWith({ key: "@", query: "", start: 6, end: 7 });
  });

  it("ignores embedded triggers, supports custom keys, and preserves IME composition", () => {
    const onSpecialKeyChange = vi.fn();
    render(<ChatComposer onSteer={vi.fn()} specialKeys={["#"]} onSpecialKeyChange={onSpecialKeyChange} />);
    const input = screen.getByLabelText("message");
    for (const value of ["https://example.com", "user@example.com", "/", "@", "$"]) fireEvent.change(input, { target: { value } });
    expect(onSpecialKeyChange).not.toHaveBeenCalled();
    fireEvent.change(input, { target: { value: "#issue" } });
    expect(onSpecialKeyChange).toHaveBeenLastCalledWith({ key: "#", query: "issue", start: 0, end: 6 });
    fireEvent.keyDown(input, { key: "Escape", isComposing: true });
    expect(onSpecialKeyChange).toHaveBeenLastCalledWith({ key: "#", query: "issue", start: 0, end: 6 });
  });

  it("shows the task list only while something is pending or in progress, and always folded", () => {
    const done = [{ id: "1", label: "a", status: "completed" as const }, { id: "2", label: "b", status: "completed" as const }];
    const { unmount, rerender } = render(<ChatPanels taskList={{ tasks: done }} />);
    expect(screen.queryByRole("button", { name: /To-dos/ })).toBeNull();          // all done: no bar at all
    rerender(<ChatPanels taskList={{ tasks: [done[0]!, { id: "2", label: "b", status: "pending" as const }] }} />);
    const bar = screen.getByRole("button", { name: /To-dos/ });
    expect(bar.getAttribute("aria-expanded")).toBe("false");                      // new to-dos: back, but folded
    fireEvent.click(bar);
    expect(bar.getAttribute("aria-expanded")).toBe("true");
    rerender(<ChatPanels taskList={{ tasks: done }} />);
    expect(screen.queryByRole("button", { name: /To-dos/ })).toBeNull();
    rerender(<ChatPanels taskList={{ tasks: [{ id: "3", label: "c", status: "pending" as const }] }} />);
    expect(screen.getByRole("button", { name: /To-dos/ }).getAttribute("aria-expanded")).toBe("false");   // returns folded again
    unmount();
  });
});
