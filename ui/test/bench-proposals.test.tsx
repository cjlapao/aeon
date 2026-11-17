import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import { cleanup, fireEvent, render, screen, waitFor } from "@testing-library/react";
import { ProposalsPage } from "../src/features/test-bench/pages/ProposalsPage";
import { McpPage } from "../src/features/test-bench/pages/McpPage";
import { AgentsPage } from "../src/features/test-bench/pages/AgentsPage";
import { PolicyPage } from "../src/features/test-bench/pages/PolicyPage";
import { installFakeDaemon } from "./helpers";
import { resetBridge } from "../src/lib/bridge";
import { notifications } from "../src/lib/notifications";

// S35 T7 — the desk and the knobs. TD7's side-door rule: no commit
// affordance anywhere, in any form. TD8: live and proposed never merged.

beforeEach(() => resetBridge());
afterEach(() => cleanup());

function refusal(code: number, message: string): () => never {
  return () => {
    throw JSON.stringify({ code, message });
  };
}

const MCP_ADD_PROPOSAL = {
  id: 7,
  kind: "mcp_server_add",
  payload: { tier: "public", server: { name: "github", transport: { type: "stdio", command: "gh-mcp" } } },
  status: "proposed",
  createdAt: "2026-10-29T10:00:00Z",
  createdByKind: "user",
  createdById: "1000",
  command: "aeon proposal commit 7",
};

const POLICY_ADD_PROPOSAL = {
  id: 8,
  kind: "policy_add",
  payload: { id: "deny-webfetch", type: "tool_access", action: "deny", target: { kind: "agent", id: "boss" } },
  status: "proposed",
  createdAt: "2026-10-29T10:05:00Z",
  createdByKind: "user",
  createdById: "1000",
  command: "aeon proposal commit 8",
};

describe("ProposalsPage", () => {
  it("has no commit affordance anywhere — a DOM query for anything resembling one finds nothing", async () => {
    installFakeDaemon({
      responses: {
        "proposal.list": { proposals: [MCP_ADD_PROPOSAL, POLICY_ADD_PROPOSAL] },
        "mcp.status": { configPath: "/x/mcp.json", present: true, servers: [], probes: {} },
        "policy.list": { policies: [] },
      },
    });
    const { container } = render(<ProposalsPage />);
    await screen.findByTestId("proposals-list");

    // Only Discard is clickable. No button, link, or role anywhere says commit.
    const clickables = [...container.querySelectorAll("button, [role='button'], a")];
    for (const el of clickables) {
      const text = (el.textContent ?? "").toLowerCase();
      expect(text).not.toContain("commit");
      expect(text.includes("apply") || text.includes("promote")).toBe(false);
    }
    expect(screen.getByTestId("proposal-discard-7")).toBeTruthy();
    expect(screen.getByTestId("proposal-discard-8")).toBeTruthy();
    // The command text IS present, as copyable text — not a button.
    const commandEl = screen.getByTestId("proposal-command-7");
    expect(commandEl.tagName.toLowerCase()).toBe("code");
    expect(commandEl.textContent).toBe("aeon proposal commit 7");
  });

  it("diffs an mcp_server_add proposal against the live server list", async () => {
    installFakeDaemon({
      responses: {
        "proposal.list": { proposals: [MCP_ADD_PROPOSAL] },
        "mcp.status": { configPath: "/x/mcp.json", present: true, servers: [{ name: "other", tier: "public", transport: "stdio", command: "other-mcp", declaredBy: [] }], probes: {} },
        "policy.list": { policies: [] },
      },
    });
    render(<ProposalsPage />);
    const diff = await screen.findByTestId("proposal-diff-7");
    expect(diff.textContent).toContain("github");
    expect(diff.textContent).toContain("not in the live config yet");
  });

  it("names the conflict when a candidate already exists live (re-validation's own case, TD3)", async () => {
    installFakeDaemon({
      responses: {
        "proposal.list": { proposals: [MCP_ADD_PROPOSAL] },
        "mcp.status": { configPath: "/x/mcp.json", present: true, servers: [{ name: "github", tier: "public", transport: "stdio", command: "gh-mcp", declaredBy: [] }], probes: {} },
        "policy.list": { policies: [] },
      },
    });
    render(<ProposalsPage />);
    const diff = await screen.findByTestId("proposal-diff-7");
    expect(diff.textContent).toContain("already configured live");
  });
});

describe("McpPage — pending servers", () => {
  it("a server backed only by an open proposal shows no Check button", async () => {
    installFakeDaemon({
      responses: {
        "mcp.status": { configPath: "/x/mcp.json", present: true, servers: [], probes: {} },
        "proposal.list": { proposals: [MCP_ADD_PROPOSAL] },
      },
    });
    render(<McpPage />);
    await screen.findByTestId("mcp-server-pending-github");
    expect(screen.queryByTestId("mcp-check-github")).toBeNull();
    expect(screen.getByTestId("mcp-server-pending-github").textContent).toContain("proposed · awaiting commit");
  });

  it("creating a proposal from the Add-server form shows the exact toast text", async () => {
    installFakeDaemon({
      responses: {
        "mcp.status": { configPath: "/x/mcp.json", present: true, servers: [], probes: {} },
        "proposal.list": { proposals: [] },
        "proposal.create": MCP_ADD_PROPOSAL,
      },
    });
    const pushSpy = vi.spyOn(notifications, "push");
    render(<McpPage />);
    fireEvent.click(await screen.findByTestId("mcp-add-open"));
    fireEvent.change(screen.getByTestId("mcp-add-name"), { target: { value: "github" } });
    fireEvent.change(screen.getByTestId("mcp-add-command"), { target: { value: "gh-mcp" } });
    fireEvent.click(screen.getByTestId("mcp-add-submit"));
    await waitFor(() => expect(pushSpy).toHaveBeenCalled());
    const call = pushSpy.mock.calls[0]![0];
    expect(call.title).toBe("Proposed · commit with aeon proposal commit 7");
  });
});

describe("AgentsPage — live writes stay live, never staged", () => {
  it("enable/disable call the method directly and refetch — no proposed language", async () => {
    let state = "disabled";
    const daemon = installFakeDaemon({
      responses: {
        "agent.list": () => ({ agents: [{ name: "reviewer", provenance: "imported", state }] }),
        "agent.enable": () => { state = "enabled"; return { agent: "reviewer", state }; },
        "tools.grants": { grants: [] },
        "mcp.status": { configPath: "/x/mcp.json", present: true, servers: [], probes: {} },
        "policy.inForce": { answers: [] },
      },
    });
    render(<AgentsPage />);
    const row = await screen.findByTestId("agent-row-reviewer");
    expect(row.textContent).toContain("disabled");
    fireEvent.click(screen.getByTestId("agent-enable-reviewer"));
    await waitFor(() => expect(daemon.methods()).toContain("agent.enable"));
    expect(daemon.paramsOf("agent.enable")).toMatchObject({ name: "reviewer" });
    // The Identity strip updates from the refetch — live immediately, no reload.
    await waitFor(() => expect(screen.getByTestId("agent-row-reviewer").textContent).toContain("enabled"));
    expect(screen.getByTestId("agent-row-reviewer").textContent?.toLowerCase()).not.toContain("proposed");
    expect(daemon.methods()).not.toContain("proposal.create");
  });
});

describe("PolicyPage — live and proposed never merge", () => {
  it("renders two separately queryable tables, never one combined list", async () => {
    installFakeDaemon({
      responses: {
        "policy.list": { policies: [{ id: "allow-read", type: "tool_access", target: "agent:boss", action: "allow", source: "operator", notAfter: null }] },
        "proposal.list": { proposals: [POLICY_ADD_PROPOSAL] },
      },
    });
    render(<PolicyPage />);
    const live = await screen.findByTestId("policies-list");
    const proposed = await screen.findByTestId("policy-proposed-list");
    expect(live.textContent).toContain("allow-read");
    expect(live.textContent).not.toContain("deny-webfetch");
    expect(proposed.textContent).toContain("deny-webfetch");
    expect(proposed.textContent).not.toContain("allow-read");
    // Structurally separate containers, not one list with two sections of the same <ul>.
    expect(live.closest("ul")).not.toBe(proposed.closest("ul"));
  });
});

// S35 T9 (criterion 6, MC11): proposal.list is a method an older daemon
// does not have. Every Trust page reading it must show the unwired chip,
// never a plausible empty — the same three-state honesty McpPage/AgentsPage
// already proved for their own new methods (mcp.status/policy.why).
describe("MC11 — proposal.list unwired, never a plausible empty", () => {
  it("ProposalsPage shows the unwired chip when proposal.list is not registered", async () => {
    installFakeDaemon({
      responses: {
        "proposal.list": refusal(-32601, 'method "proposal.list" is not registered'),
        "mcp.status": { configPath: "/x/mcp.json", present: true, servers: [], probes: {} },
        "policy.list": { policies: [] },
      },
    });
    render(<ProposalsPage />);
    expect(await screen.findByTestId("fact-unwired")).toBeTruthy();
    expect(screen.queryByTestId("proposals-empty")).toBeNull();
  });

  it("PolicyPage's proposed-policy panel shows the unwired chip when proposal.list is not registered", async () => {
    installFakeDaemon({
      responses: {
        "policy.list": { policies: [] },
        "proposal.list": refusal(-32601, 'method "proposal.list" is not registered'),
      },
    });
    render(<PolicyPage />);
    expect(await screen.findByTestId("fact-unwired")).toBeTruthy();
  });
});
