import { afterEach, beforeEach, describe, expect, it } from "vitest";
import { cleanup, fireEvent, render, screen, waitFor, within } from "@testing-library/react";
import { AgentsPage } from "../src/features/test-bench/pages/AgentsPage";
import { installFakeDaemon } from "./helpers";
import { resetBridge } from "../src/lib/bridge";

// S34 T6 — the readiness view. Payloads below are the shapes
// conformance/agents/list.json and conformance/agents/tools-grants.json
// actually carry, so a Go-side re-wording that breaks the app shows up here.

beforeEach(() => resetBridge());
afterEach(() => cleanup());

/** A daemon refusal in the shape the shell actually serialises — same helper test/automation.test.tsx uses. */
function refusal(code: number, message: string): () => never {
  return () => {
    throw JSON.stringify({ code, message });
  };
}

const ROSTER = {
  agents: [
    { name: "default", provenance: "first_party", state: "running" },
    { name: "groot-translator", provenance: "imported", state: "disabled", source: "/pkgs/groot", importedAt: "2026-10-25T09:00:00Z" },
  ],
};

const GRANTS = {
  agent: "default",
  grants: [
    { qualified: "read", tier: "core", state: "available", reason: "core: judged by the step's S11 policy gate (S12 does not re-decide core)" },
    { qualified: "github.list_issues", tier: "public", state: "unavailable", reason: "unavailable: no ToolAccess allow rule grants github.list_issues (MC10)" },
    { qualified: "github.merge_pull_request", tier: "public", state: "ask", reason: "ask: github.merge_pull_request has effect irreversible" },
    { qualified: "github.search_code", tier: "public", state: "available" },
  ],
};

const MCP_STATUS = {
  configPath: "/home/u/.config/aeon/mcp.json",
  present: true,
  servers: [{ name: "github", tier: "public", transport: "stdio", command: "gh-mcp", declaredBy: ["default"] }],
  probes: { github: { ok: true, tools: 3, ms: 120, checkedAt: "2026-10-26T14:02:11Z" } },
};

const IN_FORCE = { answers: [{ policyID: "allow-read", render: "in force · acknowledged by agent:default", state: "enforced_acknowledged" }] };

function responses(overrides: Record<string, unknown> = {}) {
  return { "agent.list": ROSTER, "tools.grants": GRANTS, "mcp.status": MCP_STATUS, "policy.inForce": IN_FORCE, "policy.why": { allowed: true, sentence: "allowed: agent:default · read" }, ...overrides };
}

describe("AgentsPage", () => {
  it("renders the roster with provenance and state", async () => {
    installFakeDaemon({ responses: responses() });
    render(<AgentsPage />);
    const row = await screen.findByTestId("agent-row-default");
    expect(row.textContent).toContain("default");
    expect(row.textContent).toContain("first_party");
    expect(row.textContent).toContain("running");
    expect(screen.getByTestId("agent-row-groot-translator").textContent).toContain("imported");
  });

  it("renders the four GrantView states as separate, queryable groups — never merged", async () => {
    installFakeDaemon({ responses: responses() });
    render(<AgentsPage />);
    fireEvent.click(await screen.findByTestId("agent-row-default"));
    await screen.findByTestId("agents-grant-group-available");
    expect(within(screen.getByTestId("agents-grant-group-available")).getByTestId("agents-grant-read")).toBeTruthy();
    expect(within(screen.getByTestId("agents-grant-group-available")).getByTestId("agents-grant-github.search_code")).toBeTruthy();
    expect(screen.getByTestId("agents-grant-group-unavailable")).toBeTruthy();
    expect(screen.getByTestId("agents-grant-group-ask")).toBeTruthy();
    // refused has no row in this fixture — the group must not render an empty shell.
    expect(screen.queryByTestId("agents-grant-group-refused")).toBeNull();
  });

  it("shows the unwired chip for an old daemon's method-not-found, never a crash or a plausible zero", async () => {
    installFakeDaemon({ responses: { "agent.list": refusal(-32601, 'method "agent.list" is not registered') } });
    render(<AgentsPage />);
    expect(await screen.findByTestId("fact-unwired")).toBeTruthy();
    expect(screen.queryByTestId("agents-roster")).toBeNull();
  });

  it("shows a real error distinctly from unwired", async () => {
    installFakeDaemon({ responses: { "agent.list": refusal(-32000, "the plugin ledger could not be read") } });
    render(<AgentsPage />);
    const cell = await screen.findByTestId("fact-error");
    expect(cell.textContent).toContain("the plugin ledger could not be read");
  });

  it("shows every strip's real CLI twin (flags cross-checked against cmd/aeon's own flag.String definitions)", async () => {
    installFakeDaemon({ responses: responses() });
    render(<AgentsPage />);
    await screen.findByTestId("agent-row-default");
    expect(screen.getAllByTestId("cli-twin")[0]?.textContent).toBe("aeon agent list --provenance --all");
    fireEvent.click(screen.getByTestId("agent-row-default"));
    await waitFor(() => expect(screen.getAllByTestId("cli-twin").length).toBeGreaterThan(1));
    const twins = screen.getAllByTestId("cli-twin").map((e) => e.textContent);
    expect(twins).toContain("aeon mcp list --agent default");
    expect(twins).toContain("aeon policy in-force --agent default");
  });

  it("refresh refetches the read methods and never calls mcp.check (TB9)", async () => {
    const daemon = installFakeDaemon({ responses: responses() });
    render(<AgentsPage />);
    fireEvent.click(await screen.findByTestId("agent-row-default"));
    await screen.findByTestId("agents-grant-group-available");
    const before = daemon.methods().filter((m) => m === "agent.list").length;
    fireEvent.click(screen.getByTestId("agents-refresh"));
    await waitFor(() => expect(daemon.methods().filter((m) => m === "agent.list").length).toBe(before + 1));
    expect(daemon.methods().filter((m) => m === "mcp.check")).toHaveLength(0);
  });

  it("the 'why' expand asks policy.why scoped to the one tool, not the whole agent", async () => {
    const daemon = installFakeDaemon({ responses: responses() });
    render(<AgentsPage />);
    fireEvent.click(await screen.findByTestId("agent-row-default"));
    fireEvent.click(await screen.findByTestId("agents-grant-why-github.list_issues"));
    await screen.findByTestId("agents-grant-why-sentence-github.list_issues");
    expect(daemon.paramsOf("policy.why")).toMatchObject({ agent: "default", type: "tool_access", tool: "github.list_issues" });
  });
});
