import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import { act, cleanup, fireEvent, render, screen, waitFor, within } from "@testing-library/react";
import { MemoryRouter, Route, Routes } from "react-router-dom";
import { AgentChat, ChatComposer, type ChatItem } from "../src/components/agent-chat";
import { HeaderSlot, HeaderSlotProvider } from "../src/app/HeaderSlot";
import { LoopWorkspaceScreen } from "../src/features/loop-workspace/LoopWorkspaceScreen";
import { AttendednessNotice } from "../src/features/loop-workspace/AttendednessNotice";
import { RunBlueprintPanel } from "../src/features/loop-workspace/RunBlueprintPanel";
import type { RunOption } from "../src/features/loop-workspace/center/useChatActions";
import { LiveController, type LiveState } from "../src/lib/live";
import { installFakeDaemon, resetFakeDaemon, type FakeDaemon } from "./helpers";
import { loop as screenLoop } from "./loop-fixtures";

// S39 T11 — the composer on an idle loop, the thread and run cards, and the
// *Run blueprint…* dialogue. The pre-existing suites (agent-chat-component,
// loop-workspace) pin that steer mode and the finished-loop rule are
// UNTOUCHED; this file pins what the slice adds.

beforeEach(() => resetFakeDaemon());
afterEach(() => { cleanup(); resetFakeDaemon(); });

const at = "2026-10-25T10:00:00Z";
const current: LiveState = { state: "current", endpoint: "/tmp/aeon.sock", daemonVersion: "1", protocolVersion: 1, generation: 1 };

describe("the thread and run cards", () => {
  it("message source chip shows for template, not for user", () => {
    const items: ChatItem[] = [
      { id: "m1", kind: "message", author: "human", text: "kickoff words", source: "template" },
      { id: "m2", kind: "message", author: "human", text: "typed words" },
      { id: "m3", kind: "message", author: "human", text: "explicit user", source: "user" },
    ];
    render(<AgentChat items={items} height="400px" />);
    expect(screen.getAllByTestId("agent-chat-message")).toHaveLength(3);
    expect(screen.getByText("from template")).toBeTruthy();
    expect(screen.queryByText(/from user/)).toBeNull(); // typed needs no label
    expect(screen.queryByText(/from undefined/)).toBeNull();
  });

  it("run card shows a floor with ≥ when the cost is not exact and never shows $0 for absent cost", () => {
    const items: ChatItem[] = [
      { id: "run:1", kind: "run", runSeq: 1, blueprint: "implement", version: 3, state: "done", startedBy: "user", cost: { usdAtLeast: 0.5, exact: false } },
      { id: "run:2", kind: "run", runSeq: 2, blueprint: "survey", state: "running", startedBy: "agent" },
    ];
    render(<AgentChat items={items} height="400px" />);
    const [card0, card1] = screen.getAllByTestId("agent-chat-run") as [HTMLElement, HTMLElement];
    expect(card0.getAttribute("data-state")).toBe("done");
    expect(within(card0).getByText("Run 1 · implement")).toBeTruthy();
    expect(within(card0).getByText("≥ $0.50")).toBeTruthy(); // the P32 floor wears its ≥
    expect(within(card0).getByText("started by user")).toBeTruthy();
    expect(within(card1).getByText("—")).toBeTruthy(); // unknown is unknown
    expect(screen.queryByText(/\$0\.00/)).toBeNull(); // never a confident zero
  });
});

describe("the composer in message mode", () => {
  it("message mode composer has no To picker and no Stop; Send calls onMessage", async () => {
    const onMessage = vi.fn(async () => undefined);
    const onSteer = vi.fn(async () => undefined);
    render(<ChatComposer mode="message" onMessage={onMessage} onSteer={onSteer}
      targets={[{ value: "surveyor", label: "surveyor" }]} routes={[]} efforts={[]} />);
    expect(screen.queryByText(/To:/)).toBeNull();
    expect(screen.queryByRole("button", { name: /Stop/ })).toBeNull();
    expect(screen.getByRole("button", { name: "Send" })).toBeTruthy();
    const box = screen.getByPlaceholderText("Message the loop…");
    fireEvent.change(box, { target: { value: "  check the flaky test  " } });
    fireEvent.click(screen.getByRole("button", { name: "Send" }));
    await waitFor(() => expect(onMessage).toHaveBeenCalledWith("check the flaky test", undefined));
    expect(onSteer).not.toHaveBeenCalled();
    // The text clears on send, like the steer composer.
    expect((screen.getByPlaceholderText("Message the loop…") as HTMLTextAreaElement).value).toBe("");
  });

  it("steer mode is unchanged: To:, Stop and the Steer button all still render", () => {
    render(<ChatComposer onSteer={vi.fn()} onStop={vi.fn()}
      targets={[{ value: "surveyor", label: "surveyor" }]} routes={[]} efforts={[]} />);
    expect(screen.getByText(/To:/)).toBeTruthy();
    expect(screen.getByRole("button", { name: /Stop/ })).toBeTruthy();
    expect(screen.getByRole("button", { name: "Steer" })).toBeTruthy();
    expect(screen.getByPlaceholderText("Message an agent…")).toBeTruthy();
  });
});

describe("the run_start ask card", () => {
  // The permission question parks on its own tool item — the ask the agent
  // earns renders THERE (ApprovalCard), on top of the daemon's body, with
  // the same approve/deny path. No second answer path (Do NOT).
  const askItem = (runStart?: { blueprint: string; goal?: string; task?: string }): ChatItem => ({
    id: "t", agent: "chat", kind: "tool", tool: "run_start", summary: "implement", status: "awaiting",
    verdict: { verdict: "asked", reason: "ask: run-start" },
    approval: { id: "41", prompt: "The agent asked to start a blueprint run.", kind: "permission", options: ["deny", "approve"], ...(runStart ? { runStart } : {}) },
  });

  it("names the blueprint and the record when the input is known, and answers approve", async () => {
    const onAnswer = vi.fn();
    render(<AgentChat items={[askItem({ blueprint: "goal-to-brief", goal: "g1" })]} actions={{ onAnswer }} height="400px" />);
    const card = screen.getByTestId("agent-chat-approval");
    expect(within(card).getByText("Start blueprint goal-to-brief on goal g1")).toBeTruthy();
    // The daemon's body is still there verbatim under the heading.
    expect(within(card).getByText("The agent asked to start a blueprint run.")).toBeTruthy();
    fireEvent.click(within(card).getByTestId("agent-chat-approve"));
    await waitFor(() => expect(onAnswer).toHaveBeenCalledWith(expect.objectContaining({ id: "41" }), "approve"));
  });

  it("falls back to naming the tool — never an invented blueprint", () => {
    render(<AgentChat items={[askItem()]} actions={{ onAnswer: vi.fn() }} height="400px" />);
    expect(screen.getByTestId("agent-chat-run-start-heading").textContent).toBe("Start a blueprint run");
    expect(screen.queryByText(/Start blueprint/)).toBeNull();
  });

  it("renders no heading at all for another tool", () => {
    render(<AgentChat items={[{ id: "t2", agent: "chat", kind: "tool", tool: "bash", summary: "git push", status: "awaiting",
      approval: { id: "42", prompt: "Allow git push?", kind: "permission", options: ["deny", "approve"] } }]}
      actions={{ onAnswer: vi.fn() }} height="400px" />);
    expect(screen.queryByTestId("agent-chat-run-start-heading")).toBeNull();
  });

  it("a run_start card in a readOnly chat cannot be answered; once answered it is a row", () => {
    // The existing rendering decides both: pending asks show their buttons
    // disabled (nobody can answer from here), and the ANSWERED card collapses
    // back to a row that says who decided — a decided run is not a card
    // anyone can still press. That is the S11 contract, run_start included.
    render(<AgentChat items={[askItem({ blueprint: "implement" })]} readOnly actions={{ onAnswer: vi.fn() }} height="400px" />);
    expect(screen.getByTestId("agent-chat-approve").hasAttribute("disabled")).toBe(true);
    cleanup();
    const answered = { ...askItem({ blueprint: "implement" }), status: "done",
      approval: { id: "41", prompt: "…", kind: "permission", options: ["deny", "approve"], runStart: { blueprint: "implement" }, answer: "approve" } } as ChatItem;
    render(<AgentChat items={[answered]} actions={{ onAnswer: vi.fn() }} height="400px" />);
    expect(screen.queryByTestId("agent-chat-approve")).toBeNull();
  });
});

describe("the attendedness notice", () => {
  it("shows the three states, each in its own words, and nothing for an absent field", () => {
    const { rerender } = render(<AttendednessNotice attended={true} />);
    let el = screen.getByTestId("attendedness");
    expect(el.getAttribute("data-state")).toBe("attended");
    expect(el.textContent).toBe("Attended — a person is here to confirm asks.");
    rerender(<AttendednessNotice attended={false} />);
    el = screen.getByTestId("attendedness");
    expect(el.getAttribute("data-state")).toBe("unattended");
    expect(el.textContent).toBe("Unattended — asks are denied at once. run_start cannot be confirmed here.");
    rerender(<AttendednessNotice attended={null} />);
    el = screen.getByTestId("attendedness");
    expect(el.getAttribute("data-state")).toBe("undeclared");
    expect(el.textContent).toBe("Never declared — treated as unattended. run_start cannot be confirmed here.");
    rerender(<AttendednessNotice attended={undefined} />);
    expect(screen.queryByTestId("attendedness")).toBeNull(); // an absent word is not a no
  });
});

// --- the workspace: D3 and D8 as the screen shows them -----------------------------------------

const shell = {
  workspace_list: () => ({ entries: [], truncated: false }),
  workspace_read: (a: Record<string, unknown> | undefined) => ({ kind: "code", content: `# contents of ${String(a?.path)}`, dataUrl: null, reason: null }),
};

function screenOf(opts: { live?: LiveState; loop?: Record<string, unknown>; responses?: Record<string, unknown | ((params: unknown) => unknown)> } = {}): { daemon: FakeDaemon } {
  vi.stubGlobal("ResizeObserver", class { observe() {} disconnect() {} });
  vi.stubGlobal("matchMedia", (q: string) => ({ matches: false, addEventListener() {}, removeEventListener() {}, media: q }));
  const daemon = installFakeDaemon({ shell, responses: {
    "loop.show": screenLoop(opts.loop as never), subscribe: { subscriptionId: "s1", from: 0 },
    "routes.list": { routes: [{ id: "anthropic/sonnet", provider: "anthropic", model: "sonnet" }] },
    "loop.transcript": { messages: [], hasOlder: false, hasNewer: false },
    "workspace.git.status": { state: "ok", branch: "main", ahead: 1, behind: 0, changed: [] },
    "workspace.git.diff": { state: "ok", diff: "" }, "terminal.list": [], "terminal.open": { terminalId: "t-1", shell: "/bin/zsh", cwd: "/repo" },
    ...opts.responses,
  } });
  render(<MemoryRouter initialEntries={["/loops/loop-1"]}><HeaderSlotProvider>
    <div data-testid="header"><HeaderSlot side="start" /><HeaderSlot side="end" /></div>
    <Routes>
      <Route path="/loops/:id" element={<LoopWorkspaceScreen controller={new LiveController()} live={opts.live ?? current} canWrite />} />
    </Routes>
  </HeaderSlotProvider></MemoryRouter>);
  return { daemon };
}

const subscribed = async (daemon: FakeDaemon) => {
  await waitFor(() => expect(daemon.methods()).toContain("subscribe"));
};

describe("the loop workspace on a resting loop", () => {
  beforeEach(() => { try { window.localStorage.clear(); } catch { /* none */ } });
  afterEach(() => vi.unstubAllGlobals());

  it("an idle loop shows the composer in message mode", async () => {
    const { daemon } = screenOf({ loop: { status: "idle" } });
    await subscribed(daemon);
    expect(await screen.findByTestId("agent-chat-composer")).toBeTruthy();
    expect(screen.getByPlaceholderText("Message the loop…")).toBeTruthy();
    // The idle loop is also where the Run blueprint… door lives.
    expect(screen.getByTestId("run-blueprint-open")).toBeTruthy();
  });

  it("a done loop still hides the composer", async () => {
    const { daemon } = screenOf({ loop: { status: "done" } });
    await subscribed(daemon);
    await waitFor(() => expect(screen.queryByTestId("agent-chat-composer")).toBeNull());
  });

  it("a running step on an idle loop flips the composer to steer", async () => {
    const { daemon } = screenOf({ loop: { status: "running" } });
    await subscribed(daemon);
    // Rest first: the run finishes and the loop rests (CR1).
    act(() => daemon.emit({ subscriptionId: "s1", seq: 50, kind: "loop.run.finished", at, payload: { loopId: "loop-1", runSeq: 1, outcome: "ok", status: "idle" } }));
    await waitFor(() => expect(screen.getByPlaceholderText("Message the loop…")).toBeTruthy());
    // D3: a turn dispatched on the resting loop — the composer steers again.
    act(() => daemon.emit({ subscriptionId: "s1", seq: 51, kind: "loop.step.dispatched", at, payload: { loopId: "loop-1", step: "turn", lane: "turn", stepId: 21, agent: "chat" } }));
    await waitFor(() => expect(screen.getByText(/To:/)).toBeTruthy());
    expect(screen.queryByPlaceholderText("Message the loop…")).toBeNull();
  });

  it("sending in message mode calls loop.message with the loop id", async () => {
    const { daemon } = screenOf({ loop: { status: "idle" } });
    await subscribed(daemon);
    const box = await screen.findByPlaceholderText("Message the loop…");
    fireEvent.change(box, { target: { value: "please run" } });
    fireEvent.click(screen.getByRole("button", { name: "Send" }));
    await waitFor(() => expect(daemon.methods()).toContain("loop.message"));
    expect(daemon.paramsOf("loop.message")).toEqual({ id: "loop-1", text: "please run" });
  });

  it("an unattended idle loop says so above the composer", async () => {
    const { daemon } = screenOf({ loop: { status: "idle", attended: false } });
    await subscribed(daemon);
    const el = await screen.findByTestId("attendedness");
    expect(el.getAttribute("data-state")).toBe("unattended");
    expect(el.textContent).toBe("Unattended — asks are denied at once. run_start cannot be confirmed here.");
    // The composer is still there — unattended is not disabled; only the
    // agent's asks are denied, the human peer's door is untouched.
    expect(screen.getByPlaceholderText("Message the loop…")).toBeTruthy();
  });

  it("a never-declared loop gets its own words, not the unattended ones", async () => {
    const { daemon } = screenOf({ loop: { status: "running", attended: null } });
    await subscribed(daemon);
    const el = await screen.findByTestId("attendedness");
    expect(el.getAttribute("data-state")).toBe("undeclared");
    expect(el.textContent).toBe("Never declared — treated as unattended. run_start cannot be confirmed here.");
  });

  it("a refused message shows the daemon's sentence", async () => {
    const { daemon } = screenOf({ loop: { status: "idle" }, responses: {
      "loop.message": () => ({ refused: { kind: "running", detail: "the loop is already running; this message is queued beside the turn", stepId: 5 } }),
    } });
    await subscribed(daemon);
    const box = await screen.findByPlaceholderText("Message the loop…");
    fireEvent.change(box, { target: { value: "still there?" } });
    fireEvent.click(screen.getByRole("button", { name: "Send" }));
    expect((await screen.findByRole("alert")).textContent).toContain("the loop is already running; this message is queued beside the turn");
  });
});

// --- the *Run blueprint…* dialogue ---------------------------------------------------------------

const OPTIONS = [
  { blueprint: "implement", version: 3, runnable: false, reason: 'blueprint "implement" needs goal approved — none was given',
    candidates: [{ kind: "goal" as const, id: "g1", title: "Add export", state: "approved" }] },
  { blueprint: "survey", version: 1, runnable: true },
];

const rowRun = (row: HTMLElement): HTMLButtonElement => within(row).getByRole("button", { name: "Run" }) as HTMLButtonElement;

describe("the Run blueprint panel", () => {
  it("unmet blueprints are listed disabled with their reason; choosing a candidate enables Run", async () => {
    const load = vi.fn(async (): Promise<RunOption[]> => OPTIONS);
    render(<RunBlueprintPanel load={load} onRun={vi.fn()} onClose={vi.fn()} />);
    expect(await screen.findByText('blueprint "implement" needs goal approved — none was given')).toBeTruthy();
    expect(screen.getAllByRole("listitem")).toHaveLength(2); // entries are NEVER hidden
    const unmet = screen.getByTestId("run-blueprint-implement");
    const met = screen.getByTestId("run-blueprint-survey");
    expect(rowRun(unmet).hasAttribute("disabled")).toBe(true);
    expect(rowRun(met).hasAttribute("disabled")).toBe(false);
    fireEvent.change(screen.getByLabelText("goal or task for implement"), { target: { value: "0" } });
    expect(rowRun(unmet).hasAttribute("disabled")).toBe(false);
  });

  it("Run sends goalId for a goal candidate and taskId for a task candidate, plus the typed message", async () => {
    const onRun = vi.fn(async () => undefined);
    const onClose = vi.fn();
    const load = vi.fn(async (): Promise<RunOption[]> => [
      OPTIONS[0]!,
      { blueprint: "fix", version: 2, runnable: false, reason: "needs task approved", candidates: [{ kind: "task", id: "t1", title: "Flake", state: "approved" }] },
    ]);
    render(<RunBlueprintPanel load={load} onRun={onRun} onClose={onClose} />);
    await screen.findByTestId("run-blueprint-implement");
    fireEvent.change(screen.getByLabelText("goal or task for implement"), { target: { value: "0" } });
    fireEvent.change(screen.getByLabelText("First message (optional)"), { target: { value: "start now" } });
    fireEvent.click(rowRun(screen.getByTestId("run-blueprint-implement")));
    await waitFor(() => expect(onRun).toHaveBeenCalledWith({ blueprint: "implement", goalId: "g1", message: "start now" }));
    await waitFor(() => expect(onClose).toHaveBeenCalled()); // success closes; loop.run.started moves the view
  });

  it("Run sends taskId for a task candidate", async () => {
    const onRunTask = vi.fn(async () => undefined);
    render(<RunBlueprintPanel load={async (): Promise<RunOption[]> => [{ blueprint: "fix", version: 2, runnable: false, reason: "needs task approved", candidates: [{ kind: "task", id: "t1", title: "Flake", state: "approved" }] }]} onRun={onRunTask} onClose={vi.fn()} />);
    fireEvent.change(await screen.findByLabelText("goal or task for fix"), { target: { value: "0" } });
    fireEvent.click(rowRun(screen.getByTestId("run-blueprint-fix")));
    await waitFor(() => expect(onRunTask).toHaveBeenCalledWith({ blueprint: "fix", taskId: "t1" }));
  });

  it("a refused run keeps the panel open and shows the sentence", async () => {
    const sentence = 'blueprint "implement" needs goal approved — none was given. You could use one of these:';
    const onRun = vi.fn(async () => { throw new Error(sentence); });
    const onClose = vi.fn();
    const load = vi.fn(async (): Promise<RunOption[]> => [OPTIONS[1]!]);
    render(<RunBlueprintPanel load={load} onRun={onRun} onClose={onClose} />);
    fireEvent.click(rowRun(await screen.findByTestId("run-blueprint-survey")));
    expect((await screen.findByRole("alert")).textContent).toContain(sentence);
    expect(screen.getByTestId("run-blueprint-panel")).toBeTruthy(); // still open — the conversation is not over
    expect(onClose).not.toHaveBeenCalled();
  });
});
