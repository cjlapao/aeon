import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import { cleanup, fireEvent, render, screen, waitFor } from "@testing-library/react";
import { MemoryRouter, Route, Routes } from "react-router-dom";
import { LoopScreen } from "../src/features/loops/pages/LoopScreen";
import { LiveController } from "../src/lib/live";
import { installFakeDaemon, resetFakeDaemon, type FakeDaemon } from "./helpers";

// T10 — the app shows ELIGIBILITY and REFUSAL from the daemon. The screen
// holds no opinion about whether a retry would work: it renders the
// daemon's `retryEligible` / `retryRefusal`, and the button posts
// `loop.retry_handler_block` for exactly the loop on screen.

const live = { state: "current" as const, endpoint: "test", daemonVersion: "1", protocolVersion: 1, generation: 1 };

function blockedLoop(extra: Record<string, unknown> = {}) {
  return {
    id: "l1", slug: "l1", blueprint: "dev-loop", goal: "Ship it", root: "/tmp/work", status: "blocked",
    position: { lane: "implement" }, lastReason: 'invalid params: unknown handler "golang-engineer"',
    lanes: [{ id: "implement", structure: "direct", status: "blocked", steps: [{ id: "implement", kind: "model-step", status: "pending" }] }],
    ...extra,
  };
}

function renderLoop() {
  render(<MemoryRouter initialEntries={["/loops/l1"]}><Routes><Route path="/loops/:id"
    element={<LoopScreen controller={new LiveController()} live={live} canWrite />} /></Routes></MemoryRouter>);
}

beforeEach(() => { vi.stubGlobal("ResizeObserver", class { observe() {} disconnect() {} }); });
afterEach(() => { cleanup(); resetFakeDaemon(); vi.unstubAllGlobals(); });

describe("loop missing-handler retry (T10)", () => {
  it("offers the retry when the DAEMON says the block is eligible", async () => {
    let daemon: FakeDaemon;
    daemon = installFakeDaemon({ responses: {
      "loop.show": () => blockedLoop({ retryEligible: true, retryAgent: "golang-engineer" }),
      "loop.retry_handler_block": (params: unknown) => ({ retried: true, status: "running", position: { lane: "implement" }, echoed: params }),
      subscribe: { subscriptionId: "s1", from: 0 },
    } });
    renderLoop();
    const panel = await screen.findByTestId("loop-retry");
    expect(panel.textContent).toContain("golang-engineer");
    fireEvent.click(screen.getByRole("button", { name: /Retry blocked step/ }));
    await waitFor(() => {
      const call = daemon.calls.find((c) => c.method === "loop.retry_handler_block");
      expect(call?.params).toEqual({ id: "l1" });
    });
  });

  it("shows the daemon's refusal verbatim and offers no button", async () => {
    installFakeDaemon({ responses: {
      "loop.show": () => blockedLoop({
        lastReason: 'invalid params: unknown handler "task-coordinator"',
        retryRefusal: { kind: "handler-still-missing", detail: "the registry still has no handler \"task-coordinator\" — retry becomes possible when it registers; nothing dispatches meanwhile" },
      }),
      subscribe: { subscriptionId: "s1", from: 0 },
    } });
    renderLoop();
    const refusal = await screen.findByTestId("loop-retry-refusal");
    expect(refusal.textContent).toContain("handler-still-missing");
    expect(refusal.textContent).toContain("task-coordinator");
    expect(screen.queryByRole("button", { name: /Retry blocked step/ })).toBeNull();
  });

  it("renders a refusal that arrives only when the button is pressed", async () => {
    installFakeDaemon({ responses: {
      "loop.show": () => blockedLoop({ retryEligible: true, retryAgent: "golang-engineer" }),
      "loop.retry_handler_block": () => ({ refused: { kind: "receipt-exists", detail: "the position already holds step row 7 — a completed or in-flight receipt is never replayed" } }),
      subscribe: { subscriptionId: "s1", from: 0 },
    } });
    renderLoop();
    await screen.findByTestId("loop-retry");
    fireEvent.click(screen.getByRole("button", { name: /Retry blocked step/ }));
    const note = await screen.findByTestId("loop-retry-note");
    expect(note.textContent).toContain("receipt-exists");
    expect(note.textContent).toContain("never replayed");
  });

  it("says nothing about retries on a loop the daemon did not flag", async () => {
    installFakeDaemon({ responses: {
      "loop.show": () => blockedLoop(), // no retry fields at all
      subscribe: { subscriptionId: "s1", from: 0 },
    } });
    renderLoop();
    await screen.findByTestId("workflow-view");
    expect(screen.queryByTestId("loop-retry")).toBeNull();
  });
});
