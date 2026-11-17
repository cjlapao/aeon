import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import { cleanup, render, screen, waitFor } from "@testing-library/react";
import { MemoryRouter } from "react-router-dom";
import { ProjectsScreen } from "../src/features/projects/ProjectsScreen";
import { LiveController } from "../src/lib/live";
import { resetBridge, setInvoker } from "../src/lib/bridge";
import type { LiveState } from "../src/lib/live";

const current: LiveState = { state: "current", endpoint: "/tmp/aeon.sock", daemonVersion: "1.0", protocolVersion: 1, generation: 1 };

beforeEach(() => {
  vi.stubGlobal("ResizeObserver", class { observe() {} disconnect() {} });
});
afterEach(() => { cleanup(); resetBridge(); vi.unstubAllGlobals(); });

// A "project" is a root component: parentId empty makes the row a root the
// screen lists.
function root(overrides: Partial<Record<string, unknown>> = {}) {
  return { id: "p1", name: "Platform", shortDescription: "shared services", root: "/home/dev/platform", slug: "platform", icon: "", parentId: "",
    createdAt: "2026-01-01T00:00:00Z", updatedAt: "2026-01-01T00:00:00Z", ...overrides };
}

describe("ProjectsScreen (useRpcQuery)", () => {
  it("renders the roots the daemon returns, via component.list", async () => {
    setInvoker(async (cmd, args) => {
      if (cmd === "endpoint_path") return "/tmp/aeon.sock";
      if (cmd === "rpc_call") {
        const method = (args as { method: string }).method;
        if (method === "component.list") return { components: [root(), root({ id: "p2", name: "Core" })] };
        throw new Error(`unexpected ${method}`);
      }
      throw new Error(`unexpected ${cmd}`);
    });
    render(<MemoryRouter><ProjectsScreen controller={new LiveController()} live={current} /></MemoryRouter>);
    expect(await screen.findByText("Platform")).toBeTruthy();
    expect(screen.getByText("Core")).toBeTruthy();
  });

  it("shows the daemon's own error, wrapped with the component-tier guidance, on a refused component.list", async () => {
    setInvoker(async (cmd, args) => {
      if (cmd === "endpoint_path") return "/tmp/aeon.sock";
      if (cmd === "rpc_call") {
        const method = (args as { method: string }).method;
        if (method === "component.list") throw new Error("no such table: component");
        throw new Error(`unexpected ${method}`);
      }
      throw new Error(`unexpected ${cmd}`);
    });
    render(<MemoryRouter><ProjectsScreen controller={new LiveController()} live={current} /></MemoryRouter>);
    await waitFor(() => expect(screen.getByText(/Component records are unavailable/)).toBeTruthy());
    expect(screen.getByText(/no such table: component/)).toBeTruthy();
  });
});
