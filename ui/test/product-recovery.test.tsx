import { afterEach, beforeEach, expect, it } from "vitest";
import { act, cleanup, fireEvent, render, screen, waitFor } from "@testing-library/react";
import { MemoryRouter } from "react-router-dom";
import { Diagnostics } from "../src/features/blueprints/panels/Diagnostics";
import { LiveController } from "../src/lib/live";
import { SystemScreen } from "../src/features/system/SystemScreen";
import { ProductApp } from "../src/app/router";
import { installFakeDaemon, resetFakeDaemon } from "./helpers";

beforeEach(() => {
  vi.stubGlobal("ResizeObserver", class { observe() {} disconnect() {} });
  window.matchMedia = vi.fn(() => ({ matches: false, media: "", onchange: null, addEventListener: () => {}, removeEventListener: () => {}, addListener: () => {}, removeListener: () => {}, dispatchEvent: () => false })) as unknown as typeof window.matchMedia;
  window.location.hash = "#/loops";
});
afterEach(() => { cleanup(); resetFakeDaemon(); window.location.hash = "#/"; vi.unstubAllGlobals(); });

/** The doors S48's drawer opens for the row it shows, so the S9-era fixtures
 *  below answer everything the screen asks and the connection stays current. */
const drawerDoors = {
  "blueprint.history": { revisions: [] },
  "blueprint.usage": { actions: [], agents: [], loopsStarted: 0, queueItems: [], schedules: [], skills: [], triggers: [] },
  "blueprint.lint": { advisories: [], evaluatedAt: "2026-11-02T00:00:00Z" },
  "blueprint.diff": { aLabel: "current", bLabel: "current", diff: "", equal: true },
  "blueprint.duplicate": { changed: true, name: "dev-loop-copy", origin: "user", references: [], report: { blueprint: "dev-loop-copy", diagnostics: [], schemaVersion: 1, validCount: 1, version: 1 }, revision: 1, version: 1 },
  "proposal.list": { proposals: [] },
} as const;

describe("S9 product surfaces", () => {
  it("renders all diagnostic paths and keeps strict findings identical", () => {
    const report = { blueprint: "dev-loop", validCount: 4, diagnostics: [
      { state: "unverifiable" as const, kind: "missing-code-action", path: "lanes[build].step", message: "registry absent", slice: "S16" },
      { state: "invalid" as const, kind: "unknown-agent", path: "lanes[qa].agent", message: "unregistered", hint: "Use a known agent" },
    ] };
    const view = render(<Diagnostics report={report} strict={false} />);
    expect(screen.getByText("lanes[build].step")).toBeTruthy();
    expect(screen.getByText(/S16/)).toBeTruthy();
    expect(screen.getByText("lanes[qa].agent")).toBeTruthy();
    expect(screen.getByText("Use a known agent")).toBeTruthy();
    const before = screen.getByTestId("blueprint-diagnostics").querySelector("ol")?.textContent;
    view.rerender(<Diagnostics report={report} strict />);
    expect(screen.getByTestId("blueprint-diagnostics").querySelector("ol")?.textContent).toBe(before);
  });

  it("explains a diagnostic from its kind even when its prose is unusable", () => {
    render(<Diagnostics strict={false} report={{ blueprint: "dev-loop", validCount: 0, diagnostics: [
      { state: "unverifiable", kind: "action-registry", path: "lanes[build].step.action", message: "nonsense", slice: "S16" },
      { state: "invalid", kind: "unknown-question", path: "lanes[gate].step.question", message: "nonsense" },
    ] }} />);
    expect(screen.getByText(/Code action registry not given to this validator/)).toBeTruthy();
    expect(screen.getByText(/Question is not declared/)).toBeTruthy();
    // S48 T12 changed what the panel shows beneath the label: the daemon's own
    // sentence is now rendered verbatim rather than filtered, because a report
    // the app quietly rewrote is a report nobody can check against the daemon.
    // What the KIND still carries the meaning — the two labels above are found
    // even with prose this useless, which is the property this test exists for.
    expect(screen.getAllByText("nonsense")).toHaveLength(2);
  });

  // The S9-era Flows screen asked the daemon to validate EVERY listed name on
  // load so a library-wide verdict column could be filled. That column is gone: the
  // library is a gallery that asks for names and summaries only, and a verdict is
  // asked once, for the one flow a person opens.
  it("asks for no verdict while it only lists the library", async () => {
    const daemon = installFakeDaemon({ negotiated: { daemonVersion: "1.0", protocolVersion: 1, endpoint: "/tmp/aeon.sock" },
      responses: { "loop.list": { loops: [], all: true }, "blueprint.list": { names: ["ready", "broken"] },
        "routes.list": { routes: [], providers: [], persisted: true }, ...drawerDoors, "subscribe": { subscriptionId: "s1", from: 0 } } });
    render(<ProductApp />);
    fireEvent.click(screen.getByRole("link", { name: "Flows" }));
    await waitFor(() => expect(daemon.methods()).toContain("blueprint.list"));
    expect(daemon.methods()).not.toContain("blueprint.validate");
  });

  it("says so when the library is empty", async () => {
    installFakeDaemon({ negotiated: { daemonVersion: "1.0", protocolVersion: 1, endpoint: "/tmp/aeon.sock" },
      responses: { "loop.list": { loops: [], all: true }, "blueprint.list": { names: [] },
        "routes.list": { routes: [], providers: [], persisted: true }, ...drawerDoors, "subscribe": { subscriptionId: "s1", from: 0 } } });
    render(<ProductApp />);
    fireEvent.click(screen.getByRole("link", { name: "Flows" }));
    expect(await screen.findByText("No flows yet")).toBeTruthy();
  });

  it("shows the recovery tool and receipt state from a retained daemon event", async () => {
    const daemon = installFakeDaemon({ responses: { "subscribe": { subscriptionId: "s1", from: 0 } } });
    render(<MemoryRouter><SystemScreen controller={new LiveController()}
      live={{ state: "current", endpoint: "/tmp/aeon.sock", daemonVersion: "1", protocolVersion: 1, generation: 1 }} />
      </MemoryRouter>);
    await waitFor(() => expect(daemon.methods()).toContain("subscribe"));
    act(() => daemon.emit({ kind: "recovery.resolved", seq: 8, at: "2026-10-18T08:00:00Z", payload: {
      stepId: 4, outcome: "failed", case: "C", failure: { kind: "receipt-unknown", tool: "bash", detail: "outcome unknown" },
    } }));
    expect(await screen.findByText(/tool bash · receipt receipt-unknown/)).toBeTruthy();
    expect(screen.getByText(/case C/)).toBeTruthy();
  });

  it("distinguishes saved-tail, model-reissue and uncertain-tool recovery cases", async () => {
    const daemon = installFakeDaemon({ responses: { "subscribe": { subscriptionId: "s1", from: 0 } } });
    render(<MemoryRouter><SystemScreen controller={new LiveController()}
      live={{ state: "current", endpoint: "/tmp/aeon.sock", daemonVersion: "1", protocolVersion: 1, generation: 1 }} />
      </MemoryRouter>);
    await waitFor(() => expect(daemon.methods()).toContain("subscribe"));
    act(() => {
      daemon.emit({ kind: "recovery.step", seq: 1, payload: { stepId: 1, case: "A" } });
      daemon.emit({ kind: "recovery.step", seq: 2, payload: { stepId: 2, case: "B" } });
      daemon.emit({ kind: "recovery.step", seq: 3, payload: { stepId: 3, case: "C" } });
      daemon.emit({ kind: "recovery.resolved", seq: 4, payload: { stepId: 2, case: "B", outcome: "ok",
        priorCost: { state: "unknown", reason: "stream-aborted-early" }, reissueCost: { state: "known", usd: 0.04 } } });
    });
    expect(screen.getByText("saved tail completed")).toBeTruthy();
    expect(screen.getAllByText("model call reissued; earlier attempt may also be billed").length).toBeGreaterThan(0);
    expect(screen.getByText("tool receipt reconciliation")).toBeTruthy();
    expect(screen.getByText(/Before crash: .* · Reissued work: \$0\.0400/)).toBeTruthy();
  });

  it("limits the System recovery report to the latest daemon start", async () => {
    const daemon = installFakeDaemon({ responses: { "subscribe": { subscriptionId: "s1", from: 0 } } });
    render(<MemoryRouter><SystemScreen controller={new LiveController()}
      live={{ state: "current", endpoint: "/tmp/aeon.sock", daemonVersion: "1", protocolVersion: 1, generation: 1 }} />
      </MemoryRouter>);
    await waitFor(() => expect(daemon.methods()).toContain("subscribe"));
    act(() => {
      daemon.emit({ kind: "recovery.started", seq: 1, payload: { total: 1 } });
      daemon.emit({ kind: "recovery.resolved", seq: 2, payload: { stepId: 11, outcome: "failed", failure: { kind: "old", tool: "bash" } } });
      daemon.emit({ kind: "recovery.finished", seq: 3, payload: { total: 1, recovered: 0, failed: 1 } });
      daemon.emit({ kind: "recovery.started", seq: 4, payload: { total: 2 } });
      daemon.emit({ kind: "recovery.finished", seq: 5, payload: { total: 2, recovered: 2, failed: 0 } });
    });
    expect(screen.getByText("Last start: 2 recovered of 2 · 0 failed")).toBeTruthy();
    expect(screen.queryByText(/step 11/)).toBeNull();
    expect(screen.getByText(/#4/)).toBeTruthy();
  });

  it("states plainly when the last daemon start needed no recovery", async () => {
    const daemon = installFakeDaemon({ responses: { "subscribe": { subscriptionId: "s1", from: 0 } } });
    render(<MemoryRouter><SystemScreen controller={new LiveController()}
      live={{ state: "current", endpoint: "/tmp/aeon.sock", daemonVersion: "1", protocolVersion: 1, generation: 1 }} />
      </MemoryRouter>);
    await waitFor(() => expect(daemon.methods()).toContain("subscribe"));
    act(() => {
      daemon.emit({ kind: "recovery.started", seq: 1, payload: { total: 0 } });
      daemon.emit({ kind: "recovery.finished", seq: 2, payload: { total: 0, recovered: 0, failed: 0 } });
    });
    expect(screen.getByText("No steps needed recovery on the last daemon start.")).toBeTruthy();
  });
});
