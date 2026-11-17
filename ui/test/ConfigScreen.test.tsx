import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import { cleanup, render, screen } from "@testing-library/react";
import { MemoryRouter, Route, Routes } from "react-router-dom";
import { LiveController, type LiveState } from "../src/lib/live";

// S72 CF10: the /config screen's four states — redirect, in-shell not-found
// (unknown and planned), the not-connected alert, and the boundary — plus
// the bare frame. A fresh module graph per test so the registry each case
// builds is its own.

beforeEach(() => {
  vi.resetModules();
});
afterEach(() => {
  cleanup();
  vi.restoreAllMocks();
});

const current: LiveState = { state: "current", endpoint: "/tmp/aeon-test.sock", daemonVersion: "1.0", protocolVersion: 1, generation: 1 };
const connecting: LiveState = { state: "connecting", endpoint: "/tmp/aeon-test.sock" };

async function renderConfig(path: string, live: LiveState) {
  const { registerConfigPage } = await import("../src/features/config/lib/configRegistry");
  registerConfigPage({ id: "ready-one", label: "Ready one", group: "general", order: 10, status: "ready", summary: "the ready page", render: () => <div data-testid="ready-one-body">ok</div> });
  registerConfigPage({ id: "bare-one", label: "Bare one", group: "general", order: 20, frame: "bare", status: "ready", render: () => <div data-testid="bare-one-body" /> });
  registerConfigPage({ id: "boom", label: "Boom", group: "general", order: 30, status: "ready", render: () => { throw new Error("page exploded"); } });
  registerConfigPage({ id: "no-conn", label: "No connection needed", group: "general", order: 40, needsConnection: false, status: "ready", render: () => <div data-testid="no-conn-body" /> });
  registerConfigPage({ id: "planned-one", label: "Planned one", group: "access", order: 10, status: "planned", summary: "Its UI is not built yet." });
  const { ConfigScreen } = await import("../src/features/config/ConfigScreen");
  return render(
    <MemoryRouter initialEntries={[path]}>
      <Routes>
        <Route path="/config/:page?" element={<ConfigScreen controller={new LiveController()} live={live} canWrite={live.state === "current"} />} />
      </Routes>
    </MemoryRouter>,
  );
}

describe("ConfigScreen (S72 CF5–CF7/CF10)", () => {
  it("with no :page redirects to the first ready page and renders it framed", async () => {
    await renderConfig("/config", current);
    expect(await screen.findByTestId("ready-one-body")).toBeTruthy();
    expect(screen.getByTestId("config-page-frame").getAttribute("data-page")).toBe("ready-one");
    expect(screen.getByRole("heading", { name: "Ready one", level: 1 })).toBeTruthy();
    expect(screen.getByText("General")).toBeTruthy(); // the group breadcrumb line
    expect(screen.getByText("the ready page")).toBeTruthy(); // the summary line
  });

  it("a frame-bare page renders without the h1", async () => {
    await renderConfig("/config/bare-one", current);
    expect(await screen.findByTestId("bare-one-body")).toBeTruthy();
    expect(screen.queryByRole("heading", { name: "Bare one" })).toBeNull();
    expect(screen.getByText("General")).toBeTruthy(); // the breadcrumb survives; only the h1 stands down (CF6)
  });

  it("an unknown id gets the in-shell not-found, not a redirect that hides the typo", async () => {
    await renderConfig("/config/missing", current);
    expect(await screen.findByTestId("config-page-missing")).toBeTruthy();
    expect(screen.getByRole("heading", { name: "Page not found" })).toBeTruthy();
    expect(screen.getByText(/“missing” is not a configuration page/)).toBeTruthy();
    expect(screen.getByRole("link", { name: "Go to Ready one →" })).toBeTruthy();
  });

  it("a planned id gets the not-found saying it is planned, with its summary", async () => {
    await renderConfig("/config/planned-one", current);
    expect(await screen.findByTestId("config-page-missing")).toBeTruthy();
    expect(screen.getByText(/Planned one is planned — its page is not wired yet/)).toBeTruthy();
    expect(screen.getByText(/Its UI is not built yet/)).toBeTruthy();
  });

  it("with no connection a needs-connection page shows the shell's not-connected alert, never an empty page", async () => {
    await renderConfig("/config/ready-one", connecting);
    expect(await screen.findByTestId("tab-needs-connection")).toBeTruthy();
    expect(screen.queryByTestId("ready-one-body")).toBeNull();
    expect(screen.queryByTestId("config-page-frame")).toBeNull();
  });

  it("a needsConnection: false page still renders without a connection", async () => {
    await renderConfig("/config/no-conn", connecting);
    expect(await screen.findByTestId("no-conn-body")).toBeTruthy();
    expect(screen.queryByTestId("tab-needs-connection")).toBeNull();
  });

  it("a page that throws is caught by the TabBoundary, naming the error", async () => {
    const errors = vi.spyOn(console, "error").mockImplementation(() => {});
    await renderConfig("/config/boom", current);
    expect(await screen.findByTestId("tab-boundary")).toBeTruthy();
    expect(screen.getByTestId("tab-boundary-error").textContent).toContain("page exploded");
    expect(errors).toHaveBeenCalled(); // the boundary re-logs (TabBoundary's own contract)
  });
});
