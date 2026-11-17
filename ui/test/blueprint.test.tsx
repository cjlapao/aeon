import { afterEach, beforeEach, describe, expect, it } from "vitest";
import { act, cleanup, fireEvent, render, screen, waitFor } from "@testing-library/react";
import { BlueprintPage } from "../src/features/test-bench/pages/BlueprintPage";
import { resetBridge } from "../src/lib/bridge";
import { installFakeDaemon } from "./helpers";

beforeEach(() => resetBridge());
afterEach(() => { cleanup(); resetBridge(); });

describe("S8 blueprint test bench", () => {
  it("validates an edit, saves it, and shows diagnostics", async () => {
    const daemon = installFakeDaemon({ responses: {
      "blueprint.list": { names: ["two-step", "dev-loop"] },
      "blueprint.show": { path: "/state/blueprints/two-step.yaml", content: "id: two-step\n" },
      "blueprint.validate": { blueprint: "two-step", validCount: 4, diagnostics: [{ state: "unverifiable", path: "lanes[1].step", message: "future action", slice: "S16" }] },
      "blueprint.save": { path: "/state/blueprints/two-step.yaml", report: { blueprint: "two-step", validCount: 4, diagnostics: [] } },
      subscribe: { subscriptionId: "blueprints", from: 0 },
    } });
    render(<BlueprintPage />);
    await waitFor(() => expect(screen.getByTestId("blueprint-yaml")).toHaveProperty("value", "id: two-step\n"));
    fireEvent.change(screen.getByTestId("blueprint-yaml"), { target: { value: "id: two-step\nversion: 2\n" } });
    fireEvent.click(screen.getByText("Validate"));
    await screen.findByText(/4 valid · 0 invalid · 1 unverifiable/);
    expect(daemon.paramsOf("blueprint.validate")).toEqual({ name: "two-step", content: "id: two-step\nversion: 2\n", mode: "standard" });
    fireEvent.click(screen.getByText("Save source"));
    await waitFor(() => expect(daemon.methods()).toContain("blueprint.save"));
    expect(screen.getByTestId("blueprint-report").textContent).toContain("0 unverifiable");
  });

  it("starts a loop, shows its pinned ladder, and answers a gate", async () => {
    const view = { id: "abc", blueprint: "two-step", version: 1, goal: "Survey", root: "/repo", status: "waiting",
      questionId: 7, gateAnswers: ["approve", "abort"], position: { lane: "approve" }, pinnedContent: "id: two-step\n", lanes: [
        { id: "survey", status: "ok", steps: [{ id: "survey", kind: "model-step", status: "done", outcome: "ok" }] },
        { id: "approve", status: "waiting", steps: [{ id: "approve", kind: "gate", status: "pending" }] },
      ] };
    const daemon = installFakeDaemon({ responses: {
      "blueprint.list": { names: ["two-step"] }, "blueprint.show": { path: "/state/two-step.yaml", content: "id: two-step\n" },
      "loop.start": { id: "abc" }, "loop.show": view, "loop.answer": { id: "abc", status: "running" },
      subscribe: { subscriptionId: "blueprints", from: 0 },
    } });
    render(<BlueprintPage />);
    fireEvent.change(screen.getByTestId("blueprint-root"), { target: { value: "/repo" } });
    fireEvent.click(screen.getByText("Start loop"));
    await screen.findByTestId("blueprint-ladder");
    expect(daemon.paramsOf("loop.start")).toMatchObject({ blueprint: "two-step", workspaceRoot: "/repo" });
    expect(screen.getByTestId("pinned-yaml").textContent).toContain("id: two-step");
    fireEvent.click(screen.getByText("Answer gate"));
    await waitFor(() => expect(daemon.paramsOf("loop.answer")).toEqual({ id: "abc", answer: "approve" }));
    await act(async () => daemon.emit({ seq: 1, kind: "loop.advanced", payload: { loopId: "abc", outcome: "approve" } }));
    expect(screen.getByTestId("blueprint-events").textContent).toContain("loop.advanced");
  });
});
