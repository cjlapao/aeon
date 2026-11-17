import { afterEach, beforeEach, expect, it, vi } from "vitest";
import { act, cleanup, fireEvent, render, screen, waitFor, within } from "@testing-library/react";
import { MemoryRouter, Route, Routes } from "react-router-dom";
import { blockedLine } from "../src/features/loops/components/Ladder";
import { LoopScreen } from "../src/features/loops/pages/LoopScreen";
import { Workflow, workflowData } from "../src/features/loops/components/Workflow";
import { LoopFiles } from "../src/features/loops/components/LoopFiles";
import { LiveController } from "../src/lib/live";
import { useState } from "react";
import { installFakeDaemon, resetFakeDaemon } from "./helpers";

beforeEach(() => { vi.stubGlobal("ResizeObserver", class { observe() {} disconnect() {} }); window.location.hash = "#/loops"; });
afterEach(() => { cleanup(); resetFakeDaemon(); window.location.hash = "#/"; vi.unstubAllGlobals(); });

describe("S9 product surfaces", () => {
  it("keeps direct model and gate steps out of substeps and opens their own details", async () => {
    installFakeDaemon({ responses: {
      "loop.show": { id: "l1", slug: "l1", blueprint: "two-step", goal: "Survey Go", root: "/tmp/work", status: "done",
        position: { lane: "approve" }, lanes: [
          { id: "survey", structure: "direct", status: "ok", produces: ["SURVEY.md"], steps: [{ id: "survey", kind: "model-step", status: "done", stepId: 7,
            cost: { state: "known", usd: 0.12 }, wallMs: 3000 }] },
          { id: "approve", structure: "direct", status: "ok", steps: [{ id: "approve", kind: "gate", status: "done", outcome: "approve" }] },
        ] },
      "step.show": { id: 7, status: "done", messages: [{ role: "assistant", content: [
        { kind: "tool_call", name: "write_file", input: { path: "SURVEY.md" } },
        { kind: "text", text: "The survey found one Go package." },
      ] }] },
      "loop.file.read": { path: "SURVEY.md", content: "# Survey\n\nOne Go package." },
      "subscribe": { subscriptionId: "s1", from: 0 },
    } });
    const live = { state: "current" as const, endpoint: "test", daemonVersion: "1", protocolVersion: 1, generation: 1 };
    render(<MemoryRouter initialEntries={["/loops/l1"]}><Routes><Route path="/loops/:id"
      element={<LoopScreen controller={new LiveController()} live={live} canWrite />} />
      <Route path="/components" element={<p>Component list</p>} /></Routes></MemoryRouter>);
    await screen.findByTestId("workflow-view");
    const tracker = screen.getByTestId("workflow-view");
    const header = within(tracker).getByTestId("workflow-summary");
    const goal = within(header).getByRole("textbox", { name: "Loop goal" }) as HTMLTextAreaElement;
    expect(goal.value).toBe("Survey Go");
    expect(goal.readOnly).toBe(true);
    expect(goal.rows).toBe(3);
    expect(goal.style.height).toBe("5.5rem");
    const topRow = within(header).getByTestId("workflow-header-row");
    expect(within(topRow).getByText("/tmp/work")).toBeTruthy();
    const rightGroup = within(topRow).getByTestId("workflow-header-right");
    expect(rightGroup.contains(within(topRow).getByText("/tmp/work"))).toBe(true);
    expect(rightGroup.textContent).toContain("done");
    const identity = within(topRow).getByTestId("workflow-header-identity");
    expect(within(identity).getByRole("heading", { name: "two-step" })).toBeTruthy();
    expect(within(identity).getByText(/Blueprint workflow/).className).toContain("text-sm");
    expect(within(topRow).queryByText("Workspace folder")).toBeNull();
    const back = within(topRow).getByRole("button", { name: "Back to component loops" });
    expect(back.querySelector("svg")).toBeTruthy();
    expect(back.querySelector("span.sr-only")?.textContent).toBe("Back to component loops");
    expect(topRow.contains(goal)).toBe(false);
    expect(goal.parentElement?.parentElement?.className).toContain("w-full");
    expect(screen.queryByRole("heading", { name: "Survey Go" })).toBeNull();
    expect(within(tracker).getAllByRole("heading", { name: "approve" })).toHaveLength(2);
    expect(workflowData({ blueprint: "two-step", status: "done", position: { lane: "approve" }, lanes: [
      { id: "survey", structure: "direct", status: "ok", steps: [{ id: "survey", kind: "model-step", status: "done" }] },
      { id: "approve", structure: "direct", status: "ok", steps: [{ id: "approve", kind: "gate", status: "done" }] },
    ] }).steps.every((step) => step.subSteps === undefined)).toBe(true);
    expect(screen.getByTestId("workflow-substep-details").textContent).toContain("Approval gate");
    expect(within(screen.getByTestId("workflow-substep-details")).queryByText("Cost")).toBeNull();
    expect(within(screen.getByTestId("workflow-substep-details")).queryByText("Step time")).toBeNull();
    expect(within(tracker).queryByText("Sub-steps")).toBeNull();
    fireEvent.click(within(tracker).getAllByRole("button", { name: /survey/i })[0]!);
    expect(within(tracker).getAllByRole("heading", { name: "survey" })).toHaveLength(2);
    expect(within(tracker).getByText(/Cost: \$0\.1200/)).toBeTruthy();
    expect(within(tracker).queryByText("Sub-steps")).toBeNull();
    const details = await screen.findByTestId("workflow-substep-details");
    expect(await within(details).findByText("The survey found one Go package.")).toBeTruthy();
    expect(await within(details).findByRole("heading", { name: "Survey" })).toBeTruthy();
    expect(screen.getByRole("link", { name: /Open full step/ }).getAttribute("href")).toBe("/steps/7");
    fireEvent.click(within(tracker).getAllByRole("button", { name: /approve/i })[0]!);
    expect(within(tracker).getAllByRole("heading", { name: "approve" })).toHaveLength(2);
    expect(screen.getByTestId("workflow-substep-details").textContent).toContain("Recorded decision: approve");
    fireEvent.click(back);
    expect(await screen.findByText("Component list")).toBeTruthy();
  });

  it("identifies an older daemon file API without claiming the workspace is empty", async () => {
    installFakeDaemon({ responses: {
      "loop.files": () => { throw JSON.stringify({ code: -32601, message: 'method "loop.files" is not registered' }); },
      "loop.file.read": () => { throw JSON.stringify({ code: -32601, message: 'method "loop.file.read" is not registered' }); },
      "loop.read": { documents: [] },
    } });
    render(<LoopFiles id="l1" slug="l1" root="/tmp/work" produced={["SURVEY.md"]}
      live={{ state: "current", endpoint: "test", daemonVersion: "old", protocolVersion: 1, generation: 1 }} />);
    expect((await screen.findByRole("alert")).textContent).toContain("Rebuild and restart the daemon");
    expect(screen.queryByText("No visible files here.")).toBeNull();
    fireEvent.click(screen.getByRole("button", { name: /SURVEY\.md/ }));
    await waitFor(() => expect(screen.getAllByRole("alert").length).toBe(2));
    expect(screen.getAllByRole("alert")[1]?.textContent).toContain("same socket and state database");
  });

  it("shows a blocked active lane even when its gate answer was saved as done", () => {
    const loop = { blueprint: "two-step", status: "blocked", position: { lane: "approve" },
      startedAt: "2026-10-19T08:00:00Z", updatedAt: "2026-10-19T08:01:00Z",
      lanes: [
        { id: "survey", structure: "direct" as const, status: "ok", steps: [{ id: "survey", kind: "model-step", status: "done", outcome: "ok", wallMs: 2000, cost: { state: "known" as const, usd: 0.1 } }] },
        { id: "approve", structure: "direct" as const, status: "ok", steps: [{ id: "approve", kind: "gate", status: "done", outcome: "revise" }] },
        { id: "summarise", structure: "direct" as const, status: "pending", steps: [{ id: "summarise", kind: "model-step", status: "pending" }] },
      ] };
    const data = workflowData(loop, "gate answer revise: reject has no rework target");
    // loop.status "blocked" never suspends anything (runtime/loop.go) — it's
    // the loop's own terminal readout, not a paused/resumable state.
    expect(data.steps[1]?.status).toBe("attention");
    expect(data.steps[1]?.subSteps).toBeUndefined();
    expect(data.steps[1]?.description).toContain("Outcome: revise");
    expect(data.steps[2]?.status).toBe("not_started");
    render(<Workflow loop={loop} reason="gate answer revise: reject has no rework target" now={Date.parse(loop.updatedAt)} current />);
    const summary = screen.getByTestId("workflow-summary");
    expect(summary.querySelector("h1")?.textContent).toBe("two-step");
    expect(summary.textContent).toContain("Total model cost");
    expect(summary.textContent).toContain("Wall elapsed");
    expect(summary.textContent).toContain("Recorded step time");
    expect(screen.getByTestId("workflow-parts").textContent).not.toContain("Total model cost");
    expect(screen.getByRole("alert").textContent).toContain("Blocked at approve");
    expect(screen.getByRole("alert").textContent).toContain("reject has no rework target");
    expect(screen.getByTestId("workflow-view").textContent).toContain("$0.1000");
    expect(screen.getByTestId("workflow-view").textContent).toContain("2s");
    expect(screen.getByTestId("workflow-view").textContent).toContain("1m");
  });

  it("keeps a one-step ladder nested even when its step has the lane's name", () => {
    const data = workflowData({ blueprint: "review", status: "running", position: { lane: "review" }, lanes: [
      { id: "review", structure: "ladder", status: "running", steps: [
        { id: "review", kind: "model-step", status: "running" },
      ] },
    ] });
    expect(data.steps[0]?.subSteps).toHaveLength(1);
    expect(data.steps[0]?.subSteps?.[0]?.id).toBe("review");
  });

  it("shows an approval request on the active lane even after an earlier gate answer", () => {
    const loop = { blueprint: "two-step", status: "waiting", position: { lane: "approve" },
      gateAnswers: ["approve", "revise"], gatePrompt: "Approve the survey?",
      lanes: [{ id: "approve", structure: "direct" as const, status: "ok", steps: [{ id: "approve", kind: "gate", status: "done", outcome: "revise" }] }] };
    const data = workflowData(loop);
    expect(data.steps[0]?.status).toBe("blocked");
    expect(data.steps[0]?.badge).toBe("Approval requested");
    expect(data.steps[0]?.subSteps).toBeUndefined();
    expect(data.steps[0]?.meta).toBe("Approval gate");
    expect(data.steps[0]?.elapsed).toBeUndefined();
    expect(data.steps[0]?.description).toContain("Approval gate");
    render(<Workflow loop={loop} now={Date.now()} current />);
    expect(screen.getByRole("status").textContent).toContain("Approval requested at approve");
    expect(screen.getByRole("link", { name: /Go to decision/ }).getAttribute("href")).toBe("#loop-decision");
  });

  it("routes both gate action placements through one callback and disables them while pending", () => {
    const loop = { blueprint: "two-step", status: "waiting", position: { lane: "approve" },
      gateAnswers: ["approve", "decline"], gatePrompt: "Review the survey?", lanes: [
        { id: "survey", structure: "direct" as const, status: "ok", steps: [{ id: "survey", kind: "model-step", status: "done" }] },
        { id: "approve", structure: "direct" as const, status: "waiting", steps: [{ id: "approve", kind: "gate", status: "waiting" }] },
      ] };
    const onGateAnswer = vi.fn();
    const view = render(<Workflow loop={loop} now={Date.now()} current onGateAnswer={onGateAnswer} />);
    expect(screen.getAllByRole("button", { name: "Approve" })).toHaveLength(2);
    fireEvent.click(screen.getAllByRole("button", { name: "Decline" })[0]!);
    expect(onGateAnswer).toHaveBeenCalledWith("approve", "decline");
    view.rerender(<Workflow loop={loop} now={Date.now()} current onGateAnswer={onGateAnswer} gateBusy />);
    expect(screen.getAllByRole("button", { name: "Approve" }).every((button) => button.hasAttribute("disabled"))).toBe(true);
    view.rerender(<Workflow loop={loop} now={Date.now()} current />);
    expect(screen.queryByRole("button", { name: "Approve" })).toBeNull();
    expect(screen.queryByRole("button", { name: /survey/i })).toBeNull();
  });

  it("submits a saved gate from either tracker placement only once", async () => {
    let accept!: (value: object) => void;
    const daemon = installFakeDaemon({ responses: {
      "loop.show": { id: "l1", slug: "l1", blueprint: "two-step", goal: "Survey Go", root: "/tmp/work", status: "waiting",
        position: { lane: "approve" }, questionId: 7, gateAnswers: ["approve", "decline"], gatePrompt: "Review the survey?",
        lanes: [{ id: "approve", structure: "direct", status: "waiting", steps: [{ id: "approve", kind: "gate", status: "waiting" }] }] },
      "loop.answer": () => new Promise((resolve) => { accept = resolve; }),
      "subscribe": { subscriptionId: "s1", from: 0 },
    } });
    const live = { state: "current" as const, endpoint: "test", daemonVersion: "1", protocolVersion: 1, generation: 1 };
    render(<MemoryRouter initialEntries={["/loops/l1"]}><Routes><Route path="/loops/:id"
      element={<LoopScreen controller={new LiveController()} live={live} canWrite />} /></Routes></MemoryRouter>);
    const buttons = await screen.findAllByRole("button", { name: "Approve" });
    expect(buttons).toHaveLength(2);
    fireEvent.click(buttons[0]!);
    fireEvent.click(buttons[1]!);
    expect(daemon.methods().filter((method) => method === "loop.answer")).toHaveLength(1);
    expect(screen.getAllByRole("button", { name: "Approve" }).every((button) => button.hasAttribute("disabled"))).toBe(true);
    await act(async () => accept({ status: "running" }));
    await waitFor(() => expect(within(screen.getByTestId("loop-thread")).getByRole("status").textContent).toContain("Answer accepted by the daemon"));
  });

  it("keeps the rail, step card and body card on one parent selection", () => {
    const loop = { blueprint: "two-step", status: "done", position: { lane: "approve" }, lanes: [
      { id: "survey", structure: "direct" as const, status: "ok", steps: [{ id: "survey", kind: "model-step", status: "done" }] },
      { id: "approve", structure: "direct" as const, status: "ok", steps: [{ id: "approve", kind: "gate", status: "done" }] },
    ] };
    function Parts() {
      const [lane, setLane] = useState("approve");
      return <Workflow loop={loop} now={Date.now()} current selectedLane={lane} onLaneSelect={setLane}
        details={<p>Details for {lane}</p>} />;
    }
    render(<Parts />);
    const parts = screen.getByTestId("workflow-parts");
    const [railColumn, bodyColumn] = [...parts.children];
    expect(within(railColumn as HTMLElement).getByRole("progressbar")).toBeTruthy();
    expect(within(bodyColumn?.children[0] as HTMLElement).getByRole("heading", { name: "approve" })).toBeTruthy();
    expect(within(bodyColumn?.children[1] as HTMLElement).getByText("Details for approve")).toBeTruthy();
    expect(screen.queryByRole("group", { name: "Workflow layout" })).toBeNull();
    fireEvent.click(within(parts).getByRole("button", { name: /survey/i }));
    expect(within(parts).getByText("Details for survey")).toBeTruthy();
    expect(within(parts).getByRole("heading", { name: "survey" })).toBeTruthy();
    expect(within(parts).queryByText("Details for approve")).toBeNull();
  });

  it("keeps incomplete cost as a floor in the workflow summary", () => {
    const loop = { blueprint: "two-step", status: "waiting", position: { lane: "approve" },
      lanes: [
        { id: "survey", status: "ok", steps: [{ id: "survey", kind: "model-step", status: "done", cost: { state: "partial" as const, usdAtLeast: 0.001161, missing: ["provider usage"] } }] },
        { id: "approve", status: "waiting", steps: [{ id: "approve", kind: "gate", status: "waiting" }] },
      ] };
    render(<Workflow loop={loop} now={Date.now()} current />);
    expect(screen.getByTestId("workflow-view").textContent).toContain("at least $0.001161");
    expect(workflowData(loop).steps[1]?.meta).toBe("Approval gate");
    expect(workflowData(loop).steps[1]?.subSteps?.[0]?.note).toBe("Approval gate");
    expect(workflowData(loop).steps[1]?.subSteps?.[0]?.duration).toBeUndefined();
  });
  it("keeps pending, not-needed, and skipped distinct and shows the condition", () => {
    const data = workflowData({ blueprint: "conditions", status: "running", position: { lane: "pending" }, lanes: [
      { id: "pending", status: "pending" },
      { id: "conditional", status: "not-needed", condition: "mode != simple" },
      { id: "omitted", status: "skipped" },
    ] });
    expect(data.steps.map((step) => step.status)).toEqual(["running", "skipped", "skipped"]);
    expect(data.steps[1]?.description).toContain("mode != simple");
  });

  it("shows a floor for total cost when a step has partial accounting", () => {
    render(<Workflow loop={{ blueprint: "cost", status: "done", position: { lane: "work" }, lanes: [{ id: "work", status: "done", steps: [
      { id: "a", kind: "model", status: "done", cost: { state: "known", usd: 0.1 } },
      { id: "b", kind: "model", status: "done", cost: { state: "partial", usdAtLeast: 0.2 } },
    ] }] }} now={Date.now()} current />);
    expect(screen.getAllByText(/at least \$0\.3000/).length).toBeGreaterThan(0);
  });

  it("nests iterated items and includes their steps in the total", () => {
    const loop: Parameters<typeof workflowData>[0] = { blueprint: "iteration", status: "running", position: { lane: "build" }, lanes: [{ id: "build", status: "running", iterate: "tasks", itemCount: 2, items: [
      { id: "first", status: "done", steps: [{ id: "write", kind: "model", status: "done", stepId: 1, cost: { state: "known", usd: 0.1 } }] },
      { id: "second", status: "running", steps: [{ id: "write", kind: "model", status: "pending", cost: { state: "partial", usdAtLeast: 0.2 } }] },
    ] }] };
    const data = workflowData(loop);
    expect(data.steps[0]?.subSteps?.map((step) => step.id)).toEqual(["first/write", "second/write"]);
    render(<Workflow loop={loop} now={Date.now()} current />);
    expect(screen.getByTestId("workflow-view").textContent).toContain("at least $0.3000");
  });

  it("renders an unimplemented code action from its structured blocked reason", () => {
    expect(blockedLine({ kind: "unimplemented-action", detail: "ignored prose", action: "git-pr", slice: "S16" }))
      .toBe("git-pr is not built yet — arrives at S16");
  });
});
