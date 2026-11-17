import { afterEach, beforeEach, expect, it } from "vitest";
import { act, cleanup, fireEvent, render, screen, waitFor, within } from "@testing-library/react";
import { MemoryRouter, Route, Routes } from "react-router-dom";
import { LoopScreen } from "../src/features/loops/pages/LoopScreen";
import { LiveController } from "../src/lib/live";
import { ProductApp } from "../src/app/router";
import { installFakeDaemon, resetFakeDaemon } from "./helpers";

beforeEach(() => { vi.stubGlobal("ResizeObserver", class { observe() {} disconnect() {} }); window.location.hash = "#/"; });
afterEach(() => { cleanup(); resetFakeDaemon(); window.location.hash = "#/"; vi.unstubAllGlobals(); });

describe("S9 product surfaces", () => {
  it("shows the awaiting gate with its real question, and opens the loop from it", async () => {
    window.location.hash = "#/";
    const loop = { id: "l9", slug: "l9", title: "Survey packages", componentRoot: "/tmp/work", status: "waiting",
      phase: "approve", updatedAt: "2026-10-27T08:00:00Z", startedAt: "2026-10-27T07:00:00Z", awaiting: true, mode: "standard" };
    installFakeDaemon({ negotiated: { daemonVersion: "1.0", protocolVersion: 1, endpoint: "/tmp/aeon.sock" },
      responses: { "loop.list": { loops: [loop], all: true }, "component.list": { components: [] }, "blueprint.list": { names: [] },
        "routes.list": { routes: [], providers: [], persisted: true }, "subscribe": { subscriptionId: "s1", from: 0 },
        "question.pending": { pending: true, question: { id: 1, kind: "approve", prompt: "Ship the dependency bump?", answers: ["approve", "revise"], askedAt: "2026-10-27T08:00:00Z", stepId: 4 } },
        "loop.show": { id: "l9", slug: "l9", blueprint: "two-step", goal: "Bump deps", root: "/tmp/work", status: "waiting",
          position: { lane: "approve" }, lanes: [{ id: "approve", status: "waiting", steps: [] }] } } });
    render(<ProductApp />);
    expect(await screen.findByTestId("home-gate-banner")).toBeTruthy();
    expect(screen.getByText("Ship the dependency bump?")).toBeTruthy();
    fireEvent.click(screen.getByRole("button", { name: /Answer/ }));
    expect(await screen.findByTestId("loop-workspace")).toBeTruthy();
  });

  it("fills the goal from a quick starter", async () => {
    window.location.hash = "#/";
    installFakeDaemon({ responses: { "loop.list": { loops: [], all: true }, "component.list": { components: [] }, "blueprint.list": { names: [] },
      "routes.list": { routes: [], providers: [], persisted: true }, "subscribe": { subscriptionId: "s1", from: 0 } } });
    render(<ProductApp />);
    await screen.findByTestId("product-home");
    fireEvent.click(screen.getByRole("button", { name: "PR review" }));
    expect((screen.getByPlaceholderText(/Describe a task/) as HTMLTextAreaElement).value).toContain("Review the open pull request");
  });

  it("moves the ladder from an event snapshot without rereading loop.show", async () => {
    const daemon = installFakeDaemon({ responses: {
      "loop.show": { id: "l1", slug: "l1", blueprint: "two-step", goal: "Survey", root: "/tmp/work", status: "running",
        position: { lane: "survey" }, lanes: [{ id: "survey", status: "running", steps: [] }], pinnedContent: "original: yes" },
      "subscribe": { subscriptionId: "s1", from: 0 },
    } });
    render(<MemoryRouter initialEntries={["/loops/l1"]}><Routes><Route path="/loops/:id" element={<LoopScreen
      controller={new LiveController()} live={{ state: "current", endpoint: "/tmp/aeon.sock", daemonVersion: "1", protocolVersion: 1, generation: 1 }} canWrite />}
      /></Routes></MemoryRouter>);
    await screen.findByText("Pinned blueprint source");
    await waitFor(() => expect(daemon.methods()).toContain("subscribe"));
    const before = daemon.methods().filter((method) => method === "loop.show").length;
    act(() => daemon.emit({ kind: "loop.advanced", seq: 2, at: "2026-10-19T08:00:00Z", payload: { loopId: "l1",
      snapshot: { id: "l1", slug: "l1", blueprint: "two-step", goal: "Survey", root: "/tmp/work", status: "running",
        position: { lane: "approve" }, lanes: [{ id: "approve", status: "running", steps: [] }] },
    } }));
    expect(within(screen.getByTestId("workflow-view")).getByRole("heading", { name: "approve" })).toBeTruthy();
    expect(screen.getByText("original: yes")).toBeTruthy();
    expect(daemon.methods().filter((method) => method === "loop.show")).toHaveLength(before);
  });

});
