import { afterEach, beforeEach, describe, expect, it } from "vitest";
import { act, cleanup, fireEvent, render, screen, waitFor } from "@testing-library/react";
import { RecoveryPage } from "../src/features/test-bench/pages/RecoveryPage";
import { installFakeDaemon } from "./helpers";
import { resetBridge } from "../src/lib/bridge";

beforeEach(() => resetBridge());
afterEach(() => { cleanup(); resetBridge(); });

describe("recovery test bench", () => {
  it("shows an incomplete bash receipt and the saved failure reason", async () => {
    const daemon = installFakeDaemon({ responses: {
      subscribe: { subscriptionId: "recovery-sub", from: 0 },
      "step.show": { id: 17, status: "done", outcome: "failed", componentRoot: "/repo", cost: { state: "unknown" },
        recovery: { case: "C", reissued: false, reason: "unknown-tool-outcome: bash receipt started but not completed", updatedAt: "2026-10-17T11:00:00Z",
          failure: { kind: "unknown-tool-outcome", tool: "bash", toolUseId: "u1", detail: "bash receipt started but not completed" } },
        receipts: [{ toolUseId: "u1", tool: "bash", argDigest: "abc", startedAt: "2026-10-17T10:59:00Z", resultCaptured: false }] },
    } });
    render(<RecoveryPage />);
    await waitFor(() => expect(daemon.methods()).toContain("subscribe"));
    expect(daemon.paramsOf("subscribe")).toEqual({ after: 0, filter: { kinds: ["recovery.*"] } });
    fireEvent.change(screen.getByTestId("recovery-step-id"), { target: { value: "17" } });
    fireEvent.click(screen.getByTestId("recovery-inspect"));
    await screen.findByTestId("recovery-record");
    expect(screen.getByTestId("recovery-record").textContent).toContain("bash receipt started but not completed");
    expect(screen.getByTestId("recovery-failure").textContent).toContain("unknown-tool-outcome · bash · u1");
    expect(screen.getByTestId("recovery-receipts").textContent).toContain("outcome unknown");
    expect(screen.getByTestId("recovery-receipts").textContent).toContain("completed —");
  });

  it("shows progress events once and refreshes the inspected step after resolution", async () => {
    let resolved = false;
    const daemon = installFakeDaemon({ responses: {
      subscribe: { subscriptionId: "recovery-sub", from: 0 },
      "step.show": () => ({ id: 4, status: resolved ? "done" : "running", outcome: resolved ? "ok" : undefined,
        cost: { state: "unknown" }, recovery: resolved ? { case: "B", reissued: true, reason: "model call re-issued", updatedAt: "now" } : undefined }),
    } });
    render(<RecoveryPage />);
    await waitFor(() => expect(daemon.methods()).toContain("subscribe"));
    fireEvent.change(screen.getByTestId("recovery-step-id"), { target: { value: "4" } });
    fireEvent.click(screen.getByTestId("recovery-inspect"));
    await screen.findByTestId("recovery-step");
    await act(async () => {
      daemon.emit({ seq: 10, kind: "recovery.step", payload: { stepId: 4, case: "B", index: 2, total: 3 } });
      daemon.emit({ seq: 10, kind: "recovery.step", payload: { stepId: 4, case: "B", index: 2, total: 3 } });
      resolved = true;
      daemon.emit({ seq: 11, kind: "recovery.resolved", payload: { stepId: 4, outcome: "ok" } });
    });
    await waitFor(() => expect(screen.getByTestId("recovery-record").textContent).toContain("model call reissued"));
    expect(screen.getByTestId("recovery-events").querySelectorAll("li")).toHaveLength(2);
    expect(screen.getByTestId("recovery-events").textContent).toContain("2 of 3");
  });

  it("shows structured failure details from the recovery event", async () => {
    const daemon = installFakeDaemon({ responses: { subscribe: { subscriptionId: "recovery-sub", from: 0 } } });
    render(<RecoveryPage />);
    await waitFor(() => expect(daemon.methods()).toContain("subscribe"));
    await act(async () => daemon.emit({ seq: 12, kind: "recovery.resolved", payload: {
      stepId: 7, outcome: "failed", failure: { kind: "unknown-tool-outcome", tool: "bash", detail: "result missing" },
    } }));
    expect(screen.getByTestId("recovery-events").textContent).toContain("unknown-tool-outcome · bash · result missing");
  });
});
