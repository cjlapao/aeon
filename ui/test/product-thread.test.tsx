import { afterEach, beforeEach, expect, it, vi } from "vitest";
import { act, cleanup, fireEvent, render, screen, waitFor, within } from "@testing-library/react";
import { MemoryRouter, Route, Routes } from "react-router-dom";
import { Thread } from "../src/features/loops/components/Thread";
import { StepScreen, Transcript } from "../src/features/loops/pages/StepScreen";
import { LiveController } from "../src/lib/live";
import { installFakeDaemon, resetFakeDaemon } from "./helpers";

beforeEach(() => { vi.stubGlobal("ResizeObserver", class { observe() {} disconnect() {} }); window.location.hash = "#/loops"; });
afterEach(() => { cleanup(); resetFakeDaemon(); window.location.hash = "#/"; vi.unstubAllGlobals(); });

describe("S9 product surfaces", () => {
  it("renders step questions and gates through the same decision card", () => {
    const loop = { id: "l1", slug: "l1", blueprint: "two-step", goal: "demo", root: "/tmp", status: "waiting",
      position: { lane: "approve" }, questionId: 7, gateAnswers: ["yes", "no"], lanes: [] };
    const first = render(<MemoryRouter><Thread loop={loop} events={[]} canWrite stale={false} onConfirmed={vi.fn()} /></MemoryRouter>);
    const html = screen.getByTestId("question-card").outerHTML;
    first.unmount();
    render(<MemoryRouter><Thread loop={{ ...loop, gateAnswers: [] }} events={[]}
      stepQuestion={{ id: 7, stepId: 4, body: "Decision needed at approve", options: ["yes", "no"] }}
      canWrite stale={false} onConfirmed={vi.fn()} /></MemoryRouter>);
    expect(screen.getByTestId("question-card").outerHTML).toBe(html);
  });

  it("does not show a refused answer and reenables its options", async () => {
    const daemon = installFakeDaemon({ responses: { "loop.answer": () => { throw new Error("answer rejected"); } } });
    const loop = { id: "l1", slug: "l1", blueprint: "two-step", goal: "demo", root: "/tmp", status: "waiting",
      position: { lane: "approve" }, questionId: 7, gateAnswers: ["yes", "no"], lanes: [] };
    render(<MemoryRouter><Thread loop={loop} events={[]} canWrite stale={false} onConfirmed={vi.fn()} /></MemoryRouter>);
    fireEvent.click(screen.getByRole("button", { name: "yes" }));
    await waitFor(() => expect(screen.getByRole("alert").textContent).toContain("answer rejected"));
    expect(screen.getByRole("button", { name: "yes" }).hasAttribute("disabled")).toBe(false);
    expect(screen.queryByText("You: yes")).toBeNull();
    expect(daemon.methods().filter((method) => method === "loop.answer")).toHaveLength(1);
  });

  it("shows a step question's JSON-RPC refusal without claiming acceptance", async () => {
    installFakeDaemon({ responses: { answer: () => { throw new Error("question 7 was already answered"); } } });
    const loop = { id: "l1", slug: "l1", blueprint: "two-step", goal: "demo", root: "/tmp", status: "waiting",
      position: { lane: "survey" }, questionId: 7, gateAnswers: [], lanes: [] };
    render(<MemoryRouter><Thread loop={loop} events={[]}
      stepQuestion={{ id: 7, stepId: 4, body: "Which approach?", options: ["in-place", "new-table"] }}
      canWrite stale={false} onConfirmed={vi.fn()} /></MemoryRouter>);
    fireEvent.click(screen.getByRole("button", { name: "new-table" }));
    await waitFor(() => expect(screen.getByRole("alert").textContent).toContain("already answered"));
    expect(screen.queryByRole("status")).toBeNull();
    expect(screen.getByRole("button", { name: "in-place" }).hasAttribute("disabled")).toBe(false);
  });

  it("keeps two windows honest when one gate answer wins before its event arrives", async () => {
    let calls = 0;
    installFakeDaemon({ responses: { "loop.answer": () => {
      calls += 1;
      if (calls === 2) throw new Error("gate 7 was already answered");
      return { status: "running" };
    } } });
    const loop = { id: "l1", slug: "l1", blueprint: "two-step", goal: "demo", root: "/tmp", status: "waiting",
      position: { lane: "approve" }, questionId: 7, gateAnswers: ["approve", "abort"], lanes: [] };
    render(<MemoryRouter><div data-testid="window-one"><Thread loop={loop} events={[]} canWrite stale={false}
      onConfirmed={vi.fn().mockResolvedValue(undefined)} /></div><div data-testid="window-two"><Thread loop={loop} events={[]}
      canWrite stale={false} onConfirmed={vi.fn().mockResolvedValue(undefined)} /></div></MemoryRouter>);
    const one = within(screen.getByTestId("window-one"));
    const two = within(screen.getByTestId("window-two"));
    fireEvent.click(one.getByRole("button", { name: "approve" }));
    await waitFor(() => expect(one.getByRole("status").textContent).toContain("Answer accepted by the daemon"));
    fireEvent.click(two.getByRole("button", { name: "abort" }));
    await waitFor(() => expect(two.getByRole("alert").textContent).toContain("already answered"));
    expect(two.queryByRole("status")).toBeNull();
    expect(two.getByRole("button", { name: "abort" }).hasAttribute("disabled")).toBe(false);
    expect(two.getByRole("list").textContent).not.toContain("You");
    expect(calls).toBe(2);
  });

  it("keeps an accepted answer outside the daemon-sequenced event list", async () => {
    let accept!: (value: object) => void;
    installFakeDaemon({ responses: { "loop.answer": () => new Promise((resolve) => { accept = resolve; }) } });
    const loop = { id: "l1", slug: "l1", blueprint: "two-step", goal: "demo", root: "/tmp", status: "waiting",
      position: { lane: "approve" }, questionId: 7, gateAnswers: ["yes", "no"], lanes: [] };
    render(<MemoryRouter><Thread loop={loop} events={[{ subscriptionId: "s1", seq: 3, kind: "loop.waiting",
      at: "2026-10-18T08:00:00Z", payload: { loopId: "l1", questionId: 7, prompt: "Continue?" } }]}
      canWrite stale={false} onConfirmed={vi.fn().mockResolvedValue(undefined)} /></MemoryRouter>);
    fireEvent.click(screen.getByRole("button", { name: "yes" }));
    expect(screen.queryByRole("status")).toBeNull();
    expect(screen.getByRole("button", { name: "yes" }).hasAttribute("disabled")).toBe(true);
    await act(async () => accept({ status: "running" }));
    expect(screen.getByRole("status").textContent).toContain("Answer accepted by the daemon: yes");
    expect(screen.getByRole("list").textContent).not.toContain("Answer accepted");
  });

  it("sends a pending decision only once and blocks answers while stale", async () => {
    let accept!: (value: object) => void;
    const daemon = installFakeDaemon({ responses: { "loop.answer": () => new Promise((resolve) => { accept = resolve; }) } });
    const loop = { id: "l1", slug: "l1", blueprint: "two-step", goal: "demo", root: "/tmp", status: "waiting",
      position: { lane: "approve" }, questionId: 7, gateAnswers: ["yes", "no"], lanes: [] };
    const view = render(<MemoryRouter><Thread loop={loop} events={[]} canWrite stale={false}
      onConfirmed={vi.fn().mockResolvedValue(undefined)} /></MemoryRouter>);
    fireEvent.click(screen.getByRole("button", { name: "yes" }));
    fireEvent.click(screen.getByRole("button", { name: "yes" }));
    expect(daemon.methods().filter((method) => method === "loop.answer")).toHaveLength(1);
    expect(screen.getByRole("button", { name: "yes" }).hasAttribute("disabled")).toBe(true);
    await act(async () => accept({ status: "running" }));
    view.rerender(<MemoryRouter><Thread loop={loop} events={[]} canWrite stale
      onConfirmed={vi.fn()} /></MemoryRouter>);
    expect(screen.getByText(/Answers are paused until the daemon reconnects/)).toBeTruthy();
    fireEvent.click(screen.getByRole("button", { name: "no" }));
    expect(daemon.methods().filter((method) => method === "loop.answer")).toHaveLength(1);
  });

  it("orders readable thread events by sequence and keeps the question in place", () => {
    const loop = { id: "l1", slug: "l1", blueprint: "two-step", goal: "demo", root: "/tmp", status: "waiting",
      position: { lane: "approve" }, questionId: 7, gateAnswers: ["yes", "no"], lanes: [] };
    render(<MemoryRouter><Thread loop={loop} events={[
      { subscriptionId: "s1", seq: 9, kind: "loop.waiting", at: "2026-10-18T08:00:00Z", payload: { loopId: "l1", questionId: 7, prompt: "Continue?" } },
      { subscriptionId: "s1", seq: 3, kind: "loop.started", at: "2026-10-18T09:00:00Z", payload: { loopId: "l1" } },
      { subscriptionId: "s1", seq: 8, kind: "step.tool.end", at: "2026-10-18T08:30:00Z", payload: { stepId: 4 } },
    ]} canWrite stale={false} onConfirmed={vi.fn()} /></MemoryRouter>);
    const entries = [...screen.getByRole("list").querySelectorAll("li")];
    expect(entries.map((item) => item.textContent?.slice(0, 2))).toEqual(["#3", "#9"]);
    expect(entries[0]!.textContent).toContain("Loop started");
    expect(entries[1]!.textContent).toContain("Decision requested");
    expect(entries[1]!.querySelector('[data-testid="question-card"]')).toBeTruthy();
    expect(screen.queryByText("loop.waiting")).toBeNull();
  });

  it("places confirmed step and gate answers in daemon sequence order", () => {
    const loop = { id: "l1", slug: "l1", blueprint: "two-step", goal: "demo", root: "/tmp", status: "done",
      position: { lane: "approve" }, questionId: 0, gateAnswers: [], lanes: [] };
    render(<MemoryRouter><Thread loop={loop} events={[
      { subscriptionId: "s1", seq: 6, kind: "loop.advanced", at: "2026-10-18T08:00:06Z", payload: { loopId: "l1", lane: "approve", questionId: 8, answer: "approve", outcome: "approve" } },
      { subscriptionId: "s1", seq: 4, kind: "question.answered", at: "2026-10-18T08:00:04Z", payload: { stepId: 4, questionId: 7, answer: "in-place" } },
      { subscriptionId: "s1", seq: 3, kind: "loop.waiting", at: "2026-10-18T08:00:03Z", payload: { loopId: "l1", questionId: 7, prompt: "Which approach?" } },
    ]} canWrite stale={false} onConfirmed={vi.fn()} /></MemoryRouter>);
    const entries = [...screen.getByRole("list").querySelectorAll("li")];
    expect(entries.map((item) => item.textContent?.slice(0, 2))).toEqual(["#3", "#4", "#6"]);
    expect(entries[1]!.textContent).toContain("YouStep 4in-place");
    expect(entries[2]!.textContent).toContain("Youapprove");
  });

  it("keeps the step question text after it is answered without a duplicate waiting row", () => {
    const loop = { id: "l1", slug: "l1", blueprint: "two-step", goal: "demo", root: "/tmp", status: "done",
      position: { lane: "survey" }, questionId: 0, gateAnswers: [], lanes: [] };
    render(<MemoryRouter><Thread loop={loop} events={[
      { subscriptionId: "s1", seq: 3, kind: "question.asked", at: "2026-10-18T08:00:03Z", payload: { id: 7, stepId: 4, body: "Which approach?", options: ["in-place"] } },
      { subscriptionId: "s1", seq: 4, kind: "loop.waiting", at: "2026-10-18T08:00:04Z", payload: { loopId: "l1", questionId: 7 } },
      { subscriptionId: "s1", seq: 5, kind: "question.answered", at: "2026-10-18T08:00:05Z", payload: { stepId: 4, questionId: 7, answer: "in-place" } },
    ]} canWrite stale={false} onConfirmed={vi.fn()} /></MemoryRouter>);
    expect(screen.getByText("Which approach?")).toBeTruthy();
    expect(screen.getAllByText("Decision requested")).toHaveLength(1);
    expect([...screen.getByRole("list").querySelectorAll("li")].map((item) => item.textContent?.slice(0, 2))).toEqual(["#3", "#5"]);
  });

  it("shows the durable gate prompt when its waiting event was pruned", () => {
    const loop = { id: "l1", slug: "l1", blueprint: "two-step", goal: "demo", root: "/tmp", status: "waiting",
      position: { lane: "approve" }, questionId: 7, gateAnswers: ["approve", "abort"], gatePrompt: "Approve the survey?", lanes: [] };
    render(<MemoryRouter><Thread loop={loop} events={[]} canWrite stale={false} onConfirmed={vi.fn()} /></MemoryRouter>);
    expect(screen.getByText("Approve the survey?")).toBeTruthy();
    expect(screen.getByRole("button", { name: "approve" })).toBeTruthy();
  });

  it("shows full tool output on expand and hides thinking signatures", () => {
    const output = "long-result-".repeat(100);
    render(<Transcript messages={[{ role: "assistant", content: [
      { kind: "thinking", signature: "secret-signature" },
      { kind: "tool_result", name: "read", text: output },
    ] }]} />);
    expect(screen.getByText(/Thinking occurred/)).toBeTruthy();
    expect(screen.queryByText(/secret-signature/)).toBeNull();
    fireEvent.click(screen.getByText(/read:/));
    expect(screen.getByText((text) => text.includes(output))).toBeTruthy();
  });

  it("grows a running step from events without rereading the transcript", async () => {
    const daemon = installFakeDaemon({ responses: { "step.show": { id: 4, agent: "surveyor", phase: "build", status: "running",
      cost: { state: "unknown", reason: "usage pending" }, messages: [] }, "subscribe": { subscriptionId: "s1", from: 0 } } });
    render(<MemoryRouter initialEntries={["/steps/4"]}><Routes><Route path="/steps/:id" element={<StepScreen
      controller={new LiveController()} live={{ state: "current", endpoint: "/tmp/aeon.sock", daemonVersion: "1", protocolVersion: 1, generation: 1 }} />}
      /></Routes></MemoryRouter>);
    await screen.findByText("No transcript entries yet.");
    await waitFor(() => expect(daemon.methods()).toContain("subscribe"));
    const before = daemon.methods().filter((method) => method === "step.show").length;
    act(() => daemon.emit({ kind: "model.delta", seq: 3, at: "2026-10-19T08:00:00Z", payload: { stepId: 4, text: "Checking package imports" } }));
    expect(screen.getByLabelText("Live transcript events").textContent).toContain("Checking package imports");
    expect(daemon.methods().filter((method) => method === "step.show")).toHaveLength(before);
  });

  it("shows both case B cost components and an uncertain total", async () => {
    installFakeDaemon({ responses: { "step.show": { id: 4, agent: "surveyor", phase: "build", status: "done",
      cost: { state: "unknown", reason: "stream-aborted-early", usdAtLeast: 0.02 }, messages: [],
      recovery: { case: "B", reissued: true, priorCost: { state: "unknown", reason: "stream-aborted-early" },
        reissueCost: { state: "known", usd: 0.02 } } }, "subscribe": { subscriptionId: "s1", from: 0 } } });
    render(<MemoryRouter initialEntries={["/steps/4"]}><Routes><Route path="/steps/:id" element={<StepScreen
      controller={new LiveController()} live={{ state: "current", endpoint: "/tmp/aeon.sock", daemonVersion: "1", protocolVersion: 1, generation: 1 }} />}
      /></Routes></MemoryRouter>);
    expect(await screen.findByText(/Before crash, including an interrupted call/)).toBeTruthy();
    // S18 T10: both figures now carry Go's MoneyString form — five decimals
    // (D16: two renderers, one money function; the partial/known clause and
    // the floor clause both go through `money`).
    expect(screen.getByText(/Reissued work after restart: \$0\.02000/)).toBeTruthy();
    expect(screen.getByText(/Cost: .*at least \$0\.02000 was measured/)).toBeTruthy();
  });

  it("shows a stored compaction summary cost separately within the step total", async () => {
    installFakeDaemon({ responses: { "step.show": { id: 4, agent: "surveyor", phase: "build", status: "done",
      cost: { state: "partial", usdAtLeast: 0.23 }, contextManifest: { files: [], compactions: [
        { start: 1, end: 3, estimateBefore: 12000, estimateAfter: 1200,
          summary: "Earlier work summarized", cost: { state: "partial", usdAtLeast: 0.02 } },
      ] }, messages: [] }, "subscribe": { subscriptionId: "s1", from: 0 } } });
    render(<MemoryRouter initialEntries={["/steps/4"]}><Routes><Route path="/steps/:id" element={<StepScreen
      controller={new LiveController()} live={{ state: "current", endpoint: "/tmp/aeon.sock", daemonVersion: "1", protocolVersion: 1, generation: 1 }} />}
      /></Routes></MemoryRouter>);
    expect(await screen.findByText(/Summary model call: at least \$0\.0200/)).toBeTruthy();
    expect(screen.getByText(/Cost: at least \$0\.2300/)).toBeTruthy();
    expect(screen.getByText(/Included in the step total/)).toBeTruthy();
    fireEvent.click(screen.getByText("Show summary"));
    expect(screen.getByText("Earlier work summarized")).toBeTruthy();
  });
});
