import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import { cleanup, fireEvent, render, screen, waitFor, within } from "@testing-library/react";
import { MemoryRouter } from "react-router-dom";
import { I18nProvider } from "@cjlapao/ui-kit";
import { McpSection } from "./McpSection";
import { APP_LOCALES } from "../../../lib/i18n";
import { LiveController } from "../../../lib/live";
import type { LiveState } from "../../../lib/live";
import { installFakeDaemon, resetFakeDaemon, type FakeDaemon } from "../../../../test/helpers";

const currentLive = {
  state: "current",
  endpoint: "/run/user/1000/aeon/rpc.sock",
  daemonVersion: "9.9.9",
  protocolVersion: 7,
  generation: 1,
} as const satisfies LiveState;

/** A MOCK vocabulary stands in for the daemon's — the words are fixture
 *  facts here, never source facts (the guard test keeps them out of the
 *  production sources). */
const vocabulary = {
  transports: ["commandish", "urlhttp"],
  rerun: ["safe", "detectable", "unsafe"],
  effect: ["near", "far", "forever"],
  grammars: { envName: "^[A-Z][A-Z0-9_]*$", server: "a name", tool: "a name" },
};

function serverRow(overrides: Record<string, unknown> = {}) {
  return {
    agent: "",
    declaredBy: 0,
    enabled: true,
    name: "gh",
    origin: "user",
    status: "live",
    tier: "public",
    updatedAt: "2026-11-01T00:00:00Z",
    tools: [{ name: "list_issues", classified: true, rerun: "safe", effect: "near" }],
    transport: { type: "commandish", command: "gh-mcp", args: ["--mcp"], envNames: ["GITHUB_TOKEN"] },
    ...overrides,
  };
}

function serverDetail(overrides: Record<string, unknown> = {}) {
  return {
    changes: [],
    declaredBy: ["tester"],
    server: {
      agent: "",
      declaredBy: 1,
      enabled: true,
      name: "gh",
      origin: "user",
      status: "live",
      tier: "public",
      tools: [{ name: "list_issues", classified: true, rerun: "safe", effect: "near" }],
      transport: { type: "commandish", command: "gh-mcp", args: ["--mcp"], envNames: ["GITHUB_TOKEN"] },
      updatedAt: "2026-11-01T00:00:00Z",
    },
    ...overrides,
  };
}

const grantsAnswer = {
  agent: "tester",
  core: null,
  declares: ["gh.list_issues"],
  grants: [
    {
      qualified: "gh.list_issues",
      state: "unavailable",
      tier: "public",
      reason: "refused: no allow rule matched gh.list_issues — declaring is not allowing",
    },
  ],
};

const baseResponses: Record<string, unknown> = {
  "agent.list": { agents: [{ slug: "tester", name: "tester", provenance: "first-party", state: "active" }] },
  "mcp.vocabulary": vocabulary,
  "mcp.servers": { servers: [serverRow()] },
  "mcp.server": serverDetail(),
  "mcp.changes": { changes: [], statement: "no MCP configuration changes recorded." },
  "mcp.grants": (params: unknown) => ({ ...grantsAnswer, agent: (params as { agent: string }).agent }),
  "policy.vocabulary": {
    vocabulary: {
      types: ["tool_access"],
      targetKinds: ["agent", "all-agents"],
      sources: ["operator"],
      actions: ["allow", "deny", "ask"],
      authorship: [{ source: "operator", action: "allow", allowed: true }],
      specs: [{ type: "tool_access", fields: [{ name: "toolName", kind: "string", required: true, description: "" }] }],
    },
  },
  "proposal.list": { proposals: [] },
  "mcp.processes": { processes: [{ name: "gh", transport: "commandish", state: "running", pid: 4242, startedAt: "2026-11-08T10:00:00Z", restarts: 1, tools: 1, lastExit: "" }] },
  "mcp.process.logs": { name: "gh", next: 2, lines: [{ seq: 1, at: "", stream: "stderr", text: "listening" }, { seq: 2, at: "", stream: "stdout", text: "ready" }] },
  "mcp.process.stop": { process: { name: "gh", transport: "commandish", state: "stopped", restarts: 1, tools: 1, lastExit: "stopped" } },
  "mcp.server.share": { changed: true, grants: [] },
  "policy.add": { policy: { id: "mcp-gh-all", describe: "ok" }, before: { answers: [], target: "" }, after: { answers: [], target: "" }, warnings: [] },
  "policy.remove": { policy: { id: "mcp-gh-all" }, before: { answers: [], target: "" }, after: { answers: [], target: "" } },
  "mcp.process.restart": { process: { name: "gh", transport: "commandish", state: "running", pid: 4243, restarts: 2, tools: 1, lastExit: "" } },
};

beforeEach(() => {
  window.matchMedia = vi.fn(() => ({
    matches: false,
    media: "",
    onchange: null,
    addEventListener: () => {},
    removeEventListener: () => {},
    addListener: () => {},
    removeListener: () => {},
    dispatchEvent: () => false,
  })) as unknown as typeof window.matchMedia;
});

afterEach(() => {
  cleanup();
  resetFakeDaemon();
});

function renderSection(responses: Record<string, unknown> = {}, live: LiveState = currentLive): FakeDaemon {
  const daemon = installFakeDaemon({ responses: { ...baseResponses, ...responses } });
  render(
    <I18nProvider locales={APP_LOCALES}>
      <MemoryRouter>
        <McpSection controller={new LiveController()} live={live} />
      </MemoryRouter>
    </I18nProvider>,
  );
  return daemon;
}

/** Choose the first agent in the tag picker: open it, click the first option. */
async function pickFirstAgent(): Promise<void> {
  const trigger = await waitFor(() => {
    const found = document.querySelector('[data-testid="agent-tag-picker"] button[aria-haspopup="listbox"]:not([disabled])');
    if (found === null) throw new Error("the agent picker is not ready");
    return found as HTMLElement;
  });
  fireEvent.click(trigger);
  const option = await waitFor(() => {
    const found = document.querySelector('li[role="option"]');
    if (found === null) throw new Error("the roster is not in the list yet");
    return found as HTMLElement;
  });
  fireEvent.click(option);
  fireEvent.keyDown(document.activeElement ?? document.body, { key: "Escape" });
}

async function openTab(label: string): Promise<void> {
  fireEvent.click(await screen.findByRole("tab", { name: label }));
}

describe("McpSection — Servers tab", () => {
  it("renders the daemon's level-0 sentence for an empty store", async () => {
    renderSection({ "mcp.servers": { servers: [] } });
    const empty = await screen.findByTestId("mcp-empty");
    expect(within(empty).getByText(/no MCP servers configured\. tools available: the core nine only \(S11\)/)).toBeTruthy();
  });

  it("the server row shows env NAMES as chips and never a value", async () => {
    renderSection();
    await screen.findByTestId("server-list");
    const chips = screen.getAllByTestId("env-chip");
    expect(chips.map((chip) => chip.textContent?.replace("×", "").trim())).toContain("GITHUB_TOKEN");
    // A value, had one ever crossed the wire, must not appear anywhere.
    expect(document.body.textContent).not.toContain("ghp_supersecret");
    expect(document.body.textContent).not.toContain("SECRET");
  });

  it("an unclassified tool shows the warning chip and no default class", async () => {
    renderSection({
      "mcp.servers": {
        servers: [serverRow({ tools: [{ name: "create_issue", classified: false, rerun: "", effect: "" }] })],
      },
    });
    await screen.findByTestId("server-list");
    expect(screen.getByTestId("unclassified-chip").textContent).toBe("UNCLASSIFIED");
    // The form's selects show the EMPTY class, never a preselected "safe".
    fireEvent.click(screen.getByTestId("add-server"));
    await screen.findByTestId("mcp-server-form");
    fireEvent.click(screen.getByText("Add tool"));
    const row = await screen.findByTestId("tool-class-row");
    const selects = within(row).getAllByRole("combobox") as HTMLSelectElement[];
    expect(selects[0]?.value).toBe("");
    expect(selects[1]?.value).toBe("");
    expect(within(row).getByText("UNCLASSIFIED")).toBeTruthy();
  });

  it("the form is built from the vocabulary — changing the daemon's words changes the options", async () => {
    renderSection({
      "mcp.vocabulary": { ...vocabulary, rerun: ["fine", "risky"], effect: ["quiet", "loud"] },
    });
    fireEvent.click(await screen.findByTestId("add-server"));
    await screen.findByTestId("mcp-server-form");
    fireEvent.click(screen.getByText("Add tool"));
    const row = await screen.findByTestId("tool-class-row");
    const selects = within(row).getAllByRole("combobox") as HTMLSelectElement[];
    await waitFor(() =>
      expect(Array.from(selects[0]?.options ?? []).map((o) => o.value)).toEqual(["", "fine", "risky"]),
    );
    expect(Array.from(selects[1]?.options ?? []).map((o) => o.value)).toEqual(["", "quiet", "loud"]);
    const transportSelect = screen.getByLabelText("Transport") as HTMLSelectElement;
    expect(Array.from(transportSelect.options).map((o) => o.value)).toEqual(["", "commandish", "urlhttp"]);
  });

  it("an env entry with = is refused client-side with the daemon-environment line; nothing is sent", async () => {
    const daemon = renderSection();
    fireEvent.click(await screen.findByTestId("add-server"));
    await screen.findByTestId("mcp-server-form");
    fireEvent.change(screen.getByLabelText("Name"), { target: { value: "gh" } });
    const transport = screen.getByLabelText("Transport") as HTMLSelectElement;
    fireEvent.change(transport, { target: { value: "commandish" } });
    fireEvent.change(screen.getByLabelText("Command"), { target: { value: "gh-mcp" } });
    fireEvent.change(screen.getByLabelText("Env variable name"), { target: { value: "GITHUB_TOKEN=abc123" } });
    fireEvent.click(screen.getByText("Add name"));
    expect((await screen.findByTestId("env-refusal")).textContent).toContain(
      "enter the variable’s name — values come from the daemon’s environment",
    );
    expect(daemon.calls.filter((c) => c.method === "mcp.server.add")).toHaveLength(0);
  });

  it("a refusal shows the validator's sentence VERBATIM and keeps the draft", async () => {
    const sentence =
      'refused: server "gh" tool create_issue: effect class "farfar" is not near, far or forever';
    const daemon = renderSection({
      "mcp.server.add": () => {
        throw new Error(sentence);
      },
    });
    fireEvent.click(await screen.findByTestId("add-server"));
    await screen.findByTestId("mcp-server-form");
    const form = await screen.findByTestId("mcp-server-form");
    fireEvent.change(within(form).getByLabelText("Name"), { target: { value: "gh" } });
    fireEvent.change(within(form).getByLabelText("Tier") as HTMLSelectElement, { target: { value: "public" } });
    fireEvent.change(within(form).getByLabelText("Transport") as HTMLSelectElement, { target: { value: "commandish" } });
    fireEvent.change(within(form).getByLabelText("Command"), { target: { value: "gh-mcp" } });
    // A server needs at least one tool before it can be added.
    fireEvent.click(within(form).getByText("Add tool"));
    fireEvent.change(within(form).getByLabelText("Tool name"), { target: { value: "create_issue" } });
    fireEvent.click(screen.getByTestId("form-submit"));
    const refusal = await screen.findByTestId("form-refusal");
    expect(refusal.textContent).toContain(sentence);
    // The draft SURVIVES the refusal — every field still carries what was typed.
    expect((screen.getByLabelText("Name") as HTMLInputElement).value).toBe("gh");
    expect((screen.getByLabelText("Command") as HTMLInputElement).value).toBe("gh-mcp");
    expect(daemon.calls.filter((c) => c.method === "mcp.server.add")).toHaveLength(1);
  });

  it("a server with no tools cannot be added, and the form says why", async () => {
    const daemon = renderSection();
    fireEvent.click(await screen.findByTestId("add-server"));
    const form = await screen.findByTestId("mcp-server-form");
    fireEvent.change(within(form).getByLabelText("Name"), { target: { value: "gh" } });
    fireEvent.change(within(form).getByLabelText("Tier") as HTMLSelectElement, { target: { value: "public" } });
    fireEvent.change(within(form).getByLabelText("Transport") as HTMLSelectElement, { target: { value: "commandish" } });
    fireEvent.change(within(form).getByLabelText("Command"), { target: { value: "gh-mcp" } });
    expect((screen.getByTestId("form-submit") as HTMLButtonElement).disabled).toBe(true);
    expect(screen.getByTestId("form-errors").textContent).toContain("add at least one tool");
    expect(daemon.calls.filter((c) => c.method === "mcp.server.add")).toHaveLength(0);
  });

  it("Inspect reads the server's tools with their descriptions, and only then can the server be added", async () => {
    const daemon = renderSection({
      "mcp.server.inspect": {
        serverName: "GitHub MCP Server", serverVersion: "0.6.2", protocolVersion: "2025-06-18", ms: 4200,
        tools: [
          { name: "create_issue", description: "Create a new issue.", inputSchema: { type: "object", properties: { owner: {}, repo: {} } }, annotations: { destructiveHint: true } },
          { name: "list_repos", description: "List repositories.", annotations: { readOnlyHint: true } },
        ],
      },
    });
    fireEvent.click(await screen.findByTestId("add-server"));
    const form = await screen.findByTestId("mcp-server-form");
    fireEvent.change(within(form).getByLabelText("Tier") as HTMLSelectElement, { target: { value: "public" } });
    fireEvent.change(within(form).getByLabelText("Transport") as HTMLSelectElement, { target: { value: "commandish" } });
    // Not runnable until the transport is complete.
    expect((screen.getByTestId("inspect-run") as HTMLButtonElement).disabled).toBe(true);
    fireEvent.change(within(form).getByLabelText("Command"), { target: { value: "npx" } });
    fireEvent.change(within(form).getByLabelText("Arguments"), { target: { value: "-y @modelcontextprotocol/server-github" } });
    fireEvent.click(screen.getByTestId("inspect-run"));

    const list = await screen.findByTestId("inspect-tools");
    expect(daemon.paramsOf("mcp.server.inspect")).toEqual({ transport: { type: "commandish", command: "npx", args: ["-y", "@modelcontextprotocol/server-github"] } });
    // The server's own words: description, the parameters it takes, and its hints.
    expect(within(list).getByTestId("inspect-tool-create_issue").textContent).toContain("Create a new issue.");
    expect(within(list).getByTestId("inspect-tool-create_issue").textContent).toContain("owner, repo");
    expect(within(list).getByTestId("inspect-tool-create_issue").textContent).toContain("destructive");
    // Before the tools are used the form still has none, so Add stays blocked.
    expect((screen.getByTestId("form-submit") as HTMLButtonElement).disabled).toBe(true);

    fireEvent.click(within(list).getByLabelText("list_repos")); // unpick one
    fireEvent.click(screen.getByTestId("inspect-use"));
    // The picked tool arrives UNCLASSIFIED — a server's hints classify nothing — and the name is suggested.
    const rows = within(form).getAllByTestId("tool-class-row");
    expect(rows).toHaveLength(1);
    expect((within(rows[0] as HTMLElement).getByLabelText("Tool name") as HTMLInputElement).value).toBe("create_issue");
    expect((within(rows[0] as HTMLElement).getByLabelText("Rerun class") as HTMLSelectElement).value).toBe("");
    expect((within(form).getByLabelText("Name") as HTMLInputElement).value).toBe("github-mcp-server");
    expect((screen.getByTestId("form-submit") as HTMLButtonElement).disabled).toBe(false);
  });

  it("stored variable values reach the inspected command only when the box is ticked", async () => {
    const daemon = renderSection({
      "mcp.server.inspect": { serverName: "x", serverVersion: "1", protocolVersion: "2025-06-18", ms: 1, tools: [{ name: "a", description: "A" }] },
    });
    fireEvent.click(await screen.findByTestId("add-server"));
    const form = await screen.findByTestId("mcp-server-form");
    fireEvent.change(within(form).getByLabelText("Transport") as HTMLSelectElement, { target: { value: "commandish" } });
    fireEvent.change(within(form).getByLabelText("Command"), { target: { value: "npx" } });
    // No env names declared: nothing to hand over, and no box.
    expect(screen.queryByTestId("inspect-use-variables")).toBeNull();
    fireEvent.change(within(form).getByLabelText("Env variable name") as HTMLInputElement, { target: { value: "GITHUB_TOKEN" } });
    fireEvent.click(within(form).getByText("Add name"));
    const box = within(await screen.findByTestId("inspect-use-variables")).getByLabelText("give the command my stored values") as HTMLInputElement;
    expect(box.checked).toBe(false);
    fireEvent.click(screen.getByTestId("inspect-run"));
    await screen.findByTestId("inspect-tools");
    expect((daemon.paramsOf("mcp.server.inspect") as Record<string, unknown>).useVariables).toBeUndefined();
    fireEvent.click(box);
    fireEvent.click(screen.getByTestId("inspect-run"));
    await waitFor(() => expect((daemon.paramsOf("mcp.server.inspect") as Record<string, unknown>).useVariables).toBe(true));
  });

  it("a bulk choice classifies only the tools still unclassified", async () => {
    renderSection({
      "mcp.server.inspect": {
        serverName: "x", serverVersion: "1", protocolVersion: "2025-06-18", ms: 1,
        tools: [{ name: "a", description: "A" }, { name: "b", description: "B" }, { name: "c", description: "C" }],
      },
    });
    fireEvent.click(await screen.findByTestId("add-server"));
    const form = await screen.findByTestId("mcp-server-form");
    fireEvent.change(within(form).getByLabelText("Transport") as HTMLSelectElement, { target: { value: "commandish" } });
    fireEvent.change(within(form).getByLabelText("Command"), { target: { value: "x" } });
    fireEvent.click(screen.getByTestId("inspect-run"));
    await screen.findByTestId("inspect-tools");
    fireEvent.click(screen.getByTestId("inspect-use"));
    const rows = within(form).getAllByTestId("tool-class-row");
    expect(rows).toHaveLength(3);
    // The person classifies one by hand...
    fireEvent.change(within(rows[0] as HTMLElement).getByLabelText("Rerun class"), { target: { value: "unsafe" } });
    fireEvent.change(within(rows[0] as HTMLElement).getByLabelText("Effect class"), { target: { value: "forever" } });
    // ...then makes ONE explicit bulk choice for the rest.
    const bulk = screen.getByTestId("bulk-classify");
    fireEvent.change(within(bulk).getByLabelText("Rerun class"), { target: { value: "safe" } });
    fireEvent.change(within(bulk).getByLabelText("Effect class"), { target: { value: "near" } });
    fireEvent.click(within(bulk).getByText("Apply"));
    const after = within(form).getAllByTestId("tool-class-row");
    const classes = after.map((row) => [(within(row).getByLabelText("Rerun class") as HTMLSelectElement).value, (within(row).getByLabelText("Effect class") as HTMLSelectElement).value]);
    expect(classes).toEqual([["unsafe", "forever"], ["safe", "near"], ["safe", "near"]]);
  });

  it("an inspect refusal shows the daemon's own sentence, not the generic title", async () => {
    const what = 'refused: could not start the server and complete its handshake: spawning nope: executable file not found in $PATH';
    renderSection({
      "mcp.server.inspect": () => {
        throw JSON.stringify({ code: -31853, message: "the MCP management door refused", data: { what, means: "", todo: "", fatal: false } });
      },
    });
    fireEvent.click(await screen.findByTestId("add-server"));
    const form = await screen.findByTestId("mcp-server-form");
    fireEvent.change(within(form).getByLabelText("Transport") as HTMLSelectElement, { target: { value: "commandish" } });
    fireEvent.change(within(form).getByLabelText("Command"), { target: { value: "nope" } });
    fireEvent.click(screen.getByTestId("inspect-run"));
    const shown = await screen.findByTestId("inspect-error");
    expect(shown.textContent).toContain(what);
  });

  it("a tool with only one class set is named and marked, not just refused", async () => {
    renderSection();
    fireEvent.click(await screen.findByTestId("add-server"));
    const form = await screen.findByTestId("mcp-server-form");
    fireEvent.change(within(form).getByLabelText("Transport") as HTMLSelectElement, { target: { value: "commandish" } });
    fireEvent.change(within(form).getByLabelText("Command"), { target: { value: "x" } });
    fireEvent.click(within(form).getByText("Add tool"));
    fireEvent.click(within(form).getByText("Add tool"));
    const names = within(form).getAllByLabelText("Tool name");
    fireEvent.change(names[0] as HTMLElement, { target: { value: "create_issue" } });
    fireEvent.change(names[1] as HTMLElement, { target: { value: "list_repos" } });
    // Only the rerun class on the first tool.
    // (The bulk-classify bar also carries class selects, so address the row's own.)
    fireEvent.change(within(within(form).getAllByTestId("tool-class-row")[0] as HTMLElement).getByLabelText("Rerun class"), { target: { value: "safe" } });
    const rows = within(form).getAllByTestId("tool-class-row");
    expect((rows[0] as HTMLElement).getAttribute("data-incomplete")).toBe("true");
    expect((rows[1] as HTMLElement).getAttribute("data-incomplete")).toBeNull();
    expect(screen.getByTestId("half-classified-list").textContent).toContain("create_issue");
    expect(screen.getByTestId("half-classified-list").textContent).not.toContain("list_repos");
    // Completing the row clears the mark.
    fireEvent.change(within(within(form).getAllByTestId("tool-class-row")[0] as HTMLElement).getByLabelText("Effect class"), { target: { value: "near" } });
    expect(screen.queryByTestId("half-classified-list")).toBeNull();
    expect((within(form).getAllByTestId("tool-class-row")[0] as HTMLElement).getAttribute("data-incomplete")).toBeNull();
  });

  it("an environment variable cannot be empty: a name needs a stored variable or a typed value", async () => {
    const daemon = renderSection({ "vault.list": { variables: [{ name: "GITHUB_PAT", secret: true, agentVisible: false, updatedAt: "" }, { name: "REGION", secret: false, agentVisible: true, value: "eu", updatedAt: "" }] } });
    fireEvent.click(await screen.findByTestId("add-server"));
    const form = await screen.findByTestId("mcp-server-form");
    fireEvent.change(within(form).getByLabelText("Name"), { target: { value: "gh" } });
    fireEvent.change(within(form).getByLabelText("Tier") as HTMLSelectElement, { target: { value: "public" } });
    fireEvent.change(within(form).getByLabelText("Transport") as HTMLSelectElement, { target: { value: "commandish" } });
    fireEvent.change(within(form).getByLabelText("Command"), { target: { value: "gh-mcp" } });
    fireEvent.click(within(form).getByText("Add tool"));
    fireEvent.change(within(form).getByLabelText("Tool name"), { target: { value: "create_issue" } });
    // Two env names, neither bound.
    for (const name of ["GITHUB_TOKEN", "GITHUB_REGION"]) {
      fireEvent.change(within(form).getByLabelText("Env variable name") as HTMLInputElement, { target: { value: name } });
      fireEvent.click(within(form).getByText("Add name"));
    }
    expect(screen.getByTestId("env-unbound-GITHUB_TOKEN")).toBeTruthy();
    expect(screen.getByTestId("env-unbound-GITHUB_REGION")).toBeTruthy();
    expect((screen.getByTestId("form-submit") as HTMLButtonElement).disabled).toBe(true);
    expect(screen.getByTestId("form-errors").textContent).toContain("every environment variable needs a value");

    // One bound to a stored variable...
    await waitFor(() => expect(within(screen.getByTestId("env-binding-GITHUB_TOKEN")).getByText("GITHUB_PAT")).toBeTruthy());
    fireEvent.change(within(screen.getByTestId("env-binding-GITHUB_TOKEN")).getByLabelText("GITHUB_TOKEN source"), { target: { value: "GITHUB_PAT" } });
    expect(screen.queryByTestId("env-unbound-GITHUB_TOKEN")).toBeNull();
    // ...and one given a typed value (write-only, hidden).
    const row = screen.getByTestId("env-binding-GITHUB_REGION");
    fireEvent.change(within(row).getByLabelText("GITHUB_REGION source"), { target: { value: "__value" } });
    const typed = within(row).getByLabelText("GITHUB_REGION value") as HTMLInputElement;
    expect(typed.type).toBe("password");
    fireEvent.change(typed, { target: { value: "eu-west-1" } });
    expect((screen.getByTestId("form-submit") as HTMLButtonElement).disabled).toBe(false);

    fireEvent.click(screen.getByTestId("form-submit"));
    await waitFor(() => expect(daemon.paramsOf("mcp.server.add")).toBeTruthy());
    const sent = (daemon.paramsOf("mcp.server.add") as { transport: Record<string, unknown> }).transport;
    expect(sent.env).toEqual(["GITHUB_TOKEN", "GITHUB_REGION"]);
    expect(sent.envFrom).toEqual({ GITHUB_TOKEN: "GITHUB_PAT" });
    expect(sent.envValues).toEqual({ GITHUB_REGION: "eu-west-1" });
  });

  it("choosing a variable replaces a typed value, and removing a name drops its binding", async () => {
    renderSection({ "vault.list": { variables: [{ name: "TOKEN_VAR", secret: true, agentVisible: false, updatedAt: "" }] } });
    fireEvent.click(await screen.findByTestId("add-server"));
    const form = await screen.findByTestId("mcp-server-form");
    fireEvent.change(within(form).getByLabelText("Transport") as HTMLSelectElement, { target: { value: "commandish" } });
    fireEvent.change(within(form).getByLabelText("Command"), { target: { value: "x" } });
    fireEvent.change(within(form).getByLabelText("Env variable name") as HTMLInputElement, { target: { value: "TOKEN" } });
    fireEvent.click(within(form).getByText("Add name"));
    const row = screen.getByTestId("env-binding-TOKEN");
    fireEvent.change(within(row).getByLabelText("TOKEN source"), { target: { value: "__value" } });
    fireEvent.change(within(row).getByLabelText("TOKEN value"), { target: { value: "secret-typed" } });
    await waitFor(() => expect(within(row).getByText("TOKEN_VAR")).toBeTruthy());
    fireEvent.change(within(row).getByLabelText("TOKEN source"), { target: { value: "TOKEN_VAR" } });
    expect(within(row).queryByLabelText("TOKEN value")).toBeNull(); // the typed value is gone
    expect(row.textContent).toContain("bound to TOKEN_VAR");
    fireEvent.click(within(screen.getByTestId("env-chips")).getByText("TOKEN ×"));
    expect(screen.queryByTestId("env-binding-TOKEN")).toBeNull();
  });

  it("the list shows the process state and PID; the drawer shows logs and controls Stop / Restart", async () => {
    const daemon = renderSection();
    await waitFor(() => expect(screen.getByTestId("process-state").textContent).toContain("4242"));
    fireEvent.click(screen.getByTestId("server-open"));
    const panel = await screen.findByTestId("process-panel");
    await waitFor(() => expect(within(panel).getByTestId("process-logs").textContent).toContain("! listening"));
    expect(within(panel).getByTestId("process-logs").textContent).toContain("ready");
    expect((within(panel).getByTestId("process-start") as HTMLButtonElement).disabled).toBe(true);
    fireEvent.click(within(panel).getByTestId("process-stop"));
    await waitFor(() => expect(daemon.paramsOf("mcp.process.stop")).toEqual({ name: "gh" }));
    fireEvent.click(within(panel).getByTestId("process-restart"));
    await waitFor(() => expect(daemon.paramsOf("mcp.process.restart")).toEqual({ name: "gh" }));
  });

  it("one confirmation shares the server with every agent and writes the all-agents allow rule", async () => {
    const daemon = renderSection();
    await screen.findByTestId("server-list");
    fireEvent.click(screen.getByTestId("server-open"));
    const panel = await screen.findByTestId("access-panel");
    expect(within(panel).getByTestId("access-state").textContent).toBe("not shared");
    fireEvent.click(within(panel).getByTestId("access-all"));
    const confirmInput = await screen.findByPlaceholderText("gh");
    fireEvent.change(confirmInput, { target: { value: "gh" } });
    fireEvent.keyDown(confirmInput, { key: "Enter" });
    await waitFor(() => expect(daemon.paramsOf("policy.add")).toBeTruthy());
    expect(daemon.paramsOf("mcp.server.share")).toEqual({ name: "gh", shared: true });
    expect(daemon.paramsOf("policy.add")).toMatchObject({
      id: "mcp-gh-all",
      type: "tool_access",
      action: "allow",
      target: { kind: "all-agents", id: "" },
      spec: { toolName: "mcp.gh.*.*", state: "allow" },
    });
  });

  it("system server: no edit affordance, but duplicate is offered", async () => {
    renderSection({ "mcp.server": serverDetail({ server: { ...serverDetail().server, origin: "system" } }) });
    await screen.findByTestId("server-list");
    fireEvent.click(screen.getByTestId("server-open"));
    const drawer = await screen.findByTestId("server-drawer");
    expect(await within(drawer).findByTestId("drawer-system-note")).toBeTruthy();
    expect(within(drawer).queryByTestId("drawer-edit")).toBeNull();
    expect(within(drawer).getByTestId("drawer-duplicate")).toBeTruthy();
  });

  it("remove names the declaring agents and offers cascade explicitly, default OFF", async () => {
    renderSection();
    await screen.findByTestId("server-list");
    fireEvent.click(screen.getByTestId("server-open"));
    const drawer = await screen.findByTestId("server-drawer");
    fireEvent.click(await within(drawer).findByTestId("drawer-remove"));
    const box = await screen.findByTestId("remove-confirm");
    expect(box.textContent).toContain("Declared by: tester");
    const checkbox = within(box).getByLabelText("Also revoke these agents’ declarations of this server (cascade)") as HTMLInputElement;
    expect(checkbox.checked).toBe(false);
  });

  it("the grants view shows reasons VERBATIM and null core as \"whole core set\"", async () => {
    renderSection();
    await openTab("Grants");
    // The picker offers the roster; choosing the row sends mcp.grants.
    await pickFirstAgent();
    expect((await screen.findByTestId("core-whole")).textContent).toContain("whole core set");
    expect((await screen.findByTestId("grant-reason")).textContent).toContain(
      "refused: no allow rule matched gh.list_issues — declaring is not allowing",
    );
    expect(screen.getByTestId("grant-state").textContent).toContain("unavailable");
  });

  it("declare calls ONLY mcp.grant.declare, then offers Allow in policy prefilled — no hidden combined call", async () => {
    const daemon = renderSection({
      "mcp.grant.declare": () => ({
        changed: true,
        grants: [
          {
            agent: "tester",
            before: [],
            after: [
              { qualified: "gh.new_tool", state: "unavailable", tier: "public", reason: "refused: no allow rule matched gh.new_tool" },
            ],
          },
        ],
        warnings: [],
      }),
    });
    await openTab("Grants");
    await pickFirstAgent();
    await screen.findByTestId("grant-flow");
    fireEvent.change(screen.getByLabelText("Qualified tool"), { target: { value: "gh.new_tool" } });
    fireEvent.click(screen.getByTestId("declare-button"));
    const declareInput = await screen.findByPlaceholderText("tester");
    fireEvent.change(declareInput, { target: { value: "tester" } });
    fireEvent.keyDown(declareInput, { key: "Enter" });
    expect((await screen.findByTestId("allow-in-policy")).textContent).toContain("unavailable (no allow)");
    expect(daemon.calls.filter((c) => c.method === "mcp.grant.declare")).toHaveLength(1);
    // NO policy write has happened — the offer stands, unacted.
    expect(daemon.calls.filter((c) => c.method === "policy.add")).toHaveLength(0);
    // Opening the offer shows S45's form PREFILLED with the qualified name.
    fireEvent.click(screen.getByTestId("allow-open"));
    const toolName = await screen.findByLabelText("toolName");
    expect((toolName as HTMLInputElement).value).toBe("gh.new_tool");
    expect(daemon.calls.filter((c) => c.method === "policy.add")).toHaveLength(0);
  });
});

describe("McpSection — Proposals · Audit · Import", () => {
  it("the proposals tab filters to the MCP kinds and reuses the desk", async () => {
    const row = (id: number, kind: string) => ({
      id,
      kind,
      status: "proposed",
      createdByKind: "agent",
      createdById: "golang-engineer",
      createdAt: "2026-11-01T00:00:00Z",
      command: `aeon proposal commit ${id}`,
      payload: { server: "gh", reason: "widened" },
    });
    const daemon = renderSection({
      "proposal.list": { proposals: [row(7, "mcp_server_add"), row(8, "policy_add"), row(9, "mcp_grant_revoke")] },
      "proposal.get": { digest: "aabbccdd11223344", preview: {}, proposal: row(7, "mcp_server_add") },
    });
    await openTab("Proposals");
    await screen.findByTestId("proposal-card-7");
    expect(screen.getByTestId("proposal-card-9")).toBeTruthy();
    expect(screen.queryByTestId("proposal-card-8")).toBeNull(); // policy_add is not MCP's desk
    // Committing through the desk sends {id, digest} — S45's own door.
    const card = screen.getByTestId("proposal-card-7");
    // Commit needs the SHOWN digest — wait for proposal.get to land before
    // clicking (the button is honest: no digest, no modal, no call).
    await within(card).findByTestId("proposal-digest-7");
    fireEvent.click(within(card).getByText("Commit"));
    const commitInput = await screen.findByPlaceholderText("7");
    fireEvent.change(commitInput, { target: { value: "7" } });
    fireEvent.keyDown(commitInput, { key: "Enter" });
    await waitFor(() =>
      expect(daemon.calls).toContainEqual({ method: "proposal.commit", params: { id: 7, digest: "aabbccdd11223344" } }),
    );
  });

  it("audit renders the daemon's statement for the empty table", async () => {
    renderSection();
    await openTab("Audit");
    expect(await screen.findByText("no MCP configuration changes recorded.")).toBeTruthy();
  });

  it("import dry-run shows a names-only diff; replace requires the typed confirm", async () => {
    const daemon = renderSection({
      "mcp.importFile": (params: unknown) => {
        const mode = (params as { mode: string }).mode;
        return {
          mode,
          outcome: mode === "dry-run" ? "planned" : "replaced",
          path: "/home/cjlapao/.config/aeon/mcp.json",
          servers: 2,
          tools: 5,
          declares: 1,
          diff: {
            serversAdded: ["gh"],
            serversRemoved: [],
            toolsAdded: ["gh.list_issues"],
            toolsRemoved: [],
            declaresAdded: ["tester:gh.list_issues"],
            declaresRemoved: [],
            envNames: ["GITHUB_TOKEN"],
          },
        };
      },
    });
    fireEvent.click(await screen.findByTestId("mcp-import"));
    fireEvent.click(await screen.findByTestId("import-dry"));
    const preview = await screen.findByTestId("import-preview");
    expect(preview.textContent).toContain("GITHUB_TOKEN");
    expect(preview.textContent).toContain("gh.list_issues");
    expect(daemon.calls.filter((c) => c.method === "mcp.importFile" && (c.params as { mode: string }).mode === "replace")).toHaveLength(0);
    fireEvent.click(screen.getByTestId("import-replace"));
    const replaceInput = await screen.findByPlaceholderText("replace");
    expect(document.body.textContent).toContain("2 servers");
    fireEvent.change(replaceInput, { target: { value: "replace" } });
    fireEvent.keyDown(replaceInput, { key: "Enter" });
    await waitFor(() =>
      expect(daemon.calls.some((c) => c.method === "mcp.importFile" && (c.params as { mode: string }).mode === "replace")).toBe(true),
    );
  });

  it("a refused adoption is shown as a warning with the daemon's sentence", async () => {
    renderSection({
      "mcp.importFile": () => ({
        mode: "dry-run",
        outcome: "refused",
        path: "/home/cjlapao/.config/aeon/mcp.json",
        servers: 0,
        tools: 0,
        declares: 0,
        detail: "refused: server gh transport command is empty — the file is left untouched",
      }),
    });
    fireEvent.click(await screen.findByTestId("mcp-import"));
    fireEvent.click(await screen.findByTestId("import-dry"));
    const refused = await screen.findByTestId("import-refused");
    expect(refused.textContent).toContain("refused: server gh transport command is empty — the file is left untouched");
  });
});

describe("McpSection — cross-cutting", () => {
  it("offline disables every write", async () => {
    const offline = { ...currentLive, state: "reconnecting" } as unknown as LiveState;
    renderSection({}, offline);
    const add = await screen.findByTestId("add-server");
    expect((add as HTMLButtonElement).disabled).toBe(true);
    expect(screen.getByTestId("mcp-reconnecting")).toBeTruthy();
  });

  it("after a write the rows are RE-READ from the daemon, never edited optimistically", async () => {
    const daemon = renderSection({ "mcp.server.setEnabled": () => ({ changed: true, grants: [], server: {} }) });
    await screen.findByTestId("server-list");
    const before = daemon.calls.filter((c) => c.method === "mcp.servers").length;
    fireEvent.click(screen.getByTestId("enabled-switch"));
    await waitFor(() =>
      expect(daemon.calls.some((c) => c.method === "mcp.server.setEnabled")).toBe(true),
    );
    await waitFor(() => expect(daemon.calls.filter((c) => c.method === "mcp.servers").length).toBeGreaterThan(before));
  });
});
