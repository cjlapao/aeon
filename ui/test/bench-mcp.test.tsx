import { afterEach, beforeEach, describe, expect, it } from "vitest";
import { cleanup, fireEvent, render, screen, waitFor } from "@testing-library/react";
import { McpPage } from "../src/features/test-bench/pages/McpPage";
import { installFakeDaemon } from "./helpers";
import { resetBridge } from "../src/lib/bridge";

// S34 T7 — the config-and-check view. Payloads mirror
// conformance/routes/mcp-status-{absent,present,broken}.json exactly, so a
// Go-side re-wording that breaks the app shows up here.

beforeEach(() => resetBridge());
afterEach(() => cleanup());

function refusal(code: number, message: string): () => never {
  return () => {
    throw JSON.stringify({ code, message });
  };
}

const ABSENT = { configPath: "/home/operator/.config/aeon/mcp.json", present: false, probes: {} };

const PRESENT = {
  configPath: "/home/operator/.config/aeon/mcp.json",
  present: true,
  servers: [
    { name: "github", tier: "public", transport: "stdio", command: "gh-mcp", declaredBy: ["reviewer"] },
    { name: "scratch", tier: "private", transport: "http", url: "http://127.0.0.1:9100", declaredBy: ["reviewer"] },
  ],
  probes: { github: { ok: true, tools: 12, ms: 480, checkedAt: "2026-10-26T14:02:11Z" } },
};

const BROKEN = {
  configPath: "/home/operator/.config/aeon/mcp.json",
  present: true,
  loadError: "mcp: server \"scratch\" is configured twice (public and reviewer) — the same server cannot be two tiers or two agents' (MC12)",
  probes: {},
};

describe("McpPage", () => {
  it("renders the absent-config state by the exact path it looked at", async () => {
    installFakeDaemon({ responses: { "mcp.status": ABSENT } });
    render(<McpPage />);
    const cell = await screen.findByTestId("mcp-absent");
    expect(cell.textContent).toContain("/home/operator/.config/aeon/mcp.json");
    expect(screen.queryByTestId("mcp-servers-table")).toBeNull();
  });

  it("renders the broken-config state with the real loadError, never a partial server list", async () => {
    installFakeDaemon({ responses: { "mcp.status": BROKEN } });
    render(<McpPage />);
    const cell = await screen.findByTestId("mcp-broken");
    expect(cell.textContent).toContain("configured twice");
    expect(screen.queryByTestId("mcp-servers-table")).toBeNull();
  });

  it("renders the servers table with the chip vocabulary for checked and not-checked rows", async () => {
    installFakeDaemon({ responses: { "mcp.status": PRESENT } });
    render(<McpPage />);
    await screen.findByTestId("mcp-servers-table");
    const github = screen.getByTestId("mcp-server-github");
    expect(github.textContent).toContain("ok");
    expect(github.textContent).toContain("12 tools");
    const scratch = screen.getByTestId("mcp-server-scratch");
    expect(scratch.querySelector('[data-testid="probe-chip-not-checked"]')).toBeTruthy();
  });

  it("shows the unwired chip for an old daemon's method-not-found, never a crash", async () => {
    installFakeDaemon({ responses: { "mcp.status": refusal(-32601, 'method "mcp.status" is not registered') } });
    render(<McpPage />);
    expect(await screen.findByTestId("fact-unwired")).toBeTruthy();
  });

  it("never calls mcp.check on load or on refresh", async () => {
    const daemon = installFakeDaemon({ responses: { "mcp.status": PRESENT } });
    render(<McpPage />);
    await screen.findByTestId("mcp-servers-table");
    fireEvent.click(screen.getByTestId("mcp-refresh"));
    await waitFor(() => expect(daemon.methods().filter((m) => m === "mcp.status").length).toBe(2));
    expect(daemon.methods().filter((m) => m === "mcp.check")).toHaveLength(0);
  });

  it("the Check button fires exactly one mcp.check call even across a rapid double-click", async () => {
    let resolveCheck: (() => void) | undefined;
    const daemon = installFakeDaemon({
      responses: {
        "mcp.status": PRESENT,
        "mcp.check": () =>
          new Promise((resolve) => {
            resolveCheck = () => resolve({ server: "github", ok: true, tools: [{ name: "x", description: "" }], ms: 10, checkedAt: "2026-10-29T00:00:00Z" });
          }),
      },
    });
    render(<McpPage />);
    const button = await screen.findByTestId("mcp-check-github");
    fireEvent.click(button);
    fireEvent.click(button);
    fireEvent.click(button);
    await waitFor(() => expect(screen.getByTestId("mcp-check-github").textContent).toBe("checking…"));
    expect(daemon.methods().filter((m) => m === "mcp.check")).toHaveLength(1);
    resolveCheck?.();
    await waitFor(() => expect(screen.getByTestId("mcp-check-github").textContent).toBe("Check"));
    expect(await screen.findByTestId("probe-chip-ok")).toBeTruthy();
  });
});
