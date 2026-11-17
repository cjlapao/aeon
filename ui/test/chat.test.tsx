import { afterEach, beforeEach, describe, expect, it } from "vitest";
import { act, cleanup, fireEvent, render, screen, waitFor } from "@testing-library/react";
import { MemoryRouter, Route, Routes } from "react-router-dom";
import { StepScreen } from "../src/features/loops/pages/StepScreen";
import { LiveController } from "../src/lib/live";
import { installFakeDaemon, resetFakeDaemon, routeView, routesList } from "./helpers";

// The chat view (the "we now have a chat concept in the loop" work): real-time
// streaming including questions, stop/continue, and the provider + reasoning
// effort picker available once stopped. All of it rides the existing S5/S6/S9
// transcript, event and answer shapes — nothing here is a new protocol
// capability except `step.stop` / `step.continue` themselves.

beforeEach(() => resetFakeDaemon());
afterEach(() => { cleanup(); resetFakeDaemon(); });

const live = { state: "current" as const, endpoint: "/tmp/aeon.sock", daemonVersion: "1", protocolVersion: 1, generation: 1 };

function renderStep() {
  return render(<MemoryRouter initialEntries={["/steps/4"]}><Routes><Route path="/steps/:id"
    element={<StepScreen controller={new LiveController()} live={live} />} /></Routes></MemoryRouter>);
}

describe("the step chat view", () => {
  it("shows a thinking block's own text instead of withholding it", async () => {
    installFakeDaemon({ responses: {
      "step.show": { id: 4, agent: "surveyor", phase: "build", status: "done", cost: { state: "known", usd: 0.01 },
        messages: [{ role: "assistant", content: [
          { kind: "thinking", text: "Checking whether the file already exists first.", signature: "sig-1" },
          { kind: "text", text: "It does not, so I will create it." },
        ] }] },
      "routes.list": routesList([]), subscribe: { subscriptionId: "s1", from: 0 },
    } });
    renderStep();
    expect(await screen.findByText(/Checking whether the file already exists first\./)).toBeTruthy();
    expect(screen.queryByText(/sig-1/)).toBeNull();
    expect(screen.getByText("It does not, so I will create it.")).toBeTruthy();
  });

  it("falls back to the withheld notice for a signature-only thinking block", async () => {
    installFakeDaemon({ responses: {
      "step.show": { id: 4, agent: "surveyor", phase: "build", status: "done", cost: { state: "known", usd: 0.01 },
        messages: [{ role: "assistant", content: [{ kind: "thinking", signature: "sig-1" }] }] },
      "routes.list": routesList([]), subscribe: { subscriptionId: "s1", from: 0 },
    } });
    renderStep();
    expect(await screen.findByText("Thinking occurred.")).toBeTruthy();
  });

  it("streams a live thinking delta and a tool call without rereading the transcript", async () => {
    const daemon = installFakeDaemon({ responses: {
      "step.show": { id: 4, agent: "surveyor", phase: "build", status: "running", cost: { state: "unknown", reason: "usage pending" }, messages: [] },
      "routes.list": routesList([]), subscribe: { subscriptionId: "s1", from: 0 },
    } });
    renderStep();
    await screen.findByText("No transcript entries yet.");
    await waitFor(() => expect(daemon.methods()).toContain("subscribe"));
    const before = daemon.methods().filter((m) => m === "step.show").length;
    act(() => daemon.emit({ kind: "model.delta", seq: 3, at: "2026-10-22T08:00:00Z", payload: { stepId: 4, tool: "thinking", text: "Weighing two approaches." } }));
    act(() => daemon.emit({ kind: "step.tool.start", seq: 4, at: "2026-10-22T08:00:01Z", payload: { stepId: 4, tool: "read" } }));
    act(() => daemon.emit({ kind: "step.tool.end", seq: 5, at: "2026-10-22T08:00:02Z", payload: { stepId: 4, tool: "read", result: "12 lines" } }));
    const live = screen.getByLabelText("Live transcript events");
    expect(live.textContent).toContain("Weighing two approaches.");
    expect(live.textContent).toContain("read started");
    expect(live.textContent).toContain("read finished: 12 lines");
    expect(daemon.methods().filter((m) => m === "step.show")).toHaveLength(before);
  });

  it("answers an inline question from the chat and clears it once the step resumes", async () => {
    let answered = false;
    const question = { id: 9, stepId: 4, kind: "choice", body: "Small edit or rewrite?", options: ["small edit", "rewrite"] };
    const daemon = installFakeDaemon({ responses: {
      "step.show": () => answered ?
        { id: 4, agent: "surveyor", phase: "build", status: "running", cost: { state: "unknown", reason: "usage pending" }, messages: [] } :
        { id: 4, agent: "surveyor", phase: "build", status: "suspended", cost: { state: "unknown", reason: "usage pending" }, messages: [], question },
      answer: (params: unknown) => { answered = true; return { stepId: (params as { stepId: number }).stepId, questionId: 9, answeredAt: "2026-10-22T08:00:00Z", status: "running" }; },
      "routes.list": routesList([]), subscribe: { subscriptionId: "s1", from: 0 },
    } });
    renderStep();
    await screen.findByTestId("question-card");
    expect(screen.getByText("Small edit or rewrite?")).toBeTruthy();
    fireEvent.click(screen.getByRole("button", { name: "small edit" }));
    await waitFor(() => expect(daemon.methods()).toContain("answer"));
    expect(daemon.paramsOf("answer")).toEqual({ stepId: 4, questionId: 9, answer: "small edit" });
    await waitFor(() => expect(screen.queryByTestId("question-card")).toBeNull());
  });

  it("stops a running step and offers to change the provider and reasoning effort before continuing", async () => {
    let stopped = false;
    const daemon = installFakeDaemon({ responses: {
      "step.show": () => stopped ?
        { id: 4, agent: "surveyor", phase: "build", status: "suspended", pauseReason: "user", cost: { state: "unknown", reason: "usage pending" }, messages: [] } :
        { id: 4, agent: "surveyor", phase: "build", status: "running", cost: { state: "unknown", reason: "usage pending" }, messages: [] },
      "step.stop": (params: unknown) => { stopped = true; return { stepId: (params as { stepId: number }).stepId, stopped: true }; },
      "step.continue": { stepId: 4, kind: "complete", outcome: "ok", usage: {}, cost: { state: "known", usd: 0 }, iterations: 1 },
      "routes.list": routesList([routeView({ id: "anthropic", provider: "anthropic", model: "claude-sonnet-5" }),
        routeView({ id: "openai-fast", provider: "openai", model: "gpt-fast" })]),
      subscribe: { subscriptionId: "s1", from: 0 },
    } });
    renderStep();
    await screen.findByTestId("step-stop");
    fireEvent.click(screen.getByTestId("step-stop"));
    await waitFor(() => expect(daemon.methods()).toContain("step.stop"));
    expect(daemon.paramsOf("step.stop")).toEqual({ stepId: 4 });
    await screen.findByTestId("step-continue-panel");
    fireEvent.change(screen.getByTestId("step-continue-route"), { target: { value: "openai-fast" } });
    fireEvent.change(screen.getByTestId("step-continue-effort"), { target: { value: "low" } });
    fireEvent.click(screen.getByTestId("step-continue-submit"));
    await waitFor(() => expect(daemon.methods()).toContain("step.continue"));
    expect(daemon.paramsOf("step.continue")).toEqual({ stepId: 4, route: "openai-fast", reasoningEffort: "low" });
  });

  it("continues with the step's own provider and effort when neither is changed", async () => {
    const daemon = installFakeDaemon({ responses: {
      "step.show": { id: 4, agent: "surveyor", phase: "build", status: "suspended", pauseReason: "user", cost: { state: "unknown", reason: "usage pending" }, messages: [] },
      "step.continue": { stepId: 4, kind: "complete", outcome: "ok", usage: {}, cost: { state: "known", usd: 0 }, iterations: 1 },
      "routes.list": routesList([]), subscribe: { subscriptionId: "s1", from: 0 },
    } });
    renderStep();
    await screen.findByTestId("step-continue-panel");
    fireEvent.click(screen.getByTestId("step-continue-submit"));
    await waitFor(() => expect(daemon.methods()).toContain("step.continue"));
    expect(daemon.paramsOf("step.continue")).toEqual({ stepId: 4 });
  });
});
