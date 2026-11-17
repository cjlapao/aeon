import { afterEach, beforeEach, describe, expect, it } from "vitest";
import { act, cleanup, render, screen, waitFor } from "@testing-library/react";
import { useLoopWorkspace } from "../src/features/loop-workspace/useLoopWorkspace";
import type { LiveState } from "../src/lib/live";
import { installFakeDaemon, resetFakeDaemon } from "./helpers";
import { LoopBlockedBanner } from "../src/features/loop-workspace/LoopBlockedBanner";

// The loop page, from the data hook up. Each later task adds its own describe.

beforeEach(() => resetFakeDaemon());
afterEach(() => { cleanup(); resetFakeDaemon(); });

const current: LiveState = { state: "current", endpoint: "/tmp/aeon.sock", daemonVersion: "1", protocolVersion: 1, generation: 1 };
const loopShow = {
  id: "loop-1", slug: "two-step", blueprint: "two-step", goal: "Survey", root: "/repo", status: "running",
  position: { lane: "survey" },
  lanes: [{ id: "survey", status: "running", steps: [{ id: "survey", kind: "model", status: "running", stepId: 12 }] },
    { id: "approve", status: "not-started", steps: [{ id: "approve", kind: "gate", status: "not-started" }] }],
};
const at = "2026-10-25T10:00:00Z";

function Probe({ live = current }: { live?: LiveState }) {
  const ws = useLoopWorkspace("loop-1", live);
  return <div>
    <span data-testid="loaded">{ws.loop ? ws.loop.blueprint : "none"}</span>
    <span data-testid="connected">{String(ws.connected)}</span>
    <span data-testid="error">{ws.error ?? ""}</span>
    <ul data-testid="items">{ws.items.map((i) => <li key={i.id}>{i.kind === "tool" ? `${i.tool}:${i.summary}` : i.kind}</li>)}</ul>
  </div>;
}

describe("useLoopWorkspace", () => {
  it("reads the loop and streams only its own events", async () => {
    const daemon = installFakeDaemon({ responses: { "loop.show": loopShow, subscribe: { subscriptionId: "s1", from: 0 }, "loop.transcript": { messages: [], hasOlder: false, hasNewer: false } } });
    render(<Probe />);
    await waitFor(() => expect(screen.getByTestId("loaded").textContent).toBe("two-step"));
    await waitFor(() => expect(daemon.methods()).toContain("subscribe"));
    act(() => daemon.emit({ subscriptionId: "s1", seq: 5, kind: "loop.step.dispatched", at, payload: { loopId: "loop-1", step: "survey", stepId: 12, agent: "surveyor" } }));
    act(() => daemon.emit({ subscriptionId: "s1", seq: 6, kind: "step.tool.start", at, payload: { stepId: 12, agent: "surveyor", tool: "read", summary: "go.mod", toolUseId: "t1" } }));
    act(() => daemon.emit({ subscriptionId: "s1", seq: 7, kind: "step.tool.start", at, payload: { stepId: 99, agent: "other", tool: "bash", summary: "NOT MINE", toolUseId: "t2" } }));
    expect(await screen.findByText("read:go.mod")).toBeTruthy();
    expect(screen.queryByText(/NOT MINE/)).toBeNull();
  });

  it("a loop with nothing in the ring still has its steps", async () => {
    installFakeDaemon({ responses: { "loop.show": loopShow, subscribe: { subscriptionId: "s1", from: 0 }, "loop.transcript": { messages: [], hasOlder: false, hasNewer: false } } });
    render(<Probe />);
    await waitFor(() => expect(screen.getByTestId("loaded").textContent).toBe("two-step"));
    expect(screen.getByTestId("items").children).toHaveLength(0);
    expect(screen.getByTestId("error").textContent).toBe("");
  });

  it("keeps a step the snapshot names even when its dispatch aged out of the ring", async () => {
    const daemon = installFakeDaemon({ responses: { "loop.show": loopShow, subscribe: { subscriptionId: "s1", from: 0 }, "loop.transcript": { messages: [], hasOlder: false, hasNewer: false } } });
    render(<Probe />);
    await waitFor(() => expect(screen.getByTestId("loaded").textContent).toBe("two-step"));
    await waitFor(() => expect(daemon.methods()).toContain("subscribe"));
    act(() => daemon.emit({ subscriptionId: "s1", seq: 9, kind: "step.tool.start", at, payload: { stepId: 12, agent: "surveyor", tool: "list", summary: ".", toolUseId: "t9" } }));
    expect(await screen.findByText("list:.")).toBeTruthy();
  });

  it("re-reads loop.show when the loop moves", async () => {
    const daemon = installFakeDaemon({ responses: { "loop.show": loopShow, subscribe: { subscriptionId: "s1", from: 0 }, "loop.transcript": { messages: [], hasOlder: false, hasNewer: false } } });
    render(<Probe />);
    await waitFor(() => expect(screen.getByTestId("loaded").textContent).toBe("two-step"));
    await waitFor(() => expect(daemon.methods()).toContain("subscribe"));
    const before = daemon.methods().filter((m) => m === "loop.show").length;
    act(() => daemon.emit({ subscriptionId: "s1", seq: 10, kind: "loop.advanced", at, payload: { loopId: "loop-1", lane: "survey", outcome: "ok" } }));
    await waitFor(() => expect(daemon.methods().filter((m) => m === "loop.show").length).toBe(before + 1));
  });

  it("reports a dropped connection and keeps the last state", async () => {
    installFakeDaemon({ responses: { "loop.show": loopShow, subscribe: { subscriptionId: "s1", from: 0 }, "loop.transcript": { messages: [], hasOlder: false, hasNewer: false } } });
    const { rerender } = render(<Probe />);
    await waitFor(() => expect(screen.getByTestId("loaded").textContent).toBe("two-step"));
    rerender(<Probe live={{ state: "disconnected", endpoint: "/tmp/aeon.sock", reason: "socket closed" }} />);
    expect(screen.getByTestId("connected").textContent).toBe("false");
    expect(screen.getByTestId("loaded").textContent).toBe("two-step");
  });
});

// --- Task 4: the agents panel in the workflow rail -------------------------------------------

import { fireEvent, within } from "@testing-library/react";
import { chatItemsFromEvents, deriveSession, type ChatEventLike } from "../src/components/agent-chat";
import { AgentsPanel } from "../src/features/loop-workspace/rail/AgentsPanel";

const panelEvents: ChatEventLike[] = [
  { seq: 1, kind: "loop.step.dispatched", at, payload: { loopId: "loop-1", lane: "survey", step: "survey", stepId: 11, agent: "surveyor" } },
  { seq: 2, kind: "loop.step.dispatched", at, payload: { loopId: "loop-1", lane: "survey", step: "survey", stepId: 12, agent: "linter" } },
  { seq: 3, kind: "step.tool.start", at, payload: { stepId: 11, agent: "surveyor", tool: "write_file", summary: "SURVEY.md", toolUseId: "t1" } },
  { seq: 4, kind: "step.done", at, payload: { stepId: 12, agent: "linter", outcome: "ok" } },
];
const panelItems = chatItemsFromEvents(panelEvents);
const panelRuns = deriveSession(panelEvents).runs;
const footprint = { perStep: [{ lane: "start", costUsd: 0.0001, partial: false }, { lane: "survey", costUsd: 0.0011, partial: false }, { lane: "approve", costUsd: undefined, partial: false }], totalUsd: 0.0012, partial: false };

function panel(over: Partial<React.ComponentProps<typeof AgentsPanel>> = {}) {
  return render(<AgentsPanel collapsed={false} runs={panelRuns} items={panelItems} selected="" onSelect={() => {}} footprint={footprint} live {...over} />);
}

describe("the agents panel", () => {
  it("lists the loop view first, then each agent with its state and latest activity", () => {
    panel();
    expect(screen.getByText("2 Total")).toBeTruthy();
    const rows = screen.getAllByRole("button");
    expect(rows.map((r) => r.getAttribute("aria-label"))).toEqual(["Loop view – all activity", "surveyor", "linter"]);
    const surveyor = within(screen.getByRole("button", { name: "surveyor" }));
    expect(surveyor.getByText("running")).toBeTruthy();
    expect(surveyor.getByText("write_file SURVEY.md")).toBeTruthy();
    expect(within(screen.getByRole("button", { name: "linter" })).getByText("done")).toBeTruthy();
  });

  it("selecting an agent zooms the chat, selecting the loop view returns, and the selection is marked", () => {
    const picked: string[] = [];
    const { rerender } = panel({ onSelect: (a) => picked.push(a), selected: "linter" });
    expect(screen.getByRole("button", { name: "linter" }).getAttribute("aria-pressed")).toBe("true");
    expect(screen.getByRole("button", { name: "surveyor" }).getAttribute("aria-pressed")).toBe("false");
    fireEvent.click(screen.getByRole("button", { name: "surveyor" }));
    fireEvent.click(screen.getByRole("button", { name: "Loop view – all activity" }));
    expect(picked).toEqual(["surveyor", ""]);
    rerender(<AgentsPanel collapsed={false} runs={panelRuns} items={panelItems} selected="" onSelect={() => {}} footprint={footprint} live />);
    expect(screen.getByRole("button", { name: "Loop view – all activity" }).getAttribute("aria-pressed")).toBe("true");
  });

  it("collapsed is a column of avatars with accessible names and none of the expanded text", () => {
    const picked: string[] = [];
    panel({ collapsed: true, onSelect: (a) => picked.push(a) });
    expect(screen.getAllByRole("button").map((b) => b.getAttribute("aria-label"))).toEqual(["Loop view", "surveyor", "linter"]);
    expect(screen.queryByText("ORCHESTRATED AGENTS")).toBeNull();
    expect(screen.queryByText("write_file SURVEY.md")).toBeNull();
    fireEvent.click(screen.getByRole("button", { name: "linter" }));
    expect(picked).toEqual(["linter"]);
  });

  it("shows the footprint as cost per step and in total, and says when the total is only a floor", () => {
    const { unmount } = panel();
    expect(screen.getByText("$0.0012")).toBeTruthy();
    expect(screen.getByText("LOOP FOOTPRINT")).toBeTruthy();
    expect(screen.getAllByTestId("footprint-bar")).toHaveLength(2); // the lane with no cost has no bar
    unmount();
    panel({ footprint: { ...footprint, partial: true } });
    expect(screen.getByText("at least $0.0012")).toBeTruthy();
  });

  it("says so when there is no agent or no cost yet", () => {
    panel({ runs: [], footprint: { perStep: [], totalUsd: undefined, partial: false } });
    expect(screen.getByText("0 Total")).toBeTruthy();
    expect(screen.getByText("No cost reported yet")).toBeTruthy();
  });
});

// --- Task 5: the centre — chat wiring, git status, the page bar ------------------------------

import { renderHook } from "@testing-library/react";
import { vi } from "vitest";
import { useChatActions } from "../src/features/loop-workspace/center/useChatActions";
import { useGitStatus } from "../src/features/loop-workspace/center/useGitStatus";
import { PageBar } from "../src/features/loop-workspace/center/PageBar";

const chatEvents: ChatEventLike[] = [
  { seq: 1, kind: "loop.step.dispatched", at, payload: { loopId: "loop-1", lane: "survey", step: "survey", stepId: 12, agent: "surveyor" } },
  { seq: 2, kind: "question.asked", at, payload: { id: 601, stepId: 12, agent: "surveyor", kind: "permission", body: "Allow?", options: ["deny", "approve"] } },
];

function actionsHost(responses: Record<string, unknown> = {}) {
  const daemon = installFakeDaemon({ responses: { "step.steer": { steerId: 41, state: "pending" }, ...responses } });
  const host = { loopId: "loop-1", session: deriveSession(chatEvents), rememberSteer: vi.fn(), refresh: vi.fn(async () => {}) };
  const onDiff = vi.fn();
  const trace = vi.fn();
  const { result } = renderHook(() => useChatActions(host, "/repo", onDiff, trace));
  return { daemon, host, onDiff, trace, chat: result.current };
}

describe("useChatActions", () => {
  it("steers the agent's running step, sends only what the person changed, and remembers the text", async () => {
    const { daemon, host, chat } = actionsHost();
    await chat.onSteer({ target: "surveyor", text: "Also read main.go" });
    expect(daemon.paramsOf("step.steer")).toEqual({ stepId: 12, text: "Also read main.go" });
    expect(host.rememberSteer).toHaveBeenCalledWith(41, "Also read main.go");
    await chat.onSteer({ target: "surveyor", text: "", route: "anthropic/opus", reasoningEffort: "high" });
    expect(daemon.paramsOf("step.steer")).toEqual({ stepId: 12, route: "anthropic/opus", reasoningEffort: "high" });
    expect(host.rememberSteer).toHaveBeenCalledTimes(1);
  });

  it("refuses to steer or stop an agent that has no running step, saying which", async () => {
    const { chat } = actionsHost();
    await expect(chat.onSteer({ target: "linter", text: "x" })).rejects.toThrow("linter has no running step");
    await expect(chat.onStop("linter")).rejects.toThrow("linter has no running step");
  });

  it("stops by step id", async () => {
    const { daemon, chat } = actionsHost();
    await chat.onStop("surveyor");
    expect(daemon.paramsOf("step.stop")).toEqual({ stepId: 12 });
  });

  it("answers a question with the step that asked it, and a gate by loop, then re-reads the loop", async () => {
    const { daemon, host, chat } = actionsHost();
    await chat.actions.onAnswer!({ id: "601", prompt: "Allow?", kind: "permission" }, "approve");
    expect(daemon.paramsOf("answer")).toEqual({ stepId: 12, questionId: 601, answer: "approve" });
    await chat.actions.onGate!("gate:90", "approve");
    expect(daemon.paramsOf("loop.answer")).toEqual({ id: "loop-1", answer: "approve" });
    expect(host.refresh).toHaveBeenCalled();
  });

  it("sends a gate's feedback note with the answer, and nothing extra without one", async () => {
    const { daemon, chat } = actionsHost();
    await chat.actions.onGate!("gate:90", "revise", "Add the packages list.");
    expect(daemon.paramsOf("loop.answer")).toEqual({ id: "loop-1", answer: "revise", feedback: "Add the packages list." });
    await chat.actions.onGate!("gate:90", "approve");
    expect(daemon.paramsOf("loop.answer")).toEqual({ id: "loop-1", answer: "approve" });
  });

  it("withdraws a steer and fetches a diff", async () => {
    const { daemon, onDiff, chat } = actionsHost({ "workspace.git.diff": { state: "ok", diff: "+ new line" } });
    await chat.actions.onCancelSteer!(7);
    expect(daemon.paramsOf("step.steer.cancel")).toEqual({ steerId: 7 });
    await chat.actions.onViewDiff!({ path: "SURVEY.md" });
    expect(daemon.paramsOf("workspace.git.diff")).toEqual({ componentRoot: "/repo", path: "SURVEY.md" });
    expect(onDiff).toHaveBeenCalledWith("SURVEY.md", "+ new line");
  });

  it("traces every call it makes", async () => {
    const { trace, chat } = actionsHost();
    await chat.onStop("surveyor");
    expect(trace.mock.calls.flat().join("\n")).toMatch(/step\.stop/);
  });
});

describe("useGitStatus", () => {
  it("reads the status, and again when the refresh key moves", async () => {
    const daemon = installFakeDaemon({ responses: { "workspace.git.status": { state: "ok", branch: "main", changed: [{ path: "a", index: " ", worktree: "M" }], ahead: 1, behind: 0 } } });
    const { result, rerender } = renderHook(({ key }) => useGitStatus("/repo", key), { initialProps: { key: 0 } });
    await waitFor(() => expect(result.current?.state).toBe("ok"));
    expect(daemon.paramsOf("workspace.git.status")).toEqual({ componentRoot: "/repo" });
    rerender({ key: 1 });
    await waitFor(() => expect(daemon.methods().filter((m) => m === "workspace.git.status")).toHaveLength(2));
  });

  it("turns a failed call into an honest 'unavailable', not a thrown error or an empty repo", async () => {
    installFakeDaemon({ responses: { "workspace.git.status": () => { throw new Error("git is not installed"); } } });
    const { result } = renderHook(() => useGitStatus("/repo", 0));
    await waitFor(() => expect(result.current?.state).toBe("unavailable"));
    expect(result.current?.reason).toMatch(/git is not installed/);
  });
});

describe("the page bar", () => {
  const bar = (over: Record<string, unknown> = {}) => render(<PageBar title="Loop view" status="running · survey"
    git={{ state: "ok", branch: "main", ahead: 1, behind: 0, changed: [{ path: "a", index: " ", worktree: "M" }, { path: "b", index: "?", worktree: "?" }, { path: "c", index: "A", worktree: " " }] }}
    terminalOpen={false} onToggleTerminal={() => {}} inspectorOpen={false} onToggleInspector={() => {}} {...over} />);

  it("shows the git state as branch, changed count and commits ahead", () => {
    bar();
    expect(screen.getByTestId("git-pill").textContent).toMatch(/main.*3 changed.*↑1/);
  });

  it("says so for a workspace that is not a repository or where git could not answer", () => {
    const { unmount } = bar({ git: { state: "not-a-repo" } });
    expect(screen.getByTestId("git-pill").textContent).toBe("not a git repository");
    unmount();
    bar({ git: { state: "unavailable", reason: "git is not installed" } });
    expect(screen.getByTestId("git-pill").textContent).toBe("git unavailable");
    expect(screen.getByTestId("git-pill").getAttribute("title")).toBe("git is not installed");
  });

  it("toggles the terminal and the inspector, and shows their state", () => {
    const calls: string[] = [];
    const { unmount } = bar({ onToggleTerminal: () => calls.push("terminal"), onToggleInspector: () => calls.push("inspector") });
    fireEvent.click(screen.getByRole("button", { name: "Terminal" }));
    fireEvent.click(screen.getByRole("button", { name: "Toggle inspector" }));
    expect(calls).toEqual(["terminal", "inspector"]);
    expect(screen.getByRole("button", { name: "Terminal" }).getAttribute("aria-pressed")).toBe("false");
    unmount();
    bar({ terminalOpen: true, inspectorOpen: true });
    expect(screen.getByRole("button", { name: "Terminal" }).getAttribute("aria-pressed")).toBe("true");
  });

  it("zoomed to an agent, the title is a way back to the loop view", () => {
    const back = vi.fn();
    bar({ agent: "surveyor", onBackToLoop: back });
    fireEvent.click(screen.getByRole("button", { name: "Loop view" }));
    expect(back).toHaveBeenCalled();
    expect(screen.getByText("surveyor")).toBeTruthy();
  });

  it("opens a menu with the classic view", () => {
    const picked = vi.fn();
    bar({ menu: [{ label: "Classic view", onSelect: picked }] });
    fireEvent.click(screen.getByRole("button", { name: "More" }));
    fireEvent.click(screen.getByRole("menuitem", { name: "Classic view" }));
    expect(picked).toHaveBeenCalled();
  });
});

// --- Task 6: the inspector and its Step tab --------------------------------------------------

import { Inspector } from "../src/features/loop-workspace/inspector/Inspector";
import { StepTab } from "../src/features/loop-workspace/inspector/StepTab";
import { stepTelemetry, workflowFromLoop } from "../src/features/loop-workspace/workspaceModel";
import { derive, loop as fixtureLoop } from "./loop-fixtures";
// `MemoryRouter` is imported once, further down this file (react-router-dom) — ES imports are
// file-scoped regardless of where the `import` line sits, so it's already in scope here.

function inspector(over: Record<string, unknown> = {}) {
  const calls: string[] = [];
  const view = render(<Inspector tab="step" onTabChange={(t: string) => calls.push(`tab:${t}`)}
    openFiles={[{ path: "SURVEY.md", added: 18 }]} onCloseFile={(p: string) => calls.push(`close:${p}`)}
    step={<p>step body</p>} files={<p>files body</p>} file={(p: string) => <p>file body {p}</p>} {...over} />);
  return { calls, ...view };
}

describe("the inspector", () => {
  // Collapse (the rail strip), width and resize belong to the docked
  // SidePanel itself now — see the screen tests; the body only draws the
  // tab strip and the active tab.
  it("shows Step, Files and a tab per open file with its added lines, and the active tab's body", () => {
    const { calls } = inspector();
    expect(screen.getAllByRole("tab").map((t) => t.textContent)).toEqual(["Step", "Files", "SURVEY.md+18"]);
    expect(screen.getByRole("tab", { name: /Step/ }).getAttribute("aria-selected")).toBe("true");
    expect(screen.getByText("step body")).toBeTruthy();
    fireEvent.click(screen.getByRole("tab", { name: "Files" }));
    fireEvent.click(screen.getByRole("button", { name: "Close SURVEY.md" }));
    expect(calls).toEqual(["tab:files", "close:SURVEY.md"]);
  });

  it("shows a file tab's own body", () => {
    inspector({ tab: "file:SURVEY.md" });
    expect(screen.getByText("file body SURVEY.md")).toBeTruthy();
    expect(screen.queryByText("step body")).toBeNull();
  });
});

describe("the step tab", () => {
  const tab = (over: Record<string, unknown> = {}, loopOver = {}) => {
    const { items, session } = derive();
    const l = fixtureLoop(loopOver);
    const wf = workflowFromLoop(l, session, items);
    const opened: string[] = [];
    const view = render(<MemoryRouter><StepTab telemetry={stepTelemetry(l, session, items, "survey")} step={wf.steps.find((s) => s.id === "survey")!}
      position={{ index: 2, total: 4 }} onOpenFile={(p) => opened.push(p)} {...over} /></MemoryRouter>);
    return { opened, ...view };
  };

  it("shows the telemetry, the gate that follows, the agents and calls under SUB-STEPS, and the tool invocations", () => {
    tab();
    expect(screen.getByText("Step 2 of 4")).toBeTruthy();
    expect(screen.getByText("STEP TELEMETRY")).toBeTruthy();
    expect(screen.getByText("2 running")).toBeTruthy();
    expect(screen.getByText("SURVEY.md", { selector: "[data-testid=produces]" })).toBeTruthy();
    expect(screen.getByText("NEXT: APPROVAL GATE")).toBeTruthy();
    expect(screen.getByText(/review and approve SURVEY\.md before step summarise/)).toBeTruthy();
    const subs = screen.getByTestId("sub-steps");
    expect(within(subs).getByText("surveyor")).toBeTruthy();
    expect(within(subs).getByText("linter")).toBeTruthy();
    expect(screen.getByText("TOOL INVOCATIONS")).toBeTruthy();
    expect(screen.getByText("3 calls")).toBeTruthy();
  });

  it("lists the files changed and opens one", () => {
    const { opened } = tab();
    expect(screen.getByText("1 file")).toBeTruthy();
    fireEvent.click(screen.getByRole("button", { name: /SURVEY\.md.*\+18/ }));
    expect(opened).toEqual(["SURVEY.md"]);
  });

  it("does not show a cost it was not given as $0", () => {
    const bare = { lanes: [{ id: "survey", status: "running", structure: "direct", steps: [{ id: "survey", kind: "model-step", status: "running", stepId: 11 }] }] };
    tab({}, bare);
    expect(screen.getByTestId("cost").textContent).toMatch(/not reported/);
    expect(screen.queryByText(/\$0/)).toBeNull();
  });

  it("omits the sub-steps, tools and gate sections it has nothing for", () => {
    const empty = { status: "not-started", activeAgents: 0, tools: [], filesChanged: [] };
    render(<MemoryRouter><StepTab telemetry={empty} step={{ id: "summarise", label: "summarise", status: "not_started" }} position={{ index: 4, total: 4 }} onOpenFile={() => {}} /></MemoryRouter>);
    expect(screen.queryByText("SUB-STEPS")).toBeNull();
    expect(screen.queryByText("TOOL INVOCATIONS")).toBeNull();
    expect(screen.queryByText("FILES CHANGED")).toBeNull();
    expect(screen.queryByText(/NEXT: APPROVAL GATE/)).toBeNull();
  });

  // The live chat above is a bounded, shared ring (internal/protocol/events.go) that forgets —
  // this link to `step.show`'s durable transcript is the only way back to an agent's old
  // conversation once its turns have aged out of it, which is what this whole tab exists for.
  it("links each agent sub-step to its own full, persisted conversation", () => {
    tab();
    const links = within(screen.getByTestId("sub-steps")).getAllByTestId("open-step-conversation");
    expect(links.length).toBeGreaterThan(0);
    for (const link of links) expect(link.getAttribute("href")).toMatch(/^\/steps\/\d+$/);
  });

  it("gives the surveyor and linter sub-steps their own distinct step links", () => {
    tab();
    const subs = screen.getByTestId("sub-steps");
    const surveyorRow = within(subs).getByText("surveyor").closest("li")!;
    const linterRow = within(subs).getByText("linter").closest("li")!;
    expect(within(surveyorRow).getByRole("link").getAttribute("href")).toBe("/steps/11");
    expect(within(linterRow).getByRole("link").getAttribute("href")).toBe("/steps/12");
  });
});

// --- Task 7: the Files tab (kit Tree) and the open-file tab ----------------------------------

import { FilesTab } from "../src/features/loop-workspace/inspector/FilesTab";
import { FileTab } from "../src/features/loop-workspace/inspector/FileTab";

const rootListing = { entries: [
  { name: "cmd", path: "cmd", kind: "directory" }, { name: "SURVEY.md", path: "SURVEY.md", kind: "file" },
  { name: "go.mod", path: "go.mod", kind: "file" }, { name: ".git", path: ".git", kind: "directory" },
  { name: "node_modules", path: "node_modules", kind: "directory" }, { name: ".gitignore", path: ".gitignore", kind: "file" },
], truncated: false };
const shell = {
  component_list: (a: Record<string, unknown> | undefined) => a?.path === "."
    ? rootListing : { entries: [{ name: "main.go", path: "cmd/main.go", kind: "file" }], truncated: false },
  component_read: (a: Record<string, unknown> | undefined) => ({ kind: "code", content: `# contents of ${String(a?.path)}\nline two`, dataUrl: null, reason: null }),
};

describe("the files tab", () => {
  beforeEach(() => {
    vi.stubGlobal("ResizeObserver", class { observe() {} disconnect() {} });
    vi.stubGlobal("matchMedia", () => ({ matches: false, addEventListener() {}, removeEventListener() {} }));
  });
  afterEach(() => vi.unstubAllGlobals());

  const scope = new Map<string, "read" | "created" | "modified">([["go.mod", "read"], ["SURVEY.md", "created"], ["cmd/main.go", "modified"]]);
  const changes = [{ path: "SURVEY.md", linesAdded: 18, linesRemoved: 0 }];
  const files = (over: Record<string, unknown> = {}) => {
    const opened: string[] = [];
    const view = render(<FilesTab componentRoot="/repo" scope={scope} changes={changes} git={{ state: "ok", changed: [] }} onOpenFile={(p) => opened.push(p)} {...over} />);
    return { opened, ...view };
  };

  it("lists the root, hides what a project tree should not show, and loads a folder when it is opened", async () => {
    const daemon = installFakeDaemon({ shell, responses: { "workspace.git.diff": { state: "ok", diff: "" } } });
    files();
    expect(await screen.findByRole("treeitem", { name: "cmd" })).toBeTruthy();
    expect(screen.getByRole("treeitem", { name: "go.mod" })).toBeTruthy();
    expect(screen.getByRole("treeitem", { name: ".gitignore" })).toBeTruthy();
    expect(screen.queryByRole("treeitem", { name: ".git" })).toBeNull();
    expect(screen.queryByRole("treeitem", { name: "node_modules" })).toBeNull();
    fireEvent.click(screen.getByRole("treeitem", { name: "cmd" }));
    expect(await screen.findByRole("treeitem", { name: "main.go" })).toBeTruthy();
    expect(daemon.shellCalls.filter((c) => c.cmd === "component_list").map((c) => c.args!.path)).toEqual([".", "cmd"]);
  });

  it("summarises the files in scope and the lines added", async () => {
    installFakeDaemon({ shell });
    files();
    expect(await screen.findByText("3 files in scope (1 created)")).toBeTruthy();
    expect(screen.getByTestId("scope-added").textContent).toBe("+18");
  });

  it("marks a file the loop wrote, and one it only read", async () => {
    installFakeDaemon({ shell });
    const { container } = files();
    await screen.findByRole("treeitem", { name: "SURVEY.md" });
    const created = container.querySelector("[data-marker=created]")!;
    expect(created.closest("[role=treeitem]")!.textContent).toContain("SURVEY.md");
    const read = container.querySelectorAll("[data-marker=read]");
    expect(read).toHaveLength(1);
    expect(read[0]!.closest("[role=treeitem]")!.textContent).toContain("go.mod");
  });

  it("a workspace that is not a repository still marks files from the loop's own writes, and git changes mark the rest", async () => {
    installFakeDaemon({ shell });
    const none = files({ git: { state: "not-a-repo" } });
    await screen.findByRole("treeitem", { name: "SURVEY.md" });
    expect(none.container.querySelectorAll("[data-marker=created]").length).toBeGreaterThan(0);
    none.unmount();
    const repo = files({ scope: new Map(), changes: [], git: { state: "ok", changed: [{ path: "go.mod", index: " ", worktree: "M" }] } });
    await screen.findByRole("treeitem", { name: "go.mod" });
    expect(repo.container.querySelectorAll("[data-marker=modified]")).toHaveLength(1);
  });

  it("filters the tree", async () => {
    installFakeDaemon({ shell });
    files();
    await screen.findByRole("treeitem", { name: "go.mod" });
    fireEvent.change(screen.getByPlaceholderText("Filter files…"), { target: { value: "go.m" } });
    expect(screen.getByRole("treeitem", { name: "go.mod" })).toBeTruthy();
    expect(screen.queryByRole("treeitem", { name: "SURVEY.md" })).toBeNull();
  });

  it("selecting a file shows its card with what was written, its diff, and opens it", async () => {
    installFakeDaemon({ shell, responses: { "workspace.git.diff": { state: "ok", diff: "+ # Survey\n+ Module: hello" } } });
    const { opened } = files();
    fireEvent.click(await screen.findByRole("treeitem", { name: "SURVEY.md" }));
    const card = await screen.findByTestId("file-card");
    expect(within(card).getByText("SURVEY.md")).toBeTruthy();
    expect(within(card).getByText("created")).toBeTruthy();
    expect(within(card).getByText("+18")).toBeTruthy();
    expect(await within(card).findByText("+ # Survey")).toBeTruthy();
    fireEvent.click(within(card).getByRole("button", { name: "Open" }));
    expect(opened).toEqual(["SURVEY.md"]);
  });

  it("falls back to the file's own first lines when git has no diff for it", async () => {
    installFakeDaemon({ shell, responses: { "workspace.git.diff": { state: "not-a-repo", diff: "" } } });
    files({ git: { state: "not-a-repo" } });
    fireEvent.click(await screen.findByRole("treeitem", { name: "go.mod" }));
    expect(await screen.findByText("# contents of go.mod")).toBeTruthy();
  });

  it("asks the shell nothing until the loop's workspace root is known", async () => {
    const daemon = installFakeDaemon({ shell });
    files({ componentRoot: "" });
    await act(async () => { await Promise.resolve(); });
    expect(daemon.shellCalls).toHaveLength(0);
    expect(screen.queryByRole("alert")).toBeNull();
  });

  it("says why a folder could not be listed", async () => {
    installFakeDaemon({ shell: { ...shell, component_list: () => { throw new Error("Path is not a directory"); } } });
    files();
    expect((await screen.findByRole("alert")).textContent).toMatch(/Path is not a directory/);
  });
});

describe("the open-file tab", () => {
  it("shows the diff when git has one, the file's text otherwise, and a failure's reason", async () => {
    installFakeDaemon({ shell, responses: { "workspace.git.diff": { state: "ok", diff: "+ added line" } } });
    const diff = render(<FileTab componentRoot="/repo" path="SURVEY.md" />);
    expect((await screen.findByTestId("diff-view")).textContent).toContain("added line");
    diff.unmount();
    installFakeDaemon({ shell, responses: { "workspace.git.diff": { state: "not-a-repo", diff: "" } } });
    const text = render(<FileTab componentRoot="/repo" path="go.mod" />);
    expect(await screen.findByText(/# contents of go\.mod/)).toBeTruthy();
    text.unmount();
    installFakeDaemon({ shell: { ...shell, component_read: () => { throw new Error("Cannot read file"); } }, responses: { "workspace.git.diff": { state: "not-a-repo", diff: "" } } });
    render(<FileTab componentRoot="/repo" path="x.bin" />);
    expect((await screen.findByRole("alert")).textContent).toMatch(/Cannot read file/);
  });
});

// --- Task 8: the screen ------------------------------------------------------------------------

import { MemoryRouter, Route, Routes, useLocation } from "react-router-dom";
import { HeaderSlot, HeaderSlotProvider } from "../src/app/HeaderSlot";
import { APP_ROUTES, layoutFor } from "../src/app/routes";
import { LoopWorkspaceScreen } from "../src/features/loop-workspace/LoopWorkspaceScreen";
import { LiveController } from "../src/lib/live";
import { events as fixtureEvents, loop as screenLoop } from "./loop-fixtures";

function Where() { return <span data-testid="where">{useLocation().pathname}</span>; }

function screenOf(opts: { live?: LiveState; loop?: Record<string, unknown>; narrow?: boolean; evs?: boolean } = {}) {
  vi.stubGlobal("ResizeObserver", class { observe() {} disconnect() {} });
  vi.stubGlobal("matchMedia", (q: string) => ({ matches: Boolean(opts.narrow) && q.includes("max-width"), addEventListener() {}, removeEventListener() {}, media: q }));
  const daemon = installFakeDaemon({ shell, responses: {
    "loop.show": screenLoop(opts.loop as never), subscribe: { subscriptionId: "s1", from: 0 }, "routes.list": { routes: [{ id: "anthropic/sonnet", provider: "anthropic", model: "sonnet" }] },
    "loop.transcript": { messages: [], hasOlder: false, hasNewer: false },
    "workspace.git.status": { state: "ok", branch: "main", ahead: 1, behind: 0, changed: [{ path: "SURVEY.md", index: "?", worktree: "?" }] },
    "workspace.git.diff": { state: "ok", diff: "+ # Survey" }, "terminal.list": [], "terminal.open": { terminalId: "t-1", shell: "/bin/zsh", cwd: "/repo" },
  } });
  const view = render(<MemoryRouter initialEntries={["/loops/loop-1"]}><HeaderSlotProvider>
    <div data-testid="header"><HeaderSlot side="start" /><HeaderSlot side="end" /></div>
    <Routes>
      <Route path="/loops/:id" element={<LoopWorkspaceScreen controller={new LiveController()} live={opts.live ?? current} canWrite />} />
      <Route path="/loops/:id/classic" element={<p>classic page</p>} />
      <Route path="/" element={<p>loops list</p>} />
    </Routes><Where />
  </HeaderSlotProvider></MemoryRouter>);
  const emitAll = async () => {
    await waitFor(() => expect(daemon.methods()).toContain("subscribe"));
    for (const e of fixtureEvents()) act(() => daemon.emit({ subscriptionId: "s1", seq: e.seq + 100, kind: e.kind, at: e.at, payload: e.payload }));
  };
  return { daemon, emitAll, ...view };
}

describe("the loop workspace screen", () => {
  beforeEach(() => { try { window.localStorage.clear(); } catch { /* none */ } });
  afterEach(() => vi.unstubAllGlobals());

  it("puts the breadcrumb, the engine state and the nav in the app header — with Telemetry and Agents not yet available and no Config", async () => {
    screenOf();
    const header = within(screen.getByTestId("header"));
    expect(await header.findByText("two-step")).toBeTruthy();
    expect(header.getByText("repo")).toBeTruthy();
    expect(header.getByText("ENGINE READY")).toBeTruthy();
    expect(header.getByRole("link", { name: "Loop" }).getAttribute("aria-current")).toBe("page");
    expect(header.getByRole("button", { name: "Telemetry" }).hasAttribute("disabled")).toBe(true);
    expect(header.getByRole("button", { name: "Agents" }).getAttribute("title")).toBe("Coming next");
    expect(header.queryByText("Config")).toBeNull();
  });

  it("the classic page is still routed, and reachable from the page's menu", async () => {
    expect(layoutFor("/loops/loop-1")).toBe("workspace");
    expect(layoutFor("/loops/loop-1/classic")).toBe("page");
    expect(APP_ROUTES.find((r) => r.id === "loopClassic")?.path).toBe("/loops/:id/classic");
    screenOf();
    fireEvent.click(await screen.findByRole("button", { name: "More" }));
    fireEvent.click(screen.getByRole("menuitem", { name: "Classic view" }));
    expect(await screen.findByText("classic page")).toBeTruthy();
    expect(screen.getByTestId("where").textContent).toBe("/loops/loop-1/classic");
  });

  it("starts with the rail and the inspector open in a wide window — the current step on screen immediately — and both closed in a narrow one", async () => {
    const wide = screenOf();
    expect(await screen.findByRole("button", { name: "Collapse workflow rail" })).toBeTruthy();
    expect(screen.getByRole("button", { name: "Collapse panel" })).toBeTruthy();
    expect(await screen.findByRole("tab", { name: "Step" })).toBeTruthy();
    wide.unmount();
    screenOf({ narrow: true });
    expect(await screen.findByRole("button", { name: "Expand workflow rail" })).toBeTruthy();
    expect(screen.getByRole("button", { name: "Expand panel" })).toBeTruthy();
  });

  it("the collapsed inspector is the panel's rail strip, and its toggle opens the body", async () => {
    screenOf({ narrow: true });
    fireEvent.click(await screen.findByRole("button", { name: "Expand panel" }));
    expect(await screen.findByRole("tab", { name: "Step" })).toBeTruthy();
  });

  it("remembers the inspector being closed", async () => {
    screenOf();
    fireEvent.click(await screen.findByRole("button", { name: "Toggle inspector" }));
    await waitFor(() => expect(window.localStorage.getItem("aeon.loop.inspector")).toBe("false"));
  });

  it("a loop with nothing in the ring still shows its steps and says the chat is empty", async () => {
    screenOf();
    expect(await screen.findByText("survey")).toBeTruthy(); // the rail's step, from loop.show
    expect(screen.getByText(/no activity for this loop/i)).toBeTruthy();
  });

  it("selecting an agent in the rail zooms the chat to it, and the loop view brings everything back", async () => {
    const s = screenOf();
    await s.emitAll();
    await screen.findByRole("button", { name: "linter" });
    // Interleaved agents form consecutive groups: surveyor, linter, surveyor.
    expect(screen.getAllByTestId("agent-chat-agent-header")).toHaveLength(3);
    fireEvent.click(screen.getByRole("button", { name: "linter" }));
    expect(screen.queryAllByTestId("agent-chat-agent-header")).toHaveLength(0);
    expect(within(screen.getByTestId("page-bar")).getByText("linter")).toBeTruthy();
    fireEvent.click(screen.getByRole("button", { name: "Loop view – all activity" }));
    expect(screen.getAllByTestId("agent-chat-agent-header")).toHaveLength(3);
  });

  it("a finished loop has no message box", async () => {
    const s = screenOf({ loop: { status: "done" } });
    await s.emitAll();
    act(() => s.daemon.emit({ subscriptionId: "s1", seq: 900, kind: "loop.finished", at: "2026-10-25T11:00:00Z", payload: { loopId: "loop-1", status: "done" } }));
    await screen.findByText(/Loop finished/);
    expect(screen.queryByTestId("agent-chat-composer")).toBeNull();
  });

  it("says why steering is off while the daemon is not connected", async () => {
    screenOf({ live: { state: "disconnected", endpoint: "/tmp/aeon.sock", reason: "socket closed" } });
    expect(await screen.findByPlaceholderText(/disconnected from the daemon/i)).toBeTruthy();
  });

  it("opens a file the step changed as its own inspector tab, with its diff", async () => {
    const s = screenOf();
    await s.emitAll();
    // The inspector now starts open on a wide window — the step tab showing
    // the changed file is already on screen, with nothing to toggle first.
    fireEvent.click(await screen.findByRole("button", { name: /SURVEY\.md.*\+18/ }));
    const tab = await screen.findByRole("tab", { name: /SURVEY\.md/ });
    expect(tab.getAttribute("aria-selected")).toBe("true");
    expect((await screen.findByTestId("diff-view")).textContent).toContain("# Survey");
  });

  it("the Terminal button opens a shell in the project root", async () => {
    const s = screenOf();
    fireEvent.click(await screen.findByRole("button", { name: "Terminal" }));
    await waitFor(() => expect(s.daemon.paramsOf("terminal.open")).toEqual({ componentRoot: "/repo", cols: 80, rows: 24 }));
  });
});

describe("a loop parked on a question (S76)", () => {
  it("draws the question the store still owes an answer for, though the ring never saw it", async () => {
    const waiting = { ...loopShow, status: "waiting" };
    installFakeDaemon({ responses: { "loop.show": waiting, subscribe: { subscriptionId: "s1", from: 0 }, "loop.transcript": { messages: [], hasOlder: false, hasNewer: false },
      "question.pending": { pending: true, question: { id: 77, kind: "clarify", prompt: "Which scope do you want?", answers: ["root file", "scoped files"], stepId: 12 } } } });
    function QuestionProbe() {
      const ws = useLoopWorkspace("loop-1", current);
      return <ul data-testid="kinds">{ws.items.map((i) => <li key={i.id}>{i.kind === "question" ? `question:${i.questions[0]?.prompt}:${i.questions[0]?.id}` : i.kind}</li>)}</ul>;
    }
    render(<QuestionProbe />);
    expect(await screen.findByText("question:Which scope do you want?:77")).toBeTruthy();
  });

  it("draws a parked gate from loop.show itself", async () => {
    const waiting = { ...loopShow, status: "waiting", questionId: 5, gateAnswers: ["approve", "reject"], gatePrompt: "Ship it?" };
    installFakeDaemon({ responses: { "loop.show": waiting, subscribe: { subscriptionId: "s1", from: 0 }, "loop.transcript": { messages: [], hasOlder: false, hasNewer: false } } });
    function GateProbe() {
      const ws = useLoopWorkspace("loop-1", current);
      return <ul>{ws.items.map((i) => <li key={i.id}>{i.kind === "gate" ? `gate:${i.title}` : i.kind}</li>)}</ul>;
    }
    render(<GateProbe />);
    expect(await screen.findByText("gate:Ship it?")).toBeTruthy();
  });
});

describe("a reopened chat recovers what the loop is still owed (S76)", () => {
  const base = { subscribe: { subscriptionId: "s1", from: 0 }, "loop.transcript": { messages: [], hasOlder: false, hasNewer: false } };
  function Kinds() {
    const ws = useLoopWorkspace("loop-1", current);
    return <ul>{ws.items.map((i) => <li key={i.id}>{i.kind === "question"
      ? `question:${i.questions.map((q) => `${q.prompt}=${q.answer ?? "?"}`).join("|")}`
      : i.kind === "steer" ? `steer:${i.text}:${i.state}` : i.kind}</li>)}</ul>;
  }

  it("draws the WHOLE block of questions as one card, the answered ones keeping their answers", async () => {
    installFakeDaemon({ responses: { ...base, "loop.show": { ...loopShow, status: "waiting" },
      "loop.questions": { questions: [
        { id: 1, stepId: 12, kind: "clarify", prompt: "Which scope?", answers: ["root", "scoped"], answer: "root", askedAt: at, answeredAt: at },
        { id: 2, stepId: 12, kind: "clarify", prompt: "Which tone?", answers: ["terse", "full"], askedAt: at },
      ] } } });
    render(<Kinds />);
    expect(await screen.findByText("question:Which scope?=root|Which tone?=?")).toBeTruthy();
  });

  it("brings back an instruction that was queued and never delivered", async () => {
    installFakeDaemon({ responses: { ...base, "loop.show": { ...loopShow, status: "running" },
      "step.steers": [{ steerId: 9, state: "pending", text: "also check the tests", createdAt: at }] } });
    render(<Kinds />);
    expect(await screen.findByText("steer:also check the tests:queued")).toBeTruthy();
  });

  it("does not draw a question twice when the ring still holds it", async () => {
    const daemon = installFakeDaemon({ responses: { ...base, "loop.show": { ...loopShow, status: "waiting" },
      "loop.questions": { questions: [{ id: 3, stepId: 12, kind: "clarify", prompt: "Live one?", answers: ["y"], askedAt: at }] } } });
    render(<Kinds />);
    await waitFor(() => expect(daemon.methods()).toContain("subscribe"));
    act(() => daemon.emit({ subscriptionId: "s1", seq: 8, kind: "loop.step.dispatched", at, payload: { loopId: "loop-1", step: "survey", stepId: 12, agent: "surveyor" } }));
    act(() => daemon.emit({ subscriptionId: "s1", seq: 9, kind: "question.asked", at, payload: { id: 3, kind: "clarify", body: "Live one?", options: ["y"], stepId: 12, agent: "surveyor", loopId: "loop-1" } }));
    await waitFor(() => expect(screen.getAllByText(/^question:Live one\?/)).toHaveLength(1));
  });
});

describe("a blocked loop (S76)", () => {
  it("says why, and resumes through the daemon's own verdict", async () => {
    const daemon = installFakeDaemon({ responses: {
      "loop.control": { loopId: "loop-1", status: "blocked", eligibility: { stop: { eligible: false }, cancel: { eligible: true }, raise: { eligible: false }, resume: { eligible: true } }, openRequests: [], events: [] },
      "loop.resume": { how: "re-run", loopId: "loop-1", resumed: true, status: "running" } } });
    const refreshed = vi.fn();
    render(<LoopBlockedBanner loop={{ ...loopShow, status: "blocked", lastReason: "exceeded MaxIterations (12)" } as never} canWrite onResumed={refreshed} />);
    expect(screen.getByText("exceeded MaxIterations (12)")).toBeTruthy();
    await waitFor(() => expect((screen.getByTestId("loop-blocked-resume") as HTMLButtonElement).disabled).toBe(false));
    fireEvent.click(screen.getByTestId("loop-blocked-note-toggle"));
    fireEvent.change(screen.getByLabelText("Note for the agent"), { target: { value: "keep going" } });
    fireEvent.click(screen.getByTestId("loop-blocked-resume"));
    await waitFor(() => expect(daemon.methods()).toContain("loop.resume"));
    expect(daemon.paramsOf("loop.resume")).toEqual({ id: "loop-1", message: "keep going" });
    await waitFor(() => expect(refreshed).toHaveBeenCalled());
  });

  it("shows the daemon's refusal and no working button when resume is not eligible", async () => {
    installFakeDaemon({ responses: { "loop.control": { loopId: "loop-1", status: "blocked", eligibility: {
      stop: { eligible: false }, cancel: { eligible: true }, raise: { eligible: false },
      resume: { eligible: false, refusal: { kind: "not-resumable", detail: "this loop was cancelled" } } }, openRequests: [], events: [] } } });
    render(<LoopBlockedBanner loop={{ ...loopShow, status: "blocked" } as never} canWrite onResumed={() => {}} />);
    expect((await screen.findByTestId("loop-blocked-refusal")).textContent).toBe("this loop was cancelled");
    expect((screen.getByTestId("loop-blocked-resume") as HTMLButtonElement).disabled).toBe(true);
  });
});

// A reopened loop is drawn from its journal (loop-replay): the page reads the loop's own stored events, then
// subscribes from the daemon's head, so nothing is missed and nothing is read twice.
describe("a reopened loop replays from its journal", () => {
  const journalRow = (seq: number, kind: string, payload: Record<string, unknown>) => ({ seq, kind, at, payload });
  const journal = {
    events: [
      journalRow(101, "loop.step.dispatched", { loopId: "loop-1", step: "survey", stepId: 12, agent: "surveyor" }),
      journalRow(102, "step.turn.completed", { stepId: 12, loopId: "loop-1", agent: "surveyor", iteration: 1, text: "Surveyed the repo.", thinking: "Look first." }),
      journalRow(103, "step.tool.start", { stepId: 12, loopId: "loop-1", agent: "surveyor", tool: "read", summary: "go.mod", toolUseId: "t1" }),
      journalRow(104, "step.tool.end", { stepId: 12, loopId: "loop-1", tool: "read", toolUseId: "t1", result: "ok", durationMs: 40 }),
    ],
    hasOlder: false, hasNewer: false, head: 104,
  };

  it("reads the loop's journal, subscribes after its head, and draws the turn and the call with no live events", async () => {
    const daemon = installFakeDaemon({ responses: { "loop.show": loopShow, "loop.events": journal, subscribe: { subscriptionId: "s1", from: 105 }, "loop.transcript": { messages: [], hasOlder: false, hasNewer: false } } });
    render(<Probe />);
    expect(await screen.findByText("read:go.mod")).toBeTruthy();
    expect(screen.getByTestId("items").textContent).toContain("text"); // the finished turn, from the journal
    expect(daemon.methods()).toContain("loop.events");
    expect(daemon.paramsOf("subscribe")).toMatchObject({ after: 104 });
    expect(daemon.paramsOf("loop.events")).toMatchObject({ id: "loop-1" });
  });

  it("falls back to the old way on a daemon that has no journal", async () => {
    const daemon = installFakeDaemon({ responses: { "loop.show": loopShow, subscribe: { subscriptionId: "s1", from: 0 }, "loop.transcript": { messages: [], hasOlder: false, hasNewer: false } } });
    render(<Probe />);
    await waitFor(() => expect(daemon.methods()).toContain("subscribe"));
    expect(daemon.paramsOf("subscribe")).toMatchObject({ after: 0 });
  });

  it("shows the loop running once every question is answered and the agent has acted", async () => {
    const waiting = { ...loopShow, status: "waiting" };
    const rows = {
      events: [
        journalRow(201, "question.asked", { id: 7, stepId: 12, loopId: "loop-1", kind: "permission", body: "ok?" }),
        journalRow(202, "question.answered", { questionId: 7, stepId: 12, answer: "approve" }),
        journalRow(203, "step.tool.start", { stepId: 12, loopId: "loop-1", tool: "bash", toolUseId: "t9", summary: "ls" }),
      ], hasOlder: false, hasNewer: false, head: 203,
    };
    installFakeDaemon({ responses: { "loop.show": waiting, "loop.events": rows, subscribe: { subscriptionId: "s1", from: 204 }, "loop.transcript": { messages: [], hasOlder: false, hasNewer: false } } });
    function Status() { const ws = useLoopWorkspace("loop-1", current); return <span data-testid="status">{ws.loop?.status ?? "none"}</span>; }
    render(<Status />);
    await waitFor(() => expect(screen.getByTestId("status").textContent).toBe("running"));
  });
});
