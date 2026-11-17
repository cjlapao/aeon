import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import { act, cleanup, fireEvent, render, screen, waitFor, within } from "@testing-library/react";
import { MemoryRouter } from "react-router-dom";
import { AgentChat, type ChatItem } from "../src/components/agent-chat";
import { AgentChatPage } from "../src/features/test-bench/pages/AgentChatPage";
import { installFakeDaemon, resetFakeDaemon } from "./helpers";

// The renderer and the composer: what a person can see and do. The adapters
// have their own file; here every item is handed in directly.

beforeEach(() => resetFakeDaemon());
afterEach(() => { cleanup(); resetFakeDaemon(); vi.useRealTimers(); });

const at = "2026-10-24T10:42:01Z";
const items: ChatItem[] = [
  { id: "d1", kind: "divider", tone: "orchestrator", text: "Orchestrator → step survey · dispatched surveyor + linter", highlights: ["surveyor", "linter"] },
  { id: "l1", at, agent: "linter", kind: "tool", tool: "bash", summary: "Find unformatted files", status: "done", result: "2 files", durationMs: 900 },
  { id: "l2", at, agent: "linter", kind: "thinking", text: "The tree has uncommitted work.\nSecond line." },
  { id: "s1", at, agent: "surveyor", kind: "text", text: "Checking `TASKS.md` first." },
  { id: "s2", at, agent: "surveyor", kind: "tool", tool: "write_file", summary: "SURVEY.md", status: "done", file: { path: "SURVEY.md", linesAdded: 18 } },
  { id: "q1", at, agent: "surveyor", kind: "question", questions: [
    { id: "1", prompt: "Runtime?", label: "Target runtime", kind: "clarify", options: ["Go 1.23"], answer: "Go 1.23" },
    { id: "2", prompt: "Strictness?", kind: "clarify", options: ["default", "strict"] },
  ] },
  { id: "gate:9", kind: "gate", title: "Approve SURVEY.md (+18 lines)", stepLabel: "Step 3 Gate", preview: "+ # Survey\n+ Module: hello", options: ["approve", "reject"], state: "awaiting" },
  { id: "st", kind: "steer", target: "surveyor", text: "Also mention the Go version.", state: "queued", steerId: 7 },
];
const agents = { surveyor: { id: "surveyor", role: "lead-explorer" }, linter: { id: "linter", role: "ast-vet" } };

describe("the agent chat stream", () => {
  it("groups consecutive items under one agent header with its role", () => {
    render(<AgentChat items={items} agents={agents} />);
    const headers = screen.getAllByTestId("agent-chat-agent-header");
    expect(headers.map((h) => h.textContent)).toEqual([
      expect.stringContaining("linter"), expect.stringContaining("surveyor"),
    ]);
    expect(within(headers[0]!).getByText("ast-vet")).toBeTruthy();
    expect(screen.getByText("Find unformatted files")).toBeTruthy();
    expect(screen.getByText("TASKS.md").tagName).toBe("CODE");
  });

  it("zooms into one agent and drops the others", () => {
    render(<AgentChat items={items} agents={agents} agentFilter="linter" />);
    expect(screen.getByText("Find unformatted files")).toBeTruthy();
    expect(screen.queryByText(/Checking/)).toBeNull();
    expect(screen.queryByTestId("agent-chat-gate")).toBeNull();
    expect(screen.queryByTestId("agent-chat-agent-header")).toBeNull();
    // The dispatch divider names linter, so it stays.
    expect(screen.getByTestId("agent-chat-divider")).toBeTruthy();
  });

  it("expands a thinking line into its full text", () => {
    render(<AgentChat items={items} />);
    expect(screen.queryByText(/Second line/)).toBeNull();
    fireEvent.click(screen.getByText("The tree has uncommitted work."));
    expect(screen.getByText(/Second line/)).toBeTruthy();
  });

  it("shows a write as a file card and asks the host for its diff", () => {
    const onViewDiff = vi.fn();
    render(<AgentChat items={items} actions={{ onViewDiff }} />);
    const card = screen.getByTestId("agent-chat-file");
    expect(within(card).getByText("+18")).toBeTruthy();
    fireEvent.click(within(card).getByText(/view diff/));
    expect(onViewDiff).toHaveBeenCalledWith({ path: "SURVEY.md", linesAdded: 18 });
  });

  it("shows answered questions in the grid and answers a pending one", async () => {
    const onAnswer = vi.fn();
    render(<AgentChat items={items} actions={{ onAnswer }} />);
    const card = screen.getByTestId("agent-chat-question");
    expect(within(card).getByText("1/2 answered")).toBeTruthy();
    expect(within(card).getByText("Q1: Target runtime")).toBeTruthy();
    fireEvent.click(within(card).getByRole("button", { name: "strict" }));
    await waitFor(() => expect(onAnswer).toHaveBeenCalledWith(expect.objectContaining({ id: "2" }), "strict"));
  });

  it("toggles the answers with a pill that says how many, and keeps the state on the button", () => {
    render(<AgentChat items={[{ id: "q", agent: "a", kind: "question", questions: [
      { id: "1", prompt: "Runtime?", kind: "clarify", answer: "Go 1.23" },
      { id: "2", prompt: "Strict?", kind: "clarify", answer: "default" },
    ] }]} />);
    const toggle = screen.getByTestId("agent-chat-answers-toggle");
    // A card that arrives already answered (a reopened loop) starts folded.
    expect(toggle.getAttribute("aria-expanded")).toBe("false");
    expect(toggle.textContent).toBe("Show 2 answers");
    fireEvent.click(toggle);
    expect(toggle.getAttribute("aria-expanded")).toBe("true");
    expect(toggle.textContent).toBe("Hide answers");
    fireEvent.click(toggle);
    fireEvent.click(toggle);
    // Re-opening re-mounts the tiles so their staggered rise plays again.
    const tiles = screen.getByTestId("agent-chat-answers").querySelectorAll(".agent-chat-tile");
    expect([...tiles].map((t) => (t as HTMLElement).style.getPropertyValue("--i"))).toEqual(["0", "1"]);
  });

  it("approves a gate, and asks for feedback before rejecting", async () => {
    const onGate = vi.fn();
    render(<AgentChat items={items} actions={{ onGate }} />);
    const gate = screen.getByTestId("agent-chat-gate");
    fireEvent.click(within(gate).getByRole("button", { name: /Reject with feedback/ }));
    expect(onGate).not.toHaveBeenCalled();
    fireEvent.change(within(gate).getByLabelText("feedback"), { target: { value: "Add the packages list." } });
    fireEvent.click(within(gate).getByRole("button", { name: /Send reject/ }));
    await waitFor(() => expect(onGate).toHaveBeenCalledWith("gate:9", "reject", "Add the packages list."));
    fireEvent.click(within(gate).getByRole("button", { name: /Approve & continue loop/ }));
    await waitFor(() => expect(onGate).toHaveBeenCalledWith("gate:9", "approve", undefined));
  });

  it("a plan gate offers the tasks, and a revision needs the person's words", async () => {
    const onGate = vi.fn();
    const onShowTasks = vi.fn();
    const plan = { id: "gate:plan", kind: "gate", title: "Approve the plan and start building?", stepLabel: "plan-gate", options: ["approve", "revise", "abort"], state: "awaiting" } as unknown as ChatItem;
    render(<AgentChat items={[plan]} actions={{ onGate, onShowTasks }} />);
    fireEvent.click(screen.getByTestId("agent-chat-show-tasks"));
    expect(onShowTasks).toHaveBeenCalledTimes(1);
    fireEvent.click(screen.getByRole("button", { name: /^Revise/ }));
    const send = screen.getByRole("button", { name: "Send revision request" });
    expect((send as HTMLButtonElement).disabled).toBe(true); // no words, no revision
    fireEvent.change(screen.getByLabelText("feedback"), { target: { value: "Split T3." } });
    fireEvent.click(send);
    await waitFor(() => expect(onGate).toHaveBeenCalledWith("gate:plan", "revise", "Split T3."));
  });

  it("withdraws a queued steer", () => {
    const onCancelSteer = vi.fn();
    render(<AgentChat items={items} actions={{ onCancelSteer }} />);
    const steer = screen.getByTestId("agent-chat-steer");
    expect(within(steer).getByText("queued · after this turn")).toBeTruthy();
    fireEvent.click(within(steer).getByText("withdraw"));
    expect(onCancelSteer).toHaveBeenCalledWith(7);
  });

  it("read-only hides the composer and disables every answering control", () => {
    render(<AgentChat items={items} readOnly actions={{ onAnswer: vi.fn(), onGate: vi.fn(), onCancelSteer: vi.fn() }}
      composer={{ targets: [{ value: "surveyor", label: "surveyor" }], onSteer: vi.fn() }} />);
    expect(screen.queryByTestId("agent-chat-composer")).toBeNull();
    // A question nobody answered stays visible in history, its options inert.
    expect(screen.getByRole("button", { name: "strict" }).hasAttribute("disabled")).toBe(true);
    expect(screen.getByRole("button", { name: /Approve & continue loop/ }).hasAttribute("disabled")).toBe(true);
    expect(screen.queryByText("withdraw")).toBeNull();
  });

  it("labels each tool status distinctly", () => {
    const statuses = ["running", "error", "skipped", "awaiting", "denied"] as const;
    render(<AgentChat items={statuses.map((status, i) => ({ id: `t${i}`, agent: "a", kind: "tool", tool: "bash", summary: status, status }))} />);
    expect(screen.getByRole("status", { name: "running" })).toBeTruthy();
    for (const label of ["failed", "skipped · steered", "awaiting permission", "denied by policy"]) expect(screen.getByText(label)).toBeTruthy();
  });
});

describe("tool approval, thinking and auto-approve", () => {
  const approvalItem: ChatItem = { id: "t", agent: "a", kind: "tool", tool: "bash", summary: "git push", status: "awaiting",
    verdict: { verdict: "asked", reason: "ask: pushes-ask" },
    approval: { id: "41", prompt: "Allow git push?", kind: "permission", options: ["deny", "approve"] } };

  it("renders a parked call as an open approval card and answers it", async () => {
    const onAnswer = vi.fn();
    render(<AgentChat items={[approvalItem]} actions={{ onAnswer }} />);
    const card = screen.getByTestId("agent-chat-approval");
    expect(within(card).getByText("Allow git push?")).toBeTruthy();
    expect(within(card).getByText(/pushes-ask/)).toBeTruthy();
    fireEvent.click(within(card).getByTestId("agent-chat-approve"));
    await waitFor(() => expect(onAnswer).toHaveBeenCalledWith(expect.objectContaining({ id: "41" }), "approve"));
  });

  it("offers the loop-wide override beside a permission ask, only when the host provides it", () => {
    const onAllowAll = vi.fn();
    const { unmount } = render(<AgentChat items={[approvalItem]} actions={{ onAnswer: vi.fn(), onAllowAll }} />);
    fireEvent.click(screen.getByTestId("agent-chat-allow-all"));
    expect(onAllowAll).toHaveBeenCalledTimes(1);
    unmount();
    render(<AgentChat items={[approvalItem]} actions={{ onAnswer: vi.fn() }} />);
    expect(screen.queryByTestId("agent-chat-allow-all")).toBeNull();
  });

  it("shows the loop's permission mode as a shield menu and reports the choice", () => {
    const onAsk = vi.fn(); const onFull = vi.fn();
    const composer = { onSteer: vi.fn(), targets: [{ value: "a", label: "a" }], defaultTarget: "a", access: { mode: "ask" as const, label: "Ask", onAsk, onFull } };
    const { unmount } = render(<AgentChat items={[]} composer={composer} />);
    fireEvent.click(screen.getByTestId("agent-chat-access"));
    fireEvent.click(screen.getByTestId("agent-chat-access-full"));
    expect(onFull).toHaveBeenCalledTimes(1);
    expect(screen.queryByTestId("agent-chat-access-menu")).toBeNull();
    unmount();
    render(<AgentChat items={[]} composer={{ ...composer, access: { ...composer.access, mode: "full", label: "Approve all" } }} />);
    expect(screen.getByTestId("agent-chat-access").textContent).toContain("Approve all");
    fireEvent.click(screen.getByTestId("agent-chat-access"));
    fireEvent.click(screen.getByTestId("agent-chat-access-ask"));
    expect(onAsk).toHaveBeenCalledTimes(1);
  });

  it("replaces the card's override button with a statement once everything is allowed", () => {
    render(<AgentChat items={[approvalItem]} actions={{ onAnswer: vi.fn(), onAllowAll: vi.fn(), allowedAll: true }} />);
    expect(screen.queryByTestId("agent-chat-allow-all")).toBeNull();
    expect(screen.getByTestId("agent-chat-allowed-all")).toBeTruthy();
  });

  it("says the loop is working between events, and stays quiet when something already shows the work", () => {
    const done: ChatItem = { id: "t1", at, agent: "a", kind: "tool", tool: "read", summary: "go.mod", status: "done", durationMs: 12 };
    const { rerender } = render(<AgentChat items={[done]} working />);
    expect(screen.getByTestId("agent-chat-working").textContent).toContain("a is working");
    rerender(<AgentChat items={[{ ...done, status: "running" } as ChatItem]} working />);
    expect(screen.queryByTestId("agent-chat-working")).toBeNull();
    rerender(<AgentChat items={[{ id: "x", at, agent: "a", kind: "thinking", text: "hmm", streaming: true }]} working />);
    expect(screen.queryByTestId("agent-chat-working")).toBeNull();
    rerender(<AgentChat items={[approvalItem]} working />);
    expect(screen.queryByTestId("agent-chat-working")).toBeNull();
    rerender(<AgentChat items={[done]} />);
    expect(screen.queryByTestId("agent-chat-working")).toBeNull();
  });

  it("disables the approval buttons when read-only", () => {
    render(<AgentChat items={[approvalItem]} readOnly actions={{ onAnswer: vi.fn() }} />);
    expect(screen.getByTestId("agent-chat-approve").hasAttribute("disabled")).toBe(true);
  });

  it("collapses an answered approval back to a row that says who decided", () => {
    render(<AgentChat items={[{ ...approvalItem, status: "done", approval: { ...(approvalItem as { approval: object }).approval, answer: "approve" } } as ChatItem]} />);
    expect(screen.queryByTestId("agent-chat-approval")).toBeNull();
    expect(screen.getByText("approved by you")).toBeTruthy();
  });

  it("marks a long thought and toggles it open and closed", () => {
    const long = Array.from({ length: 200 }, (_, i) => `word${i}`).join(" ");
    render(<AgentChat items={[{ id: "th", agent: "a", kind: "thinking", text: long }]} />);
    expect(screen.getByText("long · 200 words")).toBeTruthy();
    const toggle = screen.getByRole("button", { expanded: false });
    fireEvent.click(toggle);
    expect(toggle.getAttribute("aria-expanded")).toBe("true");
    fireEvent.click(toggle);
    expect(toggle.getAttribute("aria-expanded")).toBe("false");
  });

  it("flips the auto-approve chip between Ask and Auto-approve", () => {
    const onChange = vi.fn();
    const { rerender } = render(<AgentChat items={[]} composer={{ targets: [], onSteer: vi.fn(), autoApprove: { on: false, onChange } }} />);
    const chip = screen.getByTestId("agent-chat-auto-approve");
    expect(chip.textContent).toBe("Ask");
    fireEvent.click(chip);
    expect(onChange).toHaveBeenCalledWith(true);
    rerender(<AgentChat items={[]} composer={{ targets: [], onSteer: vi.fn(), autoApprove: { on: true, onChange } }} />);
    expect(screen.getByTestId("agent-chat-auto-approve").textContent).toBe("Auto-approve");
    expect(screen.getByTestId("agent-chat-auto-approve").getAttribute("aria-pressed")).toBe("true");
  });

  it("locks the chip and says why", () => {
    render(<AgentChat items={[]} composer={{ targets: [], onSteer: vi.fn(), autoApprove: { on: false, onChange: vi.fn(), lockedReason: "destructive tools need you" } }} />);
    const chip = screen.getByTestId("agent-chat-auto-approve");
    expect(chip.hasAttribute("disabled")).toBe(true);
    expect(chip.getAttribute("title")).toBe("destructive tools need you");
  });
});

describe("expandable rows and the bash box", () => {
  const bash: ChatItem = { id: "b", agent: "linter", kind: "tool", tool: "bash", toolUseId: "b1", status: "done", durationMs: 400,
    summary: "Find the registry tests",
    command: "cd /home/me/aeon && grep -n 'func Test' internal/handler/registry_test.go | head -20",
    output: "14:func TestRegistryResolvesByName(t *testing.T) {\n27:func TestUnknownHandlerIsADomainRefusal(t *testing.T) {",
    outputTruncated: true };

  it("swaps the row icon for the chevron: both glyphs share the slot, and opening flips the state", () => {
    const { container } = render(<AgentChat items={[bash]} />);
    const slot = container.querySelector(".agent-chat-rowicon")!;
    expect(slot.getAttribute("data-expandable")).toBe("true");
    expect(slot.getAttribute("data-open")).toBeNull();
    expect(slot.querySelector('[data-icon="bash"]')).toBeTruthy();
    expect(slot.querySelector('[data-icon="chevron-hover"]')).toBeTruthy();
    expect(slot.querySelector('[data-icon="chevron-expanded"]')).toBeTruthy();
    fireEvent.click(screen.getByRole("button", { expanded: false }));
    expect(slot.getAttribute("data-open")).toBe("true");
  });

  it("does not offer the chevron on a row with nothing to expand", () => {
    const { container } = render(<AgentChat items={[{ id: "l", agent: "a", kind: "tool", tool: "list", status: "running" }]} />);
    expect(container.querySelector(".agent-chat-rowicon")!.getAttribute("data-expandable")).toBeNull();
    expect(container.querySelector('[data-icon="chevron-hover"]')).toBeNull();
  });

  it("opens bash into the command, then a rule, then its output — and says the stream cut it", () => {
    render(<AgentChat items={[bash]} />);
    fireEvent.click(screen.getByRole("button", { expanded: false }));
    const box = screen.getByTestId("agent-chat-bash");
    expect(within(box).getByTestId("agent-chat-bash-command").textContent).toBe(bash.kind === "tool" ? bash.command : "");
    expect(within(box).getByTestId("agent-chat-bash-output").textContent).toContain("TestUnknownHandlerIsADomainRefusal");
    expect(within(box).getByText(/the transcript has all of it/)).toBeTruthy();
  });

  it("puts who decided and what policy said at the top of the box, above the command", () => {
    const approved: ChatItem = { ...bash, verdict: { verdict: "allowed", reason: `allowed: agent:linter · "commands" · source operator` },
      approval: { id: "9", prompt: "Allow the grep?", kind: "permission", answer: "approve" } } as ChatItem;
    render(<AgentChat items={[approved]} />);
    fireEvent.click(screen.getByRole("button", { expanded: false }));
    const box = screen.getByTestId("agent-chat-bash");
    const strip = within(box).getByTestId("agent-chat-decisions");
    expect(within(strip).getByText("You approved")).toBeTruthy();
    expect(within(strip).getByText("Allow the grep?")).toBeTruthy();
    expect(within(strip).getByText("Policy allowed")).toBeTruthy();
    expect(within(strip).getByText("commands")).toBeTruthy();
    expect(within(strip).getByText("source operator")).toBeTruthy();
    // The strip comes first in the box, before the command.
    expect(strip.compareDocumentPosition(within(box).getByTestId("agent-chat-bash-command")) & Node.DOCUMENT_POSITION_FOLLOWING).toBeTruthy();
  });

  it("says a declined call was declined, and a denied one was policy", () => {
    const declined = { ...bash, output: undefined, outputTruncated: false, status: "denied", approval: { id: "9", prompt: "Allow?", kind: "permission", answer: "deny" } } as ChatItem;
    const denied = { id: "d", agent: "a", kind: "tool", tool: "read", summary: "secret", status: "denied", result: "refused: no-secrets",
      verdict: { verdict: "denied", reason: `refused: agent:a · "no-secrets"` } } as ChatItem;
    render(<AgentChat items={[declined, denied]} />);
    for (const b of screen.getAllByRole("button", { expanded: false })) fireEvent.click(b);
    expect(screen.getByText("You declined")).toBeTruthy();
    expect(screen.getByText("Policy denied")).toBeTruthy();
    expect(screen.getByText("no-secrets")).toBeTruthy();
    // A call that never ran says so instead of repeating the refusal.
    expect(screen.getByText("not run — you declined it")).toBeTruthy();
  });

  it("copies the command", async () => {
    const writes: string[] = [];
    Object.defineProperty(navigator, "clipboard", { configurable: true, value: { writeText: (t: string) => { writes.push(t); return Promise.resolve(); } } });
    render(<AgentChat items={[bash]} />);
    fireEvent.click(screen.getByRole("button", { expanded: false }));
    fireEvent.click(screen.getByTestId("agent-chat-copy"));
    expect(await screen.findByText("Copied")).toBeTruthy();
    expect(writes).toEqual([bash.kind === "tool" ? bash.command : ""]);
  });

  it("boxes only the hidden part of a thought, not the row", () => {
    render(<AgentChat items={[{ id: "th", agent: "a", kind: "thinking", text: "First line.\nSecond line." }]} />);
    expect(screen.queryByTestId("agent-chat-thinking-body")).toBeNull();
    fireEvent.click(screen.getByText("First line."));
    expect(screen.getByTestId("agent-chat-thinking-body").textContent).toContain("Second line.");
    // The row itself is still the plain line, not inside the box.
    expect(screen.getByTestId("agent-chat-thinking-body").contains(screen.getByText("First line."))).toBe(false);
  });
});

describe("the composer", () => {
  const targets = [{ value: "surveyor", label: "surveyor" }, { value: "linter", label: "linter" }];
  const routes = [{ value: "anthropic/sonnet", label: "Anthropic · Sonnet" }, { value: "anthropic/opus", label: "Anthropic · Opus" }];
  const efforts = [{ value: "low", label: "low" }, { value: "medium", label: "medium" }];

  it("sends a steer to the chosen agent on Enter, without route/effort it did not change", async () => {
    const onSteer = vi.fn();
    render(<AgentChat items={[]} composer={{ targets, routes, efforts, currentRoute: "anthropic/sonnet", currentEffort: "medium", onSteer }} />);
    fireEvent.change(screen.getByLabelText("send to"), { target: { value: "linter" } });
    fireEvent.change(screen.getByLabelText("message"), { target: { value: "Run vet too." } });
    fireEvent.keyDown(screen.getByLabelText("message"), { key: "Enter" });
    await waitFor(() => expect(onSteer).toHaveBeenCalledWith({ target: "linter", text: "Run vet too." }));
    await waitFor(() => expect((screen.getByLabelText("message") as HTMLTextAreaElement).value).toBe(""));
  });

  it("shows 'default' for a route/effort it does not know, and never sends it", async () => {
    const onSteer = vi.fn();
    render(<AgentChat items={[]} composer={{ targets, routes, efforts, onSteer }} />);
    expect(screen.getByText("Effort: default")).toBeTruthy();
    fireEvent.change(screen.getByLabelText("message"), { target: { value: "go" } });
    fireEvent.click(screen.getByRole("button", { name: /Steer/ }));
    await waitFor(() => expect(onSteer).toHaveBeenCalledWith({ target: "surveyor", text: "go" }));
  });

  it("Shift+Enter is a newline, not a send", () => {
    const onSteer = vi.fn();
    render(<AgentChat items={[]} composer={{ targets, onSteer }} />);
    fireEvent.change(screen.getByLabelText("message"), { target: { value: "line" } });
    fireEvent.keyDown(screen.getByLabelText("message"), { key: "Enter", shiftKey: true });
    expect(onSteer).not.toHaveBeenCalled();
  });

  it("sends a provider/effort change with no text", async () => {
    const onSteer = vi.fn();
    render(<AgentChat items={[]} composer={{ targets, routes, efforts, currentRoute: "anthropic/sonnet", currentEffort: "medium", onSteer }} />);
    const steer = screen.getByRole("button", { name: /Steer/ });
    expect(steer.hasAttribute("disabled")).toBe(true);
    fireEvent.change(screen.getByLabelText("provider"), { target: { value: "anthropic/opus" } });
    fireEvent.change(screen.getByLabelText("reasoning effort"), { target: { value: "low" } });
    expect(screen.getByText(/applies from surveyor's next turn/)).toBeTruthy();
    fireEvent.click(steer);
    await waitFor(() => expect(onSteer).toHaveBeenCalledWith({ target: "surveyor", text: "", route: "anthropic/opus", reasoningEffort: "low" }));
  });

  it("stops the selected agent, and shows a failed call's reason", async () => {
    const onStop = vi.fn().mockRejectedValue(new Error("step 4 is not running"));
    render(<AgentChat items={[]} composer={{ targets, onSteer: vi.fn(), onStop }} />);
    fireEvent.click(screen.getByRole("button", { name: /Stop/ }));
    await waitFor(() => expect(onStop).toHaveBeenCalledWith("surveyor"));
    expect(await screen.findByRole("alert")).toHaveProperty("textContent", "step 4 is not running");
  });

  it("in the zoomed view sends to that agent and hides the To picker", async () => {
    const onSteer = vi.fn();
    render(<AgentChat items={[]} agentFilter="linter" composer={{ targets, onSteer }} />);
    expect(screen.queryByLabelText("send to")).toBeNull();
    fireEvent.change(screen.getByLabelText("message"), { target: { value: "hi" } });
    fireEvent.click(screen.getByRole("button", { name: /Steer/ }));
    await waitFor(() => expect(onSteer).toHaveBeenCalledWith({ target: "linter", text: "hi" }));
  });

  it("says why it is disabled when nothing is running", () => {
    render(<AgentChat items={[]} composer={{ targets: [], onSteer: vi.fn(), disabled: true, disabledReason: "No agent is running — nothing to steer." }} />);
    expect(screen.getByPlaceholderText("No agent is running — nothing to steer.")).toBeTruthy();
  });
});

describe("the Agent chat bench page", () => {
  it("replays a scenario through the adapter, answers locally, and logs the callback", async () => {
    render(<AgentChatPage />);
    // Non-interactive scenarios open at their end.
    fireEvent.change(screen.getByLabelText("scenario"), { target: { value: "two-agents" } });
    expect(await screen.findByTestId("agent-chat-gate")).toBeTruthy();
    fireEvent.click(screen.getByRole("button", { name: /Approve & continue loop/ }));
    await waitFor(() => expect(screen.getByTestId("agent-chat-gate").getAttribute("data-state")).toBe("answered"));
    expect(within(screen.getByTestId("agent-chat-log")).getByText(/loop.answer gate:90: approve/)).toBeTruthy();
  });

  it("steps through a scenario one event at a time from a reset", () => {
    render(<AgentChatPage />);
    fireEvent.click(screen.getByTestId("agent-chat-reset"));
    expect(screen.getByText("Nothing has happened yet.")).toBeTruthy();
    fireEvent.click(screen.getByTestId("agent-chat-step"));
    expect(screen.getByText(/loop started/)).toBeTruthy();
  });

  it("walks the interactive scenario: it stops for an approval, two questions, the gate, and auto-approve answers the last one", async () => {
    render(<AgentChatPage />);
    fireEvent.click(screen.getByText("instant"));
    // Stop 1: the go vet call is parked as an approval card.
    expect((await screen.findByTestId("agent-chat-waiting")).textContent).toMatch(/go vet/);
    const card = screen.getByTestId("agent-chat-approval");
    expect(within(card).getByText(/^ask: agent:surveyor/)).toBeTruthy();
    fireEvent.click(within(card).getByTestId("agent-chat-approve"));
    // Approved: the same call resumes in place and finishes; stop 2 is the questions.
    await waitFor(() => expect(screen.getByTestId("agent-chat-waiting").textContent).toMatch(/questions/));
    expect(screen.getByText("approved by you")).toBeTruthy();
    expect(screen.getAllByText("go vet ./...")).toHaveLength(1);
    fireEvent.click(screen.getByRole("button", { name: "Go 1.23 (go.mod)" }));
    fireEvent.change(screen.getByLabelText("answer"), { target: { value: "Mention the vet baseline" } });
    fireEvent.click(screen.getByRole("button", { name: "Answer" }));
    // Stop 3: the gate.
    await waitFor(() => expect(screen.getByTestId("agent-chat-waiting").textContent).toMatch(/gate/));
    fireEvent.click(screen.getByRole("button", { name: /Approve & continue loop/ }));
    // Stop 4: a second approval — auto-approve answers it.
    await waitFor(() => expect(screen.getByTestId("agent-chat-waiting").textContent).toMatch(/git diff/));
    fireEvent.click(screen.getByTestId("agent-chat-auto-approve"));
    expect(await screen.findByText(/Loop finished/, {}, { timeout: 4000 })).toBeTruthy();
    expect(within(screen.getByTestId("agent-chat-log")).getByText(/auto-approved question 604/)).toBeTruthy();
    expect(screen.queryByTestId("agent-chat-waiting")).toBeNull();
    expect(screen.queryByTestId("agent-chat-composer")).toBeNull();
    // Real timers (the auto-approve pause, the typing of the scenario) make this walk take
    // seconds; the default 5s budget is too tight when the whole suite shares the CPU.
  }, 20_000);

  it("draws the whole loop page from the interactive scenario: the rail, the agents and the parked approval move together", async () => {
    // The inspector now opens by default, so the Step tab's "Open
    // conversation" link mounts immediately and needs a router to sit in.
    render(<MemoryRouter><AgentChatPage /></MemoryRouter>);
    fireEvent.click(screen.getByText("instant"));
    fireEvent.click(await screen.findByText("Full page"));
    const page = within(await screen.findByTestId("agent-chat-full-page"));
    // Stop 1: the call is parked on an approval card in the chat, and the rail's step says an answer is wanted.
    const card = await page.findByTestId("agent-chat-approval");
    const step = () => page.getAllByRole("button").find((b) => /^survey/.test(b.textContent ?? ""))!;
    expect(step().textContent).toMatch(/Answer requested/);
    expect(within(page.getByRole("button", { name: "surveyor" })).getByText("running")).toBeTruthy();
    expect(page.getByTestId("page-bar").textContent).toMatch(/waiting · survey/);
    fireEvent.click(within(card).getByTestId("agent-chat-approve"));
    // Approved: the loop runs on to the questions, and the step is running again.
    await waitFor(() => expect(screen.getByTestId("agent-chat-waiting").textContent).toMatch(/questions/));
    expect(step().textContent).toMatch(/RUNNING/);
    expect(page.getByTestId("page-bar").textContent).toMatch(/running · survey/);
  });

  it("follows the 'no' branch when the approval is declined", async () => {
    render(<AgentChatPage />);
    fireEvent.click(screen.getByText("instant"));
    fireEvent.click(within(await screen.findByTestId("agent-chat-approval")).getByTestId("agent-chat-decline"));
    expect(await screen.findByText(/won't run it/)).toBeTruthy();
    expect(screen.getByText("declined by you")).toBeTruthy();
  });

  it("in live mode, steers the running agent's step through the daemon", async () => {
    const daemon = installFakeDaemon({ responses: {
      "loop.list": { all: true, loops: [{ id: "loop-1", title: "two-step", status: "running", componentRoot: "/repo", slug: "two-step" }] },
      "routes.list": { routes: [] },
      subscribe: { subscriptionId: "s-live", from: 0 },
      "step.steer": { steerId: 41, state: "pending" },
    } });
    render(<AgentChatPage />);
    fireEvent.click(screen.getByText("Live (daemon)"));
    await waitFor(() => expect(daemon.methods()).toContain("subscribe"));
    act(() => daemon.emit({ subscriptionId: "s-live", seq: 5, kind: "loop.step.dispatched", at, payload: { loopId: "loop-1", step: "survey", stepId: 12, agent: "surveyor" } }));
    act(() => daemon.emit({ subscriptionId: "s-live", seq: 6, kind: "step.tool.start", at, payload: { stepId: 12, agent: "surveyor", tool: "read", summary: "go.mod", toolUseId: "t1" } }));
    // An event from another subscription never reaches this view.
    act(() => daemon.emit({ subscriptionId: "other", seq: 7, kind: "step.tool.start", at, payload: { stepId: 12, agent: "surveyor", tool: "bash", summary: "SHOULD NOT SHOW", toolUseId: "t2" } }));
    expect(await screen.findByText("go.mod")).toBeTruthy();
    expect(screen.queryByText("SHOULD NOT SHOW")).toBeNull();
    fireEvent.change(screen.getByLabelText("message"), { target: { value: "Also read main.go" } });
    fireEvent.click(screen.getByRole("button", { name: /Steer/ }));
    await waitFor(() => expect(daemon.paramsOf("step.steer")).toEqual({ stepId: 12, text: "Also read main.go" }));
    act(() => daemon.emit({ subscriptionId: "s-live", seq: 8, kind: "step.steer.queued", at, payload: { stepId: 12, agent: "surveyor", steerId: 41, hasText: true } }));
    expect(await screen.findByText("Also read main.go")).toBeTruthy();
  });
});

// --- S42: the FIFO history window's load-older affordance ----------------------------------

describe("the agent chat stream — loading older turns", () => {
  it("offers a manual fallback when there is more to load, and calls back when it's clicked", () => {
    const onLoadOlder = vi.fn();
    render(<AgentChat items={items} hasOlder onLoadOlder={onLoadOlder} />);
    fireEvent.click(screen.getByText("Load earlier turns"));
    expect(onLoadOlder).toHaveBeenCalledTimes(1);
  });

  it("shows a loading indicator instead of the button while fetching, and neither once nothing is left to load", () => {
    const { rerender } = render(<AgentChat items={items} hasOlder loadingOlder onLoadOlder={vi.fn()} />);
    expect(screen.getByText("Loading earlier turns…")).toBeTruthy();
    expect(screen.queryByText("Load earlier turns")).toBeNull();

    rerender(<AgentChat items={items} hasOlder={false} loadingOlder={false} onLoadOlder={vi.fn()} />);
    expect(screen.queryByText("Loading earlier turns…")).toBeNull();
    expect(screen.queryByText("Load earlier turns")).toBeNull();
  });

  it("fetches the window before it when the reader scrolls near the top", () => {
    const onLoadOlder = vi.fn();
    render(<AgentChat items={items} hasOlder onLoadOlder={onLoadOlder} />);
    const el = screen.getByTestId("agent-chat-stream");
    Object.defineProperty(el, "scrollTop", { value: 40, configurable: true, writable: true });
    Object.defineProperty(el, "scrollHeight", { value: 2000, configurable: true, writable: true });
    Object.defineProperty(el, "clientHeight", { value: 400, configurable: true, writable: true });
    fireEvent.scroll(el);
    expect(onLoadOlder).toHaveBeenCalledTimes(1);
  });

  it("does not fetch again while already loading, or once nothing older is left", () => {
    const onLoadOlder = vi.fn();
    const { rerender } = render(<AgentChat items={items} hasOlder loadingOlder onLoadOlder={onLoadOlder} />);
    const el = screen.getByTestId("agent-chat-stream");
    Object.defineProperty(el, "scrollTop", { value: 10, configurable: true, writable: true });
    Object.defineProperty(el, "scrollHeight", { value: 2000, configurable: true, writable: true });
    Object.defineProperty(el, "clientHeight", { value: 400, configurable: true, writable: true });
    fireEvent.scroll(el);
    expect(onLoadOlder).not.toHaveBeenCalled();

    rerender(<AgentChat items={items} hasOlder={false} loadingOlder={false} onLoadOlder={onLoadOlder} />);
    fireEvent.scroll(el);
    expect(onLoadOlder).not.toHaveBeenCalled();
  });

  it("holds the viewport's visual position steady when older turns land, instead of pinning to the bottom", () => {
    const older: ChatItem = { id: "older1", at, agent: "surveyor", kind: "text", text: "An earlier turn." };
    const { rerender } = render(<AgentChat items={items} hasOlder onLoadOlder={vi.fn()} />);
    const el = screen.getByTestId("agent-chat-stream");
    Object.defineProperty(el, "scrollHeight", { value: 1000, configurable: true, writable: true });
    Object.defineProperty(el, "scrollTop", { value: 300, configurable: true, writable: true });
    Object.defineProperty(el, "clientHeight", { value: 400, configurable: true, writable: true });

    // The fetch starts: the anchor captures today's height before anything changes.
    rerender(<AgentChat items={items} hasOlder loadingOlder onLoadOlder={vi.fn()} />);

    // It lands: taller content, prepended — simulated here by growing scrollHeight
    // the way the real DOM would once the extra rows are in it.
    Object.defineProperty(el, "scrollHeight", { value: 1240, configurable: true, writable: true });
    rerender(<AgentChat items={[older, ...items]} hasOlder={false} loadingOlder={false} onLoadOlder={vi.fn()} />);

    // Held steady by exactly the height the prepend added — not yanked to the bottom.
    expect(el.scrollTop).toBe(540);
  });
});

describe("a failed step's row", () => {
  it("opens to the whole message instead of cutting it short", () => {
    const reason = "the model call failed: model: openai answered 400: {\n  \"error\": { \"message\": \"Invalid 'tools[16].name'\" }\n}";
    render(<AgentChat items={[{ id: "f", kind: "divider", tone: "danger", text: `chat finished · failed · ${reason}`, detail: reason }]} />);
    expect(screen.queryByTestId("agent-chat-divider-detail")).toBeNull();
    fireEvent.click(screen.getByTestId("agent-chat-divider-toggle"));
    expect(screen.getByTestId("agent-chat-divider-detail").textContent).toBe(reason);
  });
});
