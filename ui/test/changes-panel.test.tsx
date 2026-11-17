import { describe, expect, it, vi } from "vitest";
import { fireEvent, render, screen, within } from "@testing-library/react";
import { ChangesPanel } from "../src/components/agent-chat";
import { ChatComposer } from "../src/components/agent-chat/ChatComposer";
import { PageBar } from "../src/features/loop-workspace/center/PageBar";

const files = Array.from({ length: 12 }, (_, i) => ({ path: `src/f${i}.ts`, added: i + 1, removed: 1 }));

describe("the changes panel", () => {
  it("starts folded, lists five, adds five at a time, and folds back to five", () => {
    const onOpenFile = vi.fn();
    render(<ChangesPanel files={files} onOpenFile={onOpenFile} />);
    const head = screen.getByRole("button", { name: /Edited 12 files/ });
    expect(head.getAttribute("aria-expanded")).toBe("false");
    fireEvent.click(head);
    const list = () => screen.getByTestId("agent-chat-changes-list");
    expect(within(list()).getAllByRole("button")).toHaveLength(5);
    fireEvent.click(screen.getByTestId("agent-chat-changes-more"));
    expect(within(list()).getAllByRole("button")).toHaveLength(10);
    fireEvent.click(screen.getByTestId("agent-chat-changes-more"));
    expect(within(list()).getAllByRole("button")).toHaveLength(12);
    expect(screen.queryByTestId("agent-chat-changes-more")).toBeNull();
    fireEvent.click(screen.getByTestId("agent-chat-changes-less"));
    expect(within(list()).getAllByRole("button")).toHaveLength(5);
    fireEvent.click(within(list()).getByText("src/f2.ts"));
    expect(onOpenFile).toHaveBeenCalledWith("src/f2.ts");
  });

  it("adds up the counts, and a file with no count shows none", () => {
    render(<ChangesPanel files={[{ path: "a", added: 3, removed: 1 }, { path: "b", binary: true }]} />);
    expect(screen.getByRole("button", { name: /Edited 2 files/ }).textContent).toContain("+3");
  });
});

describe("the page bar's git pill", () => {
  it("opens the changes when the host has a changes view", () => {
    const onOpenChanges = vi.fn();
    render(<PageBar title="Loop view" status="running" terminalOpen={false} onToggleTerminal={() => undefined} inspectorOpen onToggleInspector={() => undefined}
      git={{ state: "ok", branch: "main", changed: [{ path: "a", index: "M", worktree: " " }] }} onOpenChanges={onOpenChanges} />);
    fireEvent.click(screen.getByTestId("git-pill"));
    expect(onOpenChanges).toHaveBeenCalledTimes(1);
  });
});

describe("the composer's model controls", () => {
  const routes = [{ value: "qwen", label: "Qwen", thinking: true, reasoningEfforts: ["low", "high"] }, { value: "plain", label: "Plain" }];
  const base = { onSteer: vi.fn(), targets: [{ value: "planner", label: "planner · done" }], routes, currentRoute: "qwen", steerable: false };

  it("offers thinking for a route that thinks, hides effort when thinking is off, and sets the loop's choice at once", () => {
    const onChange = vi.fn();
    const { rerender } = render(<ChatComposer {...base} loopOptions={{ thinking: "", effort: "low", onChange }} />);
    expect(screen.getByLabelText("thinking")).toBeTruthy();
    expect(screen.getByLabelText("reasoning effort")).toBeTruthy();
    fireEvent.change(screen.getByLabelText("thinking"), { target: { value: "off" } });
    expect(onChange).toHaveBeenCalledWith({ thinking: "off" });
    rerender(<ChatComposer {...base} loopOptions={{ thinking: "off", effort: "low", onChange }} />);
    expect(screen.queryByLabelText("reasoning effort")).toBeNull();
    // With nobody running, an effort change is the loop's, applied at once.
    rerender(<ChatComposer {...base} loopOptions={{ thinking: "on", effort: "low", onChange }} />);
    fireEvent.change(screen.getByLabelText("reasoning effort"), { target: { value: "high" } });
    expect(onChange).toHaveBeenLastCalledWith({ reasoningEffort: "high" });
  });

  it("offers no thinking switch for a route without one, and lists the agents that took part", () => {
    render(<ChatComposer {...base} currentRoute="plain" loopOptions={{ thinking: "", effort: "", onChange: vi.fn() }} />);
    expect(screen.queryByLabelText("thinking")).toBeNull();
    expect(screen.getByLabelText("send to")).toBeTruthy();
  });

  it("shows the changes button only when there are changes, and reports the toggle", () => {
    const onToggle = vi.fn();
    const { rerender } = render(<ChatComposer {...base} changesToggle={{ count: 0, on: false, onToggle }} />);
    expect(screen.queryByTestId("agent-chat-changes-toggle")).toBeNull();
    rerender(<ChatComposer {...base} changesToggle={{ count: 7, on: false, onToggle }} />);
    fireEvent.click(screen.getByTestId("agent-chat-changes-toggle"));
    expect(onToggle).toHaveBeenCalledTimes(1);
  });

  it("shows the loop's own title beside the page title, with the full request on hover", () => {
    render(<PageBar title="Loop view" loopTitle="Greeting CLI with history" loopGoal="Turn the hello program into a small, tested greeting CLI with history" status="running"
      terminalOpen={false} onToggleTerminal={() => undefined} inspectorOpen onToggleInspector={() => undefined} git={null} />);
    const title = screen.getByTestId("loop-title");
    expect(title.textContent).toBe("Greeting CLI with history");
    expect(title.getAttribute("title")).toContain("Turn the hello program");
  });
});
